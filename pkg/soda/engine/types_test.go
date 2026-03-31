package engine

import (
	"encoding/json"
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
			expected: "",
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

func TestScopeKeyIsEmpty(t *testing.T) {
	empty := ScopeKey{}
	if !empty.IsEmpty() {
		t.Error("ScopeKey{} should be empty")
	}
	if (ScopeKey{Namespace: "ns", Name: "name"}).IsEmpty() {
		t.Error("ScopeKey{ns, name} should not be empty")
	}
	if (ScopeKey{Name: "name"}).IsEmpty() {
		t.Error("ScopeKey{name only} should not be empty")
	}
	if (ScopeKey{Namespace: "ns"}).IsEmpty() {
		t.Error("ScopeKey{namespace only} should not be empty")
	}
}

func TestScopeKeyMarshalTextRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		key  ScopeKey
	}{
		{name: "normal key", key: ScopeKey{Namespace: "scylla", Name: "my-cluster"}},
		{name: "empty key", key: ScopeKey{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, err := tt.key.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error: %v", err)
			}

			var got ScopeKey
			if err := got.UnmarshalText(text); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(text), err)
			}

			if got != tt.key {
				t.Errorf("round-trip: got %+v, want %+v", got, tt.key)
			}
		})
	}
}

func TestScopeKeyUnmarshalText_InvalidFormat(t *testing.T) {
	var k ScopeKey
	err := k.UnmarshalText([]byte("no-slash-here"))
	if err == nil {
		t.Error("expected error for text without '/'")
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

func TestToSerializableResult_WithData(t *testing.T) {
	type testData struct {
		Version string `json:"version"`
		Count   int    `json:"count"`
	}
	r := &CollectorResult{
		Status:  CollectorPassed,
		Message: "collected",
		Data:    &testData{Version: "6.0.0", Count: 3},
		Artifacts: []Artifact{
			{RelativePath: "nodes.yaml", Description: "node list"},
		},
	}

	sr, err := toSerializableResult(r)
	if err != nil {
		t.Fatalf("toSerializableResult() error: %v", err)
	}

	if sr.Status != CollectorPassed {
		t.Errorf("Status = %v, want %v", sr.Status, CollectorPassed)
	}
	if sr.Message != "collected" {
		t.Errorf("Message = %q, want %q", sr.Message, "collected")
	}
	if len(sr.Artifacts) != 1 {
		t.Fatalf("Artifacts length = %d, want 1", len(sr.Artifacts))
	}
	if sr.Artifacts[0].RelativePath != "nodes.yaml" {
		t.Errorf("Artifact path = %q, want %q", sr.Artifacts[0].RelativePath, "nodes.yaml")
	}

	// Verify Data was marshaled correctly.
	if sr.Data == nil {
		t.Fatal("Data is nil, expected json.RawMessage")
	}
	expectedJSON := `{"version":"6.0.0","count":3}`
	if string(sr.Data) != expectedJSON {
		t.Errorf("Data = %s, want %s", string(sr.Data), expectedJSON)
	}
}

func TestToSerializableResult_NilData(t *testing.T) {
	r := &CollectorResult{
		Status:  CollectorFailed,
		Message: "failed to collect",
	}

	sr, err := toSerializableResult(r)
	if err != nil {
		t.Fatalf("toSerializableResult() error: %v", err)
	}

	if sr.Status != CollectorFailed {
		t.Errorf("Status = %v, want %v", sr.Status, CollectorFailed)
	}
	if sr.Data != nil {
		t.Errorf("Data = %v, want nil", sr.Data)
	}
}

func TestToSerializableResult_NilArtifacts(t *testing.T) {
	r := &CollectorResult{
		Status:  CollectorPassed,
		Message: "ok",
	}

	sr, err := toSerializableResult(r)
	if err != nil {
		t.Fatalf("toSerializableResult() error: %v", err)
	}

	// Nil artifacts in the original should remain nil in serializable form.
	if sr.Artifacts != nil {
		t.Errorf("Artifacts = %v, want nil", sr.Artifacts)
	}
}

func TestVitalsToSerializable_AllScopes(t *testing.T) {
	v := NewVitals()

	type nodeData struct {
		NodeCount int `json:"node_count"`
	}
	type clusterData struct {
		Status string `json:"status"`
	}
	type podData struct {
		OS string `json:"os"`
	}

	v.Store("node-resources", ClusterWide, ScopeKey{}, &CollectorResult{
		Status:  CollectorPassed,
		Message: "3 nodes",
		Data:    &nodeData{NodeCount: 3},
		Artifacts: []Artifact{
			{RelativePath: "nodes.yaml", Description: "node list"},
		},
	})

	clusterKey := ScopeKey{Namespace: "scylla", Name: "my-cluster"}
	v.Store("cluster-status", PerScyllaCluster, clusterKey, &CollectorResult{
		Status:  CollectorPassed,
		Message: "cluster OK",
		Data:    &clusterData{Status: "ready"},
	})

	podKey := ScopeKey{Namespace: "scylla", Name: "pod-0"}
	v.Store("os-info", PerPod, podKey, &CollectorResult{
		Status:  CollectorPassed,
		Message: "os collected",
		Data:    &podData{OS: "Ubuntu"},
	})

	sv, err := v.ToSerializable()
	if err != nil {
		t.Fatalf("ToSerializable() error: %v", err)
	}

	// Check ClusterWide.
	if len(sv.ClusterWide) != 1 {
		t.Fatalf("ClusterWide length = %d, want 1", len(sv.ClusterWide))
	}
	cwResult := sv.ClusterWide["node-resources"]
	if cwResult == nil {
		t.Fatal("ClusterWide[node-resources] is nil")
	}
	if cwResult.Message != "3 nodes" {
		t.Errorf("ClusterWide message = %q, want %q", cwResult.Message, "3 nodes")
	}
	if string(cwResult.Data) != `{"node_count":3}` {
		t.Errorf("ClusterWide data = %s, want %s", string(cwResult.Data), `{"node_count":3}`)
	}

	// Check PerCluster.
	if len(sv.PerCluster) != 1 {
		t.Fatalf("PerCluster length = %d, want 1", len(sv.PerCluster))
	}
	pcResult := sv.PerCluster[clusterKey]["cluster-status"]
	if pcResult == nil {
		t.Fatal("PerCluster[cluster-status] is nil")
	}
	if string(pcResult.Data) != `{"status":"ready"}` {
		t.Errorf("PerCluster data = %s, want %s", string(pcResult.Data), `{"status":"ready"}`)
	}

	// Check PerPod.
	if len(sv.PerPod) != 1 {
		t.Fatalf("PerPod length = %d, want 1", len(sv.PerPod))
	}
	ppResult := sv.PerPod[podKey]["os-info"]
	if ppResult == nil {
		t.Fatal("PerPod[os-info] is nil")
	}
	if string(ppResult.Data) != `{"os":"Ubuntu"}` {
		t.Errorf("PerPod data = %s, want %s", string(ppResult.Data), `{"os":"Ubuntu"}`)
	}
}

func TestVitalsToSerializable_Empty(t *testing.T) {
	v := NewVitals()

	sv, err := v.ToSerializable()
	if err != nil {
		t.Fatalf("ToSerializable() error: %v", err)
	}

	if len(sv.ClusterWide) != 0 {
		t.Errorf("ClusterWide length = %d, want 0", len(sv.ClusterWide))
	}
	if len(sv.PerCluster) != 0 {
		t.Errorf("PerCluster length = %d, want 0", len(sv.PerCluster))
	}
	if len(sv.PerPod) != 0 {
		t.Errorf("PerPod length = %d, want 0", len(sv.PerPod))
	}
}

func TestVitalsToSerializable_JSONRoundTrip(t *testing.T) {
	v := NewVitals()

	type testData struct {
		Value string `json:"value"`
	}

	v.Store("collector-a", ClusterWide, ScopeKey{}, &CollectorResult{
		Status:  CollectorPassed,
		Message: "ok",
		Data:    &testData{Value: "hello"},
		Artifacts: []Artifact{
			{RelativePath: "output.log", Description: "log file"},
		},
	})

	podKey := ScopeKey{Namespace: "ns", Name: "pod-1"}
	v.Store("collector-b", PerPod, podKey, &CollectorResult{
		Status:  CollectorFailed,
		Message: "error occurred",
	})

	// Serialize to JSON.
	sv, err := v.ToSerializable()
	if err != nil {
		t.Fatalf("ToSerializable() error: %v", err)
	}

	data, err := json.Marshal(sv)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	// Deserialize back.
	var sv2 SerializableVitals
	if err := json.Unmarshal(data, &sv2); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	// Verify ClusterWide round-tripped correctly.
	cwResult := sv2.ClusterWide["collector-a"]
	if cwResult == nil {
		t.Fatal("round-tripped ClusterWide[collector-a] is nil")
	}
	if cwResult.Status != CollectorPassed {
		t.Errorf("Status = %v, want %v", cwResult.Status, CollectorPassed)
	}
	if cwResult.Message != "ok" {
		t.Errorf("Message = %q, want %q", cwResult.Message, "ok")
	}
	if string(cwResult.Data) != `{"value":"hello"}` {
		t.Errorf("Data = %s, want %s", string(cwResult.Data), `{"value":"hello"}`)
	}
	if len(cwResult.Artifacts) != 1 || cwResult.Artifacts[0].RelativePath != "output.log" {
		t.Errorf("Artifacts did not round-trip correctly: %+v", cwResult.Artifacts)
	}

	// Verify PerPod round-tripped correctly.
	ppResult := sv2.PerPod[podKey]["collector-b"]
	if ppResult == nil {
		t.Fatal("round-tripped PerPod[collector-b] is nil")
	}
	if ppResult.Status != CollectorFailed {
		t.Errorf("Status = %v, want %v", ppResult.Status, CollectorFailed)
	}
	if ppResult.Message != "error occurred" {
		t.Errorf("Message = %q, want %q", ppResult.Message, "error occurred")
	}
	if ppResult.Data != nil {
		t.Errorf("Data = %v, want nil", ppResult.Data)
	}
}
