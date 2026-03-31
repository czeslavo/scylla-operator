package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// CollectorScope defines the scope at which a collector operates.
type CollectorScope int

const (
	// ClusterWide collectors run once per diagnostic run.
	ClusterWide CollectorScope = iota
	// PerScyllaCluster collectors run once per targeted ScyllaCluster/ScyllaDBDatacenter.
	PerScyllaCluster
	// PerPod collectors run once per Scylla pod.
	PerPod
)

// String returns a human-readable representation of the collector scope.
func (s CollectorScope) String() string {
	switch s {
	case ClusterWide:
		return "ClusterWide"
	case PerScyllaCluster:
		return "PerScyllaCluster"
	case PerPod:
		return "PerPod"
	default:
		return fmt.Sprintf("CollectorScope(%d)", int(s))
	}
}

// CollectorStatus represents the outcome of a collector execution.
type CollectorStatus int

const (
	CollectorPassed CollectorStatus = iota
	CollectorFailed
	CollectorSkipped
)

// String returns a human-readable representation of the collector status.
func (s CollectorStatus) String() string {
	switch s {
	case CollectorPassed:
		return "PASSED"
	case CollectorFailed:
		return "FAILED"
	case CollectorSkipped:
		return "SKIPPED"
	default:
		return fmt.Sprintf("CollectorStatus(%d)", int(s))
	}
}

// AnalyzerStatus represents the outcome of an analyzer execution.
type AnalyzerStatus int

const (
	AnalyzerPassed AnalyzerStatus = iota
	AnalyzerSkipped
	AnalyzerWarning
	AnalyzerFailed
)

// String returns a human-readable representation of the analyzer status.
func (s AnalyzerStatus) String() string {
	switch s {
	case AnalyzerPassed:
		return "PASSED"
	case AnalyzerSkipped:
		return "SKIPPED"
	case AnalyzerWarning:
		return "WARNING"
	case AnalyzerFailed:
		return "FAILED"
	default:
		return fmt.Sprintf("AnalyzerStatus(%d)", int(s))
	}
}

// CollectorID is a unique identifier for a collector.
type CollectorID string

// AnalyzerID is a unique identifier for an analyzer.
type AnalyzerID string

// ScopeKey identifies a namespaced resource (cluster or pod) used as a map key
// in the Vitals store. It is only meaningful for PerScyllaCluster and PerPod
// scopes; ClusterWide collectors use an empty ScopeKey that is not stored
// as a map key.
type ScopeKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// IsEmpty returns true if the ScopeKey has no namespace and no name,
// which is the case for ClusterWide scope.
func (k ScopeKey) IsEmpty() bool {
	return k.Namespace == "" && k.Name == ""
}

// String returns the "namespace/name" representation of the scope key.
// For an empty ScopeKey (ClusterWide), it returns an empty string.
func (k ScopeKey) String() string {
	if k.IsEmpty() {
		return ""
	}
	return k.Namespace + "/" + k.Name
}

// MarshalText implements encoding.TextMarshaler so ScopeKey can be used as
// a JSON map key. The format is "namespace/name". An empty ScopeKey marshals
// to an empty string.
func (k ScopeKey) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler so ScopeKey can be parsed
// back from a JSON map key. The expected format is "namespace/name".
// An empty string produces an empty ScopeKey.
func (k *ScopeKey) UnmarshalText(text []byte) error {
	s := string(text)
	if s == "" {
		k.Namespace = ""
		k.Name = ""
		return nil
	}
	idx := strings.Index(s, "/")
	if idx < 0 {
		return fmt.Errorf("invalid ScopeKey format %q: expected namespace/name", s)
	}
	k.Namespace = s[:idx]
	k.Name = s[idx+1:]
	return nil
}

// Artifact represents a raw file produced by a collector.
type Artifact struct {
	RelativePath string `json:"relative_path"` // Path relative to collector's artifact directory
	Description  string `json:"description"`   // Human-readable description
}

// CollectorResult holds the outcome of a single collector execution.
type CollectorResult struct {
	Status    CollectorStatus `json:"status"`
	Data      any             `json:"-"` // Concrete typed struct; not serialized directly
	Message   string          `json:"message"`
	Artifacts []Artifact      `json:"artifacts"` // Raw files written by this collector
}

// AnalyzerResult holds the outcome of a single analyzer execution.
type AnalyzerResult struct {
	Status  AnalyzerStatus `json:"status"`
	Message string         `json:"message"`
}

// Collector is the interface that all diagnostic data collectors must implement.
type Collector interface {
	ID() CollectorID
	Name() string // Human-readable description
	Scope() CollectorScope
	DependsOn() []CollectorID // Other collectors this one needs (can be empty)
	Collect(ctx context.Context, params CollectorParams) (*CollectorResult, error)
}

