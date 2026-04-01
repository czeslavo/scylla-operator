package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
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
	result, ok := vitals.Get(ScyllaOperatorConfigCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaOperatorConfigCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaOperatorConfigCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaOperatorConfigResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaOperatorConfigCollector", result.Data)
	}
	return typed, nil
}

// scyllaOperatorConfigCollector collects ScyllaOperatorConfig manifests.
type scyllaOperatorConfigCollector struct{}

var _ engine.Collector = (*scyllaOperatorConfigCollector)(nil)

// NewScyllaOperatorConfigCollector creates a new ScyllaOperatorConfigCollector.
func NewScyllaOperatorConfigCollector() engine.Collector {
	return &scyllaOperatorConfigCollector{}
}

func (c *scyllaOperatorConfigCollector) ID() engine.CollectorID {
	return ScyllaOperatorConfigCollectorID
}
func (c *scyllaOperatorConfigCollector) Name() string                    { return "ScyllaOperatorConfig manifests" }
func (c *scyllaOperatorConfigCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *scyllaOperatorConfigCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *scyllaOperatorConfigCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	configs, err := params.ResourceLister.ListScyllaOperatorConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing scyllaoperatorconfigs: %w", err)
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		for _, cfg := range configs {
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return nil, fmt.Errorf("marshaling scyllaoperatorconfig %s: %w", cfg.Name, err)
			}
			relPath, err := params.ArtifactWriter.WriteArtifact(cfg.Name+".yaml", data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for scyllaoperatorconfig %s: %w", cfg.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("ScyllaOperatorConfig %s manifest", cfg.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ScyllaOperatorConfig(s)", len(configs)),
		Data:      &ScyllaOperatorConfigResult{Count: len(configs)},
		Artifacts: artifacts,
	}, nil
}
