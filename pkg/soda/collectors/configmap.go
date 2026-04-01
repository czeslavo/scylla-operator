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
	// ConfigMapCollectorID is the unique identifier for the ConfigMapCollector.
	ConfigMapCollectorID engine.CollectorID = "ConfigMapCollector"
)

// ConfigMapResult holds metadata about collected ConfigMap manifests.
type ConfigMapResult struct {
	Count int `json:"count"`
}

// GetConfigMapResult is the typed accessor for ConfigMapCollector results.
func GetConfigMapResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ConfigMapResult, error) {
	return engine.GetResult[ConfigMapResult](vitals, ConfigMapCollectorID, scopeKey)
}

// configMapCollector collects ConfigMap manifests from operator namespaces.
type configMapCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*configMapCollector)(nil)

// NewConfigMapCollector creates a new ConfigMapCollector.
func NewConfigMapCollector() engine.ClusterWideCollector {
	return &configMapCollector{
		CollectorBase: engine.NewCollectorBase(ConfigMapCollectorID, "ConfigMap manifests", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: configmaps — get, list
func (c *configMapCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *configMapCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		configMaps, err := params.ResourceLister.ListConfigMaps(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing configmaps in namespace %s: %w", ns, err)
		}
		for i := range configMaps {
			cm := &configMaps[i]
			total++
			if params.ArtifactWriter != nil {
				data, err := yaml.Marshal(cm)
				if err != nil {
					return nil, fmt.Errorf("marshaling configmap %s/%s: %w", cm.Namespace, cm.Name, err)
				}
				filename := filepath.Join(cm.Namespace, cm.Name+".yaml")
				relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
				if err != nil {
					return nil, fmt.Errorf("writing artifact for configmap %s/%s: %w", cm.Namespace, cm.Name, err)
				}
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("ConfigMap %s/%s manifest", cm.Namespace, cm.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ConfigMap(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &ConfigMapResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
