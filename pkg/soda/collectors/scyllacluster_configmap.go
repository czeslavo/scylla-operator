package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

const (
	// ScyllaClusterConfigMapCollectorID is the unique identifier for the ScyllaClusterConfigMapCollector.
	ScyllaClusterConfigMapCollectorID engine.CollectorID = "ScyllaClusterConfigMapCollector"
)

// ScyllaClusterConfigMapResult holds metadata about collected ConfigMap manifests for a ScyllaCluster.
type ScyllaClusterConfigMapResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterConfigMapResult is the typed accessor for ScyllaClusterConfigMapCollector results.
func GetScyllaClusterConfigMapResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterConfigMapResult, error) {
	result, ok := vitals.Get(ScyllaClusterConfigMapCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterConfigMapCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterConfigMapCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterConfigMapResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterConfigMapCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterConfigMapCollector collects ConfigMap manifests owned by a ScyllaCluster.
type scyllaClusterConfigMapCollector struct{}

var _ engine.Collector = (*scyllaClusterConfigMapCollector)(nil)

// NewScyllaClusterConfigMapCollector creates a new ScyllaClusterConfigMapCollector.
func NewScyllaClusterConfigMapCollector() engine.Collector {
	return &scyllaClusterConfigMapCollector{}
}

func (c *scyllaClusterConfigMapCollector) ID() engine.CollectorID {
	return ScyllaClusterConfigMapCollectorID
}
func (c *scyllaClusterConfigMapCollector) Name() string { return "ScyllaCluster ConfigMap manifests" }
func (c *scyllaClusterConfigMapCollector) Scope() engine.CollectorScope {
	return engine.PerScyllaCluster
}
func (c *scyllaClusterConfigMapCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: configmaps — get, list
func (c *scyllaClusterConfigMapCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterConfigMapCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	configMaps, err := params.ResourceLister.ListConfigMaps(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing configmaps in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range configMaps {
		cm := &configMaps[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(cm)
			if err != nil {
				return nil, fmt.Errorf("marshaling configmap %s/%s: %w", cm.Namespace, cm.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(cm.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for configmap %s/%s: %w", cm.Namespace, cm.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("ConfigMap %s/%s manifest", cm.Namespace, cm.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ConfigMap(s) for ScyllaCluster %s/%s", len(configMaps), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterConfigMapResult{Count: len(configMaps)},
		Artifacts: artifacts,
	}, nil
}
