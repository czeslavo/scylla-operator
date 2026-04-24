package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
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
	return engine.GetResult[NodeConfigResult](vitals, NodeConfigCollectorID, scopeKey)
}

// nodeConfigCollector collects NodeConfig manifests.
type nodeConfigCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*nodeConfigCollector)(nil)

// NewNodeConfigCollector creates a new NodeConfigCollector.
func NewNodeConfigCollector() engine.ClusterWideCollector {
	return &nodeConfigCollector{
		CollectorBase: engine.NewCollectorBase(NodeConfigCollectorID, "NodeConfig manifests", "Collects ScyllaDB NodeConfig custom resource manifests.", engine.ClusterWide, nil),
	}
}

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

func (c *nodeConfigCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	nodeConfigs, err := params.ResourceLister.ListNodeConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodeconfigs: %w", err)
	}

	var artifacts []engine.Artifact
	for _, nc := range nodeConfigs {
		marshalAndWriteYAML(params.ArtifactWriter, nc.Name+".yaml",
			fmt.Sprintf("NodeConfig %s manifest", nc.Name), nc, &artifacts)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d NodeConfig(s)", len(nodeConfigs)),
		Data:      &NodeConfigResult{Count: len(nodeConfigs)},
		Artifacts: artifacts,
	}, nil
}
