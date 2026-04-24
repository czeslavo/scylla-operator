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
	return engine.GetResult[RlimitsResult](vitals, RlimitsCollectorID, podKey)
}

// ReadRlimitsOutput reads the raw rlimits.txt artifact.
func ReadRlimitsOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(RlimitsCollectorID, podKey, "rlimits.txt")
}

// rlimitsCollector collects resource limits for the scylla process from pods.
// It uses /proc/<pid>/limits because prlimit is not available in the scylla container.
type rlimitsCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*rlimitsCollector)(nil)

// NewRlimitsCollector creates a new RlimitsCollector.
func NewRlimitsCollector() engine.PerScyllaNodeCollector {
	return &rlimitsCollector{CollectorBase: engine.NewCollectorBase(RlimitsCollectorID, "Scylla process resource limits", "Reads /proc/<pid>/limits inside each Scylla pod to capture process resource limits (open files, memory, etc.).", engine.PerScyllaNode, nil)}
}

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

func (c *rlimitsCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	// prlimit is not available in the scylla container; use /proc/<pid>/limits instead.
	stdout, err := ExecInScyllaPod(ctx, params, []string{"bash", "-c", "cat /proc/$(pidof scylla)/limits"})
	if err != nil {
		return nil, fmt.Errorf("reading scylla process limits: %w", err)
	}

	raw := strings.TrimSpace(stdout)

	result := &RlimitsResult{
		RawOutput: raw,
	}

	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "rlimits.txt", []byte(raw), "Resource limits for the scylla process (/proc/<pid>/limits)", &artifacts)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   "collected scylla process resource limits",
		Artifacts: artifacts,
	}, nil
}