// Analyzer is the interface that all diagnostic analyzers must implement.
type Analyzer interface {
	ID() AnalyzerID
	Name() string             // Human-readable description
	DependsOn() []CollectorID // Collector IDs whose results this analyzer reads
	Analyze(params AnalyzerParams) *AnalyzerResult
}

// PodExecutor runs commands inside pod containers.
type PodExecutor interface {
	Execute(ctx context.Context, namespace, podName, containerName string, command []string) (stdout, stderr string, err error)
}

// ScyllaClusterLister discovers ScyllaCluster and ScyllaDBDatacenter objects.
type ScyllaClusterLister interface {
	ListScyllaClusters(ctx context.Context, namespace string) ([]ClusterInfo, error)
}

// NodeLister lists Kubernetes Node objects.
type NodeLister interface {
	ListNodes(ctx context.Context) ([]corev1.Node, error)
}

// PodLister lists pods matching a selector in a namespace.
type PodLister interface {
	ListPods(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Pod, error)
}

// ArtifactWriter is passed to collectors via CollectorParams.
// The engine creates one per (collector, scope, scope-key) invocation
// pointing at the correct subdirectory.
type ArtifactWriter interface {
	WriteArtifact(filename string, content []byte) (relativePath string, err error)
}

// ArtifactReader is passed to analyzers via AnalyzerParams.
// Backed by the output directory (live mode) or an extracted archive (offline mode).
type ArtifactReader interface {
	ReadArtifact(collectorID CollectorID, scopeKey ScopeKey, filename string) ([]byte, error)
	ListArtifacts(collectorID CollectorID, scopeKey ScopeKey) ([]Artifact, error)
}

// ClusterInfo represents a discovered ScyllaCluster or ScyllaDBDatacenter.
type ClusterInfo struct {
	Name       string
	Namespace  string
	Kind       string // "ScyllaCluster" or "ScyllaDBDatacenter"
	APIVersion string // "scylla.scylladb.com/v1" or "scylla.scylladb.com/v1alpha1"
	Object     any    // *scyllav1.ScyllaCluster or *scyllav1alpha1.ScyllaDBDatacenter
}

// PodInfo represents a discovered Scylla pod.
type PodInfo struct {
	Name           string
	Namespace      string
	ClusterName    string // from label scylla/cluster
	DatacenterName string // from label scylla/datacenter
	RackName       string // from label scylla/rack
}

// CollectorParams holds everything a collector needs during execution.
type CollectorParams struct {
	// Always available:
	Vitals *Vitals // Results from upstream collectors

	// Available based on scope:
	Cluster *ClusterInfo // Non-nil for PerScyllaCluster and PerPod
	Pod     *PodInfo     // Non-nil for PerPod

	// Dependency-injected capabilities:
	PodExecutor         PodExecutor
	ScyllaClusterLister ScyllaClusterLister
	NodeLister          NodeLister
	PodLister           PodLister
	ArtifactWriter      ArtifactWriter // Write raw artifact files
}

// AnalyzerParams holds everything an analyzer needs during execution.
type AnalyzerParams struct {
	Vitals         *Vitals        // Full vitals store with all collector results
	ArtifactReader ArtifactReader // Read raw artifact files from collectors
}

// Vitals is the central data store. It holds collector results keyed by scope.
type Vitals struct {
	ClusterWide map[CollectorID]*CollectorResult              `json:"cluster_wide"`
	PerCluster  map[ScopeKey]map[CollectorID]*CollectorResult `json:"per_cluster"`
	PerPod      map[ScopeKey]map[CollectorID]*CollectorResult `json:"per_pod"`
}

// NewVitals creates a new Vitals with initialized maps.
func NewVitals() *Vitals {
	return &Vitals{
		ClusterWide: make(map[CollectorID]*CollectorResult),
		PerCluster:  make(map[ScopeKey]map[CollectorID]*CollectorResult),
		PerPod:      make(map[ScopeKey]map[CollectorID]*CollectorResult),
	}
}

// Store stores a collector result in the appropriate scope map.
func (v *Vitals) Store(id CollectorID, scope CollectorScope, scopeKey ScopeKey, result *CollectorResult) {
	switch scope {
	case ClusterWide:
		v.ClusterWide[id] = result
	case PerScyllaCluster:
		if v.PerCluster[scopeKey] == nil {
			v.PerCluster[scopeKey] = make(map[CollectorID]*CollectorResult)
		}
		v.PerCluster[scopeKey][id] = result
	case PerPod:
		if v.PerPod[scopeKey] == nil {
			v.PerPod[scopeKey] = make(map[CollectorID]*CollectorResult)
		}
		v.PerPod[scopeKey][id] = result
	}
}

