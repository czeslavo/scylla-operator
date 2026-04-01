package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// NodeResourcesCollectorID is the unique identifier for the NodeResourcesCollector.
	NodeResourcesCollectorID engine.CollectorID = "NodeResourcesCollector"
)

// NodeResourcesResult holds the parsed node resources data.
type NodeResourcesResult struct {
	Nodes []NodeInfo `json:"nodes"`
}

// NodeInfo holds information about a single Kubernetes Node.
type NodeInfo struct {
	Name        string              `json:"name"`
	Capacity    map[string]string   `json:"capacity"` // e.g. {"cpu": "4", "memory": "16Gi"}
	Allocatable map[string]string   `json:"allocatable"`
	Labels      map[string]string   `json:"labels"`
	Conditions  []NodeConditionInfo `json:"conditions"`
}

// NodeConditionInfo holds a single node condition.
type NodeConditionInfo struct {
	Type    string `json:"type"`   // e.g. "Ready", "MemoryPressure"
	Status  string `json:"status"` // "True", "False", "Unknown"
	Message string `json:"message"`
}

// GetNodeResourcesResult is the typed accessor for NodeResourcesCollector results.
func GetNodeResourcesResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*NodeResourcesResult, error) {
	return engine.GetResult[NodeResourcesResult](vitals, NodeResourcesCollectorID, scopeKey)
}

// ReadNodesYAML reads the raw nodes.yaml artifact.
func ReadNodesYAML(reader engine.ArtifactReader, scopeKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(NodeResourcesCollectorID, scopeKey, "nodes.yaml")
}

// nodeResourcesCollector collects Kubernetes Node information.
type nodeResourcesCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*nodeResourcesCollector)(nil)

// NewNodeResourcesCollector creates a new NodeResourcesCollector.
func NewNodeResourcesCollector() engine.ClusterWideCollector {
	return &nodeResourcesCollector{
		CollectorBase: engine.NewCollectorBase(NodeResourcesCollectorID, "Kubernetes Node resources", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: nodes — get, list
func (c *nodeResourcesCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *nodeResourcesCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	nodes, err := params.ResourceLister.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	result := &NodeResourcesResult{
		Nodes: make([]NodeInfo, 0, len(nodes)),
	}

	for _, node := range nodes {
		info := NodeInfo{
			Name:        node.Name,
			Capacity:    make(map[string]string),
			Allocatable: make(map[string]string),
			Labels:      node.Labels,
			Conditions:  make([]NodeConditionInfo, 0, len(node.Status.Conditions)),
		}

		for resourceName, quantity := range node.Status.Capacity {
			info.Capacity[string(resourceName)] = quantity.String()
		}
		for resourceName, quantity := range node.Status.Allocatable {
			info.Allocatable[string(resourceName)] = quantity.String()
		}
		for _, cond := range node.Status.Conditions {
			info.Conditions = append(info.Conditions, NodeConditionInfo{
				Type:    string(cond.Type),
				Status:  string(cond.Status),
				Message: cond.Message,
			})
		}

		result.Nodes = append(result.Nodes, info)
	}

	// Write artifact.
	var artifacts []engine.Artifact
	marshalAndWriteYAML(params.ArtifactWriter, "nodes.yaml", "Raw Node objects YAML", nodes, &artifacts)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("Collected %d nodes", len(result.Nodes)),
		Artifacts: artifacts,
	}, nil
}
