package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// ScyllaClusterStatefulSetCollectorID is the unique identifier for the ScyllaClusterStatefulSetCollector.
	ScyllaClusterStatefulSetCollectorID engine.CollectorID = "ScyllaClusterStatefulSetCollector"
)

// ScyllaClusterStatefulSetResult holds metadata about collected StatefulSet manifests for a ScyllaCluster.
type ScyllaClusterStatefulSetResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterStatefulSetResult is the typed accessor for ScyllaClusterStatefulSetCollector results.
func GetScyllaClusterStatefulSetResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterStatefulSetResult, error) {
	return engine.GetResult[ScyllaClusterStatefulSetResult](vitals, ScyllaClusterStatefulSetCollectorID, scopeKey)
}

// scyllaClusterStatefulSetCollector collects StatefulSet manifests owned by a ScyllaCluster.
type scyllaClusterStatefulSetCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterStatefulSetCollector)(nil)

// NewScyllaClusterStatefulSetCollector creates a new ScyllaClusterStatefulSetCollector.
func NewScyllaClusterStatefulSetCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterStatefulSetCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterStatefulSetCollectorID, "ScyllaCluster StatefulSet manifests", engine.PerScyllaCluster, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - apps/v1: statefulsets — get, list
func (c *scyllaClusterStatefulSetCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"apps"},
			Resources: []string{"statefulsets"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterStatefulSetCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	statefulSets, err := params.ResourceLister.ListStatefulSets(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets in namespace %s: %w", sc.Namespace, err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, statefulSets,
		func(ss *appsv1.StatefulSet) string { return ss.Name + ".yaml" },
		func(ss *appsv1.StatefulSet) string {
			return fmt.Sprintf("StatefulSet %s/%s manifest", ss.Namespace, ss.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d StatefulSet(s) for ScyllaCluster %s/%s", len(statefulSets), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterStatefulSetResult{Count: len(statefulSets)},
		Artifacts: artifacts,
	}, nil
}