// Get retrieves a collector result. For ClusterWide results, scopeKey is ignored.
// For PerCluster/PerPod results, it searches in the scope-appropriate map.
func (v *Vitals) Get(id CollectorID, scopeKey ScopeKey) (*CollectorResult, bool) {
	// Check ClusterWide first (scopeKey is irrelevant for this scope).
	if result, ok := v.ClusterWide[id]; ok {
		return result, true
	}

	// Check PerCluster.
	if perCluster, ok := v.PerCluster[scopeKey]; ok {
		if result, ok := perCluster[id]; ok {
			return result, true
		}
	}

	// Check PerPod.
	if perPod, ok := v.PerPod[scopeKey]; ok {
		if result, ok := perPod[id]; ok {
			return result, true
		}
	}

	return nil, false
}

// PodKeys returns all pod-scope keys in the store, sorted for deterministic output.
func (v *Vitals) PodKeys() []ScopeKey {
	keys := make([]ScopeKey, 0, len(v.PerPod))
	for k := range v.PerPod {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
	})
	return keys
}

// ClusterKeys returns all cluster-scope keys in the store, sorted for deterministic output.
func (v *Vitals) ClusterKeys() []ScopeKey {
	keys := make([]ScopeKey, 0, len(v.PerCluster))
	for k := range v.PerCluster {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
	})
	return keys
}

// Profile defines a set of analyzers to run as a group.
type Profile struct {
	Name        string
	Description string
	Includes    []string     // Names of other profiles to compose
	Analyzers   []AnalyzerID // Analyzer IDs this profile enables
}

// SerializableCollectorResult is the JSON-safe version of CollectorResult.
// Unlike CollectorResult, the Data field is stored as json.RawMessage so it
// can be persisted to vitals.json and later loaded for offline analysis.
type SerializableCollectorResult struct {
	Status    CollectorStatus `json:"status"`
	Data      json.RawMessage `json:"data,omitempty"`
	Message   string          `json:"message"`
	Artifacts []Artifact      `json:"artifacts"`
}

// SerializableVitals is the JSON-safe version of Vitals for persistence
// to vitals.json. It mirrors the Vitals structure but uses
// SerializableCollectorResult so that Data is preserved as raw JSON.
type SerializableVitals struct {
	ClusterWide map[CollectorID]*SerializableCollectorResult              `json:"cluster_wide"`
	PerCluster  map[ScopeKey]map[CollectorID]*SerializableCollectorResult `json:"per_cluster"`
	PerPod      map[ScopeKey]map[CollectorID]*SerializableCollectorResult `json:"per_pod"`
}

// toSerializableResult converts a CollectorResult to its serializable form,
// marshaling the Data field to json.RawMessage.
func toSerializableResult(r *CollectorResult) (*SerializableCollectorResult, error) {
	sr := &SerializableCollectorResult{
		Status:    r.Status,
		Message:   r.Message,
		Artifacts: r.Artifacts,
	}
	if r.Data != nil {
		data, err := json.Marshal(r.Data)
		if err != nil {
			return nil, fmt.Errorf("marshaling collector data: %w", err)
		}
		sr.Data = data
	}
	return sr, nil
}

// ToSerializable converts the Vitals store into a fully JSON-serializable
// form suitable for writing to vitals.json.
func (v *Vitals) ToSerializable() (*SerializableVitals, error) {
	sv := &SerializableVitals{
		ClusterWide: make(map[CollectorID]*SerializableCollectorResult, len(v.ClusterWide)),
		PerCluster:  make(map[ScopeKey]map[CollectorID]*SerializableCollectorResult, len(v.PerCluster)),
		PerPod:      make(map[ScopeKey]map[CollectorID]*SerializableCollectorResult, len(v.PerPod)),
	}

	for id, r := range v.ClusterWide {
		sr, err := toSerializableResult(r)
		if err != nil {
			return nil, fmt.Errorf("converting ClusterWide result %s: %w", id, err)
		}
		sv.ClusterWide[id] = sr
	}

	for key, perCluster := range v.PerCluster {
		sv.PerCluster[key] = make(map[CollectorID]*SerializableCollectorResult, len(perCluster))
		for id, r := range perCluster {
			sr, err := toSerializableResult(r)
			if err != nil {
				return nil, fmt.Errorf("converting PerCluster result %s/%s: %w", key, id, err)
			}
			sv.PerCluster[key][id] = sr
		}
	}

	for key, perPod := range v.PerPod {
		sv.PerPod[key] = make(map[CollectorID]*SerializableCollectorResult, len(perPod))
		for id, r := range perPod {
			sr, err := toSerializableResult(r)
			if err != nil {
				return nil, fmt.Errorf("converting PerPod result %s/%s: %w", key, id, err)
			}
			sv.PerPod[key][id] = sr
		}
	}

	return sv, nil
}
