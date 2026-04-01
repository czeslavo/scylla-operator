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
	// ServiceAccountCollectorID is the unique identifier for the ServiceAccountCollector.
	ServiceAccountCollectorID engine.CollectorID = "ServiceAccountCollector"
)

// ServiceAccountResult holds metadata about collected ServiceAccount manifests.
type ServiceAccountResult struct {
	Count int `json:"count"`
}

// GetServiceAccountResult is the typed accessor for ServiceAccountCollector results.
func GetServiceAccountResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ServiceAccountResult, error) {
	return engine.GetResult[ServiceAccountResult](vitals, ServiceAccountCollectorID, scopeKey)
}

// serviceAccountCollector collects ServiceAccount manifests from operator namespaces.
type serviceAccountCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*serviceAccountCollector)(nil)

// NewServiceAccountCollector creates a new ServiceAccountCollector.
func NewServiceAccountCollector() engine.ClusterWideCollector {
	return &serviceAccountCollector{
		CollectorBase: engine.NewCollectorBase(ServiceAccountCollectorID, "ServiceAccount manifests", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: serviceaccounts — get, list
func (c *serviceAccountCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"serviceaccounts"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *serviceAccountCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		serviceAccounts, err := params.ResourceLister.ListServiceAccounts(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing serviceaccounts in namespace %s: %w", ns, err)
		}
		for i := range serviceAccounts {
			sa := &serviceAccounts[i]
			total++
			if params.ArtifactWriter != nil {
				data, err := yaml.Marshal(sa)
				if err != nil {
					return nil, fmt.Errorf("marshaling serviceaccount %s/%s: %w", sa.Namespace, sa.Name, err)
				}
				filename := filepath.Join(sa.Namespace, sa.Name+".yaml")
				relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
				if err != nil {
					return nil, fmt.Errorf("writing artifact for serviceaccount %s/%s: %w", sa.Namespace, sa.Name, err)
				}
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("ServiceAccount %s/%s manifest", sa.Namespace, sa.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ServiceAccount(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &ServiceAccountResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
