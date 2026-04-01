package analyzers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// TopologyHealthAnalyzerID is the unique identifier for the TopologyHealthAnalyzer.
	TopologyHealthAnalyzerID engine.AnalyzerID = "TopologyHealthAnalyzer"
)

// topologyHealthAnalyzer checks that all nodes in system.topology are in a
// healthy state (node_state == "normal" and upgrade_state == "done").
type topologyHealthAnalyzer struct {
	engine.AnalyzerBase
}

var _ engine.PerScyllaClusterAnalyzer = (*topologyHealthAnalyzer)(nil)

// NewTopologyHealthAnalyzer creates a new TopologyHealthAnalyzer.
func NewTopologyHealthAnalyzer() engine.PerScyllaClusterAnalyzer {
	return &topologyHealthAnalyzer{
		AnalyzerBase: engine.NewAnalyzerBase(
			TopologyHealthAnalyzerID,
			"Topology health check",
			engine.AnalyzerPerScyllaCluster,
			[]engine.CollectorID{collectors.SystemTopologyCollectorID},
		),
	}
}

func (a *topologyHealthAnalyzer) AnalyzePerScyllaCluster(params engine.PerScyllaClusterAnalyzerParams) *engine.AnalyzerResult {
	// system.topology is a cluster-wide table; every pod sees the same rows.
	// Use the first pod whose SystemTopologyCollector succeeded.
	podKeys := params.Vitals.ScyllaNodeKeys()
	if len(podKeys) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No pod topology data available",
		}
	}

	var topoResult *collectors.SystemTopologyResult
	for _, podKey := range podKeys {
		r, err := collectors.GetSystemTopologyResult(params.Vitals, podKey)
		if err == nil {
			topoResult = r
			break
		}
	}

	if topoResult == nil {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No topology info available from any pod",
		}
	}

	if len(topoResult.Nodes) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "system.topology returned no nodes",
		}
	}

	var nonNormal []string
	var upgrading []string

	for _, node := range topoResult.Nodes {
		if node.NodeState != "normal" {
			nonNormal = append(nonNormal, fmt.Sprintf("%s (state=%s)", node.HostID, node.NodeState))
		}
		if node.UpgradeState != "done" {
			upgrading = append(upgrading, fmt.Sprintf("%s (upgrade_state=%s)", node.HostID, node.UpgradeState))
		}
	}

	sort.Strings(nonNormal)
	sort.Strings(upgrading)

	if len(nonNormal) == 0 && len(upgrading) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerPassed,
			Message: fmt.Sprintf("All %d nodes are normal and upgrade is complete", len(topoResult.Nodes)),
		}
	}

	var issues []string
	if len(nonNormal) > 0 {
		issues = append(issues, fmt.Sprintf("non-normal nodes: %s", strings.Join(nonNormal, "; ")))
	}
	if len(upgrading) > 0 {
		issues = append(issues, fmt.Sprintf("nodes still upgrading: %s", strings.Join(upgrading, "; ")))
	}

	return &engine.AnalyzerResult{
		Status:  engine.AnalyzerWarning,
		Message: strings.Join(issues, " | "),
	}
}
