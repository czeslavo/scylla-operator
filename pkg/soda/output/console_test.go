package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func testEngineResult() *engine.EngineResult {
	vitals := engine.NewVitals()

	// ClusterWide collector result.
	vitals.Store("NodeResourcesCollector", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "Collected 3 nodes",
	})

	// PerCluster collector result.
	clusterKey := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster"}
	vitals.Store("ScyllaClusterStatusCollector", engine.PerScyllaCluster, clusterKey, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "3/3 members ready",
	})

	// PerScyllaNode collector results.
	podKey := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster-0"}
	vitals.Store("OSInfoCollector", engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "Ubuntu 22.04 x86_64",
	})
	vitals.Store("ScyllaVersionCollector", engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "6.2.2",
	})

	return &engine.EngineResult{
		Vitals: vitals,
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{
			"ScyllaVersionSupportAnalyzer": {
				clusterKey: {
					Status:  engine.AnalyzerPassed,
					Message: "ScyllaDB 6.2.2 is supported",
				},
			},
			"SchemaAgreementAnalyzer": {
				clusterKey: {
					Status:  engine.AnalyzerWarning,
					Message: "No schema version information available",
				},
			},
			"OSSupportAnalyzer": {
				clusterKey: {
					Status:  engine.AnalyzerFailed,
					Message: "Unknown OS: Alpine Linux",
				},
			},
		},
		ResolvedCollectors: []engine.CollectorID{
			"NodeResourcesCollector",
			"ScyllaClusterStatusCollector",
			"OSInfoCollector",
			"ScyllaVersionCollector",
		},
		ResolvedAnalyzers: []engine.AnalyzerID{
			"ScyllaVersionSupportAnalyzer",
			"SchemaAgreementAnalyzer",
			"OSSupportAnalyzer",
		},
	}
}

func testClusters() []engine.ScyllaClusterInfo {
	return []engine.ScyllaClusterInfo{
		{Name: "my-cluster", Namespace: "scylla", Kind: "ScyllaCluster"},
	}
}

func testPods() map[engine.ScopeKey][]engine.ScyllaNodeInfo {
	return map[engine.ScopeKey][]engine.ScyllaNodeInfo{
		{Namespace: "scylla", Name: "my-cluster"}: {
			{Name: "my-cluster-0", Namespace: "scylla"},
		},
	}
}

func TestConsoleWriter_WriteReport(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriterNoColor(&buf)

	result := testEngineResult()
	err := cw.WriteReport(result, "full", testClusters(), testPods())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check header.
	if !strings.Contains(output, "ScyllaDB Diagnostics (profile: full)") {
		t.Errorf("missing header, got:\n%s", output)
	}

	// Check targets section.
	if !strings.Contains(output, "Scylla Clusters:") {
		t.Errorf("missing targets section, got:\n%s", output)
	}
	if !strings.Contains(output, "scylla/my-cluster (ScyllaCluster, 1 nodes)") {
		t.Errorf("missing cluster target, got:\n%s", output)
	}

	// Check collector lines.
	if !strings.Contains(output, "Collectors:") {
		t.Errorf("missing collectors section, got:\n%s", output)
	}
	if !strings.Contains(output, "[PASSED]") {
		t.Errorf("missing PASSED status, got:\n%s", output)
	}
	if !strings.Contains(output, "NodeResourcesCollector") {
		t.Errorf("missing NodeResourcesCollector, got:\n%s", output)
	}
	if !strings.Contains(output, "Collected 3 nodes") {
		t.Errorf("missing collector message, got:\n%s", output)
	}

	// Check scoped collector line has scope label.
	if !strings.Contains(output, "scylla/my-cluster: 3/3 members ready") {
		t.Errorf("missing scoped collector message, got:\n%s", output)
	}

	// Check analysis section.
	if !strings.Contains(output, "Analysis:") {
		t.Errorf("missing analysis section, got:\n%s", output)
	}
	if !strings.Contains(output, "ScyllaDB 6.2.2 is supported") {
		t.Errorf("missing analyzer message, got:\n%s", output)
	}

	// Check summary.
	if !strings.Contains(output, "Summary:") {
		t.Errorf("missing summary, got:\n%s", output)
	}
	if !strings.Contains(output, "1 passed") {
		t.Errorf("missing passed count, got:\n%s", output)
	}
	if !strings.Contains(output, "1 warnings") {
		t.Errorf("missing warnings count, got:\n%s", output)
	}
	if !strings.Contains(output, "1 failed") {
		t.Errorf("missing failed count, got:\n%s", output)
	}
	if !strings.Contains(output, "0 skipped") {
		t.Errorf("missing skipped count, got:\n%s", output)
	}
}

