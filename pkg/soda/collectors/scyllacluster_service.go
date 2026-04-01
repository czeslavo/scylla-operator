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
	// ScyllaClusterServiceCollectorID is the unique identifier for the ScyllaClusterServiceCollector.
	ScyllaClusterServiceCollectorID engine.CollectorID = "ScyllaClusterServiceCollector"
)

// ScyllaClusterServiceResult holds metadata about collected Service manifests for a ScyllaCluster.
type ScyllaClusterServiceResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterServiceResult is the typed accessor for ScyllaClusterServiceCollector results.
func GetScyllaClusterServiceResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterServiceResult, error) {
	result, ok := vitals.Get(ScyllaClusterServiceCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterServiceCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterServiceCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterServiceResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterServiceCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterServiceCollector collects Service manifests owned by a ScyllaCluster.
type scyllaClusterServiceCollector struct{}

var _ engine.Collector = (*scyllaClusterServiceCollector)(nil)

// NewScyllaClusterServiceCollector creates a new ScyllaClusterServiceCollector.
func NewScyllaClusterServiceCollector() engine.Collector {
	return &scyllaClusterServiceCollector{}
}

func (c *scyllaClusterServiceCollector) ID() engine.CollectorID {
	return ScyllaClusterServiceCollectorID
}
func (c *scyllaClusterServiceCollector) Name() string                    { return "ScyllaCluster Service manifests" }
func (c *scyllaClusterServiceCollector) Scope() engine.CollectorScope    { return engine.PerScyllaCluster }
func (c *scyllaClusterServiceCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: services — get, list
func (c *scyllaClusterServiceCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"services"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterServiceCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	sc := params.ScyllaCluster
	selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})

	services, err := params.ResourceLister.ListServices(ctx, sc.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("listing services in namespace %s: %w", sc.Namespace, err)
	}

	var artifacts []engine.Artifact
	for i := range services {
		svc := &services[i]
		if params.ArtifactWriter != nil {
			data, err := yaml.Marshal(svc)
			if err != nil {
				return nil, fmt.Errorf("marshaling service %s/%s: %w", svc.Namespace, svc.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(svc.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for service %s/%s: %w", svc.Namespace, svc.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("Service %s/%s manifest", svc.Namespace, svc.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Service(s) for ScyllaCluster %s/%s", len(services), sc.Namespace, sc.Name),
		Data:      &ScyllaClusterServiceResult{Count: len(services)},
		Artifacts: artifacts,
	}, nil
}
