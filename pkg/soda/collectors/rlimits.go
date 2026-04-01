package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// RlimitsCollectorID is the unique identifier for the RlimitsCollector.
	RlimitsCollectorID engine.CollectorID = "RlimitsCollector"
)

// RlimitsResult holds the raw resource limits output for the scylla process.
type RlimitsResult struct {
	RawOutput string `json:"raw_output"` // Raw content of /proc/<pid>/limits for the scylla process
}

// GetRlimitsResult is the typed accessor for RlimitsCollector results.
func GetRlimitsResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*RlimitsResult, error) {
	result, ok := vitals.Get(RlimitsCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("RlimitsCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("RlimitsCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*RlimitsResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for RlimitsCollector", result.Data)
	}
	return typed, nil
}

// ReadRlimitsOutput reads the raw rlimits.txt artifact.
func ReadRlimitsOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(RlimitsCollectorID, podKey, "rlimits.txt")
}

// rlimitsCollector collects resource limits for the scylla process from pods.
// It uses /proc/<pid>/limits because prlimit is not available in the scylla container.
type rlimitsCollector struct{}

var _ engine.Collector = (*rlimitsCollector)(nil)

// NewRlimitsCollector creates a new RlimitsCollector.
func NewRlimitsCollector() engine.Collector {
	return &rlimitsCollector{}
}

func (c *rlimitsCollector) ID() engine.CollectorID          { return RlimitsCollectorID }
func (c *rlimitsCollector) Name() string                    { return "Scylla process resource limits" }
func (c *rlimitsCollector) Scope() engine.CollectorScope    { return engine.PerPod }
func (c *rlimitsCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to read /proc/$(pidof scylla)/limits)
func (c *rlimitsCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *rlimitsCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.Pod == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	// prlimit is not available in the scylla container; use /proc/<pid>/limits instead.
	stdout, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName,
		[]string{"bash", "-c", "cat /proc/$(pidof scylla)/limits"})
	if err != nil {
		return nil, fmt.Errorf("reading scylla process limits: %w", err)
	}

	raw := strings.TrimSpace(stdout)

	result := &RlimitsResult{
		RawOutput: raw,
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		if relPath, err := params.ArtifactWriter.WriteArtifact("rlimits.txt", []byte(raw)); err == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Resource limits for the scylla process (/proc/<pid>/limits)"})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   "collected scylla process resource limits",
		Artifacts: artifacts,
	}, nil
}
