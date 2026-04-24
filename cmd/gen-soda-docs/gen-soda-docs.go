// gen-soda-docs generates markdown documentation for all soda collectors,
// analyzers, and profiles. It is intended to be run via `go run` and its
// output redirected to a file.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	"github.com/scylladb/scylla-operator/pkg/soda/profiles"
)

func main() {
	allCollectors := collectors.AllCollectors()
	allCollectorMap := collectors.AllCollectorsMap()
	allAnalyzers := analyzers.AllAnalyzers()
	allAnalyzerMap := analyzers.AllAnalyzersMap()
	allProfiles := profiles.AllProfiles()

	// Build reverse map: collectorID → list of profile names that include it.
	collectorProfiles := make(map[engine.CollectorID][]string)
	analyzerProfiles := make(map[engine.AnalyzerID][]string)
	for _, p := range sortedProfiles(allProfiles) {
		resolvedC, resolvedA, err := engine.ResolveProfile(
			p.Name, allProfiles, nil, nil, allAnalyzerMap, allCollectorMap,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving profile %q: %v\n", p.Name, err)
			os.Exit(1)
		}
		for _, id := range resolvedC {
			collectorProfiles[id] = appendUnique(collectorProfiles[id], p.Name)
		}
		for _, id := range resolvedA {
			analyzerProfiles[id] = appendUnique(analyzerProfiles[id], p.Name)
		}
	}

	w := os.Stdout

	fmt.Fprintln(w, "# soda — Diagnostic Reference")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Auto-generated documentation for all collectors, analyzers, and profiles.")
	fmt.Fprintln(w)

	// --- Profiles ---
	fmt.Fprintln(w, "## Profiles")
	fmt.Fprintln(w)
	for _, p := range sortedProfiles(allProfiles) {
		resolvedC, resolvedA, _ := engine.ResolveProfile(
			p.Name, allProfiles, nil, nil, allAnalyzerMap, allCollectorMap,
		)
		fmt.Fprintf(w, "### `%s`\n\n", p.Name)
		fmt.Fprintf(w, "%s\n\n", p.Description)
		if len(p.Includes) > 0 {
			fmt.Fprintf(w, "**Includes:** %s\n\n", strings.Join(p.Includes, ", "))
		}
		fmt.Fprintf(w, "**Collectors:** %d &nbsp; **Analyzers:** %d\n\n", len(resolvedC), len(resolvedA))
	}

	// --- Collectors ---
	fmt.Fprintln(w, "## Collectors")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Total: %d\n\n", len(allCollectors))
	fmt.Fprintln(w, "| ID | Name | Description | Scope | Profiles | RBAC |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, c := range allCollectors {
		rbac := "—"
		if rp, ok := c.(engine.RBACProvider); ok {
			rules := rp.RBAC()
			if len(rules) > 0 {
				parts := make([]string, len(rules))
				for i, rule := range rules {
					apiGroup := strings.Join(rule.APIGroups, ",")
					if apiGroup == "" {
						apiGroup = "core"
					}
					parts[i] = fmt.Sprintf("%s/%s: %s", apiGroup, strings.Join(rule.Resources, ","), strings.Join(rule.Verbs, ","))
				}
				rbac = strings.Join(parts, "; ")
			}
		}

		profs := "—"
		if p := collectorProfiles[c.ID()]; len(p) > 0 {
			profs = strings.Join(p, ", ")
		}

		fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s | %s |\n",
			c.ID(), c.Name(), c.Description(), c.Scope(), profs, rbac)
	}
	fmt.Fprintln(w)

	// --- Analyzers ---
	fmt.Fprintln(w, "## Analyzers")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Total: %d\n\n", len(allAnalyzers))
	fmt.Fprintln(w, "| ID | Name | Description | Scope | Profiles | Dependencies |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, a := range allAnalyzers {
		deps := "—"
		if d := a.DependsOn(); len(d) > 0 {
			depStrs := make([]string, len(d))
			for i, id := range d {
				depStrs[i] = fmt.Sprintf("`%s`", id)
			}
			deps = strings.Join(depStrs, ", ")
		}

		profs := "—"
		if p := analyzerProfiles[a.ID()]; len(p) > 0 {
			profs = strings.Join(p, ", ")
		}

		fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s | %s |\n",
			a.ID(), a.Name(), a.Description(), a.Scope(), profs, deps)
	}
	fmt.Fprintln(w)
}

func sortedProfiles(m map[string]engine.Profile) []engine.Profile {
	// Deterministic order: full, health, logs, then any others alphabetically.
	order := map[string]int{"full": 0, "health": 1, "logs": 2}
	pp := make([]engine.Profile, 0, len(m))
	for _, p := range m {
		pp = append(pp, p)
	}
	sort.Slice(pp, func(i, j int) bool {
		oi, oki := order[pp[i].Name]
		oj, okj := order[pp[j].Name]
		if oki && okj {
			return oi < oj
		}
		if oki {
			return true
		}
		if okj {
			return false
		}
		return pp[i].Name < pp[j].Name
	})
	return pp
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
