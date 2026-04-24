package engine

import (
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

// plainCollector is a minimal CollectorMeta that does not implement RBACProvider.
type plainCollector struct {
	id    CollectorID
	scope CollectorScope
}

func (s *plainCollector) ID() CollectorID          { return s.id }
func (s *plainCollector) Name() string             { return string(s.id) }
func (s *plainCollector) Description() string      { return "" }
func (s *plainCollector) Scope() CollectorScope    { return s.scope }
func (s *plainCollector) DependsOn() []CollectorID { return nil }

// rbacCollector is a CollectorMeta that also implements RBACProvider.
type rbacCollector struct {
	plainCollector
	rules []rbacv1.PolicyRule
}

func (r *rbacCollector) RBAC() []rbacv1.PolicyRule { return r.rules }

// Compile-time check.
var _ RBACProvider = (*rbacCollector)(nil)

func TestAggregateRBAC_EmptyCollectorList(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
			},
		},
	}

	got := AggregateRBAC(nil, allCollectors)
	if len(got) != 0 {
		t.Errorf("expected no rules for empty collector list, got %d", len(got))
	}
}

func TestAggregateRBAC_CollectorsWithoutRBACProvider(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &plainCollector{id: "c1", scope: ClusterWide},
		"c2": &plainCollector{id: "c2", scope: PerScyllaNode},
	}

	got := AggregateRBAC([]CollectorID{"c1", "c2"}, allCollectors)
	if len(got) != 0 {
		t.Errorf("expected no rules when no collector implements RBACProvider, got %d", len(got))
	}
}

func TestAggregateRBAC_SingleCollector(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list"}},
			},
		},
	}

	got := AggregateRBAC([]CollectorID{"c1"}, allCollectors)
	want := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAggregateRBAC_MergesVerbsForSameGroupAndResource(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
			},
		},
		"c2": &rbacCollector{
			plainCollector: plainCollector{id: "c2", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list", "watch"}},
			},
		},
	}

	got := AggregateRBAC([]CollectorID{"c1", "c2"}, allCollectors)
	want := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAggregateRBAC_DeduplicatesSameVerbFromMultipleCollectors(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
			},
		},
		"c2": &rbacCollector{
			plainCollector: plainCollector{id: "c2", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
			},
		},
		"c3": &rbacCollector{
			plainCollector: plainCollector{id: "c3", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
			},
		},
	}

	got := AggregateRBAC([]CollectorID{"c1", "c2", "c3"}, allCollectors)
	want := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAggregateRBAC_SortsCoreGroupFirst(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"scylla.scylladb.com"}, Resources: []string{"scyllaclusters"}, Verbs: []string{"get"}},
			},
		},
		"c2": &rbacCollector{
			plainCollector: plainCollector{id: "c2", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
			},
		},
		"c3": &rbacCollector{
			plainCollector: plainCollector{id: "c3", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"list"}},
			},
		},
	}

	got := AggregateRBAC([]CollectorID{"c1", "c2", "c3"}, allCollectors)

	// Core "" should come first, then "apps", then "scylla.scylladb.com".
	if len(got) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(got))
	}
	if got[0].APIGroups[0] != "" {
		t.Errorf("first rule should be core group, got %q", got[0].APIGroups[0])
	}
	if got[1].APIGroups[0] != "apps" {
		t.Errorf("second rule should be apps group, got %q", got[1].APIGroups[0])
	}
	if got[2].APIGroups[0] != "scylla.scylladb.com" {
		t.Errorf("third rule should be scylla group, got %q", got[2].APIGroups[0])
	}
}

func TestAggregateRBAC_SortsResourcesWithinSameGroup(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods/log"}, Verbs: []string{"get"}},
				{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
				{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
			},
		},
	}

	got := AggregateRBAC([]CollectorID{"c1"}, allCollectors)

	if len(got) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(got))
	}
	wantResources := []string{"nodes", "pods/exec", "pods/log"}
	for i, want := range wantResources {
		if got[i].Resources[0] != want {
			t.Errorf("rule[%d] resource = %q, want %q", i, got[i].Resources[0], want)
		}
	}
}

func TestAggregateRBAC_MixedRBACAndNonRBACCollectors(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &plainCollector{id: "c1", scope: ClusterWide},
		"c2": &rbacCollector{
			plainCollector: plainCollector{id: "c2", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
			},
		},
		"c3": &plainCollector{id: "c3", scope: PerScyllaNode},
	}

	got := AggregateRBAC([]CollectorID{"c1", "c2", "c3"}, allCollectors)
	if len(got) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got))
	}
	want := rbacv1.PolicyRule{
		APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"},
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %v, want %v", got[0], want)
	}
}

func TestAggregateRBAC_UnknownCollectorIDIsSkipped(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
			},
		},
	}

	// "unknown" is not in the map — should be silently skipped.
	got := AggregateRBAC([]CollectorID{"unknown", "c1"}, allCollectors)
	if len(got) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got))
	}
}

func TestAggregateRBAC_MultipleRulesFromSingleCollector(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
				{APIGroups: []string{""}, Resources: []string{"pods/log"}, Verbs: []string{"get"}},
			},
		},
	}

	got := AggregateRBAC([]CollectorID{"c1"}, allCollectors)
	want := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{""}, Resources: []string{"pods/log"}, Verbs: []string{"get"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAggregateRBAC_DeterministicOutput(t *testing.T) {
	allCollectors := map[CollectorID]CollectorMeta{
		"c1": &rbacCollector{
			plainCollector: plainCollector{id: "c1", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"scylla.scylladb.com"}, Resources: []string{"scyllaclusters"}, Verbs: []string{"list"}},
			},
		},
		"c2": &rbacCollector{
			plainCollector: plainCollector{id: "c2", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
			},
		},
		"c3": &rbacCollector{
			plainCollector: plainCollector{id: "c3", scope: PerScyllaNode},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
			},
		},
		"c4": &rbacCollector{
			plainCollector: plainCollector{id: "c4", scope: ClusterWide},
			rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"scylla.scylladb.com"}, Resources: []string{"scyllaclusters"}, Verbs: []string{"get"}},
			},
		},
	}

	ids := []CollectorID{"c1", "c2", "c3", "c4"}

	// Run multiple times and verify identical output.
	first := AggregateRBAC(ids, allCollectors)
	for i := 0; i < 20; i++ {
		got := AggregateRBAC(ids, allCollectors)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("non-deterministic output on iteration %d:\n  first: %v\n  got:   %v", i, first, got)
		}
	}

	// Verify the merge happened: c1 and c4 both target scyllaclusters.
	if len(first) != 3 {
		t.Fatalf("expected 3 rules, got %d: %v", len(first), first)
	}
	// Find the scyllaclusters rule and verify merged verbs.
	for _, rule := range first {
		if len(rule.Resources) > 0 && rule.Resources[0] == "scyllaclusters" {
			wantVerbs := []string{"get", "list"}
			if !reflect.DeepEqual(rule.Verbs, wantVerbs) {
				t.Errorf("scyllaclusters verbs = %v, want %v", rule.Verbs, wantVerbs)
			}
		}
	}
}
