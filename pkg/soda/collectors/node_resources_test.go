package collectors

import (
	"context"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	sodatesting "github.com/scylladb/scylla-operator/pkg/soda/testing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeResourcesCollector_HappyPath(t *testing.T) {
	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-1",
				Labels: map[string]string{"topology.kubernetes.io/zone": "us-east1-b"},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3500m"),
					corev1.ResourceMemory: resource.MustParse("14Gi"),
				},
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Message: "kubelet is ready"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("8"),
				},
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
				},
			},
		},
	}

	fakeNodeLister := &sodatesting.FakeNodeLister{Nodes: nodes}
	fakeWriter := sodatesting.NewFakeArtifactWriter("collectors/cluster-wide/NodeResourcesCollector")

	collector := NewNodeResourcesCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:         engine.NewVitals(),
		NodeLister:     fakeNodeLister,
		ArtifactWriter: fakeWriter,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != engine.CollectorPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
	if result.Message != "Collected 2 nodes" {
		t.Errorf("message = %q, want 'Collected 2 nodes'", result.Message)
	}

	// Check typed result.
	typed, ok := result.Data.(*NodeResourcesResult)
	if !ok {
		t.Fatalf("data type = %T, want *NodeResourcesResult", result.Data)
	}
	if len(typed.Nodes) != 2 {
		t.Fatalf("nodes count = %d, want 2", len(typed.Nodes))
	}

	// Verify node-1.
	n1 := typed.Nodes[0]
	if n1.Name != "node-1" {
		t.Errorf("node[0].Name = %q, want 'node-1'", n1.Name)
	}
	if n1.Capacity["cpu"] != "4" {
		t.Errorf("node[0].Capacity[cpu] = %q, want '4'", n1.Capacity["cpu"])
	}
	if n1.Capacity["memory"] != "16Gi" {
		t.Errorf("node[0].Capacity[memory] = %q, want '16Gi'", n1.Capacity["memory"])
	}
	if n1.Allocatable["cpu"] != "3500m" {
		t.Errorf("node[0].Allocatable[cpu] = %q, want '3500m'", n1.Allocatable["cpu"])
	}
	if len(n1.Conditions) != 1 || n1.Conditions[0].Type != "Ready" {
		t.Errorf("node[0].Conditions = %v, want [{Ready True ...}]", n1.Conditions)
	}

	// Verify artifact was written.
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts count = %d, want 1", len(result.Artifacts))
	}
	if _, ok := fakeWriter.Artifacts["nodes.yaml"]; !ok {
		t.Error("nodes.yaml artifact not written")
	}
}

func TestNodeResourcesCollector_Error(t *testing.T) {
	fakeNodeLister := &sodatesting.FakeNodeLister{
		Err: context.DeadlineExceeded,
	}

	collector := NewNodeResourcesCollector()
	_, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:     engine.NewVitals(),
		NodeLister: fakeNodeLister,
	})

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNodeResourcesCollector_EmptyNodes(t *testing.T) {
	fakeNodeLister := &sodatesting.FakeNodeLister{Nodes: []corev1.Node{}}

	collector := NewNodeResourcesCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:     engine.NewVitals(),
		NodeLister: fakeNodeLister,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != engine.CollectorPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}

	typed := result.Data.(*NodeResourcesResult)
	if len(typed.Nodes) != 0 {
		t.Errorf("nodes count = %d, want 0", len(typed.Nodes))
	}
}

func TestGetNodeResourcesResult_TypedAccessor(t *testing.T) {
	vitals := engine.NewVitals()
	expected := &NodeResourcesResult{
		Nodes: []NodeInfo{{Name: "node-1"}},
	}
	vitals.Store(NodeResourcesCollectorID, engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   expected,
	})

	got, err := GetNodeResourcesResult(vitals, engine.ScopeKey{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Nodes[0].Name != "node-1" {
		t.Errorf("Name = %q, want 'node-1'", got.Nodes[0].Name)
	}
}

func TestGetNodeResourcesResult_NotFound(t *testing.T) {
	vitals := engine.NewVitals()
	_, err := GetNodeResourcesResult(vitals, engine.ScopeKey{})
	if err == nil {
		t.Fatal("expected error for missing result")
	}
}

func TestGetNodeResourcesResult_Failed(t *testing.T) {
	vitals := engine.NewVitals()
	vitals.Store(NodeResourcesCollectorID, engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status:  engine.CollectorFailed,
		Message: "something broke",
	})

	_, err := GetNodeResourcesResult(vitals, engine.ScopeKey{})
	if err == nil {
		t.Fatal("expected error for failed result")
	}
}
