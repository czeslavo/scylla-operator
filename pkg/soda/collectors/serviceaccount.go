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
	result, ok := vitals.Get(ServiceAccountCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ServiceAccountCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ServiceAccountCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ServiceAccountResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ServiceAccountCollector", result.Data)
	}
	return typed, nil
}

// serviceAccountCollector collects ServiceAccount manifests from operator namespaces.
type serviceAccountCollector struct{}

var _ engine.Collector = (*serviceAccountCollector)(nil)

// NewServiceAccountCollector creates a new ServiceAccountCollector.
func NewServiceAccountCollector() engine.Collector {
	return &serviceAccountCollector{}
}

func (c *serviceAccountCollector) ID() engine.CollectorID          { return ServiceAccountCollectorID }
func (c *serviceAccountCollector) Name() string                    { return "ServiceAccount manifests" }
func (c *serviceAccountCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *serviceAccountCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *serviceAccountCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
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
