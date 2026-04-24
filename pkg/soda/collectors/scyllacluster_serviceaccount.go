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
	// ScyllaClusterServiceAccountCollectorID is the unique identifier for the ScyllaClusterServiceAccountCollector.
	ScyllaClusterServiceAccountCollectorID engine.CollectorID = "ScyllaClusterServiceAccountCollector"
)

// ScyllaClusterServiceAccountResult holds metadata about collected ServiceAccount manifests for a ScyllaCluster.
type ScyllaClusterServiceAccountResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterServiceAccountResult is the typed accessor for ScyllaClusterServiceAccountCollector results.
func GetScyllaClusterServiceAccountResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterServiceAccountResult, error) {
	return engine.GetResult[ScyllaClusterServiceAccountResult](vitals, ScyllaClusterServiceAccountCollectorID, scopeKey)
}

// scyllaClusterServiceAccountCollector collects ServiceAccount manifests owned by a ScyllaCluster.
type scyllaClusterServiceAccountCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterServiceAccountCollector)(nil)

// NewScyllaClusterServiceAccountCollector creates a new ScyllaClusterServiceAccountCollector.
func NewScyllaClusterServiceAccountCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterServiceAccountCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterServiceAccountCollectorID, "ScyllaCluster ServiceAccount manifests", "Collects ServiceAccount manifests owned by a ScyllaCluster.", engine.PerScyllaCluster, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: serviceaccounts — get, list
func (c *scyllaClusterServiceAccountCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"serviceaccounts"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterServiceAccountCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	serviceAccounts, err := params.ResourceLister.ListServiceAccounts(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing serviceaccounts in namespace %s: %w", sc.Namespace, err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, serviceAccounts,
		func(sa *corev1.ServiceAccount) string { return sa.Name + ".yaml" },
		func(sa *corev1.ServiceAccount) string {
			return fmt.Sprintf("ServiceAccount %s/%s manifest", sa.Namespace, sa.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ServiceAccount(s) for ScyllaCluster %s/%s", len(serviceAccounts), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterServiceAccountResult{Count: len(serviceAccounts)},
		Artifacts: artifacts,
	}, nil
}
