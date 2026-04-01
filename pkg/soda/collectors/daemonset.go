package collectors

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	return engine.GetResult[DaemonSetResult](vitals, DaemonSetCollectorID, scopeKey)
}

// daemonSetCollector collects DaemonSet manifests from operator namespaces.
type daemonSetCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*daemonSetCollector)(nil)

// NewDaemonSetCollector creates a new DaemonSetCollector.
func NewDaemonSetCollector() engine.ClusterWideCollector {
	return &daemonSetCollector{
		CollectorBase: engine.NewCollectorBase(DaemonSetCollectorID, "DaemonSet manifests", engine.ClusterWide, nil),
	}
}

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

func (c *daemonSetCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		daemonSets, err := params.ResourceLister.ListDaemonSets(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing daemonsets in namespace %s: %w", ns, err)
		}
		total += len(daemonSets)
		artifacts = append(artifacts, collectAndWriteManifests(params.ArtifactWriter, daemonSets,
			func(ds *appsv1.DaemonSet) string { return filepath.Join(ds.Namespace, ds.Name+".yaml") },
			func(ds *appsv1.DaemonSet) string {
				return fmt.Sprintf("DaemonSet %s/%s manifest", ds.Namespace, ds.Name)
			},
		)...)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d DaemonSet(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &DaemonSetResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
