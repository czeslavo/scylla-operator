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
	// ScyllaClusterServiceAccountCollectorID is the unique identifier for the ScyllaClusterServiceAccountCollector.
	ScyllaClusterServiceAccountCollectorID engine.CollectorID = "ScyllaClusterServiceAccountCollector"
)

// ScyllaClusterServiceAccountResult holds metadata about collected ServiceAccount manifests for a ScyllaCluster.
type ScyllaClusterServiceAccountResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterServiceAccountResult is the typed accessor for ScyllaClusterServiceAccountCollector results.
func GetScyllaClusterServiceAccountResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterServiceAccountResult, error) {
	result, ok := vitals.Get(ScyllaClusterServiceAccountCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterServiceAccountCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterServiceAccountCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterServiceAccountResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterServiceAccountCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterServiceAccountCollector collects ServiceAccount manifests owned by a ScyllaCluster.
type scyllaClusterServiceAccountCollector struct{}

var _ engine.Collector = (*scyllaClusterServiceAccountCollector)(nil)

// NewScyllaClusterServiceAccountCollector creates a new ScyllaClusterServiceAccountCollector.
func NewScyllaClusterServiceAccountCollector() engine.Collector {
	return &scyllaClusterServiceAccountCollector{}
}

func (c *scyllaClusterServiceAccountCollector) ID() engine.CollectorID {
	return ScyllaClusterServiceAccountCollectorID
}
func (c *scyllaClusterServiceAccountCollector) Name() string {
	return "ScyllaCluster ServiceAccount manifests"
}
func (c *scyllaClusterServiceAccountCollector) Scope() engine.CollectorScope {
	return engine.PerScyllaCluster
}
func (c *scyllaClusterServiceAccountCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *scyllaClusterServiceAccountCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	serviceAccounts, err := params.ResourceLister.ListServiceAccounts(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing serviceaccounts in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range serviceAccounts {
		sa := &serviceAccounts[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(sa)
			if err != nil {
				return nil, fmt.Errorf("marshaling serviceaccount %s/%s: %w", sa.Namespace, sa.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(sa.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for serviceaccount %s/%s: %w", sa.Namespace, sa.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("ServiceAccount %s/%s manifest", sa.Namespace, sa.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ServiceAccount(s) for ScyllaCluster %s/%s", len(serviceAccounts), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterServiceAccountResult{Count: len(serviceAccounts)},
		Artifacts: artifacts,
	}, nil
}
