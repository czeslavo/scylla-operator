package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func TestJSONWriter_WriteReport(t *testing.T) {
	var buf bytes.Buffer
	jw := NewJSONWriter(&buf, "0.1.0-poc")

	result := testEngineResult()
	err := jw.WriteReport(result, "full", testClusters(), testPods())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON.
	var report JSONReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}

	// Verify metadata.
	if report.Metadata.Profile != "full" {
		t.Errorf("profile = %q, want 'full'", report.Metadata.Profile)
	}
	if report.Metadata.ToolVersion != "0.1.0-poc" {
		t.Errorf("tool_version = %q, want '0.1.0-poc'", report.Metadata.ToolVersion)
	}
	if report.Metadata.Timestamp == "" {
		t.Error("timestamp is empty")
	}

	// Verify targets.
	if len(report.Targets.ScyllaClusters) != 1 {
		t.Fatalf("clusters count = %d, want 1", len(report.Targets.ScyllaClusters))
	}
	if report.Targets.ScyllaClusters[0].Name != "my-cluster" {
		t.Errorf("cluster name = %q, want 'my-cluster'", report.Targets.ScyllaClusters[0].Name)
	}
	if report.Targets.ScyllaClusters[0].Namespace != "scylla" {
		t.Errorf("cluster namespace = %q, want 'scylla'", report.Targets.ScyllaClusters[0].Namespace)
	}
	if report.Targets.ScyllaClusters[0].Kind != "ScyllaCluster" {
		t.Errorf("cluster kind = %q, want 'ScyllaCluster'", report.Targets.ScyllaClusters[0].Kind)
	}
	if len(report.Targets.ScyllaClusters[0].Pods) != 1 || report.Targets.ScyllaClusters[0].Pods[0] != "my-cluster-0" {
		t.Errorf("cluster pods = %v, want [my-cluster-0]", report.Targets.ScyllaClusters[0].Pods)
	}

	// Verify collectors.
	if len(report.Collectors.ClusterWide) != 1 {
		t.Errorf("cluster_wide count = %d, want 1", len(report.Collectors.ClusterWide))
	}
	if res, ok := report.Collectors.ClusterWide["NodeResourcesCollector"]; ok {
		if res.Status != "passed" {
			t.Errorf("NodeResourcesCollector status = %q, want 'passed'", res.Status)
		}
		if res.Message != "Collected 3 nodes" {
			t.Errorf("NodeResourcesCollector message = %q, want 'Collected 3 nodes'", res.Message)
		}
	} else {
		t.Error("missing NodeResourcesCollector in cluster_wide")
	}

	if _, ok := report.Collectors.PerScyllaCluster["scylla/my-cluster"]; !ok {
		t.Error("missing scylla/my-cluster in per_scylla_cluster")
	}

	if _, ok := report.Collectors.PerScyllaNode["scylla/my-cluster-0"]; !ok {
		t.Error("missing scylla/my-cluster-0 in per_pod")
	}

	// Verify analysis.
	if len(report.Analysis) != 3 {
		t.Errorf("analysis count = %d, want 3", len(report.Analysis))
	}

	if res, ok := report.Analysis["ScyllaVersionSupportAnalyzer"]; ok {
		clusterKey := "scylla/my-cluster"
		if clusterRes, ok := res[clusterKey]; ok {
			if clusterRes.Status != "passed" {
				t.Errorf("ScyllaVersionSupportAnalyzer status = %q, want 'passed'", clusterRes.Status)
			}
		} else {
			t.Errorf("missing scope key %q for ScyllaVersionSupportAnalyzer", clusterKey)
		}
	} else {
		t.Error("missing ScyllaVersionSupportAnalyzer in analysis")
	}
}

func TestJSONWriter_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	jw := NewJSONWriter(&buf, "test")

	result := testEngineResult()
	if err := jw.WriteReport(result, "test-profile", testClusters(), testPods()); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Unmarshal and re-marshal to verify round-trip.
	var report JSONReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	remarshaled, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("re-marshal error: %v", err)
	}

	// Re-unmarshal to verify it's still valid.
	var report2 JSONReport
	if err := json.Unmarshal(remarshaled, &report2); err != nil {
		t.Fatalf("re-unmarshal error: %v", err)
	}

	if report2.Metadata.Profile != "test-profile" {
		t.Errorf("profile after round-trip = %q, want 'test-profile'", report2.Metadata.Profile)
	}
}

