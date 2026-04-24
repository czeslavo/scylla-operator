package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// NodeManifestCollectorID is the unique identifier for the NodeManifestCollector.
	NodeManifestCollectorID engine.CollectorID = "NodeManifestCollector"
)

// NodeManifestResult holds metadata about collected Node manifests.
type NodeManifestResult struct {
	Count int `json:"count"`
}

// GetNodeManifestResult is the typed accessor for NodeManifestCollector results.
func GetNodeManifestResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*NodeManifestResult, error) {
	return engine.GetResult[NodeManifestResult](vitals, NodeManifestCollectorID, scopeKey)
}

// nodeManifestCollector collects Kubernetes Node manifests.
type nodeManifestCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*nodeManifestCollector)(nil)

// NewNodeManifestCollector creates a new NodeManifestCollector.
func NewNodeManifestCollector() engine.ClusterWideCollector {
	return &nodeManifestCollector{
		CollectorBase: engine.NewCollectorBase(NodeManifestCollectorID, "Node manifests", "Collects Kubernetes Node manifests including capacity, allocatable resources, and conditions.", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: nodes — get, list
func (c *nodeManifestCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *nodeManifestCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	nodes, err := params.ResourceLister.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, nodes,
		func(node *corev1.Node) string { return node.Name + ".yaml" },
		func(node *corev1.Node) string {
			return fmt.Sprintf("Node %s manifest", node.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d node(s)", len(nodes)),
		Data:      &NodeManifestResult{Count: len(nodes)},
		Artifacts: artifacts,
	}, nil
}
