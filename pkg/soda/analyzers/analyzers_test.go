package analyzers

import (
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/k8s"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
)

func podVitals(status vitals.CollectorStatus, data map[string]any) vitals.Vitals {
	return vitals.Vitals{k8s.PodCollectorName: {Status: status, Data: data}}
}

func TestPodAnalyzer(t *testing.T) {
	tt := []struct {
		name   string
		v      vitals.Vitals
		status vitals.AnalyzerStatus
		msg    string
	}{
		{
			name:   "ready, guaranteed, no restarts",
			v:      podVitals(vitals.CollectorPassed, map[string]any{"ready": true, "qosClass": "Guaranteed"}),
			status: vitals.AnalyzerPassed,
			msg:    "Pod is ready, Guaranteed and has not restarted",
		},
		{
			name:   "burstable",
			v:      podVitals(vitals.CollectorPassed, map[string]any{"ready": true, "qosClass": "Burstable"}),
			status: vitals.AnalyzerWarning,
			msg:    "QoS class is Burstable, not Guaranteed: ScyllaDB can't get dedicated CPUs",
		},
		{
			name: "scylla OOMKilled",
			v: podVitals(vitals.CollectorPassed, map[string]any{"ready": true, "qosClass": "Guaranteed", "containers": []any{
				map[string]any{"name": "scylla", "restartCount": 2, "lastTerminationReason": "OOMKilled"},
			}}),
			status: vitals.AnalyzerFailed,
			msg:    "the scylla container was OOMKilled; scylla restarted 2 times (last: OOMKilled)",
		},
		{
			name:   "collector failed",
			v:      podVitals(vitals.CollectorFailed, nil),
			status: vitals.AnalyzerFailed,
			msg:    "Required ScyllaDBPodCollector was not successful",
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := RunNode(tc.v)["ScyllaDBPodAnalyzer"]
			if got.Status != tc.status || got.Message != tc.msg {
				t.Errorf("got %s %q, want %s %q", got.Status, got.Message, tc.status, tc.msg)
			}
		})
	}
}

func TestRuntimeContractAnalyzer(t *testing.T) {
	v := vitals.Vitals{collectors.ScyllaDBRuntimeContractCollector: {Status: vitals.CollectorPassed, Data: map[string]any{
		"results": []any{
			map[string]any{"id": "cqlsh", "level": "required", "satisfied": true},
			map[string]any{"id": "rpm", "level": "recommended", "satisfied": false, "detail": "not found: rpm"},
		},
	}}}
	got := RunNode(v)["ScyllaDBRuntimeContractAnalyzer"]
	if got.Status != vitals.AnalyzerWarning || got.Message != "Recommended runtime missing from the ScyllaDB image: rpm (not found: rpm)" {
		t.Errorf("unexpected result: %s %q", got.Status, got.Message)
	}
}

func TestScyllaClusterAnalyzer(t *testing.T) {
	v := vitals.Vitals{k8s.ScyllaClusterCollectorName: {Status: vitals.CollectorPassed, Data: map[string]any{
		"generation": 3, "observedGeneration": 3,
		"conditions": []any{
			map[string]any{"type": "Available", "status": "True"},
			map[string]any{"type": "Degraded", "status": "False"},
			map[string]any{"type": "Progressing", "status": "False"},
		},
		"racks": map[string]any{"a": map[string]any{"members": 3, "readyMembers": 2}},
	}}}
	got := RunCluster(v)["ScyllaClusterAnalyzer"]
	if got.Status != vitals.AnalyzerWarning || got.Message != "rack a has 2 of 3 members ready" {
		t.Errorf("unexpected result: %s %q", got.Status, got.Message)
	}
}
