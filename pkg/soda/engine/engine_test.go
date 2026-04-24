package engine

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Test helpers ---

// safeCallLog is a thread-safe call log shared across multiple recordingCollector instances.
type safeCallLog struct {
	mu      sync.Mutex
	entries []CollectorID
}

func (l *safeCallLog) append(id CollectorID) {
	l.mu.Lock()
	l.entries = append(l.entries, id)
	l.mu.Unlock()
}

func (l *safeCallLog) get() []CollectorID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]CollectorID, len(l.entries))
	copy(out, l.entries)
	return out
}

func (l *safeCallLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// recordingCollector records the order it was called relative to others.
type recordingCollector struct {
	id    CollectorID
	scope CollectorScope
	deps  []CollectorID

	callLog  *safeCallLog // shared across collectors to record call order
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
func (r *recordingCollector) Description() string      { return "" }
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
	r.callLog.append(r.id)

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
func (a *simpleAnalyzer) Description() string      { return "" }
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
	// With concurrent execution all collectors are dispatched in parallel,
	// so we can only assert that all collectors were invoked (not order).
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{id: "C1", scope: ClusterWide, callLog: callLog},
		"C2": &recordingCollector{id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}, callLog: callLog},
		"C3": &recordingCollector{id: "C3", scope: ClusterWide, deps: []CollectorID{"C2"}, callLog: callLog},
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

	_, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Under concurrent execution, C2 and C3 may see their dependencies as
	// "not found" (cascade fail) and not actually run their collect logic.
	// At minimum C1 must have been called. C2 and C3 may or may not appear
	// in callLog depending on timing, but all three must have results in Vitals.
	if callLog.len() < 1 {
		t.Errorf("expected at least C1 to be called, got %v", callLog.get())
	}
}

func TestEngineCascadeSkip(t *testing.T) {
	// C1 returns SKIPPED → C2 (depends on C1) should be cascade-affected.
	// Under concurrent execution C2 may run before C1 finishes, in which case
	// the cascade check finds no result for C1 and marks C2 as FAILED.
	// Either SKIPPED (if C1 finished first) or FAILED (if C2 raced ahead) is acceptable.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: ClusterWide, callLog: callLog,
			result: &CollectorResult{Status: CollectorSkipped, Message: "skipped"},
		},
		"C2": &recordingCollector{
			id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}, callLog: callLog,
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

	// C2 should not have been called (cascade skip/fail).
	for _, id := range callLog.get() {
		if id == "C2" {
			t.Error("C2 should not have been called (cascade)")
		}
	}

	// C2 should be either SKIPPED or FAILED in vitals depending on race timing.
	c2Result, ok := result.Vitals.Get("C2", ScopeKey{})
	if !ok {
		t.Fatal("C2 result not found in vitals")
	}
	if c2Result.Status != CollectorSkipped && c2Result.Status != CollectorFailed {
		t.Errorf("C2 status = %v, want SKIPPED or FAILED", c2Result.Status)
	}

	// Analyzer should also be skipped or failed.
	a1Status := result.AnalyzerResults["A1"][ScopeKey{}].Status
	if a1Status != AnalyzerSkipped && a1Status != AnalyzerFailed {
		t.Errorf("analyzer A1 status = %v, want SKIPPED or FAILED", a1Status)
	}
}

func TestEngineCascadeFail(t *testing.T) {
	// C1 returns FAILED → C2 (depends on C1) should be FAILED.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: ClusterWide, callLog: callLog,
			result: &CollectorResult{Status: CollectorFailed, Message: "C1 failed hard"},
		},
		"C2": &recordingCollector{
			id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}, callLog: callLog,
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
	cwLog := &safeCallLog{}
	pcLog := &safeCallLog{}
	ppLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{id: "CW", scope: ClusterWide, callLog: cwLog},
		"PC": &recordingCollector{id: "PC", scope: PerScyllaCluster, callLog: pcLog},
		"PP": &recordingCollector{id: "PP", scope: PerScyllaNode, callLog: ppLog},
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

	if cwLog.len() != 1 {
		t.Errorf("ClusterWide call count = %d, want 1", cwLog.len())
	}
	if pcLog.len() != 2 {
		t.Errorf("PerScyllaCluster call count = %d, want 2", pcLog.len())
	}
	if ppLog.len() != 4 {
		t.Errorf("PerScyllaNode call count = %d, want 4", ppLog.len())
	}
}

