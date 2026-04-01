package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// ScyllaNodeLogsCollectorID is the unique identifier for the ScyllaNodeLogsCollector.
	ScyllaNodeLogsCollectorID engine.CollectorID = "ScyllaNodeLogsCollector"
)

// ScyllaNodeLogsResult holds metadata about collected container logs for a Scylla node pod.
type ScyllaNodeLogsResult struct {
	ContainerCount int `json:"container_count"`
	ArtifactCount  int `json:"artifact_count"`
}

// scyllaNodeLogsCollector collects current and previous container logs from Scylla node pods.
type scyllaNodeLogsCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*scyllaNodeLogsCollector)(nil)

// NewScyllaNodeLogsCollector creates a new ScyllaNodeLogsCollector.
func NewScyllaNodeLogsCollector() engine.PerScyllaNodeCollector {
	return &scyllaNodeLogsCollector{CollectorBase: engine.NewCollectorBase(ScyllaNodeLogsCollectorID, "Scylla node container logs", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods — get, list
//   - core/v1: pods/log — get
func (c *scyllaNodeLogsCollector) RBAC() []rbacv1.PolicyRule {
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

func (c *scyllaNodeLogsCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	if params.PodLogFetcher == nil {
		return &engine.CollectorResult{
			Status:  engine.CollectorSkipped,
			Message: "PodLogFetcher not available (offline mode)",
		}, nil
	}

	node := params.ScyllaNode

	// List pods in the namespace and find the one matching this Scylla node by name.
	pods, err := params.ResourceLister.ListPods(ctx, node.Namespace, labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("listing pods in namespace %s: %w", node.Namespace, err)
	}

	var targetPod *struct {
		initContainerNames    []string
		regularContainerNames []string
	}
	for i := range pods {
		if pods[i].Name == node.Name {
			p := &pods[i]
			names := &struct {
				initContainerNames    []string
				regularContainerNames []string
			}{}
			for _, ic := range p.Spec.InitContainers {
				names.initContainerNames = append(names.initContainerNames, ic.Name)
			}
			for _, c := range p.Spec.Containers {
				names.regularContainerNames = append(names.regularContainerNames, c.Name)
			}
			targetPod = names
			break
		}
	}

	if targetPod == nil {
		return &engine.CollectorResult{
			Status:  engine.CollectorSkipped,
			Message: fmt.Sprintf("pod %s/%s not found", node.Namespace, node.Name),
		}, nil
	}

	// Collect all init container names followed by regular container names.
	allContainers := append(targetPod.initContainerNames, targetPod.regularContainerNames...)

	var artifacts []engine.Artifact
	containerCount := len(allContainers)

	for _, containerName := range allContainers {
		// Current logs.
		currentLogs, err := params.PodLogFetcher.GetPodLogs(ctx, node.Namespace, node.Name, containerName, false)
		if err != nil {
			// Non-fatal: log the error in the artifact filename via a best-effort approach.
			// We still continue with other containers.
		} else if params.ArtifactWriter != nil {
			filename := containerName + ".current.log"
			relPath, err := params.ArtifactWriter.WriteArtifact(filename, currentLogs)
			if err == nil {
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("Current logs for container %s in pod %s/%s", containerName, node.Namespace, node.Name),
				})
			}
		}

		// Previous logs (best-effort: skip if no previous run).
		previousLogs, err := params.PodLogFetcher.GetPodLogs(ctx, node.Namespace, node.Name, containerName, true)
		if err != nil {
			// "previous" container run doesn't exist — silently skip.
			if !isPreviousContainerNotFoundError(err) {
				// Unexpected error — also skip silently for best-effort collection.
				_ = err
			}
		} else if params.ArtifactWriter != nil {
			filename := containerName + ".previous.log"
			relPath, err := params.ArtifactWriter.WriteArtifact(filename, previousLogs)
			if err == nil {
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("Previous logs for container %s in pod %s/%s", containerName, node.Namespace, node.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: fmt.Sprintf("Collected logs from %d container(s) in pod %s/%s", containerCount, node.Namespace, node.Name),
		Data: &ScyllaNodeLogsResult{
			ContainerCount: containerCount,
			ArtifactCount:  len(artifacts),
		},
		Artifacts: artifacts,
	}, nil
}

// isPreviousContainerNotFoundError checks whether the error indicates that there
// is no previous container run to fetch logs from. This is a best-effort heuristic
// based on the Kubernetes API error message.
func isPreviousContainerNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "previous terminated container") ||
		strings.Contains(msg, "no previous") ||
		strings.Contains(msg, "PreviousContainerNotFound")
}
