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
	return engine.GetResult[ServiceResult](vitals, ServiceCollectorID, scopeKey)
}

// serviceCollector collects Service manifests from operator namespaces.
type serviceCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*serviceCollector)(nil)

// NewServiceCollector creates a new ServiceCollector.
func NewServiceCollector() engine.ClusterWideCollector {
	return &serviceCollector{
		CollectorBase: engine.NewCollectorBase(ServiceCollectorID, "Service manifests", engine.ClusterWide, nil),
	}
}

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

func (c *serviceCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
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
