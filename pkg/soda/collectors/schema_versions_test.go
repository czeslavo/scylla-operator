package collectors

import (
	"context"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	sodatesting "github.com/scylladb/scylla-operator/pkg/soda/testing"
)

func TestSchemaVersionsCollector_HappyPath(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "scylla"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"scylla/pod-0/scylla/curl -s http://localhost:10000/storage_proxy/schema_versions": {
				Stdout: `[{"key":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","value":["10.0.0.1","10.0.0.2","10.0.0.3"]}]`,
			},
		},
	}
	fakeWriter := sodatesting.NewFakeArtifactWriter()

	collector := NewSchemaVersionsCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:         engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor:    fakeExec,
		ArtifactWriter: fakeWriter,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != engine.CollectorPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}

	typed, ok := result.Data.(*SchemaVersionsResult)
	if !ok {
		t.Fatalf("data type = %T, want *SchemaVersionsResult", result.Data)
	}

	if len(typed.Versions) != 1 {
		t.Fatalf("versions count = %d, want 1", len(typed.Versions))
	}
	if typed.Versions[0].SchemaVersion != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("schema version = %q, unexpected", typed.Versions[0].SchemaVersion)
	}
	if len(typed.Versions[0].Hosts) != 3 {
		t.Errorf("hosts count = %d, want 3", len(typed.Versions[0].Hosts))
	}

	if result.Message != "1 schema version(s)" {
		t.Errorf("message = %q, want '1 schema version(s)'", result.Message)
	}

	// Artifact written.
	if _, ok := fakeWriter.Artifacts["schema-versions.json"]; !ok {
		t.Error("schema-versions.json not written")
	}
}

func TestSchemaVersionsCollector_MultipleVersions(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/curl -s http://localhost:10000/storage_proxy/schema_versions": {
				Stdout: `[{"key":"uuid-1","value":["10.0.0.1"]},{"key":"uuid-2","value":["10.0.0.2","10.0.0.3"]}]`,
			},
		},
	}

	collector := NewSchemaVersionsCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*SchemaVersionsResult)
	if len(typed.Versions) != 2 {
		t.Fatalf("versions count = %d, want 2", len(typed.Versions))
	}
	if result.Message != "2 schema version(s)" {
		t.Errorf("message = %q, want '2 schema version(s)'", result.Message)
	}
}

func TestSchemaVersionsCollector_EmptyResponse(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/curl -s http://localhost:10000/storage_proxy/schema_versions": {
				Stdout: "[]",
			},
		},
	}

	collector := NewSchemaVersionsCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*SchemaVersionsResult)
	if len(typed.Versions) != 0 {
		t.Errorf("versions count = %d, want 0", len(typed.Versions))
	}
}

func TestSchemaVersionsCollector_InvalidJSON(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/curl -s http://localhost:10000/storage_proxy/schema_versions": {
				Stdout: "not json",
			},
		},
	}

	collector := NewSchemaVersionsCollector()
	_, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSchemaVersionsCollector_ExecError(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{},
	}

	collector := NewSchemaVersionsCollector()
	_, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSchemaVersionsCollector_NilPod(t *testing.T) {
	collector := NewSchemaVersionsCollector()
	_, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals: engine.NewVitals(),
	})
	if err == nil {
		t.Fatal("expected error for nil pod")
	}
}

func TestGetSchemaVersionsResult_TypedAccessor(t *testing.T) {
	vitals := engine.NewVitals()
	expected := &SchemaVersionsResult{
		Versions: []SchemaVersionEntry{
			{SchemaVersion: "uuid-1", Hosts: []string{"10.0.0.1"}},
		},
	}
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(SchemaVersionsCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   expected,
	})

	got, err := GetSchemaVersionsResult(vitals, podKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Versions) != 1 || got.Versions[0].SchemaVersion != "uuid-1" {
		t.Errorf("unexpected result: %v", got)
	}
}

func TestParseSchemaVersions_EmptyString(t *testing.T) {
	result, err := parseSchemaVersions("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Versions) != 0 {
		t.Errorf("versions count = %d, want 0", len(result.Versions))
	}
}
