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
	// SchemaVersionsCollectorID is the unique identifier for the SchemaVersionsCollector.
	SchemaVersionsCollectorID engine.CollectorID = "SchemaVersionsCollector"
)

// SchemaVersionsResult holds the parsed schema versions data.
type SchemaVersionsResult struct {
	Versions []SchemaVersionEntry `json:"versions"`
}

// SchemaVersionEntry holds a single schema version and the hosts reporting it.
type SchemaVersionEntry struct {
	SchemaVersion string   `json:"schema_version"` // UUID
	Hosts         []string `json:"hosts"`          // IP addresses
}

// GetSchemaVersionsResult is the typed accessor for SchemaVersionsCollector results.
func GetSchemaVersionsResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*SchemaVersionsResult, error) {
	return engine.GetResult[SchemaVersionsResult](vitals, SchemaVersionsCollectorID, podKey)
}

// ReadSchemaVersionsJSON reads the raw schema-versions.json artifact.
func ReadSchemaVersionsJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SchemaVersionsCollectorID, podKey, "schema-versions.json")
}

// schemaVersionsCollector collects schema version information from the Scylla REST API.
type schemaVersionsCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*schemaVersionsCollector)(nil)

// NewSchemaVersionsCollector creates a new SchemaVersionsCollector.
func NewSchemaVersionsCollector() engine.PerScyllaNodeCollector {
	return &schemaVersionsCollector{CollectorBase: engine.NewCollectorBase(SchemaVersionsCollectorID, "Schema versions", "Queries the Scylla REST API for the schema version reported by each node, used for schema agreement analysis.", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to curl the Scylla REST API at localhost:10000)
func (c *schemaVersionsCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *schemaVersionsCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params,
		[]string{"curl", "-s", "http://localhost:10000/storage_proxy/schema_versions"})
	if err != nil {
		return nil, fmt.Errorf("querying schema versions: %w", err)
	}

	raw := strings.TrimSpace(stdout)
	result, err := parseSchemaVersions(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing schema versions response: %w", err)
	}

	// Write artifact.
	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "schema-versions.json", []byte(raw), "Raw Scylla REST API schema versions response", &artifacts)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("%d schema version(s)", len(result.Versions)),
		Artifacts: artifacts,
	}, nil
}

// parseSchemaVersions parses the Scylla REST API schema versions response.
// The response is a JSON array of objects: [{"key": "<uuid>", "value": ["host1", "host2"]}]
func parseSchemaVersions(raw string) (*SchemaVersionsResult, error) {
	if raw == "" {
		return &SchemaVersionsResult{}, nil
	}

	var entries []struct {
		Key   string   `json:"key"`
		Value []string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("unmarshaling schema versions JSON: %w", err)
	}

	result := &SchemaVersionsResult{
		Versions: make([]SchemaVersionEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		result.Versions = append(result.Versions, SchemaVersionEntry{
			SchemaVersion: entry.Key,
			Hosts:         entry.Value,
		})
	}

	return result, nil
}
