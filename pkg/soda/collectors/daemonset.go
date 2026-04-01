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
	// DaemonSetCollectorID is the unique identifier for the DaemonSetCollector.
	DaemonSetCollectorID engine.CollectorID = "DaemonSetCollector"
)

// DaemonSetResult holds metadata about collected DaemonSet manifests.
type DaemonSetResult struct {
	Count int `json:"count"`
}

// GetDaemonSetResult is the typed accessor for DaemonSetCollector results.
func GetDaemonSetResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*DaemonSetResult, error) {
	result, ok := vitals.Get(DaemonSetCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("DaemonSetCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("DaemonSetCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*DaemonSetResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for DaemonSetCollector", result.Data)
	}
	return typed, nil
}

// daemonSetCollector collects DaemonSet manifests from operator namespaces.
type daemonSetCollector struct{}

var _ engine.Collector = (*daemonSetCollector)(nil)

// NewDaemonSetCollector creates a new DaemonSetCollector.
func NewDaemonSetCollector() engine.Collector {
	return &daemonSetCollector{}
}

func (c *daemonSetCollector) ID() engine.CollectorID          { return DaemonSetCollectorID }
func (c *daemonSetCollector) Name() string                    { return "DaemonSet manifests" }
func (c *daemonSetCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *daemonSetCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - apps/v1: daemonsets — get, list
func (c *daemonSetCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"apps"},
			Resources: []string{"daemonsets"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *daemonSetCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		daemonSets, err := params.ResourceLister.ListDaemonSets(ctx, ns)
		if err != nil {
			return nil, fmt.Errorf("listing daemonsets in namespace %s: %w", ns, err)
		}
		for i := range daemonSets {
			ds := &daemonSets[i]
			total++
			if params.ArtifactWriter != nil {
				data, err := yaml.Marshal(ds)
				if err != nil {
					return nil, fmt.Errorf("marshaling daemonset %s/%s: %w", ds.Namespace, ds.Name, err)
				}
				filename := filepath.Join(ds.Namespace, ds.Name+".yaml")
				relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
				if err != nil {
					return nil, fmt.Errorf("writing artifact for daemonset %s/%s: %w", ds.Namespace, ds.Name, err)
				}
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("DaemonSet %s/%s manifest", ds.Namespace, ds.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d DaemonSet(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &DaemonSetResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
