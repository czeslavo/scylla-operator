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
	// ScyllaClusterStatefulSetCollectorID is the unique identifier for the ScyllaClusterStatefulSetCollector.
	ScyllaClusterStatefulSetCollectorID engine.CollectorID = "ScyllaClusterStatefulSetCollector"
)

// ScyllaClusterStatefulSetResult holds metadata about collected StatefulSet manifests for a ScyllaCluster.
type ScyllaClusterStatefulSetResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterStatefulSetResult is the typed accessor for ScyllaClusterStatefulSetCollector results.
func GetScyllaClusterStatefulSetResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterStatefulSetResult, error) {
	result, ok := vitals.Get(ScyllaClusterStatefulSetCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterStatefulSetCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterStatefulSetCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterStatefulSetResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterStatefulSetCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterStatefulSetCollector collects StatefulSet manifests owned by a ScyllaCluster.
type scyllaClusterStatefulSetCollector struct{}

var _ engine.Collector = (*scyllaClusterStatefulSetCollector)(nil)

// NewScyllaClusterStatefulSetCollector creates a new ScyllaClusterStatefulSetCollector.
func NewScyllaClusterStatefulSetCollector() engine.Collector {
	return &scyllaClusterStatefulSetCollector{}
}

func (c *scyllaClusterStatefulSetCollector) ID() engine.CollectorID {
	return ScyllaClusterStatefulSetCollectorID
}
func (c *scyllaClusterStatefulSetCollector) Name() string {
	return "ScyllaCluster StatefulSet manifests"
}
func (c *scyllaClusterStatefulSetCollector) Scope() engine.CollectorScope {
	return engine.PerScyllaCluster
}
func (c *scyllaClusterStatefulSetCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *scyllaClusterStatefulSetCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	statefulSets, err := params.ResourceLister.ListStatefulSets(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range statefulSets {
		ss := &statefulSets[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(ss)
			if err != nil {
				return nil, fmt.Errorf("marshaling statefulset %s/%s: %w", ss.Namespace, ss.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(ss.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for statefulset %s/%s: %w", ss.Namespace, ss.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("StatefulSet %s/%s manifest", ss.Namespace, ss.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d StatefulSet(s) for ScyllaCluster %s/%s", len(statefulSets), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterStatefulSetResult{Count: len(statefulSets)},
		Artifacts: artifacts,
	}, nil
}
