package collectors

import (
	"context"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	sodatesting "github.com/scylladb/scylla-operator/pkg/soda/testing"
)

func TestScyllaVersionCollector_HappyPath(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "scylla"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"scylla/pod-0/scylla/scylla --version": {
				Stdout: "5.4.9-0.20241017.0e4c24b49297\n",
			},
		},
	}
	fakeWriter := sodatesting.NewFakeArtifactWriter()

	collector := NewScyllaVersionCollector()
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

	typed, ok := result.Data.(*ScyllaVersionResult)
	if !ok {
		t.Fatalf("data type = %T, want *ScyllaVersionResult", result.Data)
	}

	if typed.Version != "5.4.9" {
		t.Errorf("Version = %q, want '5.4.9'", typed.Version)
	}
	if typed.Build != "0.20241017.0e4c24b49297" {
		t.Errorf("Build = %q, want '0.20241017.0e4c24b49297'", typed.Build)
	}
	if typed.Raw != "5.4.9-0.20241017.0e4c24b49297" {
		t.Errorf("Raw = %q, unexpected", typed.Raw)
	}

	// Artifact written.
	if _, ok := fakeWriter.Artifacts["scylla-version.log"]; !ok {
		t.Error("scylla-version.log not written")
	}
}

func TestScyllaVersionCollector_EnterpriseVersion(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/scylla --version": {
				Stdout: "2024.2.1-0.20241017.0e4c24b\n",
			},
		},
	}

	collector := NewScyllaVersionCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*ScyllaVersionResult)
	if typed.Version != "2024.2.1" {
		t.Errorf("Version = %q, want '2024.2.1'", typed.Version)
	}
	if typed.Build != "0.20241017.0e4c24b" {
		t.Errorf("Build = %q, want '0.20241017.0e4c24b'", typed.Build)
	}
}

func TestScyllaVersionCollector_SimpleVersion(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/scylla --version": {
				Stdout: "6.2.2\n",
			},
		},
	}

	collector := NewScyllaVersionCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*ScyllaVersionResult)
	if typed.Version != "6.2.2" {
		t.Errorf("Version = %q, want '6.2.2'", typed.Version)
	}
	if typed.Build != "" {
		t.Errorf("Build = %q, want empty", typed.Build)
	}
}

func TestScyllaVersionCollector_ExecError(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{},
	}

	collector := NewScyllaVersionCollector()
	_, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestScyllaVersionCollector_EmptyOutput(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/scylla --version": {Stdout: "\n"},
		},
	}

	collector := NewScyllaVersionCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode: pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*ScyllaVersionResult)
	if typed.Version != "" {
		t.Errorf("Version = %q, want empty", typed.Version)
	}
}

func TestGetScyllaVersionResult_TypedAccessor(t *testing.T) {
	vitals := engine.NewVitals()
	expected := &ScyllaVersionResult{Version: "6.2.2"}
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   expected,
	})

	got, err := GetScyllaVersionResult(vitals, podKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version != "6.2.2" {
		t.Errorf("Version = %q, want '6.2.2'", got.Version)
	}
}
