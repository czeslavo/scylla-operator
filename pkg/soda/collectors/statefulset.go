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
	result, ok := vitals.Get(StatefulSetCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("StatefulSetCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("StatefulSetCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*StatefulSetResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for StatefulSetCollector", result.Data)
	}
	return typed, nil
}

// statefulSetCollector collects StatefulSet manifests from operator namespaces.
type statefulSetCollector struct{}

var _ engine.Collector = (*statefulSetCollector)(nil)

// NewStatefulSetCollector creates a new StatefulSetCollector.
func NewStatefulSetCollector() engine.Collector {
	return &statefulSetCollector{}
}

func (c *statefulSetCollector) ID() engine.CollectorID          { return StatefulSetCollectorID }
func (c *statefulSetCollector) Name() string                    { return "StatefulSet manifests" }
func (c *statefulSetCollector) Scope() engine.CollectorScope    { return engine.ClusterWide }
func (c *statefulSetCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *statefulSetCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
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
