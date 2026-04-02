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
	// ScyllaClusterServiceCollectorID is the unique identifier for the ScyllaClusterServiceCollector.
	ScyllaClusterServiceCollectorID engine.CollectorID = "ScyllaClusterServiceCollector"
)

// ScyllaClusterServiceResult holds metadata about collected Service manifests for a ScyllaCluster.
type ScyllaClusterServiceResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterServiceResult is the typed accessor for ScyllaClusterServiceCollector results.
func GetScyllaClusterServiceResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterServiceResult, error) {
	return engine.GetResult[ScyllaClusterServiceResult](vitals, ScyllaClusterServiceCollectorID, scopeKey)
}

// scyllaClusterServiceCollector collects Service manifests owned by a ScyllaCluster.
type scyllaClusterServiceCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterServiceCollector)(nil)

// NewScyllaClusterServiceCollector creates a new ScyllaClusterServiceCollector.
func NewScyllaClusterServiceCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterServiceCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterServiceCollectorID, "ScyllaCluster Service manifests", engine.PerScyllaCluster, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: services — get, list
func (c *scyllaClusterServiceCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"services"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterServiceCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	services, err := params.ResourceLister.ListServices(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing services in namespace %s: %w", sc.Namespace, err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, services,
		func(svc *corev1.Service) string { return svc.Name + ".yaml" },
		func(svc *corev1.Service) string {
			return fmt.Sprintf("Service %s/%s manifest", svc.Namespace, svc.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Service(s) for ScyllaCluster %s/%s", len(services), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterServiceResult{Count: len(services)},
		Artifacts: artifacts,
	}, nil
}
