package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const (
	// NodeConfigCollectorID is the unique identifier for the NodeConfigCollector.
	NodeConfigCollectorID engine.CollectorID = "NodeConfigCollector"
)

// NodeConfigResult holds metadata about collected NodeConfig manifests.
type NodeConfigResult struct {
	Count int `json:"count"`
}

// GetNodeConfigResult is the typed accessor for NodeConfigCollector results.
func GetNodeConfigResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*NodeConfigResult, error) {
	result, ok := vitals.Get(NodeConfigCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("NodeConfigCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("NodeConfigCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*NodeConfigResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for NodeConfigCollector", result.Data)
	}
	return typed, nil
}

// nodeConfigCollector collects NodeConfig manifests.
type nodeConfigCollector struct{}

var _ engine.Collector = (*nodeConfigCollector)(nil)

// NewNodeConfigCollector creates a new NodeConfigCollector.
func NewNodeConfigCollector() engine.Collector {
	return &nodeConfigCollector{}
}

func (c *nodeConfigCollector) ID() engine.CollectorID          { return NodeConfigCollectorID }
func (c *nodeConfigCollector) Name() string                    { return "NodeConfig manifests" }
func (c *nodeConfigCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *nodeConfigCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - scylla.scylladb.com/v1alpha1: nodeconfigs — get, list
func (c *nodeConfigCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"scylla.scylladb.com"},
			Resources: []string{"nodeconfigs"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *nodeConfigCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	nodeConfigs, err := params.ResourceLister.ListNodeConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodeconfigs: %w", err)
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		for _, nc := range nodeConfigs {
			data, err := yaml.Marshal(nc)
			if err != nil {
				return nil, fmt.Errorf("marshaling nodeconfig %s: %w", nc.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(nc.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for nodeconfig %s: %w", nc.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("NodeConfig %s manifest", nc.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d NodeConfig(s)", len(nodeConfigs)),
		Data:      &NodeConfigResult{Count: len(nodeConfigs)},
		Artifacts: artifacts,
	}, nil
}
