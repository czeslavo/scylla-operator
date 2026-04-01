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
	// StatefulSetCollectorID is the unique identifier for the StatefulSetCollector.
	StatefulSetCollectorID engine.CollectorID = "StatefulSetCollector"
)

// StatefulSetResult holds metadata about collected StatefulSet manifests.
type StatefulSetResult struct {
	Count int `json:"count"`
}

// GetStatefulSetResult is the typed accessor for StatefulSetCollector results.
func GetStatefulSetResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*StatefulSetResult, error) {
	return engine.GetResult[StatefulSetResult](vitals, StatefulSetCollectorID, scopeKey)
}

// statefulSetCollector collects StatefulSet manifests from operator namespaces.
type statefulSetCollector struct {
	engine.CollectorBase
}

var _ engine.ClusterWideCollector = (*statefulSetCollector)(nil)

// NewStatefulSetCollector creates a new StatefulSetCollector.
func NewStatefulSetCollector() engine.ClusterWideCollector {
	return &statefulSetCollector{
		CollectorBase: engine.NewCollectorBase(StatefulSetCollectorID, "StatefulSet manifests", engine.ClusterWide, nil),
	}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - apps/v1: statefulsets — get, list
func (c *statefulSetCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"apps"},
			Resources: []string{"statefulsets"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func (c *statefulSetCollector) CollectClusterWide(ctx context.Context, params engine.ClusterWideCollectorParams) (*engine.CollectorResult, error) {
	var artifacts []engine.Artifact
	total := 0

	for _, ns := range operatorNamespaces {
		statefulSets, err := params.ResourceLister.ListStatefulSets(ctx, ns, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("listing statefulsets in namespace %s: %w", ns, err)
		}
		for i := range statefulSets {
			ss := &statefulSets[i]
			total++
			if params.ArtifactWriter != nil {
				data, err := yaml.Marshal(ss)
				if err != nil {
					return nil, fmt.Errorf("marshaling statefulset %s/%s: %w", ss.Namespace, ss.Name, err)
				}
				filename := filepath.Join(ss.Namespace, ss.Name+".yaml")
				relPath, err := params.ArtifactWriter.WriteArtifact(filename, data)
				if err != nil {
					return nil, fmt.Errorf("writing artifact for statefulset %s/%s: %w", ss.Namespace, ss.Name, err)
				}
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("StatefulSet %s/%s manifest", ss.Namespace, ss.Name),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Message:   fmt.Sprintf("Collected %d StatefulSet(s) across %d namespace(s)", total, len(operatorNamespaces)),
		Data:      &StatefulSetResult{Count: total},
		Artifacts: artifacts,
	}, nil
}
