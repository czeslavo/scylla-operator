package collectors

import (
	"context"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	sodatesting "github.com/scylladb/scylla-operator/pkg/soda/testing"
)

func TestOSInfoCollector_HappyPath(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "scylla"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"scylla/pod-0/scylla/uname --all": {
				Stdout: "Linux pod-0 5.15.0-1041-gke #46-Ubuntu SMP x86_64 x86_64 x86_64 GNU/Linux\n",
			},
			"scylla/pod-0/scylla/cat /etc/os-release": {
				Stdout: `NAME="Red Hat Enterprise Linux"
VERSION="9.7 (Plow)"
ID="rhel"
VERSION_ID="9.7"
PLATFORM_ID="platform:el9"
PRETTY_NAME="Red Hat Enterprise Linux 9.7 (Plow)"
`,
			},
		},
	}
	fakeWriter := sodatesting.NewFakeArtifactWriter()

	collector := NewOSInfoCollector()
	result, err := collector.CollectPerScyllaNode(context.Background(), engine.PerScyllaNodeCollectorParams{
		Vitals:         engine.NewVitals(),
		ScyllaNode:     pod,
		PodExecutor:    fakeExec,
		ArtifactWriter: fakeWriter,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != engine.CollectorPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}

	typed, ok := result.Data.(*OSInfoResult)
	if !ok {
		t.Fatalf("data type = %T, want *OSInfoResult", result.Data)
	}

	if typed.Architecture != "x86_64" {
		t.Errorf("Architecture = %q, want 'x86_64'", typed.Architecture)
	}
	if typed.KernelVersion != "5.15.0-1041-gke" {
		t.Errorf("KernelVersion = %q, want '5.15.0-1041-gke'", typed.KernelVersion)
	}
	if typed.OSName != "Red Hat Enterprise Linux" {
		t.Errorf("OSName = %q, want 'Red Hat Enterprise Linux'", typed.OSName)
	}
	if typed.OSVersion != "9.7" {
		t.Errorf("OSVersion = %q, want '9.7'", typed.OSVersion)
	}
	if typed.OSReleaseFull["ID"] != "rhel" {
		t.Errorf("OSReleaseFull[ID] = %q, want 'rhel'", typed.OSReleaseFull["ID"])
	}

	// Check artifacts.
	if len(result.Artifacts) != 2 {
		t.Errorf("artifacts count = %d, want 2", len(result.Artifacts))
	}
	if _, ok := fakeWriter.Artifacts["uname.log"]; !ok {
		t.Error("uname.log not written")
	}
	if _, ok := fakeWriter.Artifacts["os-release.log"]; !ok {
		t.Error("os-release.log not written")
	}
}

func TestOSInfoCollector_Ubuntu(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/uname --all": {
				Stdout: "Linux pod-0 6.1.0-20-amd64 #1 SMP PREEMPT_DYNAMIC Debian 6.1.85-1 x86_64 GNU/Linux\n",
			},
			"ns/pod-0/scylla/cat /etc/os-release": {
				Stdout: `NAME="Ubuntu"
VERSION_ID="22.04"
ID=ubuntu
`,
			},
		},
	}

	collector := NewOSInfoCollector()
	result, err := collector.CollectPerScyllaNode(context.Background(), engine.PerScyllaNodeCollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode:  pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*OSInfoResult)
	if typed.OSName != "Ubuntu" {
		t.Errorf("OSName = %q, want 'Ubuntu'", typed.OSName)
	}
	if typed.OSVersion != "22.04" {
		t.Errorf("OSVersion = %q, want '22.04'", typed.OSVersion)
	}
}

func TestOSInfoCollector_ARM64(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/uname --all": {
				Stdout: "Linux pod-0 5.15.0 #1 SMP aarch64 aarch64 aarch64 GNU/Linux\n",
			},
			"ns/pod-0/scylla/cat /etc/os-release": {
				Stdout: `NAME="Amazon Linux"
VERSION_ID="2023"
`,
			},
		},
	}

	collector := NewOSInfoCollector()
	result, err := collector.CollectPerScyllaNode(context.Background(), engine.PerScyllaNodeCollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode:  pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*OSInfoResult)
	if typed.Architecture != "aarch64" {
		t.Errorf("Architecture = %q, want 'aarch64'", typed.Architecture)
	}
}

func TestOSInfoCollector_UnameError(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{},
	}

	collector := NewOSInfoCollector()
	_, err := collector.CollectPerScyllaNode(context.Background(), engine.PerScyllaNodeCollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode:  pod,
		PodExecutor: fakeExec,
	})

	if err == nil {
		t.Fatal("expected error for uname failure")
	}
}

func TestOSInfoCollector_EmptyOSRelease(t *testing.T) {
	pod := &engine.ScyllaNodeInfo{Name: "pod-0", Namespace: "ns"}
	fakeExec := &sodatesting.FakePodExecutor{
		Responses: map[string]sodatesting.FakeExecResponse{
			"ns/pod-0/scylla/uname --all": {
				Stdout: "Linux pod-0 5.15.0 #1 SMP x86_64 GNU/Linux\n",
			},
			"ns/pod-0/scylla/cat /etc/os-release": {
				Stdout: "",
			},
		},
	}

	collector := NewOSInfoCollector()
	result, err := collector.CollectPerScyllaNode(context.Background(), engine.PerScyllaNodeCollectorParams{
		Vitals:      engine.NewVitals(),
		ScyllaNode:  pod,
		PodExecutor: fakeExec,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*OSInfoResult)
	if typed.OSName != "" {
		t.Errorf("OSName = %q, want empty", typed.OSName)
	}
	// Message should fall back to kernel info.
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestGetOSInfoResult_TypedAccessor(t *testing.T) {
	vitals := engine.NewVitals()
	expected := &OSInfoResult{OSName: "Ubuntu", OSVersion: "22.04"}
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   expected,
	})

	got, err := GetOSInfoResult(vitals, podKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OSName != "Ubuntu" {
		t.Errorf("OSName = %q, want 'Ubuntu'", got.OSName)
	}
}
