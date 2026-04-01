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
	// ScyllaClusterPDBCollectorID is the unique identifier for the ScyllaClusterPDBCollector.
	ScyllaClusterPDBCollectorID engine.CollectorID = "ScyllaClusterPDBCollector"
)

// ScyllaClusterPDBResult holds metadata about collected PodDisruptionBudget manifests for a ScyllaCluster.
type ScyllaClusterPDBResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterPDBResult is the typed accessor for ScyllaClusterPDBCollector results.
func GetScyllaClusterPDBResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterPDBResult, error) {
	result, ok := vitals.Get(ScyllaClusterPDBCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterPDBCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterPDBCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterPDBResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterPDBCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterPDBCollector collects PodDisruptionBudget manifests owned by a ScyllaCluster.
type scyllaClusterPDBCollector struct{}

var _ engine.Collector = (*scyllaClusterPDBCollector)(nil)

// NewScyllaClusterPDBCollector creates a new ScyllaClusterPDBCollector.
func NewScyllaClusterPDBCollector() engine.Collector {
	return &scyllaClusterPDBCollector{}
}

func (c *scyllaClusterPDBCollector) ID() engine.CollectorID { return ScyllaClusterPDBCollectorID }
func (c *scyllaClusterPDBCollector) Name() string {
	return "ScyllaCluster PodDisruptionBudget manifests"
}
func (c *scyllaClusterPDBCollector) Scope() engine.CollectorScope    { return engine.PerScyllaCluster }
func (c *scyllaClusterPDBCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *scyllaClusterPDBCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	pdbs, err := params.ResourceLister.ListPodDisruptionBudgets(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing poddisruptionbudgets in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range pdbs {
		pdb := &pdbs[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(pdb)
			if err != nil {
				return nil, fmt.Errorf("marshaling poddisruptionbudget %s/%s: %w", pdb.Namespace, pdb.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(pdb.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for poddisruptionbudget %s/%s: %w", pdb.Namespace, pdb.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("PodDisruptionBudget %s/%s manifest", pdb.Namespace, pdb.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d PodDisruptionBudget(s) for ScyllaCluster %s/%s", len(pdbs), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterPDBResult{Count: len(pdbs)},
		Artifacts: artifacts,
	}, nil
}
