package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// DiskUsageCollectorID is the unique identifier for the DiskUsageCollector.
	DiskUsageCollectorID engine.CollectorID = "DiskUsageCollector"
)

// DiskUsageResult holds the raw disk usage output from a pod.
type DiskUsageResult struct {
	RawOutput string `json:"raw_output"` // Raw output of df -h
}

// GetDiskUsageResult is the typed accessor for DiskUsageCollector results.
func GetDiskUsageResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*DiskUsageResult, error) {
	return engine.GetResult[DiskUsageResult](vitals, DiskUsageCollectorID, podKey)
}

// ReadDiskUsageOutput reads the raw df.txt artifact.
func ReadDiskUsageOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(DiskUsageCollectorID, podKey, "df.txt")
}

// diskUsageCollector collects disk usage information from Scylla pods.
type diskUsageCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*diskUsageCollector)(nil)

// NewDiskUsageCollector creates a new DiskUsageCollector.
func NewDiskUsageCollector() engine.PerScyllaNodeCollector {
	return &diskUsageCollector{CollectorBase: engine.NewCollectorBase(DiskUsageCollectorID, "Disk usage", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to run df -h)
func (c *diskUsageCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *diskUsageCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params, []string{"df", "-h"})
	if err != nil {
		return nil, fmt.Errorf("running df -h: %w", err)
	}

	raw := strings.TrimSpace(stdout)

	result := &DiskUsageResult{
		RawOutput: raw,
	}

	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "df.txt", []byte(raw), "Raw df -h output", &artifacts)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   "collected disk usage",
		Artifacts: artifacts,
	}, nil
}
