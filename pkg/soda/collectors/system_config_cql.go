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
	result, ok := vitals.Get(SystemConfigCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("SystemConfigCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("SystemConfigCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*SystemConfigResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for SystemConfigCollector", result.Data)
	}
	return typed, nil
}

// ReadSystemConfigJSON reads the system-config.json artifact.
func ReadSystemConfigJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SystemConfigCollectorID, podKey, "system-config.json")
}

// systemConfigCollector collects system.config data via cqlsh.
type systemConfigCollector struct{}

var _ engine.Collector = (*systemConfigCollector)(nil)

// NewSystemConfigCollector creates a new SystemConfigCollector.
func NewSystemConfigCollector() engine.Collector {
	return &systemConfigCollector{}
}

func (c *systemConfigCollector) ID() engine.CollectorID          { return SystemConfigCollectorID }
func (c *systemConfigCollector) Name() string                    { return "System config" }
func (c *systemConfigCollector) Scope() engine.CollectorScope    { return engine.PerScyllaNode }
func (c *systemConfigCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *systemConfigCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.ScyllaNode == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	stdout, _, err := params.PodExecutor.Execute(ctx, params.ScyllaNode.Namespace, params.ScyllaNode.Name, scyllaContainerName,
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
	if params.ArtifactWriter != nil {
		artifactBytes, jerr := cqlshRowsToJSON(systemConfigToMaps(result.Entries))
		if jerr == nil {
			if relPath, werr := params.ArtifactWriter.WriteArtifact("system-config.json", artifactBytes); werr == nil {
				artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "system.config rows (effective in-memory Scylla configuration)"})
			}
		}
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
