package collectors

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

const (
	// ServiceCollectorID is the unique identifier for the ServiceCollector.
	ServiceCollectorID engine.CollectorID = "ServiceCollector"
)

// ServiceResult holds metadata about collected Service manifests.
type ServiceResult struct {
	Count int `json:"count"`
}

// GetServiceResult is the typed accessor for ServiceCollector results.
func GetServiceResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ServiceResult, error) {
	result, ok := vitals.Get(ServiceCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ServiceCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ServiceCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ServiceResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ServiceCollector", result.Data)
	}
	return typed, nil
}

// serviceCollector collects Service manifests from operator namespaces.
type serviceCollector struct{}

var _ engine.Collector = (*serviceCollector)(nil)

// NewServiceCollector creates a new ServiceCollector.
func NewServiceCollector() engine.Collector {
	return &serviceCollector{}
}

func (c *serviceCollector) ID() engine.CollectorID          { return ServiceCollectorID }
func (c *serviceCollector) Name() string                    { return "Service manifests" }
func (c *serviceCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *serviceCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: services — get, list
func (c *serviceCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"services"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *serviceCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		services, err := params.ResourceLister.ListServices(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing services in namespace %s: %w", ns, err)
		}
		for i := range services {
			svc := &services[i]
			total++
			if params.ArtifactWriter != nil {
				data, err := yaml.Marshal(svc)
				if err != nil {
					return nil, fmt.Errorf("marshaling service %s/%s: %w", svc.Namespace, svc.Name, err)
				}
				filename := filepath.Join(svc.Namespace, svc.Name+".yaml")
				relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
				if err != nil {
					return nil, fmt.Errorf("writing artifact for service %s/%s: %w", svc.Namespace, svc.Name, err)
				}
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("Service %s/%s manifest", svc.Namespace, svc.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Service(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &ServiceResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
