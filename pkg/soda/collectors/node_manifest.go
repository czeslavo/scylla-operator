package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
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
	result, ok := vitals.Get(NodeManifestCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("NodeManifestCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("NodeManifestCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*NodeManifestResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for NodeManifestCollector", result.Data)
	}
	return typed, nil
}

// nodeManifestCollector collects Kubernetes Node manifests.
type nodeManifestCollector struct{}

var _ engine.Collector = (*nodeManifestCollector)(nil)

// NewNodeManifestCollector creates a new NodeManifestCollector.
func NewNodeManifestCollector() engine.Collector {
	return &nodeManifestCollector{}
}

func (c *nodeManifestCollector) ID() engine.CollectorID          { return NodeManifestCollectorID }
func (c *nodeManifestCollector) Name() string                    { return "Node manifests" }
func (c *nodeManifestCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *nodeManifestCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *nodeManifestCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	nodes, err := params.ResourceLister.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		for i := range nodes {
			node := &nodes[i]
			data, err := yaml.Marshal(node)
			if err != nil {
				return nil, fmt.Errorf("marshaling node %s: %w", node.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(node.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for node %s: %w", node.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("Node %s manifest", node.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d node(s)", len(nodes)),
		Data:      &NodeManifestResult{Count: len(nodes)},
		Artifacts: artifacts,
	}, nil
}
