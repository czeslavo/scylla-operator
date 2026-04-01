package collectors

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// PodManifestCollectorID is the unique identifier for the PodManifestCollector.
	PodManifestCollectorID engine.CollectorID = "PodManifestCollector"
)

// PodManifestResult holds metadata about collected Pod manifests.
type PodManifestResult struct {
	Count int `json:"count"`
}

// GetPodManifestResult is the typed accessor for PodManifestCollector results.
func GetPodManifestResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*PodManifestResult, error) {
	return engine.GetResult[PodManifestResult](vitals, PodManifestCollectorID, scopeKey)
}

// podManifestCollector collects Pod manifests from operator namespaces (no exec).
type podManifestCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*podManifestCollector)(nil)

// NewPodManifestCollector creates a new PodManifestCollector.
func NewPodManifestCollector() engine.ClusterWideCollector {
	return &podManifestCollector{
		CollectorBase: engine.NewCollectorBase(PodManifestCollectorID, "Pod manifests (operator namespaces)", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods — get, list
func (c *podManifestCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *podManifestCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		pods, err := params.ResourceLister.ListPods(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing pods in namespace %s: %w", ns, err)
		}
		total += len(pods)
		artifacts = append(artifacts, collectAndWriteManifests(params.ArtifactWriter, pods,
			func(pod *corev1.Pod) string { return filepath.Join(pod.Namespace, pod.Name+".yaml") },
			func(pod *corev1.Pod) string {
				return fmt.Sprintf("Pod %s/%s manifest", pod.Namespace, pod.Name)
			},
		)...)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Pod(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &PodManifestResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