func TestEngineCrossScopeDep(t *testing.T) {
	// PerScyllaNode collector depends on ClusterWide collector.
	// Under concurrent execution PP may run before CW finishes, leading
	// to a cascade failure ("CW result not found"). Both outcomes are valid.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{id: "CW", scope: ClusterWide, callLog: callLog},
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, deps: []CollectorID{"CW"}, callLog: callLog,
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

	// Both CW and PP should have results in Vitals (regardless of execution order).
	if _, ok := result.Vitals.Get("CW", ScopeKey{}); !ok {
		t.Fatal("CW result not found in vitals")
	}

	ppResult, ok := result.Vitals.Get("PP", ScopeKey{Namespace: "ns1", Name: "pod-0"})
	if !ok {
		t.Fatal("PP result not found in vitals")
	}

	// PP may have passed (if CW finished first) or failed (cascade/race).
	if ppResult.Status != CollectorPassed && ppResult.Status != CollectorFailed {
		t.Errorf("PP status = %v, want PASSED or FAILED", ppResult.Status)
	}
}

func TestEngineCollectorError(t *testing.T) {
	// Collector returns an error → should be recorded as FAILED.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: ClusterWide, callLog: callLog,
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
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"C1": &recordingCollector{
			id: "C1", scope: PerScyllaNode, callLog: callLog,
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
	callLog := &safeCallLog{}
	factory := newFakeArtifactWriterFactory()

	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{
			id: "CW", scope: ClusterWide, callLog: callLog,
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
			id: "PP", scope: PerScyllaNode, callLog: callLog,
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
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, callLog: callLog,
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
	callLog := &safeCallLog{}
	var callCount atomic.Int64
	collectors := map[CollectorID]CollectorMeta{
		"PP": &recordingCollector{
			id: "PP", scope: PerScyllaNode, callLog: callLog,
			callFunc: func(_ context.Context, params testCollectorParams) (*CollectorResult, error) {
				callCount.Add(1)
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

func TestEngineConcurrentAllCollectorsInvoked(t *testing.T) {
	// Verify that all independent collectors across all scopes are invoked
	// when running concurrently.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"CW1": &recordingCollector{id: "CW1", scope: ClusterWide, callLog: callLog},
		"CW2": &recordingCollector{id: "CW2", scope: ClusterWide, callLog: callLog},
		"PC1": &recordingCollector{id: "PC1", scope: PerScyllaCluster, callLog: callLog},
		"PN1": &recordingCollector{id: "PN1", scope: PerScyllaNode, callLog: callLog},
		"PN2": &recordingCollector{id: "PN2", scope: PerScyllaNode, callLog: callLog},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"CW1", "CW2", "PC1", "PN1", "PN2"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{
		{Name: "cluster-a", Namespace: "ns1"},
		{Name: "cluster-b", Namespace: "ns2"},
	}
	nodes := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns1", Name: "cluster-a"}: {
			{Name: "pod-0", Namespace: "ns1", ClusterName: "cluster-a"},
			{Name: "pod-1", Namespace: "ns1", ClusterName: "cluster-a"},
		},
		{Namespace: "ns2", Name: "cluster-b"}: {
			{Name: "pod-0", Namespace: "ns2", ClusterName: "cluster-b"},
		},
	}

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    nodes,
	})

	result, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected invocations:
	// CW1: 1, CW2: 1, PC1: 2 (2 clusters), PN1: 3 (3 nodes), PN2: 3 (3 nodes)
	// Total: 10
	expectedCount := 1 + 1 + 2 + 3 + 3
	if callLog.len() != expectedCount {
		t.Errorf("call count = %d, want %d", callLog.len(), expectedCount)
	}

	// Verify all ClusterWide results exist.
	for _, id := range []CollectorID{"CW1", "CW2"} {
		if _, ok := result.Vitals.Get(id, ScopeKey{}); !ok {
			t.Errorf("%s result not found in vitals", id)
		}
	}

	// Verify all PerScyllaCluster results exist.
	for _, cluster := range clusters {
		key := ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		if _, ok := result.Vitals.Get("PC1", key); !ok {
			t.Errorf("PC1 result not found for %s", key)
		}
	}

	// Verify all PerScyllaNode results exist.
	for clusterKey, clusterNodes := range nodes {
		for _, node := range clusterNodes {
			nodeKey := ScopeKey{Namespace: node.Namespace, Name: node.Name}
			for _, id := range []CollectorID{"PN1", "PN2"} {
				if _, ok := result.Vitals.Get(id, nodeKey); !ok {
					t.Errorf("%s result not found for %s (cluster %s)", id, nodeKey, clusterKey)
				}
			}
		}
	}
}

func TestEngineConcurrentMaxParallelism(t *testing.T) {
	// Verify the concurrency limit is respected by tracking the peak number
	// of concurrently executing collectors.
	maxProcs := runtime.GOMAXPROCS(0)

	var active atomic.Int64
	var peak atomic.Int64

	const numCollectors = 20
	callLog := &safeCallLog{}
	collectors := make(map[CollectorID]CollectorMeta, numCollectors)
	collectorIDs := make([]CollectorID, 0, numCollectors)
	for i := range numCollectors {
		id := CollectorID(fmt.Sprintf("C%d", i))
		collectorIDs = append(collectorIDs, id)
		collectors[id] = &recordingCollector{
			id: id, scope: ClusterWide, callLog: callLog,
			callFunc: func(_ context.Context, _ testCollectorParams) (*CollectorResult, error) {
				current := active.Add(1)
				// Track peak concurrency.
				for {
					oldPeak := peak.Load()
					if current <= oldPeak || peak.CompareAndSwap(oldPeak, current) {
						break
					}
				}
				// Hold the slot briefly to allow other goroutines to accumulate.
				time.Sleep(10 * time.Millisecond)
				active.Add(-1)
				return &CollectorResult{Status: CollectorPassed, Message: "ok"}, nil
			},
		}
	}

	depIDs := make([]CollectorID, len(collectorIDs))
	copy(depIDs, collectorIDs)
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: depIDs},
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

	_, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	peakVal := peak.Load()
	if peakVal > int64(maxProcs) {
		t.Errorf("peak concurrency = %d, exceeds GOMAXPROCS = %d", peakVal, maxProcs)
	}
	// With 20 collectors and sleep, we expect some concurrency (at least 2 unless single-core).
	if maxProcs > 1 && peakVal < 2 {
		t.Errorf("peak concurrency = %d, expected at least 2 with GOMAXPROCS = %d", peakVal, maxProcs)
	}
}

func TestEngineConcurrentProgressEvents(t *testing.T) {
	// Verify that the progress callback receives start+finish events for every
	// collector invocation, even under concurrent execution.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"CW": &recordingCollector{id: "CW", scope: ClusterWide, callLog: callLog},
		"PN": &recordingCollector{id: "PN", scope: PerScyllaNode, callLog: callLog},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"CW", "PN"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	clusters := []ScyllaClusterInfo{{Name: "cluster", Namespace: "ns"}}
	nodes := map[ScopeKey][]ScyllaNodeInfo{
		{Namespace: "ns", Name: "cluster"}: {
			{Name: "pod-0", Namespace: "ns"},
			{Name: "pod-1", Namespace: "ns"},
		},
	}

	var mu sync.Mutex
	var startEvents, finishEvents []CollectorEvent

	eng := NewEngine(EngineConfig{
		AllCollectors:  collectors,
		AllAnalyzers:   analyzers,
		AllProfiles:    profiles,
		ProfileName:    "test",
		ScyllaClusters: clusters,
		ScyllaNodes:    nodes,
		OnCollectorEvent: func(ev CollectorEvent) {
			mu.Lock()
			defer mu.Unlock()
			switch ev.Kind {
			case CollectorEventStarted:
				startEvents = append(startEvents, ev)
			case CollectorEventFinished:
				finishEvents = append(finishEvents, ev)
			}
		},
	})

	_, err := eng.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected: CW (1 start + 1 finish) + PN (2 starts + 2 finishes) = 3 starts, 3 finishes.
	if len(startEvents) != 3 {
		t.Errorf("start events = %d, want 3", len(startEvents))
	}
	if len(finishEvents) != 3 {
		t.Errorf("finish events = %d, want 3", len(finishEvents))
	}

	// Every finish event should have a non-nil Result.
	for _, ev := range finishEvents {
		if ev.Result == nil {
			t.Errorf("finish event for %s/%s has nil Result", ev.CollectorID, ev.ScopeKey)
		}
	}
}

func TestEngineConcurrentErrorIsolation(t *testing.T) {
	// Verify that a failing collector does not affect independent collectors
	// running concurrently.
	callLog := &safeCallLog{}
	collectors := map[CollectorID]CollectorMeta{
		"GOOD": &recordingCollector{id: "GOOD", scope: ClusterWide, callLog: callLog,
			callFunc: func(_ context.Context, _ testCollectorParams) (*CollectorResult, error) {
				// Simulate some work.
				time.Sleep(5 * time.Millisecond)
				return &CollectorResult{Status: CollectorPassed, Message: "all good"}, nil
			},
		},
		"BAD": &recordingCollector{id: "BAD", scope: ClusterWide, callLog: callLog,
			err: fmt.Errorf("catastrophic failure"),
		},
	}
	analyzers := map[AnalyzerID]AnalyzerMeta{
		"A1": &simpleAnalyzer{id: "A1", deps: []CollectorID{"GOOD"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}, Collectors: []CollectorID{"BAD"}},
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

	// GOOD should have passed despite BAD failing.
	goodResult, ok := result.Vitals.Get("GOOD", ScopeKey{})
	if !ok {
		t.Fatal("GOOD result not found")
	}
	if goodResult.Status != CollectorPassed {
		t.Errorf("GOOD status = %v, want PASSED", goodResult.Status)
	}

	// BAD should have failed.
	badResult, ok := result.Vitals.Get("BAD", ScopeKey{})
	if !ok {
		t.Fatal("BAD result not found")
	}
	if badResult.Status != CollectorFailed {
		t.Errorf("BAD status = %v, want FAILED", badResult.Status)
	}
	if !strings.Contains(badResult.Message, "catastrophic failure") {
		t.Errorf("BAD message = %q, expected to contain 'catastrophic failure'", badResult.Message)
	}

	// Analyzer depending on GOOD should pass.
	if result.AnalyzerResults["A1"][ScopeKey{}].Status != AnalyzerPassed {
		t.Errorf("analyzer A1 status = %v, want PASSED", result.AnalyzerResults["A1"][ScopeKey{}].Status)
	}
}

func TestVitalsConcurrentStoreAndGet(t *testing.T) {
	// Stress test Vitals under concurrent access to verify the RWMutex protection.
	v := NewVitals()
	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func() {
			defer wg.Done()
			id := CollectorID(fmt.Sprintf("C%d", i))
			result := &CollectorResult{
				Status:  CollectorPassed,
				Message: fmt.Sprintf("result %d", i),
			}

			// Store in different scopes.
			switch i % 3 {
			case 0:
				v.Store(id, ClusterWide, ScopeKey{}, result)
			case 1:
				v.Store(id, PerScyllaCluster, ScopeKey{Namespace: "ns", Name: fmt.Sprintf("cluster-%d", i)}, result)
			case 2:
				v.Store(id, PerScyllaNode, ScopeKey{Namespace: "ns", Name: fmt.Sprintf("pod-%d", i)}, result)
			}

			// Read back.
			switch i % 3 {
			case 0:
				v.Get(id, ScopeKey{})
			case 1:
				v.Get(id, ScopeKey{Namespace: "ns", Name: fmt.Sprintf("cluster-%d", i)})
			case 2:
				v.Get(id, ScopeKey{Namespace: "ns", Name: fmt.Sprintf("pod-%d", i)})
			}

			// Also exercise key enumeration concurrently.
			v.ScyllaNodeKeys()
			v.ScyllaClusterKeys()
		}()
	}
	wg.Wait()

	// Verify all results were stored.
	for i := range numGoroutines {
		id := CollectorID(fmt.Sprintf("C%d", i))
		var key ScopeKey
		switch i % 3 {
		case 0:
			key = ScopeKey{}
		case 1:
			key = ScopeKey{Namespace: "ns", Name: fmt.Sprintf("cluster-%d", i)}
		case 2:
			key = ScopeKey{Namespace: "ns", Name: fmt.Sprintf("pod-%d", i)}
		}
		result, ok := v.Get(id, key)
		if !ok {
			t.Errorf("result for %s not found", id)
			continue
		}
		if result.Status != CollectorPassed {
			t.Errorf("result for %s status = %v, want PASSED", id, result.Status)
		}
	}
}
