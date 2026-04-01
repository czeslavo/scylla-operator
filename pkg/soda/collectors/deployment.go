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
	// DeploymentCollectorID is the unique identifier for the DeploymentCollector.
	DeploymentCollectorID engine.CollectorID = "DeploymentCollector"
)

// DeploymentResult holds metadata about collected Deployment manifests.
type DeploymentResult struct {
	Count int `json:"count"`
}

// GetDeploymentResult is the typed accessor for DeploymentCollector results.
func GetDeploymentResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*DeploymentResult, error) {
	return engine.GetResult[DeploymentResult](vitals, DeploymentCollectorID, scopeKey)
}

// deploymentCollector collects Deployment manifests from operator namespaces.
type deploymentCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*deploymentCollector)(nil)

// NewDeploymentCollector creates a new DeploymentCollector.
func NewDeploymentCollector() engine.ClusterWideCollector {
	return &deploymentCollector{
		CollectorBase: engine.NewCollectorBase(DeploymentCollectorID, "Deployment manifests", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - apps/v1: deployments — get, list
func (c *deploymentCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *deploymentCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		deployments, err := params.ResourceLister.ListDeployments(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing deployments in namespace %s: %w", ns, err)
		}
		for i := range deployments {
			d := &deployments[i]
			total++
			if params.ArtifactWriter != nil {
				data, err := yaml.Marshal(d)
				if err != nil {
					return nil, fmt.Errorf("marshaling deployment %s/%s: %w", d.Namespace, d.Name, err)
				}
				filename := filepath.Join(d.Namespace, d.Name+".yaml")
				relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
				if err != nil {
					return nil, fmt.Errorf("writing artifact for deployment %s/%s: %w", d.Namespace, d.Name, err)
				}
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("Deployment %s/%s manifest", d.Namespace, d.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d Deployment(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &DeploymentResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
