package collectors

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const (
	// ScyllaDBDatacenterCollectorID is the unique identifier for the ScyllaDBDatacenterCollector.
	ScyllaDBDatacenterCollectorID engine.CollectorID = "ScyllaDBDatacenterCollector"
)

// ScyllaDBDatacenterResult holds metadata about collected ScyllaDBDatacenter manifests.
type ScyllaDBDatacenterResult struct {
	Count int `json:"count"`
}

// GetScyllaDBDatacenterResult is the typed accessor for ScyllaDBDatacenterCollector results.
func GetScyllaDBDatacenterResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaDBDatacenterResult, error) {
	result, ok := vitals.Get(ScyllaDBDatacenterCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaDBDatacenterCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaDBDatacenterCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaDBDatacenterResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaDBDatacenterCollector", result.Data)
	}
	return typed, nil
}

// scyllaDBDatacenterCollector collects ScyllaDBDatacenter manifests across all namespaces.
type scyllaDBDatacenterCollector struct{}

var _ engine.Collector = (*scyllaDBDatacenterCollector)(nil)

// NewScyllaDBDatacenterCollector creates a new ScyllaDBDatacenterCollector.
func NewScyllaDBDatacenterCollector() engine.Collector {
	return &scyllaDBDatacenterCollector{}
}

func (c *scyllaDBDatacenterCollector) ID() engine.CollectorID          { return ScyllaDBDatacenterCollectorID }
func (c *scyllaDBDatacenterCollector) Name() string                    { return "ScyllaDBDatacenter manifests" }
func (c *scyllaDBDatacenterCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *scyllaDBDatacenterCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - scylla.scylladb.com/v1alpha1: scylladbdatacenters — get, list
func (c *scyllaDBDatacenterCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"scylla.scylladb.com"},
			Resources: []string{"scylladbdatacenters"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaDBDatacenterCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	datacenters, err := params.ResourceLister.ListScyllaDBDatacenters(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("listing scylladbdatacenters: %w", err)
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		for _, sdc := range datacenters {
			data, err := yaml.Marshal(sdc)
			if err != nil {
				return nil, fmt.Errorf("marshaling scylladbdatacenter %s/%s: %w", sdc.Namespace, sdc.Name, err)
			}
			filename := filepath.Join(sdc.Namespace, sdc.Name+".yaml")
			relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for scylladbdatacenter %s/%s: %w", sdc.Namespace, sdc.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("ScyllaDBDatacenter %s/%s manifest", sdc.Namespace, sdc.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ScyllaDBDatacenter(s)", len(datacenters)),
		Data:      &ScyllaDBDatacenterResult{Count: len(datacenters)},
		Artifacts: artifacts,
	}, nil
}
