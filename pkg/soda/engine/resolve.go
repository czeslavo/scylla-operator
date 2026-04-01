package engine

import (
	"fmt"
	"sort"
)

// ResolveProfile resolves a profile name into the final sets of collector and
// analyzer IDs, applying enable/disable overrides and computing the transitive
// collector dependency closure.
//
// Steps:
//  1. Flatten the profile's Includes recursively (detect cycles via visited set).
//  2. Merge all Analyzers and explicit Collectors from the flattened profile set.
//  3. Add enable items, remove disable items (for analyzers).
//  4. Validate all analyzer IDs exist in the registry.
//  5. For each analyzer, walk DependsOn() → collector IDs.
//  6. For each collector, walk its DependsOn() → more collector IDs (transitive closure).
//  7. Merge explicitly listed collectors (+ their transitive deps) into the closure.
//  8. Validate cross-scope dependency constraints.
//  9. Return deduplicated, sorted lists.
func ResolveProfile(
	profileName string,
	allProfiles map[string]Profile,
	enable []AnalyzerID,
	disable []AnalyzerID,
	allAnalyzers map[AnalyzerID]Analyzer,
	allCollectors map[CollectorID]Collector,
) (resolvedCollectors []CollectorID, resolvedAnalyzers []AnalyzerID, err error) {
	// Step 1+2: Flatten profile includes and merge analyzers + explicit collectors.
	analyzerSet, explicitCollectorSet, err := flattenProfile(profileName, allProfiles, make(map[string]bool))
	if err != nil {
		return nil, nil, fmt.Errorf("resolving profile %q: %w", profileName, err)
	}

	// Step 3: Apply enable/disable overrides (analyzers only).
	for _, id := range enable {
		analyzerSet[id] = true
	}
	for _, id := range disable {
		delete(analyzerSet, id)
	}

	// Step 4: Validate all analyzer IDs exist.
	for id := range analyzerSet {
		if _, ok := allAnalyzers[id]; !ok {
			return nil, nil, fmt.Errorf("unknown analyzer ID %q", id)
		}
	}

	// Step 5+6: Compute transitive collector closure from analyzer dependencies.
	collectorSet := make(map[CollectorID]bool)
	for analyzerID := range analyzerSet {
		analyzer := allAnalyzers[analyzerID]
		for _, collectorID := range analyzer.DependsOn() {
			if err := resolveCollectorDeps(collectorID, allCollectors, collectorSet, make(map[CollectorID]bool)); err != nil {
				return nil, nil, fmt.Errorf("resolving dependencies for analyzer %q: %w", analyzerID, err)
			}
		}
	}

	// Step 7: Validate and merge explicitly listed collectors (+ their transitive deps).
	for id := range explicitCollectorSet {
		if _, ok := allCollectors[id]; !ok {
			return nil, nil, fmt.Errorf("unknown collector ID %q in profile", id)
		}
		if err := resolveCollectorDeps(id, allCollectors, collectorSet, make(map[CollectorID]bool)); err != nil {
			return nil, nil, fmt.Errorf("resolving dependencies for explicit collector %q: %w", id, err)
		}
	}

	// Step 8: Validate cross-scope dependency constraints.
	if err := validateCrossScopeDeps(collectorSet, allCollectors); err != nil {
		return nil, nil, err
	}

	// Step 9: Return sorted, deduplicated lists.
	resolvedCollectors = sortedCollectorIDs(collectorSet)
	resolvedAnalyzers = sortedAnalyzerIDs(analyzerSet)
	return resolvedCollectors, resolvedAnalyzers, nil
}

// flattenProfile recursively resolves a profile and its includes, returning the
// merged set of analyzer IDs and the merged set of explicitly listed collector IDs.
// Detects cycles via the visiting set.
func flattenProfile(name string, allProfiles map[string]Profile, visiting map[string]bool) (map[AnalyzerID]bool, map[CollectorID]bool, error) {
	if visiting[name] {
		return nil, nil, fmt.Errorf("cycle detected in profile includes: %q", name)
	}
	profile, ok := allProfiles[name]
	if !ok {
		return nil, nil, fmt.Errorf("unknown profile %q", name)
	}

	visiting[name] = true
	defer func() { visiting[name] = false }()

	analyzerResult := make(map[AnalyzerID]bool)
	collectorResult := make(map[CollectorID]bool)

	// First, resolve all included profiles.
	for _, includeName := range profile.Includes {
		includedA, includedC, err := flattenProfile(includeName, allProfiles, visiting)
		if err != nil {
			return nil, nil, err
		}
		for id := range includedA {
			analyzerResult[id] = true
		}
		for id := range includedC {
			collectorResult[id] = true
		}
	}

	// Then add this profile's own analyzers and explicit collectors.
	for _, id := range profile.Analyzers {
		analyzerResult[id] = true
	}
	for _, id := range profile.Collectors {
		collectorResult[id] = true
	}

	return analyzerResult, collectorResult, nil
}

// resolveCollectorDeps walks a collector's dependency tree, adding all
// transitively required collectors to the set. Detects cycles via visiting.
func resolveCollectorDeps(id CollectorID, allCollectors map[CollectorID]Collector, collected map[CollectorID]bool, visiting map[CollectorID]bool) error {
	if collected[id] {
		return nil // Already resolved.
	}
	if visiting[id] {
		return fmt.Errorf("cycle detected in collector dependencies: %q", id)
	}

	collector, ok := allCollectors[id]
	if !ok {
		return fmt.Errorf("unknown collector ID %q", id)
	}

	visiting[id] = true
	defer func() { visiting[id] = false }()

	// Resolve this collector's own dependencies first.
	for _, depID := range collector.DependsOn() {
		if err := resolveCollectorDeps(depID, allCollectors, collected, visiting); err != nil {
			return err
		}
	}

	collected[id] = true
	return nil
}

// validateCrossScopeDeps ensures cross-scope dependency constraints are met:
// - ClusterWide collectors cannot depend on PerScyllaCluster or PerScyllaNode collectors
// - PerScyllaCluster collectors cannot depend on PerScyllaNode collectors
func validateCrossScopeDeps(collectorSet map[CollectorID]bool, allCollectors map[CollectorID]Collector) error {
	for id := range collectorSet {
		collector := allCollectors[id]
		for _, depID := range collector.DependsOn() {
			dep := allCollectors[depID]
			if err := validateScopeDep(collector.Scope(), dep.Scope(), id, depID); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateScopeDep checks that a collector at fromScope can depend on a
// collector at depScope. Broader-scoped collectors cannot depend on
// narrower-scoped ones.
func validateScopeDep(fromScope, depScope CollectorScope, fromID, depID CollectorID) error {
	switch fromScope {
	case ClusterWide:
		if depScope != ClusterWide {
			return fmt.Errorf("ClusterWide collector %q cannot depend on %s collector %q", fromID, depScope, depID)
		}
	case PerScyllaCluster:
		if depScope == PerScyllaNode {
			return fmt.Errorf("PerScyllaCluster collector %q cannot depend on PerScyllaNode collector %q", fromID, depID)
		}
	case PerScyllaNode:
		// PerScyllaNode can depend on anything.
	}
	return nil
}

func sortedCollectorIDs(set map[CollectorID]bool) []CollectorID {
	ids := make([]CollectorID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func sortedAnalyzerIDs(set map[AnalyzerID]bool) []AnalyzerID {
	ids := make([]AnalyzerID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
