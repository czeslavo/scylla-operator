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
	result, ok := vitals.Get(DiskUsageCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("DiskUsageCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("DiskUsageCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*DiskUsageResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for DiskUsageCollector", result.Data)
	}
	return typed, nil
}

// ReadDiskUsageOutput reads the raw df.txt artifact.
func ReadDiskUsageOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(DiskUsageCollectorID, podKey, "df.txt")
}

// diskUsageCollector collects disk usage information from Scylla pods.
type diskUsageCollector struct{}

var _ engine.Collector = (*diskUsageCollector)(nil)

// NewDiskUsageCollector creates a new DiskUsageCollector.
func NewDiskUsageCollector() engine.Collector {
	return &diskUsageCollector{}
}

func (c *diskUsageCollector) ID() engine.CollectorID          { return DiskUsageCollectorID }
func (c *diskUsageCollector) Name() string                    { return "Disk usage" }
func (c *diskUsageCollector) Scope() engine.CollectorScope    { return engine.PerScyllaNode }
func (c *diskUsageCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *diskUsageCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.ScyllaNode == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	stdout, _, err := params.PodExecutor.Execute(ctx, params.ScyllaNode.Namespace, params.ScyllaNode.Name, scyllaContainerName,
		[]string{"df", "-h"})
	if err != nil {
		return nil, fmt.Errorf("running df -h: %w", err)
	}

	raw := strings.TrimSpace(stdout)

	result := &DiskUsageResult{
		RawOutput: raw,
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		if relPath, err := params.ArtifactWriter.WriteArtifact("df.txt", []byte(raw)); err == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Raw df -h output"})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   "collected disk usage",
		Artifacts: artifacts,
	}, nil
}
