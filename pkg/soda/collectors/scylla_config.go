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
	result, ok := vitals.Get(ScyllaConfigCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaConfigCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaConfigCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaConfigResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaConfigCollector", result.Data)
	}
	return typed, nil
}

// ReadScyllaConfigYAML reads the raw scylla.yaml artifact.
func ReadScyllaConfigYAML(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(ScyllaConfigCollectorID, podKey, "scylla.yaml")
}

// scyllaConfigCollector collects the Scylla configuration file from pods.
type scyllaConfigCollector struct{}

var _ engine.Collector = (*scyllaConfigCollector)(nil)

// NewScyllaConfigCollector creates a new ScyllaConfigCollector.
func NewScyllaConfigCollector() engine.Collector {
	return &scyllaConfigCollector{}
}

func (c *scyllaConfigCollector) ID() engine.CollectorID          { return ScyllaConfigCollectorID }
func (c *scyllaConfigCollector) Name() string                    { return "Scylla configuration" }
func (c *scyllaConfigCollector) Scope() engine.CollectorScope    { return engine.PerPod }
func (c *scyllaConfigCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *scyllaConfigCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.Pod == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	stdout, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName,
		[]string{"cat", scyllaConfigPath})
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", scyllaConfigPath, err)
	}

	raw := strings.TrimSpace(stdout)

	result := &ScyllaConfigResult{
		RawYAML: raw,
	}

	// Write artifact.
	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		if relPath, err := params.ArtifactWriter.WriteArtifact("scylla.yaml", []byte(raw)); err == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Raw /etc/scylla/scylla.yaml content"})
		}
	}

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
