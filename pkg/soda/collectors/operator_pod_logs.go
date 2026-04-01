package collectors

import (
	"context"
	"fmt"
	"path"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// OperatorPodLogsCollectorID is the unique identifier for the OperatorPodLogsCollector.
	OperatorPodLogsCollectorID engine.CollectorID = "OperatorPodLogsCollector"
)

// OperatorPodLogsResult holds metadata about collected container logs from operator namespaces.
type OperatorPodLogsResult struct {
	PodCount      int `json:"pod_count"`
	ArtifactCount int `json:"artifact_count"`
}

// operatorPodLogsCollector collects current and previous container logs from all pods
// in operator-owned namespaces (scylla-operator, scylla-manager, scylla-operator-node-tuning).
type operatorPodLogsCollector struct{}

var _ engine.Collector = (*operatorPodLogsCollector)(nil)

// NewOperatorPodLogsCollector creates a new OperatorPodLogsCollector.
func NewOperatorPodLogsCollector() engine.Collector {
	return &operatorPodLogsCollector{}
}

func (c *operatorPodLogsCollector) ID() engine.CollectorID          { return OperatorPodLogsCollectorID }
func (c *operatorPodLogsCollector) Name() string                    { return "Operator pod logs" }
func (c *operatorPodLogsCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *operatorPodLogsCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods — get, list (in operator namespaces)
//   - core/v1: pods/log — get (in operator namespaces)
func (c *operatorPodLogsCollector) RBAC() []rbacv1.PolicyRule {
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

func (c *operatorPodLogsCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.PodLogFetcher == nil {
		return &engine.CollectorResult{
			Status:  engine.CollectorSkipped,
			Message: "PodLogFetcher not available (offline mode)",
		}, nil
	}

	var artifacts []engine.Artifact
	totalPods := 0

	for _, ns := range operatorNamespaces {
		pods, err := params.ResourceLister.ListPods(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing pods in namespace %s: %w", ns, err)
		}

		for i := range pods {
			pod := &pods[i]
			totalPods++

			// Collect all init container names followed by regular container names.
			var containerNames []string
			for _, ic := range pod.Spec.InitContainers {
				containerNames = append(containerNames, ic.Name)
			}
			for _, c := range pod.Spec.Containers {
				containerNames = append(containerNames, c.Name)
			}

			for _, containerName := range containerNames {
				// Current logs.
				currentLogs, err := params.PodLogFetcher.GetPodLogs(ctx, ns, pod.Name, containerName, false)
				if err == nil && params.ArtifactWriter != nil {
					filename := path.Join(ns, pod.Name, containerName+".current.log")
					relPath, err := params.ArtifactWriter.WriteArtifact(filename, currentLogs)
					if err == nil {
						artifacts = append(artifacts, engine.Artifact{
							RelativePath: relPath,
							Description:  fmt.Sprintf("Current logs for container %s in pod %s/%s", containerName, ns, pod.Name),
						})
					}
				}

				// Previous logs (best-effort: skip if no previous run).
				previousLogs, err := params.PodLogFetcher.GetPodLogs(ctx, ns, pod.Name, containerName, true)
				if err == nil && params.ArtifactWriter != nil {
					filename := path.Join(ns, pod.Name, containerName+".previous.log")
					relPath, err := params.ArtifactWriter.WriteArtifact(filename, previousLogs)
					if err == nil {
						artifacts = append(artifacts, engine.Artifact{
							RelativePath: relPath,
							Description:  fmt.Sprintf("Previous logs for container %s in pod %s/%s", containerName, ns, pod.Name),
						})
					}
				}
			}
		}
	}

	return &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: fmt.Sprintf("Collected logs from %d pod(s) across operator namespaces", totalPods),
		Data: &OperatorPodLogsResult{
			PodCount:      totalPods,
			ArtifactCount: len(artifacts),
		},
		Artifacts: artifacts,
	}, nil
}
