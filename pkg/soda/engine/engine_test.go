package engine

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// --- Test helpers ---

// recordingCollector records the order it was called relative to others.
type recordingCollector struct {
	id    CollectorID
	scope CollectorScope
	deps  []CollectorID

	mu       sync.Mutex
	callLog  *[]CollectorID // shared across collectors to record call order
	result   *CollectorResult
	err      error
	callFunc func(ctx context.Context, params testCollectorParams) (*CollectorResult, error)
}

// testCollectorParams is a test-internal union of all scope-specific collector params
// to allow shared callFunc logic in test helpers.
type testCollectorParams struct {
	Vitals         *Vitals
	ScyllaCluster  *ScyllaClusterInfo
	ScyllaNode     *ScyllaNodeInfo
	PodExecutor    PodExecutor
	PodLogFetcher  PodLogFetcher
	ResourceLister ResourceLister
	ArtifactWriter ArtifactWriter
}

func (r *recordingCollector) ID() CollectorID          { return r.id }
func (r *recordingCollector) Name() string             { return string(r.id) }
func (r *recordingCollector) Scope() CollectorScope    { return r.scope }
func (r *recordingCollector) DependsOn() []CollectorID { return r.deps }

func (r *recordingCollector) CollectClusterWide(ctx context.Context, params ClusterWideCollectorParams) (*CollectorResult, error) {
	return r.doCollect(ctx, testCollectorParams{
		Vitals:         params.Vitals,
		ResourceLister: params.ResourceLister,
		PodLogFetcher:  params.PodLogFetcher,
		ArtifactWriter: params.ArtifactWriter,
	})
}

func (r *recordingCollector) CollectPerScyllaCluster(ctx context.Context, params PerScyllaClusterCollectorParams) (*CollectorResult, error) {
	return r.doCollect(ctx, testCollectorParams{
		Vitals:         params.Vitals,
		ScyllaCluster:  params.ScyllaCluster,
		ResourceLister: params.ResourceLister,
		ArtifactWriter: params.ArtifactWriter,
	})
}

func (r *recordingCollector) CollectPerScyllaNode(ctx context.Context, params PerScyllaNodeCollectorParams) (*CollectorResult, error) {
	return r.doCollect(ctx, testCollectorParams{
		Vitals:         params.Vitals,
		ScyllaCluster:  params.ScyllaCluster,
		ScyllaNode:     params.ScyllaNode,
		PodExecutor:    params.PodExecutor,
		PodLogFetcher:  params.PodLogFetcher,
		ResourceLister: params.ResourceLister,
		ArtifactWriter: params.ArtifactWriter,
	})
}

func (r *recordingCollector) doCollect(ctx context.Context, params testCollectorParams) (*CollectorResult, error) {
	r.mu.Lock()
	*r.callLog = append(*r.callLog, r.id)
	r.mu.Unlock()

	if r.callFunc != nil {
		return r.callFunc(ctx, params)
	}

	if r.err != nil {
		return nil, r.err
	}
	if r.result != nil {
		return r.result, nil
	}
	return &CollectorResult{
		Status:  CollectorPassed,
		Message: fmt.Sprintf("%s passed", r.id),
	}, nil
}

// simpleAnalyzer is a minimal analyzer for engine tests.
type simpleAnalyzer struct {
	id     AnalyzerID
	deps   []CollectorID
	result *AnalyzerResult
	fn     func(params ClusterWideAnalyzerParams) *AnalyzerResult
}

func (a *simpleAnalyzer) ID() AnalyzerID           { return a.id }
func (a *simpleAnalyzer) Name() string             { return string(a.id) }
func (a *simpleAnalyzer) Scope() AnalyzerScope     { return AnalyzerClusterWide }
func (a *simpleAnalyzer) DependsOn() []CollectorID { return a.deps }
func (a *simpleAnalyzer) AnalyzeClusterWide(params ClusterWideAnalyzerParams) *AnalyzerResult {
	if a.fn != nil {
		return a.fn(params)
	}
	if a.result != nil {
		return a.result
	}
	return &AnalyzerResult{Status: AnalyzerPassed, Message: fmt.Sprintf("%s passed", a.id)}
}

// fakeArtifactWriterFactory tracks writers created per invocation.
type fakeArtifactWriterFactory struct {
	mu      sync.Mutex
	Writers map[string]*fakeArtifactWriter // key: "collectorID/scope/scopeKey"
}

type fakeArtifactWriter struct {
	mu        sync.Mutex
	Artifacts map[string][]byte
	BaseDir   string
}

