package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// ScyllaConfigCollectorID is the unique identifier for the ScyllaConfigCollector.
	ScyllaConfigCollectorID engine.CollectorID = "ScyllaConfigCollector"

	scyllaConfigPath = "/etc/scylla/scylla.yaml"
)

// ScyllaConfigResult holds the raw Scylla configuration file content.
type ScyllaConfigResult struct {
	RawYAML string `json:"raw_yaml"` // Raw content of /etc/scylla/scylla.yaml
}

// GetScyllaConfigResult is the typed accessor for ScyllaConfigCollector results.
func GetScyllaConfigResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*ScyllaConfigResult, error) {
	return engine.GetResult[ScyllaConfigResult](vitals, ScyllaConfigCollectorID, podKey)
}

// ReadScyllaConfigYAML reads the raw scylla.yaml artifact.
func ReadScyllaConfigYAML(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(ScyllaConfigCollectorID, podKey, "scylla.yaml")
}

// scyllaConfigCollector collects the Scylla configuration file from pods.
type scyllaConfigCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*scyllaConfigCollector)(nil)

// NewScyllaConfigCollector creates a new ScyllaConfigCollector.
func NewScyllaConfigCollector() engine.PerScyllaNodeCollector {
	return &scyllaConfigCollector{CollectorBase: engine.NewCollectorBase(ScyllaConfigCollectorID, "Scylla configuration", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to cat /etc/scylla/scylla.yaml)
func (c *scyllaConfigCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *scyllaConfigCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params, []string{"cat", scyllaConfigPath})
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", scyllaConfigPath, err)
	}

	raw := strings.TrimSpace(stdout)

	result := &ScyllaConfigResult{
		RawYAML: raw,
	}

	// Write artifact.
	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "scylla.yaml", []byte(raw), "Raw /etc/scylla/scylla.yaml content", &artifacts)

	// Count non-empty, non-comment lines as a rough measure of config size.
	lineCount := 0
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			lineCount++
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("collected %s (%d config lines)", scyllaConfigPath, lineCount),
		Artifacts: artifacts,
	}, nil
}
