package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// ScyllaClusterJobLogsCollectorID is the unique identifier for the ScyllaClusterJobLogsCollector.
	ScyllaClusterJobLogsCollectorID engine.CollectorID = "ScyllaClusterJobLogsCollector"
)

// ScyllaClusterJobLogsResult holds metadata about collected container logs from cleanup job pods.
type ScyllaClusterJobLogsResult struct {
	PodCount      int `json:"pod_count"`
	ArtifactCount int `json:"artifact_count"`
}

// scyllaClusterJobLogsCollector collects current and previous container logs from
// cleanup job pods belonging to a ScyllaCluster/ScyllaDBDatacenter.
type scyllaClusterJobLogsCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaClusterCollector = (*scyllaClusterJobLogsCollector)(nil)

// NewScyllaClusterJobLogsCollector creates a new ScyllaClusterJobLogsCollector.
func NewScyllaClusterJobLogsCollector() engine.PerScyllaClusterCollector {
	return &scyllaClusterJobLogsCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaClusterJobLogsCollectorID, "ScyllaCluster cleanup job pod logs", engine.PerScyllaCluster, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods — get, list
//   - core/v1: pods/log — get
func (c *scyllaClusterJobLogsCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods/log"},
			Verbs:     []string{"get"},
		},
	}
}

func (c *scyllaClusterJobLogsCollector) CollectPerScyllaCluster(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
	if params.PodLogFetcher == nil {
		return &engine.CollectorResult{
			Status:  engine.CollectorSkipped,
			Message: "PodLogFetcher not available (offline mode)",
		}, nil
	}

	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{
		naming.PodTypeLabel: string(naming.PodTypeCleanupJob),
	})

	pods, err := params.ResourceLister.ListPods(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing cleanup job pods in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact

	for i := range pods {
		pod := &pods[i]

		// Collect all init container names followed by regular container names.
		var containerNames []string
		for _, ic := range pod.Spec.InitContainers {
			containerNames = append(containerNames, ic.Name)
		}
		for _, c := range pod.Spec.Containers {
			containerNames = append(containerNames, c.Name)
		}

		artifacts = append(artifacts, collectContainerLogs(ctx, params.PodLogFetcher, params.ArtifactWriter,
			sc.Namespace, pod.Name, containerNames, pod.Name)...)
	}

	return &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: fmt.Sprintf("Collected logs from %d cleanup job pod(s) for ScyllaCluster %s/%s", len(pods), sc.Namespace, sc.Name),
		Data: &ScyllaClusterJobLogsResult{
			PodCount:      len(pods),
			ArtifactCount: len(artifacts),
		},
		Artifacts: artifacts,
	}, nil
}