func TestJSONWriter_EmptyResult(t *testing.T) {
	var buf bytes.Buffer
	jw := NewJSONWriter(&buf, "test")

	result := &engine.EngineResult{
		Vitals:          engine.NewVitals(),
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
	}

	if err := jw.WriteReport(result, "empty", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var report JSONReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(report.Targets.ScyllaClusters) != 0 {
		t.Errorf("expected empty clusters, got %d", len(report.Targets.ScyllaClusters))
	}
	if len(report.Collectors.ClusterWide) != 0 {
		t.Errorf("expected empty cluster_wide collectors, got %d", len(report.Collectors.ClusterWide))
	}
	if len(report.Analysis) != 0 {
		t.Errorf("expected empty analysis, got %d", len(report.Analysis))
	}
}

func TestJSONWriter_AnalyzerStatuses(t *testing.T) {
	tests := []struct {
		status engine.AnalyzerStatus
		want   string
	}{
		{engine.AnalyzerPassed, "passed"},
		{engine.AnalyzerWarning, "warning"},
		{engine.AnalyzerFailed, "failed"},
		{engine.AnalyzerSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			var buf bytes.Buffer
			jw := NewJSONWriter(&buf, "test")

			result := &engine.EngineResult{
				Vitals: engine.NewVitals(),
				AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{
					"TestAnalyzer": {engine.ScopeKey{}: {Status: tt.status, Message: "test"}},
				},
			}

			jw.WriteReport(result, "test", nil, nil)

			var report JSONReport
			json.Unmarshal(buf.Bytes(), &report)

			if byScope, ok := report.Analysis["TestAnalyzer"]; ok {
				if res, ok := byScope[""]; ok {
					if res.Status != tt.want {
						t.Errorf("status = %q, want %q", res.Status, tt.want)
					}
				} else {
					t.Error("missing empty scope key for TestAnalyzer")
				}
			} else {
				t.Error("missing TestAnalyzer in analysis")
			}
		})
	}
}

func TestJSONWriter_CollectorStatuses(t *testing.T) {
	tests := []struct {
		status engine.CollectorStatus
		want   string
	}{
		{engine.CollectorPassed, "passed"},
		{engine.CollectorFailed, "failed"},
		{engine.CollectorSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			var buf bytes.Buffer
			jw := NewJSONWriter(&buf, "test")

			vitals := engine.NewVitals()
			vitals.Store("TestCollector", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
				Status:  tt.status,
				Message: "test",
			})

			result := &engine.EngineResult{
				Vitals:          vitals,
				AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
			}

			jw.WriteReport(result, "test", nil, nil)

			var report JSONReport
			json.Unmarshal(buf.Bytes(), &report)

			if res, ok := report.Collectors.ClusterWide["TestCollector"]; ok {
				if res.Status != tt.want {
					t.Errorf("status = %q, want %q", res.Status, tt.want)
				}
			} else {
				t.Error("missing TestCollector in cluster_wide")
			}
		})
	}
}

func TestJSONWriter_CollectorDataSerialized(t *testing.T) {
	var buf bytes.Buffer
	jw := NewJSONWriter(&buf, "test")

	type testData struct {
		Foo string `json:"foo"`
		Bar int    `json:"bar"`
	}

	vitals := engine.NewVitals()
	vitals.Store("TestCollector", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "ok",
		Data:    &testData{Foo: "hello", Bar: 42},
	})

	result := &engine.EngineResult{
		Vitals:          vitals,
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
	}

	jw.WriteReport(result, "test", nil, nil)

	var report JSONReport
	json.Unmarshal(buf.Bytes(), &report)

	if res, ok := report.Collectors.ClusterWide["TestCollector"]; ok {
		if res.Data == nil {
			t.Fatal("expected non-nil data")
		}
		// Verify the data field contains our struct.
		dataStr := string(res.Data)
		if !strings.Contains(dataStr, "hello") || !strings.Contains(dataStr, "42") {
			t.Errorf("data = %s, want to contain 'hello' and '42'", dataStr)
		}
	} else {
		t.Error("missing TestCollector")
	}
}

func TestJSONWriter_CollectorArtifacts(t *testing.T) {
	var buf bytes.Buffer
	jw := NewJSONWriter(&buf, "test")

	vitals := engine.NewVitals()
	vitals.Store("TestCollector", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "ok",
		Artifacts: []engine.Artifact{
			{RelativePath: "test/nodes.yaml", Description: "Node resources"},
		},
	})

	result := &engine.EngineResult{
		Vitals:          vitals,
		AnalyzerResults: map[engine.AnalyzerID]map[engine.ScopeKey]*engine.AnalyzerResult{},
	}

	jw.WriteReport(result, "test", nil, nil)

	var report JSONReport
	json.Unmarshal(buf.Bytes(), &report)

	if res, ok := report.Collectors.ClusterWide["TestCollector"]; ok {
		if len(res.Artifacts) != 1 {
			t.Fatalf("artifacts count = %d, want 1", len(res.Artifacts))
		}
		if res.Artifacts[0].RelativePath != "test/nodes.yaml" {
			t.Errorf("artifact path = %q, want 'test/nodes.yaml'", res.Artifacts[0].RelativePath)
		}
	} else {
		t.Error("missing TestCollector")
	}
}
