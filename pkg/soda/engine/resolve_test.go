package engine

import (
	"context"
	"reflect"
	"testing"
)

// stubCollector is a minimal Collector for resolve tests.
type stubCollector struct {
	id    CollectorID
	scope CollectorScope
	deps  []CollectorID
}

func (s *stubCollector) ID() CollectorID          { return s.id }
func (s *stubCollector) Name() string             { return string(s.id) }
func (s *stubCollector) Scope() CollectorScope    { return s.scope }
func (s *stubCollector) DependsOn() []CollectorID { return s.deps }
func (s *stubCollector) Collect(_ context.Context, _ CollectorParams) (*CollectorResult, error) {
	return nil, nil
}

// stubAnalyzer is a minimal Analyzer for resolve tests.
type stubAnalyzer struct {
	id   AnalyzerID
	deps []CollectorID
}

func (s *stubAnalyzer) ID() AnalyzerID           { return s.id }
func (s *stubAnalyzer) Name() string             { return string(s.id) }
func (s *stubAnalyzer) Scope() AnalyzerScope     { return AnalyzerClusterWide }
func (s *stubAnalyzer) DependsOn() []CollectorID { return s.deps }
func (s *stubAnalyzer) Analyze(_ AnalyzerParams) *AnalyzerResult {
	return nil
}

func TestResolveProfileBasic(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod},
		"C2": &stubCollector{id: "C2", scope: ClusterWide},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
		"A2": &stubAnalyzer{id: "A2", deps: []CollectorID{"C2"}},
	}
	profiles := map[string]Profile{
		"full": {Name: "full", Analyzers: []AnalyzerID{"A1", "A2"}},
	}

	gotC, gotA, err := ResolveProfile("full", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedC := []CollectorID{"C1", "C2"}
	expectedA := []AnalyzerID{"A1", "A2"}

	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
	if !reflect.DeepEqual(gotA, expectedA) {
		t.Errorf("analyzers = %v, want %v", gotA, expectedA)
	}
}

func TestResolveProfileTransitiveDeps(t *testing.T) {
	// C3 depends on C2, C2 depends on C1, A1 depends on C3.
	// Resolution should include C1, C2, C3.
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: ClusterWide},
		"C2": &stubCollector{id: "C2", scope: ClusterWide, deps: []CollectorID{"C1"}},
		"C3": &stubCollector{id: "C3", scope: PerScyllaCluster, deps: []CollectorID{"C2"}},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C3"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	gotC, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedC := []CollectorID{"C1", "C2", "C3"}
	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
}

func TestResolveProfileComposition(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod},
		"C2": &stubCollector{id: "C2", scope: PerPod},
		"C3": &stubCollector{id: "C3", scope: ClusterWide},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
		"A2": &stubAnalyzer{id: "A2", deps: []CollectorID{"C2"}},
		"A3": &stubAnalyzer{id: "A3", deps: []CollectorID{"C3"}},
	}
	profiles := map[string]Profile{
		"base":  {Name: "base", Analyzers: []AnalyzerID{"A1"}},
		"extra": {Name: "extra", Analyzers: []AnalyzerID{"A2"}},
		"full":  {Name: "full", Includes: []string{"base", "extra"}, Analyzers: []AnalyzerID{"A3"}},
	}

	gotC, gotA, err := ResolveProfile("full", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedC := []CollectorID{"C1", "C2", "C3"}
	expectedA := []AnalyzerID{"A1", "A2", "A3"}

	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
	if !reflect.DeepEqual(gotA, expectedA) {
		t.Errorf("analyzers = %v, want %v", gotA, expectedA)
	}
}

func TestResolveProfileDeepComposition(t *testing.T) {
	// Profile chain: full → mid → base.
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod},
		"C2": &stubCollector{id: "C2", scope: PerPod},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
		"A2": &stubAnalyzer{id: "A2", deps: []CollectorID{"C2"}},
	}
	profiles := map[string]Profile{
		"base": {Name: "base", Analyzers: []AnalyzerID{"A1"}},
		"mid":  {Name: "mid", Includes: []string{"base"}},
		"full": {Name: "full", Includes: []string{"mid"}, Analyzers: []AnalyzerID{"A2"}},
	}

	_, gotA, err := ResolveProfile("full", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedA := []AnalyzerID{"A1", "A2"}
	if !reflect.DeepEqual(gotA, expectedA) {
		t.Errorf("analyzers = %v, want %v", gotA, expectedA)
	}
}

func TestResolveProfileEnableOverride(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod},
		"C2": &stubCollector{id: "C2", scope: PerPod},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
		"A2": &stubAnalyzer{id: "A2", deps: []CollectorID{"C2"}},
	}
	profiles := map[string]Profile{
		"small": {Name: "small", Analyzers: []AnalyzerID{"A1"}},
	}

	// Enable A2 on top of the "small" profile.
	_, gotA, err := ResolveProfile("small", profiles, []AnalyzerID{"A2"}, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedA := []AnalyzerID{"A1", "A2"}
	if !reflect.DeepEqual(gotA, expectedA) {
		t.Errorf("analyzers = %v, want %v", gotA, expectedA)
	}
}

