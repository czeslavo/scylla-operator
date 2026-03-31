package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
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
	result, ok := vitals.Get(SchemaVersionsCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("SchemaVersionsCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("SchemaVersionsCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*SchemaVersionsResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for SchemaVersionsCollector", result.Data)
	}
	return typed, nil
}

// ReadSchemaVersionsJSON reads the raw schema-versions.json artifact.
func ReadSchemaVersionsJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(SchemaVersionsCollectorID, podKey, "schema-versions.json")
}

// schemaVersionsCollector collects schema version information from the Scylla REST API.
type schemaVersionsCollector struct{}

// NewSchemaVersionsCollector creates a new SchemaVersionsCollector.
func NewSchemaVersionsCollector() engine.Collector {
	return &schemaVersionsCollector{}
}

func (c *schemaVersionsCollector) ID() engine.CollectorID          { return SchemaVersionsCollectorID }
func (c *schemaVersionsCollector) Name() string                    { return "Schema versions" }
func (c *schemaVersionsCollector) Scope() engine.CollectorScope    { return engine.PerPod }
func (c *schemaVersionsCollector) DependsOn() []engine.CollectorID { return nil }

func (c *schemaVersionsCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.Pod == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	stdout, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName,
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
	if params.ArtifactWriter != nil {
		if relPath, err := params.ArtifactWriter.WriteArtifact("schema-versions.json", []byte(raw)); err == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Raw Scylla REST API schema versions response"})
		}
	}

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
