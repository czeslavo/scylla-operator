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
	// ScyllaClusterPVCCollectorID is the unique identifier for the ScyllaClusterPVCCollector.
	ScyllaClusterPVCCollectorID engine.CollectorID = "ScyllaClusterPVCCollector"
)

// ScyllaClusterPVCResult holds metadata about collected PersistentVolumeClaim manifests for a ScyllaCluster.
type ScyllaClusterPVCResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterPVCResult is the typed accessor for ScyllaClusterPVCCollector results.
func GetScyllaClusterPVCResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterPVCResult, error) {
	return engine.GetResult[ScyllaClusterPVCResult](vitals, ScyllaClusterPVCCollectorID, scopeKey)
}

// scyllaClusterPVCCollector collects PersistentVolumeClaim manifests owned by a ScyllaCluster.
type scyllaClusterPVCCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterPVCCollector)(nil)

// NewScyllaClusterPVCCollector creates a new ScyllaClusterPVCCollector.
func NewScyllaClusterPVCCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterPVCCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterPVCCollectorID, "ScyllaCluster PersistentVolumeClaim manifests", engine.PerScyllaCluster, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: persistentvolumeclaims — get, list
func (c *scyllaClusterPVCCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterPVCCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	pvcs, err := params.ResourceLister.ListPersistentVolumeClaims(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing persistentvolumeclaims in namespace %s: %w", sc.Namespace, err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, pvcs,
		func(pvc *corev1.PersistentVolumeClaim) string { return pvc.Name + ".yaml" },
		func(pvc *corev1.PersistentVolumeClaim) string {
			return fmt.Sprintf("PersistentVolumeClaim %s/%s manifest", pvc.Namespace, pvc.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d PersistentVolumeClaim(s) for ScyllaCluster %s/%s", len(pvcs), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterPVCResult{Count: len(pvcs)},
		Artifacts: artifacts,
	}, nil
}
