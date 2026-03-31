package collectors

import (
	"context"
	"testing"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	sodatesting "github.com/scylladb/scylla-operator/pkg/soda/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func int32Ptr(v int32) *int32 { return &v }
func int64Ptr(v int64) *int64 { return &v }

func TestScyllaClusterStatusCollector_ScyllaCluster(t *testing.T) {
	sc := &scyllav1.ScyllaCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-cluster",
			Namespace:  "scylla",
			Generation: 5,
		},
		Status: scyllav1.ScyllaClusterStatus{
			ObservedGeneration: int64Ptr(5),
			Members:            int32Ptr(3),
			ReadyMembers:       int32Ptr(3),
			AvailableMembers:   int32Ptr(3),
			Conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionTrue, Message: "all good"},
			},
			Racks: map[string]scyllav1.RackStatus{
				"us-east1": {Members: 3, ReadyMembers: 3},
			},
		},
	}

	fakeWriter := sodatesting.NewFakeArtifactWriter()
	cluster := &engine.ClusterInfo{
		Name:      "my-cluster",
		Namespace: "scylla",
		Kind:      "ScyllaCluster",
		Object:    sc,
	}

	collector := NewScyllaClusterStatusCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:         engine.NewVitals(),
		Cluster:        cluster,
		ArtifactWriter: fakeWriter,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != engine.CollectorPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}

	typed, ok := result.Data.(*ScyllaClusterStatusResult)
	if !ok {
		t.Fatalf("data type = %T, want *ScyllaClusterStatusResult", result.Data)
	}

	if typed.Name != "my-cluster" {
		t.Errorf("Name = %q, want 'my-cluster'", typed.Name)
	}
	if typed.Kind != "ScyllaCluster" {
		t.Errorf("Kind = %q, want 'ScyllaCluster'", typed.Kind)
	}
	if typed.Members != 3 {
		t.Errorf("Members = %d, want 3", typed.Members)
	}
	if typed.ReadyMembers != 3 {
		t.Errorf("ReadyMembers = %d, want 3", typed.ReadyMembers)
	}
	if typed.ObservedGeneration != 5 {
		t.Errorf("ObservedGeneration = %d, want 5", typed.ObservedGeneration)
	}
	if len(typed.Conditions) != 1 || typed.Conditions[0].Type != "Available" {
		t.Errorf("Conditions = %v, unexpected", typed.Conditions)
	}
	if len(typed.Racks) != 1 || typed.Racks[0].Name != "us-east1" {
		t.Errorf("Racks = %v, unexpected", typed.Racks)
	}

	// Artifact written.
	if _, ok := fakeWriter.Artifacts["manifest.yaml"]; !ok {
		t.Error("manifest.yaml not written")
	}
}

func TestScyllaClusterStatusCollector_ScyllaDBDatacenter(t *testing.T) {
	sdc := &scyllav1alpha1.ScyllaDBDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-dc",
			Namespace:  "scylla",
			Generation: 3,
		},
		Status: scyllav1alpha1.ScyllaDBDatacenterStatus{
			ObservedGeneration: int64Ptr(3),
			Nodes:              int32Ptr(6),
			ReadyNodes:         int32Ptr(5),
			AvailableNodes:     int32Ptr(5),
			Conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionTrue},
			},
			Racks: []scyllav1alpha1.RackStatus{
				{Name: "rack-a", Nodes: int32Ptr(3), ReadyNodes: int32Ptr(3)},
				{Name: "rack-b", Nodes: int32Ptr(3), ReadyNodes: int32Ptr(2)},
			},
		},
	}

	cluster := &engine.ClusterInfo{
		Name:      "my-dc",
		Namespace: "scylla",
		Kind:      "ScyllaDBDatacenter",
		Object:    sdc,
	}

	collector := NewScyllaClusterStatusCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:  engine.NewVitals(),
		Cluster: cluster,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*ScyllaClusterStatusResult)
	if typed.Kind != "ScyllaDBDatacenter" {
		t.Errorf("Kind = %q, want 'ScyllaDBDatacenter'", typed.Kind)
	}
	if typed.Members != 6 {
		t.Errorf("Members = %d, want 6", typed.Members)
	}
	if typed.ReadyMembers != 5 {
		t.Errorf("ReadyMembers = %d, want 5", typed.ReadyMembers)
	}
	if len(typed.Racks) != 2 {
		t.Fatalf("Racks count = %d, want 2", len(typed.Racks))
	}
}

func TestScyllaClusterStatusCollector_NilCluster(t *testing.T) {
	collector := NewScyllaClusterStatusCollector()
	_, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals: engine.NewVitals(),
	})
	if err == nil {
		t.Fatal("expected error for nil cluster")
	}
}

func TestScyllaClusterStatusCollector_NilPointers(t *testing.T) {
	// ScyllaCluster with all nil pointer fields.
	sc := &scyllav1.ScyllaCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "ns"},
		Status:     scyllav1.ScyllaClusterStatus{},
	}

	cluster := &engine.ClusterInfo{
		Name: "empty", Namespace: "ns", Kind: "ScyllaCluster", Object: sc,
	}

	collector := NewScyllaClusterStatusCollector()
	result, err := collector.Collect(context.Background(), engine.CollectorParams{
		Vitals:  engine.NewVitals(),
		Cluster: cluster,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	typed := result.Data.(*ScyllaClusterStatusResult)
	if typed.Members != 0 || typed.ReadyMembers != 0 || typed.ObservedGeneration != 0 {
		t.Errorf("expected zero values for nil pointers, got Members=%d ReadyMembers=%d ObservedGeneration=%d",
			typed.Members, typed.ReadyMembers, typed.ObservedGeneration)
	}
}

func TestGetScyllaClusterStatusResult_TypedAccessor(t *testing.T) {
	vitals := engine.NewVitals()
	expected := &ScyllaClusterStatusResult{Name: "cluster", Members: 3, ReadyMembers: 3}
	key := engine.ScopeKey{Namespace: "ns", Name: "cluster"}
	vitals.Store(ScyllaClusterStatusCollectorID, engine.PerScyllaCluster, key, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   expected,
	})

	got, err := GetScyllaClusterStatusResult(vitals, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Members != 3 {
		t.Errorf("Members = %d, want 3", got.Members)
	}
}
