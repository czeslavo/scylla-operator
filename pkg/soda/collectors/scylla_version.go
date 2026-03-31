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
	result, ok := vitals.Get(ScyllaVersionCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaVersionCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaVersionCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaVersionResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaVersionCollector", result.Data)
	}
	return typed, nil
}

// ReadScyllaVersionOutput reads the raw scylla-version.log artifact.
func ReadScyllaVersionOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(ScyllaVersionCollectorID, podKey, "scylla-version.log")
}

// scyllaVersionCollector collects the Scylla version from pods.
type scyllaVersionCollector struct{}

var _ engine.Collector = (*scyllaVersionCollector)(nil)

// NewScyllaVersionCollector creates a new ScyllaVersionCollector.
func NewScyllaVersionCollector() engine.Collector {
	return &scyllaVersionCollector{}
}

func (c *scyllaVersionCollector) ID() engine.CollectorID          { return ScyllaVersionCollectorID }
func (c *scyllaVersionCollector) Name() string                    { return "Scylla version" }
func (c *scyllaVersionCollector) Scope() engine.CollectorScope    { return engine.PerPod }
func (c *scyllaVersionCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *scyllaVersionCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.Pod == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	stdout, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName, []string{"scylla", "--version"})
	if err != nil {
		return nil, fmt.Errorf("executing scylla --version: %w", err)
	}

	raw := strings.TrimSpace(stdout)
	result := parseScyllaVersion(raw)

	// Write artifact.
	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		if relPath, err := params.ArtifactWriter.WriteArtifact("scylla-version.log", []byte(stdout)); err == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Raw scylla --version output"})
		}
	}

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
