// Package k8s holds SODA's Kubernetes-side collectors. They write manifests and logs into the archive, structured
// by the same scopes as vitals:
//
//	kubernetes/cluster/        the ScyllaCluster as a whole (vitals: soda/vitals/_cluster.vitals.json)
//	kubernetes/nodes/<node>/   one ScyllaDB node (vitals: soda/vitals/<node>.vitals.json)
//
// Each collector returns a vitals entry in Scylla Doctor's format whose data lists the files it wrote under
// `files` (archive-relative path, size, sha256). A read the identity isn't allowed to do makes the collector
// SKIPPED with the permission it needs, never FAILED: like a missing tool in Scylla Doctor, it's a setup gap,
// not a finding about the cluster.
package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllaversioned "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	// ClusterScope is the vitals "node" name of cluster-scoped results.
	ClusterScope = "_cluster"

	clusterDir = "kubernetes/cluster"
	nodesDir   = "kubernetes/nodes"
)

func ClusterDir() string         { return clusterDir }
func NodeDir(node string) string { return filepath.Join(nodesDir, node) }

// Env is what collectors share.
type Env struct {
	KubeClient        kubernetes.Interface
	ScyllaClient      scyllaversioned.Interface
	Namespace         string
	OperatorNamespace string
	// LogsTailLines keeps only the newest lines of each log; 0 keeps everything.
	LogsTailLines int64
	ArchiveRoot   string
	// Events are the namespace's events, listed once; nil when they couldn't be listed.
	Events []corev1.Event
}

type NodeCollector interface {
	Name() string
	Collect(ctx context.Context, env *Env, pod *corev1.Pod) vitals.CollectorResult
}

type ClusterCollector interface {
	Name() string
	Collect(ctx context.Context, env *Env, sc *scyllav1.ScyllaCluster) vitals.CollectorResult
}

func NodeCollectors() []NodeCollector {
	return []NodeCollector{&PodCollector{}, &StorageCollector{}, &KubernetesNodeCollector{}}
}

func ClusterCollectors() []ClusterCollector {
	return []ClusterCollector{&ScyllaClusterCollector{}, &OperatorCollector{}}
}

// files records what a collector writes into its scope directory.
type files struct {
	env  *Env
	dir  string
	refs []vitals.FileRef
}

func newFiles(env *Env, dir string) *files { return &files{env: env, dir: dir} }

func (f *files) create(rel string) (*os.File, string, error) {
	archivePath := filepath.ToSlash(filepath.Join(f.dir, rel))
	p := filepath.Join(f.env.ArchiveRoot, archivePath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, "", err
	}
	fh, err := os.Create(p)
	return fh, archivePath, err
}

func (f *files) write(rel string, b []byte) error {
	fh, archivePath, err := f.create(rel)
	if err != nil {
		return err
	}
	defer fh.Close()
	if _, err := fh.Write(b); err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	f.refs = append(f.refs, vitals.FileRef{Path: archivePath, SizeBytes: int64(len(b)), SHA256: hex.EncodeToString(sum[:])})
	return fh.Close()
}

func (f *files) copy(rel string, r io.Reader) error {
	fh, archivePath, err := f.create(rel)
	if err != nil {
		return err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(fh, h), r)
	if err != nil {
		return err
	}
	f.refs = append(f.refs, vitals.FileRef{Path: archivePath, SizeBytes: n, SHA256: hex.EncodeToString(h.Sum(nil))})
	return fh.Close()
}

// object writes a typed object as YAML. Typed clients drop apiVersion and kind, so they are set here, and
// managedFields are left out.
func (f *files) object(rel, apiVersion, kind string, obj any) error {
	b, err := objectYAML(apiVersion, kind, obj)
	if err != nil {
		return err
	}
	return f.write(rel, b)
}

func objectMap(apiVersion, kind string, obj any) (map[string]any, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["apiVersion"], m["kind"] = apiVersion, kind
	if md, ok := m["metadata"].(map[string]any); ok {
		delete(md, "managedFields")
	}
	return m, nil
}

func objectYAML(apiVersion, kind string, obj any) ([]byte, error) {
	m, err := objectMap(apiVersion, kind, obj)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(m)
}

// events writes the given events as a v1 List, oldest first.
func (f *files) events(rel string, evs []corev1.Event) error {
	sort.SliceStable(evs, func(i, j int) bool { return lastSeen(evs[i]) < lastSeen(evs[j]) })
	items := make([]any, 0, len(evs))
	for i := range evs {
		m, err := objectMap("v1", "Event", &evs[i])
		if err != nil {
			return err
		}
		items = append(items, m)
	}
	b, err := yaml.Marshal(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})
	if err != nil {
		return err
	}
	return f.write(rel, b)
}

func lastSeen(e corev1.Event) int64 {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.UnixNano()
	case !e.EventTime.IsZero():
		return e.EventTime.UnixNano()
	default:
		return e.CreationTimestamp.UnixNano()
	}
}

// eventsFor returns the events about the given objects (kind/name).
func eventsFor(env *Env, objects map[string]bool) []corev1.Event {
	var out []corev1.Event
	for _, e := range env.Events {
		if objects[e.InvolvedObject.Kind+"/"+e.InvolvedObject.Name] {
			out = append(out, e)
		}
	}
	return out
}

// result builds a PASSED entry whose data carries the files written. The paths contain the node's name, so they
// are masked from comparisons between nodes.
func result(data map[string]any, f *files, problems []string) vitals.CollectorResult {
	if data == nil {
		data = map[string]any{}
	}
	data["files"] = f.refs
	if len(problems) > 0 {
		data["problems"] = problems
	}
	return vitals.CollectorResult{Status: vitals.CollectorPassed, Data: data, Message: "Data collected", Mask: []any{"files", "problems"}}
}

func skipped(message string) vitals.CollectorResult {
	return vitals.CollectorResult{Status: vitals.CollectorSkipped, Data: map[string]any{}, Message: message, Mask: []any{}}
}

func failed(message string) vitals.CollectorResult {
	return vitals.CollectorResult{Status: vitals.CollectorFailed, Data: map[string]any{}, Message: message, Mask: []any{}}
}

// forbiddenMessage explains a missing permission in the collector's skip message.
func forbiddenMessage(what, rbac string) string {
	return fmt.Sprintf("Not allowed to read %s; grant it with examples/soda/%s", what, rbac)
}

func isForbidden(err error) bool { return apierrors.IsForbidden(err) }