func TestConsoleWriter_NoClusters(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriterNoColor(&buf)

	result := &engine.EngineResult{
		Vitals:          engine.NewVitals(),
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
	}

	err := cw.WriteReport(result, "full", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Scylla Clusters:") {
		t.Errorf("should not show targets section when no clusters, got:\n%s", output)
	}
}

func TestConsoleWriter_AllStatusColors(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriterNoColor(&buf)

	result := &engine.EngineResult{
		Vitals: engine.NewVitals(),
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{
			"A1": {engine.ScopeKey{}: {Status: engine.AnalyzerPassed, Message: "ok"}},
			"A2": {engine.ScopeKey{}: {Status: engine.AnalyzerWarning, Message: "warn"}},
			"A3": {engine.ScopeKey{}: {Status: engine.AnalyzerFailed, Message: "fail"}},
			"A4": {engine.ScopeKey{}: {Status: engine.AnalyzerSkipped, Message: "skip"}},
		},
		ResolvedAnalyzers: []engine.AnalyzerID{"A1", "A2", "A3", "A4"},
	}

	err := cw.WriteReport(result, "test", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, expected := range []string{"PASSED", "WARNING", "FAILED", "SKIPPED"} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing status %q in output:\n%s", expected, output)
		}
	}
}

func TestConsoleWriter_CollectorStatuses(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriterNoColor(&buf)

	vitals := engine.NewVitals()
	vitals.Store("C1", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status: engine.CollectorPassed, Message: "ok",
	})
	vitals.Store("C2", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status: engine.CollectorFailed, Message: "error",
	})
	vitals.Store("C3", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status: engine.CollectorSkipped, Message: "dep failed",
	})

	result := &engine.EngineResult{
		Vitals:             vitals,
		AnalyzerResults:    map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
		ResolvedCollectors: []engine.CollectorID{"C1", "C2", "C3"},
	}

	err := cw.WriteReport(result, "test", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "[PASSED]") || !strings.Contains(output, "[FAILED]") || !strings.Contains(output, "[SKIPPED]") {
		t.Errorf("missing collector statuses in output:\n%s", output)
	}
}

func TestConsoleWriter_SummaryCount(t *testing.T) {
	tests := []struct {
		name         string
		results      map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult
		wantPassed   string
		wantWarnings string
		wantFailed   string
		wantSkipped  string
	}{
		{
			name: "all passed",
			results: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{
				"A1": {engine.ScopeKey{}: {Status: engine.AnalyzerPassed}},
				"A2": {engine.ScopeKey{}: {Status: engine.AnalyzerPassed}},
			},
			wantPassed:   "2 passed",
			wantWarnings: "0 warnings",
			wantFailed:   "0 failed",
			wantSkipped:  "0 skipped",
		},
		{
			name:         "empty",
			results:      map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
			wantPassed:   "0 passed",
			wantWarnings: "0 warnings",
			wantFailed:   "0 failed",
			wantSkipped:  "0 skipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cw := NewConsoleWriterNoColor(&buf)
			result := &engine.EngineResult{
				Vitals:          engine.NewVitals(),
				AnalyzerResults: tt.results,
			}
			cw.WriteReport(result, "test", nil, nil)
			output := buf.String()

			for _, want := range []string{tt.wantPassed, tt.wantWarnings, tt.wantFailed, tt.wantSkipped} {
				if !strings.Contains(output, want) {
					t.Errorf("missing %q in output:\n%s", want, output)
				}
			}
		})
	}
}

func TestConsoleWriterWithColor(t *testing.T) {
	// Verify that NewConsoleWriter creates a writer that doesn't panic.
	// We can't easily test ANSI codes without terminal support,
	// but we can verify it produces output.
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	result := &engine.EngineResult{
		Vitals: engine.NewVitals(),
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{
			"A1": {engine.ScopeKey{}: {Status: engine.AnalyzerPassed, Message: "ok"}},
		},
		ResolvedAnalyzers: []engine.AnalyzerID{"A1"},
	}

	err := cw.WriteReport(result, "full", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
}
