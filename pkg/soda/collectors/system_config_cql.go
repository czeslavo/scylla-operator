package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// SystemConfigCollectorID is the unique identifier for the SystemConfigCollector.
	SystemConfigCollectorID engine.CollectorID = "SystemConfigCollector"
)

// SystemConfigEntry holds a single row from system.config.
type SystemConfigEntry struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Type   string `json:"type"`
	Value  string `json:"value"`
}

// SystemConfigResult holds all rows from the system.config table.
type SystemConfigResult struct {
	Entries []SystemConfigEntry `json:"entries"`
}

// GetSystemConfigResult is the typed accessor for SystemConfigCollector results.
func GetSystemConfigResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*SystemConfigResult, error) {
	return engine.GetResult[SystemConfigResult](vitals, SystemConfigCollectorID, podKey)
}

// ReadSystemConfigJSON reads the system-config.json artifact.
func ReadSystemConfigJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SystemConfigCollectorID, podKey, "system-config.json")
}

// systemConfigCollector collects system.config data via cqlsh.
type systemConfigCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*systemConfigCollector)(nil)

// NewSystemConfigCollector creates a new SystemConfigCollector.
func NewSystemConfigCollector() engine.PerScyllaNodeCollector {
	return &systemConfigCollector{CollectorBase: engine.NewCollectorBase(SystemConfigCollectorID, "System config", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to run cqlsh inside the scylla container)
func (c *systemConfigCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *systemConfigCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params,
		[]string{"cqlsh", "127.0.0.1", "9042", "--no-color", "-e",
			"SELECT name, source, type, value FROM system.config"})
	if err != nil {
		return nil, fmt.Errorf("querying system.config: %w", err)
	}

	raw := strings.TrimSpace(stdout)
	result, err := parseSystemConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing system.config output: %w", err)
	}

	// Write artifact.
	var artifacts []engine.Artifact
	if artifactBytes, jerr := cqlshRowsToJSON(systemConfigToMaps(result.Entries)); jerr == nil {
		writeArtifact(params.ArtifactWriter, "system-config.json", artifactBytes, "system.config rows (effective in-memory Scylla configuration)", &artifacts)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("%d config entries", len(result.Entries)),
		Artifacts: artifacts,
	}, nil
}

// parseSystemConfig parses cqlsh text output for system.config.
func parseSystemConfig(output string) (*SystemConfigResult, error) {
	rows, err := parseCQLSHTable(output)
	if err != nil {
		return nil, err
	}
	entries := make([]SystemConfigEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, SystemConfigEntry{
			Name:   r["name"],
			Source: r["source"],
			Type:   r["type"],
			Value:  r["value"],
		})
	}
	return &SystemConfigResult{Entries: entries}, nil
}

// systemConfigToMaps converts []SystemConfigEntry to []map[string]string for JSON serialization.
func systemConfigToMaps(entries []SystemConfigEntry) []map[string]string {
	result := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		result = append(result, map[string]string{
			"name":   e.Name,
			"source": e.Source,
			"type":   e.Type,
			"value":  e.Value,
		})
	}
	return result
}
