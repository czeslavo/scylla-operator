package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// IndexParams holds all the information needed to generate the output directory index.
type IndexParams struct {
	// Profile that was used for this diagnostic run.
	ProfileName string

	// Clusters and Scylla nodes that were targeted.
	Clusters    []engine.ScyllaClusterInfo
	ScyllaNodes map[engine.ScopeKey][]engine.ScyllaNodeInfo

	// Engine result with resolved IDs, vitals, and analyzer results.
	Result *engine.EngineResult

	// Maps from ID to human-readable name, used for display.
	CollectorNames map[engine.CollectorID]string
	AnalyzerNames  map[engine.AnalyzerID]string

	// Path to the output directory (used to generate --from-archive hint).
	OutputDir string
}

// WriteIndex writes a README.md to the given writer, describing the contents
// of the artifact output directory.
func WriteIndex(w io.Writer, params IndexParams) error {
	fmt.Fprintf(w, "# ScyllaDB Diagnostics Report\n\n")

	// --- Run summary ---
	fmt.Fprintf(w, "## Run Summary\n\n")
	fmt.Fprintf(w, "| Property | Value |\n")
	fmt.Fprintf(w, "|----------|-------|\n")
	fmt.Fprintf(w, "| Profile | `%s` |\n", params.ProfileName)
	fmt.Fprintf(w, "| Clusters found | %d |\n", len(params.Clusters))
	totalNodes := 0
	for _, nodes := range params.ScyllaNodes {
		totalNodes += len(nodes)
	}
	fmt.Fprintf(w, "| Scylla nodes targeted | %d |\n", totalNodes)
	fmt.Fprintf(w, "\n")

	// --- Targets ---
	if len(params.Clusters) > 0 {
		fmt.Fprintf(w, "## Targets\n\n")
		for _, cluster := range params.Clusters {
			clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
			nodes := params.ScyllaNodes[clusterKey]
			fmt.Fprintf(w, "### %s/%s", cluster.Namespace, cluster.Name)
			if cluster.Kind != "" {
				fmt.Fprintf(w, " (%s)", cluster.Kind)
			}
			fmt.Fprintf(w, "\n\n")
			if len(nodes) == 0 {
				fmt.Fprintf(w, "_No Scylla nodes found._\n\n")
			} else {
				for _, node := range nodes {
					fmt.Fprintf(w, "- `%s/%s`\n", node.Namespace, node.Name)
				}
				fmt.Fprintf(w, "\n")
			}
		}
	}

	// --- Collectors ---
	fmt.Fprintf(w, "## Collectors\n\n")
	if len(params.Result.ResolvedCollectors) == 0 {
		fmt.Fprintf(w, "_No collectors were resolved._\n\n")
	} else {
		for _, id := range params.Result.ResolvedCollectors {
			name := params.CollectorNames[id]
			if name == "" {
				name = string(id)
			}
			fmt.Fprintf(w, "### %s\n\n", name)

			// List artifacts produced by this collector.
			allArtifacts := collectArtifactsForCollector(id, params.Result.Vitals)
			if len(allArtifacts) == 0 {
				fmt.Fprintf(w, "_No artifacts written._\n\n")
			} else {
				for _, entry := range allArtifacts {
					fmt.Fprintf(w, "- `%s`", entry.path)
					if entry.description != "" {
						fmt.Fprintf(w, " — %s", entry.description)
					}
					fmt.Fprintf(w, "\n")
				}
				fmt.Fprintf(w, "\n")
			}
		}
	}

	// --- Analysis Results ---
	fmt.Fprintf(w, "## Analysis Results\n\n")
	if len(params.Result.ResolvedAnalyzers) == 0 {
		fmt.Fprintf(w, "_No analyzers were resolved._\n\n")
	} else {
		// Collect all per-analyzer results and print them in a table.
		fmt.Fprintf(w, "| Analyzer | Scope | Status | Message |\n")
		fmt.Fprintf(w, "|----------|-------|--------|---------|\n")

		for _, id := range params.Result.ResolvedAnalyzers {
			name := params.AnalyzerNames[id]
			if name == "" {
				name = string(id)
			}

			scopeResults := params.Result.AnalyzerResults[id]
			if len(scopeResults) == 0 {
				fmt.Fprintf(w, "| %s | — | — | — |\n", name)
				continue
			}

			// Sort scope keys for deterministic output.
			keys := make([]engine.ScopeKey, 0, len(scopeResults))
			for k := range scopeResults {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				if keys[i].Namespace != keys[j].Namespace {
					return keys[i].Namespace < keys[j].Namespace
				}
				return keys[i].Name < keys[j].Name
			})

			for _, key := range keys {
				result := scopeResults[key]
				scope := "cluster-wide"
				if !key.IsEmpty() {
					scope = key.String()
				}
				msg := strings.ReplaceAll(result.Message, "|", "\\|")
				fmt.Fprintf(w, "| %s | %s | **%s** | %s |\n", name, scope, result.Status.String(), msg)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// --- Offline re-analysis hint ---
	fmt.Fprintf(w, "## Offline Re-Analysis\n\n")
	fmt.Fprintf(w, "To re-run the analysis against this artifact bundle without connecting to the cluster:\n\n")
	if params.OutputDir != "" {
		fmt.Fprintf(w, "```sh\nscylla-operator diagnose --from-archive=%s\n```\n\n", params.OutputDir)
	} else {
		fmt.Fprintf(w, "```sh\nscylla-operator diagnose --from-archive=<path-to-this-directory>\n```\n\n")
	}
	fmt.Fprintf(w, "If the directory has been packed into a `.tar.gz` archive, pass the archive path directly:\n\n")
	fmt.Fprintf(w, "```sh\nscylla-operator diagnose --from-archive=<path>.tar.gz\n```\n")

	return nil
}

// WriteIndexFile writes the README.md index file to the given output directory.
func WriteIndexFile(outputDir string, params IndexParams) error {
	path := filepath.Join(outputDir, "README.md")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating README.md: %w", err)
	}
	defer f.Close()

	if err := WriteIndex(f, params); err != nil {
		return fmt.Errorf("writing README.md: %w", err)
	}

	return nil
}

// artifactEntry holds a display path and description for one artifact.
type artifactEntry struct {
	path        string
	description string
}

// collectArtifactsForCollector gathers all artifacts written by a collector
// across all scopes, returning them as display paths relative to the output dir.
func collectArtifactsForCollector(id engine.CollectorID, vitals *engine.Vitals) []artifactEntry {
	var entries []artifactEntry

	// ClusterWide.
	if result, ok := vitals.ClusterWide[id]; ok {
		for _, a := range result.Artifacts {
			entries = append(entries, artifactEntry{
				path:        fmt.Sprintf("collectors/cluster-wide/%s/%s", id, a.RelativePath),
				description: a.Description,
			})
		}
	}

	// PerScyllaCluster.
	for _, key := range vitals.ScyllaClusterKeys() {
		if perCluster, ok := vitals.PerScyllaCluster[key]; ok {
			if result, ok := perCluster[id]; ok {
				for _, a := range result.Artifacts {
					entries = append(entries, artifactEntry{
						path:        fmt.Sprintf("collectors/per-scylla-cluster/%s/%s/%s/%s", key.Namespace, key.Name, id, a.RelativePath),
						description: a.Description,
					})
				}
			}
		}
	}

	// PerScyllaNode.
	for _, key := range vitals.ScyllaNodeKeys() {
		if perNode, ok := vitals.PerScyllaNode[key]; ok {
			if result, ok := perNode[id]; ok {
				for _, a := range result.Artifacts {
					entries = append(entries, artifactEntry{
						path:        fmt.Sprintf("collectors/per-scylla-node/%s/%s/%s/%s", key.Namespace, key.Name, id, a.RelativePath),
						description: a.Description,
					})
				}
			}
		}
	}

	return entries
}
