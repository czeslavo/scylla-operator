package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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

// AnalyzerScope defines whether an analyzer runs once cluster-wide or once per ScyllaCluster.
type AnalyzerScope int

const (
	// AnalyzerClusterWide analyzers run once and receive all vitals.
	AnalyzerClusterWide AnalyzerScope = iota
	// AnalyzerPerScyllaCluster analyzers run once per ScyllaCluster and receive
	// only the vitals for that cluster's pods.
	AnalyzerPerScyllaCluster
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
	Duration  time.Duration   `json:"-"`         // Wall-clock time spent in Collect(); omitted from direct JSON
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
	Scope() AnalyzerScope     // Whether this analyzer runs once or per ScyllaCluster
	DependsOn() []CollectorID // Collector IDs whose results this analyzer reads
	Analyze(params AnalyzerParams) *AnalyzerResult
}

// PodExecutor runs commands inside pod containers.
type PodExecutor interface {
	Execute(ctx context.Context, namespace, podName, containerName string, command []string) (stdout, stderr string, err error)
}

// ScyllaClusterLister discovers ScyllaCluster and ScyllaDBDatacenter objects.
type ScyllaClusterLister interface {
	ListScyllaClusters(ctx context.Context, namespace string) ([]ScyllaClusterInfo, error)
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

// ScyllaClusterInfo represents a discovered ScyllaCluster or ScyllaDBDatacenter.
type ScyllaClusterInfo struct {
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
	ScyllaCluster *ScyllaClusterInfo // Non-nil for PerScyllaCluster and PerPod
	Pod           *PodInfo           // Non-nil for PerPod

	// Dependency-injected capabilities:
	PodExecutor         PodExecutor
	ScyllaClusterLister ScyllaClusterLister
	NodeLister          NodeLister
	PodLister           PodLister
	ArtifactWriter      ArtifactWriter // Write raw artifact files
}

// AnalyzerParams holds everything an analyzer needs during execution.
type AnalyzerParams struct {
	Vitals         *Vitals            // Vitals store (full for ClusterWide, filtered for PerScyllaCluster)
	ScyllaCluster  *ScyllaClusterInfo // Non-nil for AnalyzerPerScyllaCluster
	ArtifactReader ArtifactReader     // Read raw artifact files from collectors
}

// Vitals is the central data store. It holds collector results keyed by scope.
type Vitals struct {
	ClusterWide      map[CollectorID]*CollectorResult              `json:"cluster_wide"`
	PerScyllaCluster map[ScopeKey]map[CollectorID]*CollectorResult `json:"per_scylla_cluster"`
	PerPod           map[ScopeKey]map[CollectorID]*CollectorResult `json:"per_pod"`
}

// NewVitals creates a new Vitals with initialized maps.
func NewVitals() *Vitals {
	return &Vitals{
		ClusterWide:      make(map[CollectorID]*CollectorResult),
		PerScyllaCluster: make(map[ScopeKey]map[CollectorID]*CollectorResult),
		PerPod:           make(map[ScopeKey]map[CollectorID]*CollectorResult),
	}
}

// Store stores a collector result in the appropriate scope map.
func (v *Vitals) Store(id CollectorID, scope CollectorScope, scopeKey ScopeKey, result *CollectorResult) {
	switch scope {
	case ClusterWide:
		v.ClusterWide[id] = result
	case PerScyllaCluster:
		if v.PerScyllaCluster[scopeKey] == nil {
			v.PerScyllaCluster[scopeKey] = make(map[CollectorID]*CollectorResult)
		}
		v.PerScyllaCluster[scopeKey][id] = result
	case PerPod:
		if v.PerPod[scopeKey] == nil {
			v.PerPod[scopeKey] = make(map[CollectorID]*CollectorResult)
		}
		v.PerPod[scopeKey][id] = result
	}
}

// Get retrieves a collector result. For ClusterWide results, scopeKey is ignored.
// For PerScyllaCluster/PerPod results, it searches in the scope-appropriate map.
func (v *Vitals) Get(id CollectorID, scopeKey ScopeKey) (*CollectorResult, bool) {
	// Check ClusterWide first (scopeKey is irrelevant for this scope).
	if result, ok := v.ClusterWide[id]; ok {
		return result, true
	}

	// Check PerScyllaCluster.
	if perScyllaCluster, ok := v.PerScyllaCluster[scopeKey]; ok {
		if result, ok := perScyllaCluster[id]; ok {
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

// ScyllaClusterKeys returns all ScyllaCluster-scope keys in the store, sorted for deterministic output.
func (v *Vitals) ScyllaClusterKeys() []ScopeKey {
	keys := make([]ScopeKey, 0, len(v.PerScyllaCluster))
	for k := range v.PerScyllaCluster {
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

// ForScyllaCluster returns a new Vitals scoped to a single ScyllaCluster: it
// contains the full ClusterWide map (shared, unfiltered), the single
// PerScyllaCluster entry for clusterKey, and only the PerPod entries for the
// pods belonging to that cluster (as supplied by podKeys).
func (v *Vitals) ForScyllaCluster(clusterKey ScopeKey, podKeys []ScopeKey) *Vitals {
	scoped := &Vitals{
		ClusterWide:      v.ClusterWide, // shared reference — read-only during analysis
		PerScyllaCluster: make(map[ScopeKey]map[CollectorID]*CollectorResult, 1),
		PerPod:           make(map[ScopeKey]map[CollectorID]*CollectorResult, len(podKeys)),
	}

	if perCluster, ok := v.PerScyllaCluster[clusterKey]; ok {
		scoped.PerScyllaCluster[clusterKey] = perCluster
	}

	for _, podKey := range podKeys {
		if perPod, ok := v.PerPod[podKey]; ok {
			scoped.PerPod[podKey] = perPod
		}
	}

	return scoped
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
	Status     CollectorStatus `json:"status"`
	Data       json.RawMessage `json:"data,omitempty"`
	Message    string          `json:"message"`
	Artifacts  []Artifact      `json:"artifacts"`
	DurationMs int64           `json:"duration_ms,omitempty"` // Wall-clock milliseconds spent in Collect()
}

// SerializableVitals is the JSON-safe version of Vitals for persistence
// to vitals.json. It mirrors the Vitals structure but uses
// SerializableCollectorResult so that Data is preserved as raw JSON.
type SerializableVitals struct {
	ClusterWide      map[CollectorID]*SerializableCollectorResult              `json:"cluster_wide"`
	PerScyllaCluster map[ScopeKey]map[CollectorID]*SerializableCollectorResult `json:"per_scylla_cluster"`
	PerPod           map[ScopeKey]map[CollectorID]*SerializableCollectorResult `json:"per_pod"`
}

// toSerializableResult converts a CollectorResult to its serializable form,
// marshaling the Data field to json.RawMessage.
func toSerializableResult(r *CollectorResult) (*SerializableCollectorResult, error) {
	sr := &SerializableCollectorResult{
		Status:     r.Status,
		Message:    r.Message,
		Artifacts:  r.Artifacts,
		DurationMs: r.Duration.Milliseconds(),
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
		ClusterWide:      make(map[CollectorID]*SerializableCollectorResult, len(v.ClusterWide)),
		PerScyllaCluster: make(map[ScopeKey]map[CollectorID]*SerializableCollectorResult, len(v.PerScyllaCluster)),
		PerPod:           make(map[ScopeKey]map[CollectorID]*SerializableCollectorResult, len(v.PerPod)),
	}

	for id, r := range v.ClusterWide {
		sr, err := toSerializableResult(r)
		if err != nil {
			return nil, fmt.Errorf("converting ClusterWide result %s: %w", id, err)
		}
		sv.ClusterWide[id] = sr
	}

	for key, perScyllaCluster := range v.PerScyllaCluster {
		sv.PerScyllaCluster[key] = make(map[CollectorID]*SerializableCollectorResult, len(perScyllaCluster))
		for id, r := range perScyllaCluster {
			sr, err := toSerializableResult(r)
			if err != nil {
				return nil, fmt.Errorf("converting PerScyllaCluster result %s/%s: %w", key, id, err)
			}
			sv.PerScyllaCluster[key][id] = sr
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
