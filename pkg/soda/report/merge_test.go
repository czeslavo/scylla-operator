package report

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
)

func TestMerge(t *testing.T) {
	sd := &vitals.ClusterReport{
		Failures:        vitals.Findings{"AuthenticationAnalyzer": {"Authentication is disabled": {"n0", "n1"}}},
		Warnings:        vitals.Findings{"SwapAnalyzer": {"not enough swap": {"n0"}}},
		Inconsistencies: map[string]json.RawMessage{"NICsCollector": json.RawMessage(`[["n0","n1",{}]]`)},
	}
	nodes := []NodeInput{
		{
			Summary:      vitals.NodeSummary{Name: "n1", Collection: vitals.NodeCollection{State: vitals.NodeCollectionSucceeded}},
			SODAAnalysis: vitals.Analysis{"ScyllaDBPodAnalyzer": {Status: vitals.AnalyzerWarning, Message: "Burstable"}},
		},
		{
			Summary:      vitals.NodeSummary{Name: "n0", Collection: vitals.NodeCollection{State: vitals.NodeCollectionSucceeded}},
			SODAAnalysis: vitals.Analysis{"ScyllaDBPodAnalyzer": {Status: vitals.AnalyzerWarning, Message: "Burstable"}},
		},
		{
			Summary: vitals.NodeSummary{Name: "n2", Collection: vitals.NodeCollection{State: vitals.NodeCollectionFailed, Reason: "exec failed"}},
		},
	}

	got := Merge(sd, nodes, []string{"scylla-doctor/vitals/n0.vitals.json"}, vitals.SODAExtension{})

	wantFailures := vitals.Findings{
		"AuthenticationAnalyzer": {"Authentication is disabled": {"n0", "n1"}},
		CollectionAnalyzer:       {"Scylla Doctor could not collect vitals: exec failed": {"n2"}},
	}
	if diff := cmp.Diff(wantFailures, got.Failures); diff != "" {
		t.Errorf("failures (-want +got):\n%s", diff)
	}
	wantWarnings := vitals.Findings{
		"SwapAnalyzer":        {"not enough swap": {"n0"}},
		"ScyllaDBPodAnalyzer": {"Burstable": {"n0", "n1"}},
	}
	if diff := cmp.Diff(wantWarnings, got.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
	wantSources := map[string]string{
		"AuthenticationAnalyzer": vitals.SourceScyllaDoctor,
		"SwapAnalyzer":           vitals.SourceScyllaDoctor,
		"NICsCollector":          vitals.SourceScyllaDoctor,
		"ScyllaDBPodAnalyzer":    vitals.SourceSODA,
		CollectionAnalyzer:       vitals.SourceSODA,
	}
	if diff := cmp.Diff(wantSources, got.SODA.Sources); diff != "" {
		t.Errorf("sources (-want +got):\n%s", diff)
	}
	if got.SODA.FormatVersion != FormatVersion || len(got.SODA.Nodes) != 3 || got.SODA.Nodes[0].Name != "n0" {
		t.Errorf("unexpected extension: %+v", got.SODA)
	}

	// Readers of Scylla Doctor Cluster's report keep working: same keys, plus "soda".
	b, _ := json.Marshal(got)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(b, &keys)
	for _, k := range []string{"vitals", "failures", "warnings", "inconsistencies", "soda"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}
