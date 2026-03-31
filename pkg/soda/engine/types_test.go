package engine

import (
	"testing"
)

func TestCollectorScopeString(t *testing.T) {
	tests := []struct {
		scope    CollectorScope
		expected string
	}{
		{ClusterWide, "ClusterWide"},
		{PerScyllaCluster, "PerScyllaCluster"},
		{PerPod, "PerPod"},
		{CollectorScope(99), "CollectorScope(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.scope.String(); got != tt.expected {
				t.Errorf("CollectorScope.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCollectorStatusString(t *testing.T) {
	tests := []struct {
		status   CollectorStatus
		expected string
	}{
		{CollectorPassed, "PASSED"},
		{CollectorFailed, "FAILED"},
		{CollectorSkipped, "SKIPPED"},
		{CollectorStatus(99), "CollectorStatus(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("CollectorStatus.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAnalyzerStatusString(t *testing.T) {
	tests := []struct {
		status   AnalyzerStatus
		expected string
	}{
		{AnalyzerPassed, "PASSED"},
		{AnalyzerSkipped, "SKIPPED"},
		{AnalyzerWarning, "WARNING"},
		{AnalyzerFailed, "FAILED"},
		{AnalyzerStatus(99), "AnalyzerStatus(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("AnalyzerStatus.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScopeKeyString(t *testing.T) {
	tests := []struct {
		name     string
		key      ScopeKey
		expected string
	}{
		{
			name:     "normal key",
			key:      ScopeKey{Namespace: "scylla", Name: "my-cluster"},
			expected: "scylla/my-cluster",
		},
		{
			name:     "empty namespace",
			key:      ScopeKey{Namespace: "", Name: "my-cluster"},
			expected: "/my-cluster",
		},
		{
			name:     "empty both",
			key:      ScopeKey{},
			expected: "/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.String(); got != tt.expected {
				t.Errorf("ScopeKey.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNewVitals(t *testing.T) {
	v := NewVitals()
	if v.ClusterWide == nil {
		t.Fatal("NewVitals().ClusterWide is nil")
	}
	if v.PerCluster == nil {
		t.Fatal("NewVitals().PerCluster is nil")
	}
	if v.PerPod == nil {
		t.Fatal("NewVitals().PerPod is nil")
	}
}

func TestVitalsStoreAndGet(t *testing.T) {
	v := NewVitals()

	clusterWideResult := &CollectorResult{
		Status:  CollectorPassed,
		Message: "collected 3 nodes",
	}
	perClusterResult := &CollectorResult{
		Status:  CollectorPassed,
		Message: "cluster status OK",
	}
	perPodResult := &CollectorResult{
		Status:  CollectorPassed,
		Message: "OS info collected",
	}

	clusterKey := ScopeKey{Namespace: "scylla", Name: "my-cluster"}
	podKey := ScopeKey{Namespace: "scylla", Name: "my-cluster-us-east1-0"}

	// Store results at different scopes.
	v.Store("NodeResourcesCollector", ClusterWide, ScopeKey{}, clusterWideResult)
	v.Store("ScyllaClusterStatusCollector", PerScyllaCluster, clusterKey, perClusterResult)
	v.Store("OSInfoCollector", PerPod, podKey, perPodResult)

	t.Run("Get ClusterWide result with any scopeKey", func(t *testing.T) {
		result, ok := v.Get("NodeResourcesCollector", ScopeKey{})
		if !ok {
			t.Fatal("expected to find ClusterWide result")
		}
		if result.Message != "collected 3 nodes" {
			t.Errorf("unexpected message: %q", result.Message)
		}

		// ClusterWide results are also findable with any scopeKey since they're checked first.
		result2, ok := v.Get("NodeResourcesCollector", podKey)
		if !ok {
			t.Fatal("expected to find ClusterWide result with non-empty scopeKey")
		}
		if result2 != result {
			t.Error("expected same result pointer")
		}
	})

	t.Run("Get PerCluster result", func(t *testing.T) {
		result, ok := v.Get("ScyllaClusterStatusCollector", clusterKey)
		if !ok {
			t.Fatal("expected to find PerCluster result")
		}
		if result.Message != "cluster status OK" {
			t.Errorf("unexpected message: %q", result.Message)
		}
	})

	t.Run("Get PerCluster result with wrong key", func(t *testing.T) {
		_, ok := v.Get("ScyllaClusterStatusCollector", ScopeKey{Namespace: "other", Name: "cluster"})
		if ok {
			t.Error("expected not to find result with wrong scope key")
		}
	})

	t.Run("Get PerPod result", func(t *testing.T) {
		result, ok := v.Get("OSInfoCollector", podKey)
		if !ok {
			t.Fatal("expected to find PerPod result")
		}
		if result.Message != "OS info collected" {
			t.Errorf("unexpected message: %q", result.Message)
		}
	})

	t.Run("Get PerPod result with wrong key", func(t *testing.T) {
		_, ok := v.Get("OSInfoCollector", ScopeKey{Namespace: "scylla", Name: "other-pod"})
		if ok {
			t.Error("expected not to find result with wrong scope key")
		}
	})

	t.Run("Get nonexistent collector", func(t *testing.T) {
		_, ok := v.Get("NonexistentCollector", podKey)
		if ok {
			t.Error("expected not to find nonexistent collector")
		}
	})
}

func TestVitalsStoreMultiplePerScope(t *testing.T) {
	v := NewVitals()

	pod1 := ScopeKey{Namespace: "scylla", Name: "pod-0"}
	pod2 := ScopeKey{Namespace: "scylla", Name: "pod-1"}
	pod3 := ScopeKey{Namespace: "other", Name: "pod-0"}

	v.Store("OSInfoCollector", PerPod, pod1, &CollectorResult{Status: CollectorPassed, Message: "pod-0"})
	v.Store("OSInfoCollector", PerPod, pod2, &CollectorResult{Status: CollectorPassed, Message: "pod-1"})
	v.Store("OSInfoCollector", PerPod, pod3, &CollectorResult{Status: CollectorFailed, Message: "other/pod-0 failed"})

	// Each pod key should return its own result.
	r1, ok := v.Get("OSInfoCollector", pod1)
	if !ok || r1.Message != "pod-0" {
		t.Errorf("unexpected result for pod1: ok=%v, message=%q", ok, r1.Message)
	}
	r2, ok := v.Get("OSInfoCollector", pod2)
	if !ok || r2.Message != "pod-1" {
		t.Errorf("unexpected result for pod2: ok=%v, message=%q", ok, r2.Message)
	}
	r3, ok := v.Get("OSInfoCollector", pod3)
	if !ok || r3.Message != "other/pod-0 failed" {
		t.Errorf("unexpected result for pod3: ok=%v, message=%q", ok, r3.Message)
	}
}

func TestVitalsPodKeys(t *testing.T) {
	v := NewVitals()

	// Add pods in non-sorted order.
	v.Store("OSInfoCollector", PerPod, ScopeKey{Namespace: "scylla", Name: "pod-2"}, &CollectorResult{})
	v.Store("OSInfoCollector", PerPod, ScopeKey{Namespace: "other", Name: "pod-0"}, &CollectorResult{})
	v.Store("OSInfoCollector", PerPod, ScopeKey{Namespace: "scylla", Name: "pod-0"}, &CollectorResult{})
	v.Store("OSInfoCollector", PerPod, ScopeKey{Namespace: "scylla", Name: "pod-1"}, &CollectorResult{})

	keys := v.PodKeys()
	if len(keys) != 4 {
		t.Fatalf("expected 4 pod keys, got %d", len(keys))
	}

	expected := []ScopeKey{
		{Namespace: "other", Name: "pod-0"},
		{Namespace: "scylla", Name: "pod-0"},
		{Namespace: "scylla", Name: "pod-1"},
		{Namespace: "scylla", Name: "pod-2"},
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("PodKeys()[%d] = %v, want %v", i, k, expected[i])
		}
	}
}

func TestVitalsClusterKeys(t *testing.T) {
	v := NewVitals()

	v.Store("ScyllaClusterStatusCollector", PerScyllaCluster, ScopeKey{Namespace: "scylla", Name: "cluster-b"}, &CollectorResult{})
	v.Store("ScyllaClusterStatusCollector", PerScyllaCluster, ScopeKey{Namespace: "other", Name: "cluster-a"}, &CollectorResult{})
	v.Store("ScyllaClusterStatusCollector", PerScyllaCluster, ScopeKey{Namespace: "scylla", Name: "cluster-a"}, &CollectorResult{})

	keys := v.ClusterKeys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 cluster keys, got %d", len(keys))
	}

	expected := []ScopeKey{
		{Namespace: "other", Name: "cluster-a"},
		{Namespace: "scylla", Name: "cluster-a"},
		{Namespace: "scylla", Name: "cluster-b"},
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("ClusterKeys()[%d] = %v, want %v", i, k, expected[i])
		}
	}
}

func TestVitalsEmptyKeys(t *testing.T) {
	v := NewVitals()

	podKeys := v.PodKeys()
	if len(podKeys) != 0 {
		t.Errorf("expected 0 pod keys, got %d", len(podKeys))
	}

	clusterKeys := v.ClusterKeys()
	if len(clusterKeys) != 0 {
		t.Errorf("expected 0 cluster keys, got %d", len(clusterKeys))
	}
}
