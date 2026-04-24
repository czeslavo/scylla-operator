package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// ScyllaOperatorConfigCollectorID is the unique identifier for the ScyllaOperatorConfigCollector.
	ScyllaOperatorConfigCollectorID engine.CollectorID = "ScyllaOperatorConfigCollector"
)

// ScyllaOperatorConfigResult holds metadata about collected ScyllaOperatorConfig manifests.
type ScyllaOperatorConfigResult struct {
	Count int `json:"count"`
}

// GetScyllaOperatorConfigResult is the typed accessor for ScyllaOperatorConfigCollector results.
func GetScyllaOperatorConfigResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaOperatorConfigResult, error) {
	return engine.GetResult[ScyllaOperatorConfigResult](vitals, ScyllaOperatorConfigCollectorID, scopeKey)
}

// scyllaOperatorConfigCollector collects ScyllaOperatorConfig manifests.
type scyllaOperatorConfigCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*scyllaOperatorConfigCollector)(nil)

// NewScyllaOperatorConfigCollector creates a new ScyllaOperatorConfigCollector.
func NewScyllaOperatorConfigCollector() engine.ClusterWideCollector {
	return &scyllaOperatorConfigCollector{
		CollectorBase: engine.NewCollectorBase(ScyllaOperatorConfigCollectorID, "ScyllaOperatorConfig manifests", "Collects ScyllaOperatorConfig custom resource manifests.", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - scylla.scylladb.com/v1alpha1: scyllaoperatorconfigs — get, list
func (c *scyllaOperatorConfigCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"scylla.scylladb.com"},
			Resources: []string{"scyllaoperatorconfigs"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaOperatorConfigCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	configs, err := params.ResourceLister.ListScyllaOperatorConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing scyllaoperatorconfigs: %w", err)
	}

	var artifacts []engine.Artifact
	for _, cfg := range configs {
		marshalAndWriteYAML(params.ArtifactWriter, cfg.Name+".yaml",
			fmt.Sprintf("ScyllaOperatorConfig %s manifest", cfg.Name), cfg, &artifacts)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ScyllaOperatorConfig(s)", len(configs)),
		Data:      &ScyllaOperatorConfigResult{Count: len(configs)},
		Artifacts: artifacts,
	}, nil
}