func TestResolveProfileDisableOverride(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod},
		"C2": &stubCollector{id: "C2", scope: PerPod},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
		"A2": &stubAnalyzer{id: "A2", deps: []CollectorID{"C2"}},
	}
	profiles := map[string]Profile{
		"full": {Name: "full", Analyzers: []AnalyzerID{"A1", "A2"}},
	}

	// Disable A1 from the "full" profile.
	gotC, gotA, err := ResolveProfile("full", profiles, nil, []AnalyzerID{"A1"}, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only A2 remains, so only C2 is needed.
	expectedC := []CollectorID{"C2"}
	expectedA := []AnalyzerID{"A2"}

	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
	if !reflect.DeepEqual(gotA, expectedA) {
		t.Errorf("analyzers = %v, want %v", gotA, expectedA)
	}
}

func TestResolveProfileCycleInProfiles(t *testing.T) {
	profiles := map[string]Profile{
		"a": {Name: "a", Includes: []string{"b"}},
		"b": {Name: "b", Includes: []string{"a"}},
	}

	_, _, err := ResolveProfile("a", profiles, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestResolveProfileCycleInCollectors(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod, deps: []CollectorID{"C2"}},
		"C2": &stubCollector{id: "C2", scope: PerPod, deps: []CollectorID{"C1"}},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	_, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestResolveProfileUnknownProfile(t *testing.T) {
	_, _, err := ResolveProfile("nonexistent", map[string]Profile{}, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestResolveProfileUnknownAnalyzer(t *testing.T) {
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"NonexistentAnalyzer"}},
	}

	_, _, err := ResolveProfile("test", profiles, nil, nil, map[AnalyzerID]Analyzer{}, nil)
	if err == nil {
		t.Fatal("expected error for unknown analyzer ID")
	}
}

func TestResolveProfileUnknownCollector(t *testing.T) {
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"NonexistentCollector"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	_, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, map[CollectorID]Collector{})
	if err == nil {
		t.Fatal("expected error for unknown collector ID")
	}
}

func TestResolveProfileCrossScopeViolation_ClusterWideDependsOnPerPod(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"CW": &stubCollector{id: "CW", scope: ClusterWide, deps: []CollectorID{"PP"}},
		"PP": &stubCollector{id: "PP", scope: PerPod},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"CW"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	_, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err == nil {
		t.Fatal("expected cross-scope violation error")
	}
}

func TestResolveProfileCrossScopeViolation_ClusterWideDependsOnPerCluster(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"CW": &stubCollector{id: "CW", scope: ClusterWide, deps: []CollectorID{"PC"}},
		"PC": &stubCollector{id: "PC", scope: PerScyllaCluster},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"CW"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	_, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err == nil {
		t.Fatal("expected cross-scope violation error")
	}
}

func TestResolveProfileCrossScopeViolation_PerClusterDependsOnPerPod(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"PC": &stubCollector{id: "PC", scope: PerScyllaCluster, deps: []CollectorID{"PP"}},
		"PP": &stubCollector{id: "PP", scope: PerPod},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"PC"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	_, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err == nil {
		t.Fatal("expected cross-scope violation error")
	}
}

func TestResolveProfileValidCrossScope_PerPodDependsOnClusterWide(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"CW": &stubCollector{id: "CW", scope: ClusterWide},
		"PP": &stubCollector{id: "PP", scope: PerPod, deps: []CollectorID{"CW"}},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"PP"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	gotC, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedC := []CollectorID{"CW", "PP"}
	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
}

func TestResolveProfileValidCrossScope_PerClusterDependsOnClusterWide(t *testing.T) {
	collectors := map[CollectorID]Collector{
		"CW": &stubCollector{id: "CW", scope: ClusterWide},
		"PC": &stubCollector{id: "PC", scope: PerScyllaCluster, deps: []CollectorID{"CW"}},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"PC"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1"}},
	}

	gotC, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedC := []CollectorID{"CW", "PC"}
	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
}

func TestResolveProfileEmptyProfile(t *testing.T) {
	profiles := map[string]Profile{
		"empty": {Name: "empty"},
	}

	gotC, gotA, err := ResolveProfile("empty", profiles, nil, nil, map[AnalyzerID]Analyzer{}, map[CollectorID]Collector{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gotC) != 0 {
		t.Errorf("expected 0 collectors, got %v", gotC)
	}
	if len(gotA) != 0 {
		t.Errorf("expected 0 analyzers, got %v", gotA)
	}
}

func TestResolveProfileDeduplication(t *testing.T) {
	// Two analyzers depend on the same collector.
	collectors := map[CollectorID]Collector{
		"C1": &stubCollector{id: "C1", scope: PerPod},
	}
	analyzers := map[AnalyzerID]Analyzer{
		"A1": &stubAnalyzer{id: "A1", deps: []CollectorID{"C1"}},
		"A2": &stubAnalyzer{id: "A2", deps: []CollectorID{"C1"}},
	}
	profiles := map[string]Profile{
		"test": {Name: "test", Analyzers: []AnalyzerID{"A1", "A2"}},
	}

	gotC, _, err := ResolveProfile("test", profiles, nil, nil, analyzers, collectors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// C1 should appear only once.
	expectedC := []CollectorID{"C1"}
	if !reflect.DeepEqual(gotC, expectedC) {
		t.Errorf("collectors = %v, want %v", gotC, expectedC)
	}
}

func TestResolveProfileEnableUnknownAnalyzer(t *testing.T) {
	profiles := map[string]Profile{
		"test": {Name: "test"},
	}

	_, _, err := ResolveProfile("test", profiles, []AnalyzerID{"NonexistentAnalyzer"}, nil, map[AnalyzerID]Analyzer{}, nil)
	if err == nil {
		t.Fatal("expected error for unknown enabled analyzer ID")
	}
}
