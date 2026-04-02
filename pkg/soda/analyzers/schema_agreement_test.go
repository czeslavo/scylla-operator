package analyzers

import (
	"strings"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func TestSchemaAgreementAnalyzer_Metadata(t *testing.T) {
	a := NewSchemaAgreementAnalyzer()
	if a.ID() != SchemaAgreementAnalyzerID {
		t.Errorf("ID = %q, want %q", a.ID(), SchemaAgreementAnalyzerID)
	}
	if a.Name() == "" {
		t.Error("Name() is empty")
	}
	deps := a.DependsOn()
	if len(deps) != 1 || deps[0] != collectors.SchemaVersionsCollectorID {
		t.Errorf("DependsOn = %v, want [%s]", deps, collectors.SchemaVersionsCollectorID)
	}
}

func TestSchemaAgreementAnalyzer_AllAgree(t *testing.T) {
	uuid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	vitals := engine.NewVitals()

	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.SchemaVersionsResult{
				Versions: []collectors.SchemaVersionEntry{
					{SchemaVersion: uuid, Hosts: []string{"10.0.0.1"}},
				},
			},
		})
	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.SchemaVersionsResult{
				Versions: []collectors.SchemaVersionEntry{
					{SchemaVersion: uuid, Hosts: []string{"10.0.0.1"}},
				},
			},
		})

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
	if !strings.Contains(result.Message, uuid) {
		t.Errorf("message = %q, want to contain UUID", result.Message)
	}
}

func TestSchemaAgreementAnalyzer_Disagreement(t *testing.T) {
	uuid1 := "aaaa-1111"
	uuid2 := "bbbb-2222"
	vitals := engine.NewVitals()

	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.SchemaVersionsResult{
				Versions: []collectors.SchemaVersionEntry{
					{SchemaVersion: uuid1, Hosts: []string{"10.0.0.1"}},
				},
			},
		})
	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.SchemaVersionsResult{
				Versions: []collectors.SchemaVersionEntry{
					{SchemaVersion: uuid2, Hosts: []string{"10.0.0.2"}},
				},
			},
		})

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerFailed {
		t.Errorf("status = %v, want FAILED", result.Status)
	}
	if !strings.Contains(result.Message, "disagreement") {
		t.Errorf("message = %q, want to contain 'disagreement'", result.Message)
	}
	if !strings.Contains(result.Message, uuid1) || !strings.Contains(result.Message, uuid2) {
		t.Errorf("message = %q, want to contain both UUIDs", result.Message)
	}
}

func TestSchemaAgreementAnalyzer_SinglePodMultipleVersions(t *testing.T) {
	// A single pod can report multiple schema versions (e.g. during rolling upgrade).
	uuid1 := "aaaa-1111"
	uuid2 := "bbbb-2222"
	vitals := engine.NewVitals()

	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.SchemaVersionsResult{
				Versions: []collectors.SchemaVersionEntry{
					{SchemaVersion: uuid1, Hosts: []string{"10.0.0.1"}},
					{SchemaVersion: uuid2, Hosts: []string{"10.0.0.2"}},
				},
			},
		})

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerFailed {
		t.Errorf("status = %v, want FAILED", result.Status)
	}
}

func TestSchemaAgreementAnalyzer_NoPods(t *testing.T) {
	vitals := engine.NewVitals()

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "No schema version") {
		t.Errorf("message = %q, want to contain 'No schema version'", result.Message)
	}
}

func TestSchemaAgreementAnalyzer_SkipsFailedCollector(t *testing.T) {
	vitals := engine.NewVitals()
	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status:  engine.CollectorFailed,
			Message: "exec failed",
		})

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
}

func TestSchemaAgreementAnalyzer_EmptyVersionsFromPod(t *testing.T) {
	vitals := engine.NewVitals()
	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data:   &collectors.SchemaVersionsResult{Versions: []collectors.SchemaVersionEntry{}},
		})

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	// Pod checked, but no versions reported.
	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
}

func TestSchemaAgreementAnalyzer_MixedPassAndFail(t *testing.T) {
	// One pod passes with a version, another failed collector — should still report based on available data.
	uuid := "aaaa-1111"
	vitals := engine.NewVitals()

	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.SchemaVersionsResult{
				Versions: []collectors.SchemaVersionEntry{
					{SchemaVersion: uuid, Hosts: []string{"10.0.0.1"}},
				},
			},
		})
	vitals.Store(collectors.SchemaVersionsCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status:  engine.CollectorFailed,
			Message: "connection refused",
		})

	a := NewSchemaAgreementAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	// Only one pod's data available, single version → passed.
	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}
