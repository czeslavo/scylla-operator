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
		total += len(services)
		artifacts = append(artifacts, collectAndWriteManifests(params.ArtifactWriter, services,
			func(svc *corev1.Service) string { return filepath.Join(svc.Namespace, svc.Name+".yaml") },
			func(svc *corev1.Service) string {
				return fmt.Sprintf("Service %s/%s manifest", svc.Namespace, svc.Name)
			},
		)...)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Service(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &ServiceResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
