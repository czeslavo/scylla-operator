package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// SystemTopologyCollectorID is the unique identifier for the SystemTopologyCollector.
	SystemTopologyCollectorID engine.CollectorID = "SystemTopologyCollector"
)

// SystemTopologyRow holds a single row from system.topology.
type SystemTopologyRow struct {
	HostID         string `json:"host_id"`
	NodeState      string `json:"node_state"`
	Datacenter     string `json:"datacenter"`
	Rack           string `json:"rack"`
	ReleaseVersion string `json:"release_version"`
	ShardCount     string `json:"shard_count"`
	UpgradeState   string `json:"upgrade_state"`
}

// SystemTopologyResult holds rows from the system.topology table.
type SystemTopologyResult struct {
	Nodes []SystemTopologyRow `json:"nodes"`
}

// GetSystemTopologyResult is the typed accessor for SystemTopologyCollector results.
func GetSystemTopologyResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*SystemTopologyResult, error) {
	return engine.GetResult[SystemTopologyResult](vitals, SystemTopologyCollectorID, podKey)
}

// ReadSystemTopologyJSON reads the system-topology.json artifact.
func ReadSystemTopologyJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SystemTopologyCollectorID, podKey, "system-topology.json")
}

// systemTopologyCollector collects system.topology data via cqlsh.
type systemTopologyCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*systemTopologyCollector)(nil)

// NewSystemTopologyCollector creates a new SystemTopologyCollector.
func NewSystemTopologyCollector() engine.PerScyllaNodeCollector {
	return &systemTopologyCollector{CollectorBase: engine.NewCollectorBase(SystemTopologyCollectorID, "System topology", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to run cqlsh inside the scylla container)
func (c *systemTopologyCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *systemTopologyCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params,
		[]string{"cqlsh", "127.0.0.1", "9042", "--no-color", "-e",
			"SELECT host_id, node_state, datacenter, rack, release_version, shard_count, upgrade_state FROM system.topology"})
	if err != nil {
		return nil, fmt.Errorf("querying system.topology: %w", err)
	}

	raw := strings.TrimSpace(stdout)
	result, err := parseSystemTopology(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing system.topology output: %w", err)
	}

	// Write artifact.
	var artifacts []engine.Artifact
	if artifactBytes, jerr := cqlshRowsToJSON(systemTopologyToMaps(result.Nodes)); jerr == nil {
		writeArtifact(params.ArtifactWriter, "system-topology.json", artifactBytes, "system.topology rows", &artifacts)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("%d node(s) in topology", len(result.Nodes)),
		Artifacts: artifacts,
	}, nil
}

// parseSystemTopology parses cqlsh text output for system.topology.
func parseSystemTopology(output string) (*SystemTopologyResult, error) {
	rows, err := parseCQLSHTable(output)
	if err != nil {
		return nil, err
	}
	nodes := make([]SystemTopologyRow, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, SystemTopologyRow{
			HostID:         r["host_id"],
			NodeState:      r["node_state"],
			Datacenter:     r["datacenter"],
			Rack:           r["rack"],
			ReleaseVersion: r["release_version"],
			ShardCount:     r["shard_count"],
			UpgradeState:   r["upgrade_state"],
		})
	}
	return &SystemTopologyResult{Nodes: nodes}, nil
}

// systemTopologyToMaps converts []SystemTopologyRow to []map[string]string for JSON serialization.
func systemTopologyToMaps(nodes []SystemTopologyRow) []map[string]string {
	result := make([]map[string]string, 0, len(nodes))
	for _, n := range nodes {
		result = append(result, map[string]string{
			"host_id":         n.HostID,
			"node_state":      n.NodeState,
			"datacenter":      n.Datacenter,
			"rack":            n.Rack,
			"release_version": n.ReleaseVersion,
			"shard_count":     n.ShardCount,
			"upgrade_state":   n.UpgradeState,
		})
	}
	return result
}
