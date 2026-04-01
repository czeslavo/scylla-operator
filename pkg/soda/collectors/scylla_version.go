package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// ScyllaVersionCollectorID is the unique identifier for the ScyllaVersionCollector.
	ScyllaVersionCollectorID engine.CollectorID = "ScyllaVersionCollector"
)

// ScyllaVersionResult holds the parsed Scylla version information.
type ScyllaVersionResult struct {
	Version string `json:"version"` // e.g. "2026.1.0" or "6.2.2"
	Build   string `json:"build"`   // Build identifier if present
	Raw     string `json:"raw"`     // Full raw output from scylla --version
}

// GetScyllaVersionResult is the typed accessor for ScyllaVersionCollector results.
func GetScyllaVersionResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*ScyllaVersionResult, error) {
	return engine.GetResult[ScyllaVersionResult](vitals, ScyllaVersionCollectorID, podKey)
}

// ReadScyllaVersionOutput reads the raw scylla-version.log artifact.
func ReadScyllaVersionOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(ScyllaVersionCollectorID, podKey, "scylla-version.log")
}

// scyllaVersionCollector collects the Scylla version from pods.
type scyllaVersionCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*scyllaVersionCollector)(nil)

// NewScyllaVersionCollector creates a new ScyllaVersionCollector.
func NewScyllaVersionCollector() engine.PerScyllaNodeCollector {
	return &scyllaVersionCollector{CollectorBase: engine.NewCollectorBase(ScyllaVersionCollectorID, "Scylla version", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to run scylla --version)
func (c *scyllaVersionCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *scyllaVersionCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params, []string{"scylla", "--version"})
	if err != nil {
		return nil, fmt.Errorf("executing scylla --version: %w", err)
	}

	raw := strings.TrimSpace(stdout)
	result := parseScyllaVersion(raw)

	// Write artifact.
	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "scylla-version.log", []byte(stdout), "Raw scylla --version output", &artifacts)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   result.Version,
		Artifacts: artifacts,
	}, nil
}

// parseScyllaVersion parses the output of `scylla --version`.
// Common formats:
//   - "5.4.9-0.20241017.0e4c24b49297" (OSS)
//   - "2024.2.1-0.20241017.0e4c24b49297" (Enterprise)
//   - "6.2.2" (simple)
func parseScyllaVersion(raw string) *ScyllaVersionResult {
	result := &ScyllaVersionResult{
		Raw: raw,
	}

	if raw == "" {
		return result
	}

	// Split on the first dash to separate version from build.
	// Handle cases like "5.4.9-0.20241017.xxx" → version="5.4.9", build="0.20241017.xxx"
	// And "2026.1.0" → version="2026.1.0", build=""
	parts := strings.SplitN(raw, "-", 2)
	result.Version = parts[0]
	if len(parts) > 1 {
		result.Build = parts[1]
	}

	return result
}
