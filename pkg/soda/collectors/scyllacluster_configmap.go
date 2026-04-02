package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	return engine.GetResult[ScyllaClusterConfigMapResult](vitals, ScyllaClusterConfigMapCollectorID, scopeKey)
}

// scyllaClusterConfigMapCollector collects ConfigMap manifests owned by a ScyllaCluster.
type scyllaClusterConfigMapCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterConfigMapCollector)(nil)

// NewScyllaClusterConfigMapCollector creates a new ScyllaClusterConfigMapCollector.
func NewScyllaClusterConfigMapCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterConfigMapCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterConfigMapCollectorID, "ScyllaCluster ConfigMap manifests", engine.PerScyllaCluster, nil),
	}
}

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

func (c *scyllaClusterConfigMapCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	configMaps, err := params.ResourceLister.ListConfigMaps(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing configmaps in namespace %s: %w", sc.Namespace, err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, configMaps,
		func(cm *corev1.ConfigMap) string { return cm.Name + ".yaml" },
		func(cm *corev1.ConfigMap) string {
			return fmt.Sprintf("ConfigMap %s/%s manifest", cm.Namespace, cm.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ConfigMap(s) for ScyllaCluster %s/%s", len(configMaps), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterConfigMapResult{Count: len(configMaps)},
		Artifacts: artifacts,
	}, nil
}
