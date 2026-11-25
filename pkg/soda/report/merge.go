// Package report builds SODA's combined report: Scylla Doctor Cluster's report plus SODA's findings, in Scylla
// Doctor Cluster's JSON format with an optional `soda` key, and an HTML rendering of it.
package report

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/scylladb/scylla-operator/pkg/soda/k8s"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
)

const (
	FormatVersion = "soda.scylladb.com/report/v1alpha1"
	// CollectionAnalyzer reports nodes whose Scylla Doctor collection failed or is incomplete. Scylla Doctor
	// Cluster can't, because those nodes have no usable vitals.
	CollectionAnalyzer = "SODACollectionAnalyzer"
)

type NodeInput struct {
	Summary      vitals.NodeSummary
	SODAAnalysis vitals.Analysis
}

// ClusterScope is the "node" name cluster-scoped findings are attributed to, so they keep the report's
// {message: [nodes]} shape.
const ClusterScope = k8s.ClusterScope

// Merge returns the combined report. sdReport may be nil when Scylla Doctor Cluster didn't run; clusterAnalysis
// holds SODA's cluster-scoped results; vitalsPaths are the archive-relative paths of every vitals file used.
func Merge(sdReport *vitals.ClusterReport, nodes []NodeInput, clusterAnalysis vitals.Analysis, vitalsPaths []string, ext vitals.SODAExtension) *vitals.ClusterReport {
	out := &vitals.ClusterReport{
		Vitals:          append([]string{}, vitalsPaths...),
		Failures:        vitals.Findings{},
		Warnings:        vitals.Findings{},
		Inconsistencies: map[string]json.RawMessage{},
	}
	sources := map[string]string{}
	if sdReport != nil {
		for a, msgs := range sdReport.Failures {
			for m, ns := range msgs {
				for _, n := range ns {
					out.Failures.AddFinding(a, m, n)
				}
			}
			sources[a] = vitals.SourceScyllaDoctor
		}
		for a, msgs := range sdReport.Warnings {
			for m, ns := range msgs {
				for _, n := range ns {
					out.Warnings.AddFinding(a, m, n)
				}
			}
			sources[a] = vitals.SourceScyllaDoctor
		}
		for c, raw := range sdReport.Inconsistencies {
			out.Inconsistencies[c] = raw
			sources[c] = vitals.SourceScyllaDoctor
		}
	}

	for _, n := range nodes {
		node := n.Summary.Name
		switch n.Summary.Collection.State {
		case vitals.NodeCollectionFailed:
			out.Failures.AddFinding(CollectionAnalyzer, "Scylla Doctor could not collect vitals: "+n.Summary.Collection.Reason, node)
			sources[CollectionAnalyzer] = vitals.SourceSODA
		case vitals.NodeCollectionDegraded:
			out.Failures.AddFinding(CollectionAnalyzer, "Scylla Doctor's vitals are incomplete: "+n.Summary.Collection.Reason, node)
			sources[CollectionAnalyzer] = vitals.SourceSODA
		}
		for a, r := range n.SODAAnalysis {
			switch r.Status {
			case vitals.AnalyzerFailed:
				out.Failures.AddFinding(a, r.Message, node)
				sources[a] = vitals.SourceSODA
			case vitals.AnalyzerWarning:
				out.Warnings.AddFinding(a, r.Message, node)
				sources[a] = vitals.SourceSODA
			}
		}
	}

	for a, r := range clusterAnalysis {
		switch r.Status {
		case vitals.AnalyzerFailed:
			out.Failures.AddFinding(a, r.Message, ClusterScope)
			sources[a] = vitals.SourceSODA
		case vitals.AnalyzerWarning:
			out.Warnings.AddFinding(a, r.Message, ClusterScope)
			sources[a] = vitals.SourceSODA
		}
	}

	ext.FormatVersion = FormatVersion
	ext.Sources = sources
	ext.Nodes = make([]vitals.NodeSummary, 0, len(nodes))
	for _, n := range nodes {
		ext.Nodes = append(ext.Nodes, n.Summary)
	}
	sort.Slice(ext.Nodes, func(i, j int) bool { return ext.Nodes[i].Name < ext.Nodes[j].Name })
	out.SODA = &ext
	return out
}

// Totals counts findings the way a reader would: one per analyzer and message.
func Totals(r *vitals.ClusterReport) (failures, warnings, inconsistencies int) {
	for _, msgs := range r.Failures {
		failures += len(msgs)
	}
	for _, msgs := range r.Warnings {
		warnings += len(msgs)
	}
	return failures, warnings, len(r.Inconsistencies)
}

func (n NodeInput) String() string {
	return fmt.Sprintf("%s (%s)", n.Summary.Name, n.Summary.Collection.State)
}
