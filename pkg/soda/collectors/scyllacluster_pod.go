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
	return engine.GetResult[ScyllaClusterPodResult](vitals, ScyllaClusterPodCollectorID, scopeKey)
}

// scyllaClusterPodCollector collects Pod manifests owned by a ScyllaCluster.
type scyllaClusterPodCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterPodCollector)(nil)

// NewScyllaClusterPodCollector creates a new ScyllaClusterPodCollector.
func NewScyllaClusterPodCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterPodCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterPodCollectorID, "ScyllaCluster Pod manifests", engine.PerScyllaCluster, nil),
	}
}

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

func (c *scyllaClusterPodCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
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
