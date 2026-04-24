package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// ScyllaClusterPDBCollectorID is the unique identifier for the ScyllaClusterPDBCollector.
	ScyllaClusterPDBCollectorID engine.CollectorID = "ScyllaClusterPDBCollector"
)

// ScyllaClusterPDBResult holds metadata about collected PodDisruptionBudget manifests for a ScyllaCluster.
type ScyllaClusterPDBResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterPDBResult is the typed accessor for ScyllaClusterPDBCollector results.
func GetScyllaClusterPDBResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterPDBResult, error) {
	return engine.GetResult[ScyllaClusterPDBResult](vitals, ScyllaClusterPDBCollectorID, scopeKey)
}

// scyllaClusterPDBCollector collects PodDisruptionBudget manifests owned by a ScyllaCluster.
type scyllaClusterPDBCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterPDBCollector)(nil)

// NewScyllaClusterPDBCollector creates a new ScyllaClusterPDBCollector.
func NewScyllaClusterPDBCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterPDBCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterPDBCollectorID, "ScyllaCluster PodDisruptionBudget manifests", "Collects PodDisruptionBudget manifests owned by a ScyllaCluster.", engine.PerScyllaCluster, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - policy/v1: poddisruptionbudgets — get, list
func (c *scyllaClusterPDBCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"policy"},
			Resources: []string{"poddisruptionbudgets"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterPDBCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	pdbs, err := params.ResourceLister.ListPodDisruptionBudgets(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing poddisruptionbudgets in namespace %s: %w", sc.Namespace, err)
	}

	artifacts := collectAndWriteManifests(params.ArtifactWriter, pdbs,
		func(pdb *policyv1.PodDisruptionBudget) string { return pdb.Name + ".yaml" },
		func(pdb *policyv1.PodDisruptionBudget) string {
			return fmt.Sprintf("PodDisruptionBudget %s/%s manifest", pdb.Namespace, pdb.Name)
		},
	)

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d PodDisruptionBudget(s) for ScyllaCluster %s/%s", len(pdbs), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterPDBResult{Count: len(pdbs)},
		Artifacts: artifacts,
	}, nil
}
