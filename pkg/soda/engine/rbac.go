package engine

import (
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

// AggregateRBAC collects and merges RBAC PolicyRules from all resolved
// collectors that implement RBACProvider. Rules that share the same set of
// API groups and resources are merged by unioning their verbs. The returned
// slice is sorted deterministically: by first API group (core "" sorts first),
// then by first resource, then by verb list.
func AggregateRBAC(collectorIDs []CollectorID, allCollectors map[CollectorID]CollectorMeta) []rbacv1.PolicyRule {
	// mergeKey → accumulated verb set.
	type ruleAccumulator struct {
		apiGroups []string
		resources []string
		verbs     map[string]bool
	}

	merged := make(map[string]*ruleAccumulator)
	// Maintain insertion order for deterministic iteration before final sort.
	var mergeKeys []string

	for _, id := range collectorIDs {
		collector, ok := allCollectors[id]
		if !ok {
			continue
		}
		rbacProvider, ok := collector.(RBACProvider)
		if !ok {
			continue
		}
		for _, rule := range rbacProvider.RBAC() {
			key := makeMergeKey(rule.APIGroups, rule.Resources)
			acc, exists := merged[key]
			if !exists {
				acc = &ruleAccumulator{
					apiGroups: rule.APIGroups,
					resources: rule.Resources,
					verbs:     make(map[string]bool),
				}
				merged[key] = acc
				mergeKeys = append(mergeKeys, key)
			}
			for _, v := range rule.Verbs {
				acc.verbs[v] = true
			}
		}
	}

	rules := make([]rbacv1.PolicyRule, 0, len(merged))
	for _, key := range mergeKeys {
		acc := merged[key]
		verbs := sortedStringSet(acc.verbs)
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: acc.apiGroups,
			Resources: acc.resources,
			Verbs:     verbs,
		})
	}

	sort.Slice(rules, func(i, j int) bool {
		return comparePolicyRules(rules[i], rules[j]) < 0
	})

	return rules
}

// makeMergeKey builds a deterministic string key from sorted API groups and
// resources so that rules targeting the same (groups, resources) pair merge.
func makeMergeKey(apiGroups, resources []string) string {
	sortedGroups := make([]string, len(apiGroups))
	copy(sortedGroups, apiGroups)
	sort.Strings(sortedGroups)

	sortedResources := make([]string, len(resources))
	copy(sortedResources, resources)
	sort.Strings(sortedResources)

	return strings.Join(sortedGroups, ",") + "|" + strings.Join(sortedResources, ",")
}

// comparePolicyRules returns a negative value if a sorts before b, zero if
// equal, and positive if a sorts after b. Core API group ("") sorts before
// any named group.
func comparePolicyRules(a, b rbacv1.PolicyRule) int {
	ga := firstOrEmpty(a.APIGroups)
	gb := firstOrEmpty(b.APIGroups)
	if cmp := compareAPIGroups(ga, gb); cmp != 0 {
		return cmp
	}
	ra := firstOrEmpty(a.Resources)
	rb := firstOrEmpty(b.Resources)
	if ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	va := strings.Join(a.Verbs, ",")
	vb := strings.Join(b.Verbs, ",")
	if va < vb {
		return -1
	}
	if va > vb {
		return 1
	}
	return 0
}

// compareAPIGroups compares two API group strings. The core group ("") sorts
// before any named group; otherwise lexicographic.
func compareAPIGroups(a, b string) int {
	if a == b {
		return 0
	}
	// Core group sorts first.
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	if a < b {
		return -1
	}
	return 1
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func sortedStringSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