func (f *fakeArtifactWriter) WriteArtifact(filename string, content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	copied := make([]byte, len(content))
	copy(copied, content)
	f.Artifacts[filename] = copied

	if f.BaseDir != "" {
		return f.BaseDir + "/" + filename, nil
	}
	return filename, nil
}

func newFakeArtifactWriterFactory() *fakeArtifactWriterFactory {
	return &fakeArtifactWriterFactory{
		Writers: make(map[string]*fakeArtifactWriter),
	}
}

func (f *fakeArtifactWriterFactory) NewWriter(collectorID CollectorID, scope CollectorScope, scopeKey ScopeKey) ArtifactWriter {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fmt.Sprintf("%s/%s/%s", collectorID, scope, scopeKey)
	w := &fakeArtifactWriter{
		Artifacts: make(map[string][]byte),
		BaseDir:   key,
	}
	f.Writers[key] = w
	return w
}

// --- Tests ---

func TestEngineTopoOrder(t *testing.T) {
	// C1 has no deps, C2 depends on C1, C3 depends on C2.
	// Expected order: C1, C2, C3.
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{id: "C1", scope: ClusterWide, callLog: &callLog},
		"C2": &recordingCollector{id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}, callLog: &callLog},
		"C3": &recordingCollector{id: "C3", scope: ClusterWide, deps: []CollectorID{"C2"}, callLog: &callLog},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"C3"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors: collectors,
		AllAnalyzers:  analyzers,
		AllProfiles:   profiles,
		ProfileName:   "test",
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedOrder := []CollectorID{"C1", "C2", "C3"}
	if !reflect.DeepEqual(callLog, expectedOrder) {
		t.Errorf("call order = %v, want %v", callLog, expectedOrder)
	}

	// Analyzer should have passed.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerPassed {
		t.Errorf("analyzer A1 status = %v, want PASSED", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestEngineCascadeSkip(t *testing.T) {
	// C1 returns SKIPPED → C2 (depends on C1) should be SKIPPED.
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: ClusterWide, callLog: &callLog,
			result: &CollectorResult{Status: CollectorSkipped, Message: "skipped"},
		},
		"C2": &recordingCollector{
			id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}, callLog: &callLog,
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"C2"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors: collectors,
		AllAnalyzers:  analyzers,
		AllProfiles:   profiles,
		ProfileName:   "test",
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// C2 should not have been called (cascade skip).
	if len(callLog) != 1 || callLog[0] != "C1" {
		t.Errorf("call log = %v, want [C1] (C2 should be cascade-skipped)", callLog)
	}

	// C2 should be SKIPPED in vitals.
	c2Result, ok := result.Vitals.Get("C2", ScopeKey{})
	if !ok {
		t.Fatal("C2 result not found in vitals")
	}
	if c2Result.Status != CollectorSkipped {
		t.Errorf("C2 status = %v, want SKIPPED", c2Result.Status)
	}

	// Analyzer should also be skipped.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerSkipped {
		t.Errorf("analyzer A1 status = %v, want SKIPPED", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestEngineCascadeFail(t *testing.T) {
	// C1 returns FAILED → C2 (depends on C1) should be FAILED.
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: ClusterWide, callLog: &callLog,
			result: &CollectorResult{Status: CollectorFailed, Message: "C1 failed hard"},
		},
		"C2": &recordingCollector{
			id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}, callLog: &callLog,
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"C2"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors: collectors,
		AllAnalyzers:  analyzers,
		AllProfiles:   profiles,
		ProfileName:   "test",
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// C2 should be FAILED in vitals.
	c2Result, ok := result.Vitals.Get("C2", ScopeKey{})
	if !ok {
		t.Fatal("C2 result not found in vitals")
	}
	if c2Result.Status != CollectorFailed {
		t.Errorf("C2 status = %v, want FAILED", c2Result.Status)
	}
	if !strings.Contains(c2Result.Message, "C1") {
		t.Errorf("C2 message should mention C1: %q", c2Result.Message)
	}

	// Analyzer should be failed.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerFailed {
		t.Errorf("analyzer A1 status = %v, want FAILED", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestEngineScopeIteration(t *testing.T) {
	// ClusterWide should run once, PerScyllaCluster twice, PerScyllaNode four times.
	var cwLog, pcLog, ppLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{id: "CW", scope: ClusterWide, callLog: &cwLog},
		"PC": &recordingCollector{id: "PC", scope: PerScyllaCluster, callLog: &pcLog},
		"PP": &recordingCollector{id: "PP", scope: PerScyllaNode, callLog: &ppLog},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"CW", "PC", "PP"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{
		{Name: "cluster-a", Namespace: "ns1"},
		{Name: "cluster-b", Namespace: "ns2"},
	}
	pods := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns1", Name: "cluster-a"}: {
			{Name: "pod-0", Namespace: "ns1", ClusterName: "cluster-a"},
			{Name: "pod-1", Namespace: "ns1", ClusterName: "cluster-a"},
		},
		{Namespace: "ns2", Name: "cluster-b"}: {
			{Name: "pod-0", Namespace: "ns2", ClusterName: "cluster-b"},
			{Name: "pod-1", Namespace: "ns2", ClusterName: "cluster-b"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    pods,
	})

	_, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cwLog) != 1 {
		t.Errorf("ClusterWide call count = %d, want 1", len(cwLog))
	}
	if len(pcLog) != 2 {
		t.Errorf("PerScyllaCluster call count = %d, want 2", len(pcLog))
	}
	if len(ppLog) != 4 {
		t.Errorf("PerScyllaNode call count = %d, want 4", len(ppLog))
	}
}

func TestEngineCrossScopeDep(t *testing.T) {
	// PerScyllaNode collector depends on ClusterWide collector.
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{id: "CW", scope: ClusterWide, callLog: &callLog},
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, deps: []CollectorID{"CW"}, callLog: &callLog,
			callFunc: func(_ context.Context, params testCollectorParams) (*CollectorResult, error) {
				// Verify we can access the ClusterWide result.
				result, ok := params.Vitals.Get("CW", ScopeKey{})
				if !ok {
					return &CollectorResult{Status: CollectorFailed, Message: "CW result not found"}, nil
				}
				return &CollectorResult{
					Status:  CollectorPassed,
					Message: fmt.Sprintf("PP accessed CW: %s", result.Message),
				}, nil
			},
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"PP"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{{Name: "cluster-a", Namespace: "ns1"}}
	pods := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns1", Name: "cluster-a"}: {
			{Name: "pod-0", Namespace: "ns1", ClusterName: "cluster-a"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    pods,
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CW should run before PP.
	if len(callLog) != 2 || callLog[0] != "CW" || callLog[1] != "PP" {
		t.Errorf("call order = %v, want [CW, PP]", callLog)
	}

	// Verify PP was able to read CW's result.
	ppResult, ok := result.Vitals.Get("PP", ScopeKey{Namespace: "ns1", Name: "pod-0"})
	if !ok {
		t.Fatal("PP result not found")
	}
	if !strings.Contains(ppResult.Message, "PP accessed CW") {
		t.Errorf("PP message = %q, expected it to contain 'PP accessed CW'", ppResult.Message)
	}
}

func TestEngineCollectorError(t *testing.T) {
	// Collector returns an error → should be recorded as FAILED.
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: ClusterWide, callLog: &callLog,
			err: fmt.Errorf("network timeout"),
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors: collectors,
		AllAnalyzers:  analyzers,
		AllProfiles:   profiles,
		ProfileName:   "test",
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c1Result, ok := result.Vitals.Get("C1", ScopeKey{})
	if !ok {
		t.Fatal("C1 result not found")
	}
	if c1Result.Status != CollectorFailed {
		t.Errorf("C1 status = %v, want FAILED", c1Result.Status)
	}
	if !strings.Contains(c1Result.Message, "network timeout") {
		t.Errorf("C1 message = %q, expected to contain 'network timeout'", c1Result.Message)
	}

	// Analyzer should be failed since its dependency failed.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerFailed {
		t.Errorf("analyzer A1 status = %v, want FAILED", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestEngineEmptyRegistry(t *testing.T) {
	profiles := map[string]Profile{
		"empty": {Name: "empty"},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors: map[CollectorID]CollectorMeta{},
		AllAnalyzers:  map[AnalyzerID]AnalyzerMeta{},
		AllProfiles:   profiles,
		ProfileName:   "empty",
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AnalyzerResults) != 0 {
		t.Errorf("expected 0 analyzer results, got %d", len(result.AnalyzerResults))
	}
}

func TestEngineAllCollectorsFail(t *testing.T) {
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: PerScyllaNode, callLog: &callLog,
			result: &CollectorResult{Status: CollectorFailed, Message: "C1 failed"},
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{{Name: "cluster", Namespace: "ns"}}
	pods := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns", Name: "cluster"}: {
			{Name: "pod-0", Namespace: "ns"},
			{Name: "pod-1", Namespace: "ns"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    pods,
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All pods should have FAILED results.
	for _, podKey := range result.Vitals.ScyllaNodeKeys() {
		r, ok := result.Vitals.Get("C1", podKey)
		if !ok {
			t.Errorf("C1 result not found for %s", podKey)
			continue
		}
		if r.Status != CollectorFailed {
			t.Errorf("C1 status for %s = %v, want FAILED", podKey, r.Status)
		}
	}

	// Analyzer should be FAILED.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerFailed {
		t.Errorf("analyzer A1 status = %v, want FAILED", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestEngineArtifactWriterAssignment(t *testing.T) {
	// Verify that collectors receive an ArtifactWriter and artifacts are tracked.
	var callLog []CollectorID
	factory := newFakeArtifactWriterFactory()

	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{
			id: "CW", scope: ClusterWide, callLog: &callLog,
			callFunc: func(_ context.Context, params testCollectorParams) (*CollectorResult, error) {
				if params.ArtifactWriter == nil {
					return &CollectorResult{Status: CollectorFailed, Message: "no artifact writer"}, nil
				}
				relPath, err := params.ArtifactWriter.WriteArtifact("nodes.yaml", []byte("node data"))
				if err != nil {
					return &CollectorResult{Status: CollectorFailed, Message: err.Error()}, nil
				}
				return &CollectorResult{
					Status:  CollectorPassed,
					Message: "collected nodes",
					Artifacts: []Artifact{
						{RelativePath: relPath, Description: "Node YAML"},
					},
				}, nil
			},
		},
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, callLog: &callLog,
			callFunc: func(_ context.Context, params testCollectorParams) (*CollectorResult, error) {
				if params.ArtifactWriter == nil {
					return &CollectorResult{Status: CollectorFailed, Message: "no artifact writer"}, nil
				}
				relPath, err := params.ArtifactWriter.WriteArtifact("output.log", []byte("pod output"))
				if err != nil {
					return &CollectorResult{Status: CollectorFailed, Message: err.Error()}, nil
				}
				return &CollectorResult{
					Status:  CollectorPassed,
					Message: "collected pod info",
					Artifacts: []Artifact{
						{RelativePath: relPath, Description: "Pod output"},
					},
				}, nil
			},
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"CW", "PP"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{{Name: "cluster", Namespace: "ns"}}
	pods := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns", Name: "cluster"}: {
			{Name: "pod-0", Namespace: "ns"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:         collectors,
		AllAnalyzers:          analyzers,
		AllProfiles:           profiles,
		ProfileName:           "test",
		ScyllaClusters:        clusters,
		ScyllaNodes:           pods,
		ArtifactWriterFactory: factory,
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify CW collector got its writer and wrote artifacts.
	cwResult, ok := result.Vitals.Get("CW", ScopeKey{})
	if !ok {
		t.Fatal("CW result not found")
	}
	if cwResult.Status != CollectorPassed {
		t.Errorf("CW status = %v, want PASSED", cwResult.Status)
	}
	if len(cwResult.Artifacts) != 1 || cwResult.Artifacts[0].Description != "Node YAML" {
		t.Errorf("CW artifacts = %v, unexpected", cwResult.Artifacts)
	}

	// Verify PP collector got its writer.
	ppResult, ok := result.Vitals.Get("PP", ScopeKey{Namespace: "ns", Name: "pod-0"})
	if !ok {
		t.Fatal("PP result not found")
	}
	if ppResult.Status != CollectorPassed {
		t.Errorf("PP status = %v, want PASSED", ppResult.Status)
	}
	if len(ppResult.Artifacts) != 1 || ppResult.Artifacts[0].Description != "Pod output" {
		t.Errorf("PP artifacts = %v, unexpected", ppResult.Artifacts)
	}

	// Verify the factory created separate writers for CW and PP.
	if len(factory.Writers) != 2 {
		t.Errorf("expected 2 writers, got %d", len(factory.Writers))
	}

	// Verify the CW writer has the correct content.
	cwWriterKey := fmt.Sprintf("CW/%s/%s", ClusterWide, ScopeKey{})
	if w, ok := factory.Writers[cwWriterKey]; ok {
		if string(w.Artifacts["nodes.yaml"]) != "node data" {
			t.Errorf("CW artifact content = %q, want 'node data'", string(w.Artifacts["nodes.yaml"]))
		}
	} else {
		t.Errorf("CW writer not found with key %q; available keys: %v", cwWriterKey, factoryKeys(factory))
	}
}

func factoryKeys(f *fakeArtifactWriterFactory) []string {
	keys := make([]string, 0, len(f.Writers))
	for k := range f.Writers {
		keys = append(keys, k)
	}
	return keys
}

func TestEngineAnalyzerReceivesVitals(t *testing.T) {
	// Verify analyzers receive the full Vitals store and can iterate pod keys.
	var callLog []CollectorID
	collectors := map[CollectorID]CollectorMeta{
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, callLog: &callLog,
			callFunc: func(_ context.Context, params testCollectorParams) (*CollectorResult, error) {
				return &CollectorResult{
					Status:  CollectorPassed,
					Message: fmt.Sprintf("collected %s", params.ScyllaNode.Name),
					Data:    params.ScyllaNode.Name, // Store pod name as data for testing.
				}, nil
			},
		},
	}

	var analyzerPodCount int
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{
			id: "A1", deps: []CollectorID{"PP"},
			fn: func(params ClusterWideAnalyzerParams) *AnalyzerResult {
				analyzerPodCount = len(params.Vitals.ScyllaNodeKeys())
				return &AnalyzerResult{
					Status:  AnalyzerPassed,
					Message: fmt.Sprintf("analyzed %d pods", analyzerPodCount),
				}
			},
		},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{{Name: "cluster", Namespace: "ns"}}
	pods := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns", Name: "cluster"}: {
			{Name: "pod-0", Namespace: "ns"},
			{Name: "pod-1", Namespace: "ns"},
			{Name: "pod-2", Namespace: "ns"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    pods,
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analyzerPodCount != 3 {
		t.Errorf("analyzer saw %d pod keys, want 3", analyzerPodCount)
	}

	a1 := result.AnalyzerResults["A1"][ScopeKey{}]
	if a1.Status != AnalyzerPassed {
		t.Errorf("analyzer A1 status = %v, want PASSED", a1.Status)
	}
}

func TestEngineAnalyzerMixedCollectorResults(t *testing.T) {
	// One PerScyllaNode collector passes for some pods and fails for others.
	// The analyzer should still run since at least one result passed.
	var callLog []CollectorID
	callCount := 0
	collectors := map[CollectorID]CollectorMeta{
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, callLog: &callLog,
			callFunc: func(_ context.Context, params testCollectorParams) (*CollectorResult, error) {
				callCount++
				if params.ScyllaNode.Name == "pod-1" {
					return &CollectorResult{
						Status:  CollectorFailed,
						Message: "pod-1 failed",
					}, nil
				}
				return &CollectorResult{
					Status:  CollectorPassed,
					Message: fmt.Sprintf("%s OK", params.ScyllaNode.Name),
				}, nil
			},
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"PP"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{{Name: "cluster", Namespace: "ns"}}
	pods := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns", Name: "cluster"}: {
			{Name: "pod-0", Namespace: "ns"},
			{Name: "pod-1", Namespace: "ns"},
			{Name: "pod-2", Namespace: "ns"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    pods,
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Analyzer should still run since pods 0 and 2 passed.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerPassed {
		t.Errorf("analyzer A1 status = %v, want PASSED (mixed results)", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestTopoSortCollectors(t *testing.T) {
	tests := []struct {
		name     string
		ids      []CollectorID
		deps     map[CollectorID][]CollectorID
		expected []CollectorID
		wantErr  bool
	}{
		{
			name:     "no deps",
			ids:      []CollectorID{"B", "A", "C"},
			deps:     map[CollectorID][]CollectorID{},
			expected: []CollectorID{"A", "B", "C"}, // alphabetical when no deps
		},
		{
			name: "linear chain",
			ids:  []CollectorID{"C", "B", "A"},
			deps: map[CollectorID][]CollectorID{
				"B": {"A"},
				"C": {"B"},
			},
			expected: []CollectorID{"A", "B", "C"},
		},
		{
			name: "diamond",
			ids:  []CollectorID{"D", "C", "B", "A"},
			deps: map[CollectorID][]CollectorID{
				"B": {"A"},
				"C": {"A"},
				"D": {"B", "C"},
			},
			expected: []CollectorID{"A", "B", "C", "D"},
		},
		{
			name: "cycle",
			ids:  []CollectorID{"A", "B"},
			deps: map[CollectorID][]CollectorID{
				"A": {"B"},
				"B": {"A"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collectors := make(map[CollectorID]CollectorMeta)
			for _, id := range tt.ids {
				collectors[id] = &stubCollector{id: id, deps: tt.deps[id]}
			}

			got, err := topoSortCollectors(tt.ids, collectors)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}
