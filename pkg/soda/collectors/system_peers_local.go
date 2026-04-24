package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// SystemPeersLocalCollectorID is the unique identifier for the SystemPeersLocalCollector.
	SystemPeersLocalCollectorID engine.CollectorID = "SystemPeersLocalCollector"
)

// SystemLocalRow holds a single row from system.local.
type SystemLocalRow struct {
	ClusterName    string `json:"cluster_name"`
	DataCenter     string `json:"data_center"`
	Rack           string `json:"rack"`
	HostID         string `json:"host_id"`
	SchemaVersion  string `json:"schema_version"`
	ReleaseVersion string `json:"release_version"`
}

// SystemPeerRow holds a single row from system.peers.
type SystemPeerRow struct {
	Peer          string `json:"peer"`
	DataCenter    string `json:"data_center"`
	Rack          string `json:"rack"`
	HostID        string `json:"host_id"`
	SchemaVersion string `json:"schema_version"`
}

// SystemPeersLocalResult holds the combined results from system.local and system.peers.
type SystemPeersLocalResult struct {
	Local *SystemLocalRow `json:"local"`
	Peers []SystemPeerRow `json:"peers"`
}

// GetSystemPeersLocalResult is the typed accessor for SystemPeersLocalCollector results.
func GetSystemPeersLocalResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*SystemPeersLocalResult, error) {
	return engine.GetResult[SystemPeersLocalResult](vitals, SystemPeersLocalCollectorID, podKey)
}

// ReadSystemLocalJSON reads the system-local.json artifact.
func ReadSystemLocalJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SystemPeersLocalCollectorID, podKey, "system-local.json")
}

// ReadSystemPeersJSON reads the system-peers.json artifact.
func ReadSystemPeersJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SystemPeersLocalCollectorID, podKey, "system-peers.json")
}

// systemPeersLocalCollector collects system.local and system.peers from Scylla pods via cqlsh.
type systemPeersLocalCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*systemPeersLocalCollector)(nil)

// NewSystemPeersLocalCollector creates a new SystemPeersLocalCollector.
func NewSystemPeersLocalCollector() engine.PerScyllaNodeCollector {
	return &systemPeersLocalCollector{CollectorBase: engine.NewCollectorBase(SystemPeersLocalCollectorID, "System local and peers", "Queries system.local and system.peers CQL tables via cqlsh to capture cluster membership and topology from each node's perspective.", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to run cqlsh inside the scylla container)
func (c *systemPeersLocalCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *systemPeersLocalCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	// Query system.local.
	localStdout, err := ExecInScyllaPod(ctx, params,
		[]string{"cqlsh", "127.0.0.1", "9042", "--no-color", "-e",
			"SELECT cluster_name, data_center, rack, host_id, schema_version, release_version FROM system.local"})
	if err != nil {
		return nil, fmt.Errorf("querying system.local: %w", err)
	}

	localRow, err := parseSystemLocal(strings.TrimSpace(localStdout))
	if err != nil {
		return nil, fmt.Errorf("parsing system.local output: %w", err)
	}

	// Query system.peers.
	peersStdout, err := ExecInScyllaPod(ctx, params,
		[]string{"cqlsh", "127.0.0.1", "9042", "--no-color", "-e",
			"SELECT peer, data_center, rack, host_id, schema_version FROM system.peers"})
	if err != nil {
		return nil, fmt.Errorf("querying system.peers: %w", err)
	}

	peerRows, err := parseSystemPeers(strings.TrimSpace(peersStdout))
	if err != nil {
		return nil, fmt.Errorf("parsing system.peers output: %w", err)
	}

	result := &SystemPeersLocalResult{
		Local: localRow,
		Peers: peerRows,
	}

	// Write artifacts.
	var artifacts []engine.Artifact
	if localJSON, jerr := json.Marshal(localRow); jerr == nil {
		writeArtifact(params.ArtifactWriter, "system-local.json", localJSON, "system.local row", &artifacts)
	}
	if peersJSON, jerr := json.Marshal(peerRows); jerr == nil {
		writeArtifact(params.ArtifactWriter, "system-peers.json", peersJSON, "system.peers rows", &artifacts)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("cluster=%s dc=%s rack=%s peers=%d", localRow.ClusterName, localRow.DataCenter, localRow.Rack, len(peerRows)),
		Artifacts: artifacts,
	}, nil
}

// parseSystemLocal parses cqlsh text output for system.local into a SystemLocalRow.
func parseSystemLocal(output string) (*SystemLocalRow, error) {
	rows, err := parseCQLSHTable(output)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("system.local returned no rows")
	}
	r := rows[0]
	return &SystemLocalRow{
		ClusterName:    r["cluster_name"],
		DataCenter:     r["data_center"],
		Rack:           r["rack"],
		HostID:         r["host_id"],
		SchemaVersion:  r["schema_version"],
		ReleaseVersion: r["release_version"],
	}, nil
}

// parseSystemPeers parses cqlsh text output for system.peers into a slice of SystemPeerRow.
func parseSystemPeers(output string) ([]SystemPeerRow, error) {
	rows, err := parseCQLSHTable(output)
	if err != nil {
		return nil, err
	}
	peers := make([]SystemPeerRow, 0, len(rows))
	for _, r := range rows {
		peers = append(peers, SystemPeerRow{
			Peer:          r["peer"],
			DataCenter:    r["data_center"],
			Rack:          r["rack"],
			HostID:        r["host_id"],
			SchemaVersion: r["schema_version"],
		})
	}
	return peers, nil
}
