package collectors

import (
	"context"
	"fmt"
	"path/filepath"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	// ScyllaClusterCollectorID is the unique identifier for the ScyllaClusterCollector.
	ScyllaClusterCollectorID engine.CollectorID = "ScyllaClusterCollector"
)

// ScyllaClusterResult holds metadata about collected ScyllaCluster manifests.
type ScyllaClusterResult struct {
	Count int `json:"count"`
}

// GetScyllaClusterResult is the typed accessor for ScyllaClusterCollector results.
func GetScyllaClusterResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterResult, error) {
	result, ok := vitals.Get(ScyllaClusterCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterCollector", result.Data)
	}
	return typed, nil
}

// scyllaClusterCollector collects ScyllaCluster manifests across all namespaces.
type scyllaClusterCollector struct{}

var _ engine.Collector = (*scyllaClusterCollector)(nil)

// NewScyllaClusterCollector creates a new ScyllaClusterCollector.
func NewScyllaClusterCollector() engine.Collector {
	return &scyllaClusterCollector{}
}

func (c *scyllaClusterCollector) ID() engine.CollectorID          { return ScyllaClusterCollectorID }
func (c *scyllaClusterCollector) Name() string                    { return "ScyllaCluster manifests" }
func (c *scyllaClusterCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *scyllaClusterCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - scylla.scylladb.com/v1: scyllaclusters — get, list
func (c *scyllaClusterCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"scylla.scylladb.com"},
			Resources: []string{"scyllaclusters"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *scyllaClusterCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	clusterInfos, err := params.ResourceLister.ListScyllaClusters(ctx, metav1.NamespaceAll)
	if err != nil {
		return nil, fmt.Errorf("listing scyllaclusters: %w", err)
	}

	// Filter to only ScyllaCluster kind (not ScyllaDBDatacenter which ListScyllaClusters also returns).
	var clusters []*scyllav1.ScyllaCluster
	for _, info := range clusterInfos {
		if sc, ok := info.Object.(*scyllav1.ScyllaCluster); ok {
			clusters = append(clusters, sc)
		}
	}

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		for _, sc := range clusters {
			data, err := yaml.Marshal(sc)
			if err != nil {
				return nil, fmt.Errorf("marshaling scyllacluster %s/%s: %w", sc.Namespace, sc.Name, err)
			}
			filename := filepath.Join(sc.Namespace, sc.Name+".yaml")
			relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
			if err != nil {
				return nil, fmt.Errorf("writing artifact for scyllacluster %s/%s: %w", sc.Namespace, sc.Name, err)
			}
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: relPath,
				Description:  fmt.Sprintf("ScyllaCluster %s/%s manifest", sc.Namespace, sc.Name),
			})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d ScyllaCluster(s)", len(clusters)),
		Data:      &ScyllaClusterResult{Count: len(clusters)},
		Artifacts: artifacts,
	}, nil
}
