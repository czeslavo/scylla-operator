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
	// ScyllaClusterPodCollectorID is the unique identifier for the ScyllaClusterPodCollector.
	ScyllaClusterPodCollectorID engine.CollectorID = "ScyllaClusterPodCollector"
)

// ScyllaClusterPodResult holds metadata about collected Pod manifests for a ScyllaCluster.
type ScyllaClusterPodResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterPodResult is the typed accessor for ScyllaClusterPodCollector results.
func GetScyllaClusterPodResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterPodResult, error) {
	result, ok := vitals.Get(ScyllaClusterPodCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterPodCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterPodCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterPodResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterPodCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterPodCollector collects Pod manifests owned by a ScyllaCluster.
type scyllaClusterPodCollector struct{}

var _ engine.Collector = (*scyllaClusterPodCollector)(nil)

// NewScyllaClusterPodCollector creates a new ScyllaClusterPodCollector.
func NewScyllaClusterPodCollector() engine.Collector {
	return &scyllaClusterPodCollector{}
}

func (c *scyllaClusterPodCollector) ID() engine.CollectorID          { return ScyllaClusterPodCollectorID }
func (c *scyllaClusterPodCollector) Name() string                    { return "ScyllaCluster Pod manifests" }
func (c *scyllaClusterPodCollector) Scope() engine.CollectorScope    { return engine.PerScyllaCluster }
func (c *scyllaClusterPodCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods — get, list
func (c *scyllaClusterPodCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterPodCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	pods, err := params.ResourceLister.ListPods(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing pods in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range pods {
		pod := &pods[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(pod)
			if err != nil {
				return nil, fmt.Errorf("marshaling pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(pod.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("Pod %s/%s manifest", pod.Namespace, pod.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Pod(s) for ScyllaCluster %s/%s", len(pods), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterPodResult{Count: len(pods)},
		Artifacts: artifacts,
	}, nil
}
