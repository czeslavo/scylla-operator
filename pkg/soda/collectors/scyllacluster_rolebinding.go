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
	// ScyllaClusterRoleBindingCollectorID is the unique identifier for the ScyllaClusterRoleBindingCollector.
	ScyllaClusterRoleBindingCollectorID engine.CollectorID = "ScyllaClusterRoleBindingCollector"
)

// ScyllaClusterRoleBindingResult holds metadata about collected RoleBinding manifests for a ScyllaCluster.
type ScyllaClusterRoleBindingResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterRoleBindingResult is the typed accessor for ScyllaClusterRoleBindingCollector results.
func GetScyllaClusterRoleBindingResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterRoleBindingResult, error) {
	result, ok := vitals.Get(ScyllaClusterRoleBindingCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterRoleBindingCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterRoleBindingCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterRoleBindingResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterRoleBindingCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterRoleBindingCollector collects RoleBinding manifests owned by a ScyllaCluster.
type scyllaClusterRoleBindingCollector struct{}

var _ engine.Collector = (*scyllaClusterRoleBindingCollector)(nil)

// NewScyllaClusterRoleBindingCollector creates a new ScyllaClusterRoleBindingCollector.
func NewScyllaClusterRoleBindingCollector() engine.Collector {
	return &scyllaClusterRoleBindingCollector{}
}

func (c *scyllaClusterRoleBindingCollector) ID() engine.CollectorID {
	return ScyllaClusterRoleBindingCollectorID
}
func (c *scyllaClusterRoleBindingCollector) Name() string {
	return "ScyllaCluster RoleBinding manifests"
}
func (c *scyllaClusterRoleBindingCollector) Scope() engine.CollectorScope {
	return engine.PerScyllaCluster
}
func (c *scyllaClusterRoleBindingCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - rbac.authorization.k8s.io/v1: rolebindings — get, list
func (c *scyllaClusterRoleBindingCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"rolebindings"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterRoleBindingCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	roleBindings, err := params.ResourceLister.ListRoleBindings(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing rolebindings in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range roleBindings {
		rb := &roleBindings[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(rb)
			if err != nil {
				return nil, fmt.Errorf("marshaling rolebinding %s/%s: %w", rb.Namespace, rb.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(rb.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for rolebinding %s/%s: %w", rb.Namespace, rb.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("RoleBinding %s/%s manifest", rb.Namespace, rb.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d RoleBinding(s) for ScyllaCluster %s/%s", len(roleBindings), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterRoleBindingResult{Count: len(roleBindings)},
		Artifacts: artifacts,
	}, nil
}
