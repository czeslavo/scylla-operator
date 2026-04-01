package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// ConsoleWriter writes human-readable colored diagnostic output.
type ConsoleWriter struct {
	w io.Writer

	// Color functions.
	greenFn  func(format string, a ...interface{}) string
	yellowFn func(format string, a ...interface{}) string
	redFn    func(format string, a ...interface{}) string
	grayFn   func(format string, a ...interface{}) string
	boldFn   func(format string, a ...interface{}) string
}

// NewConsoleWriter creates a ConsoleWriter that writes to w.
func NewConsoleWriter(w io.Writer) *ConsoleWriter {
	return &ConsoleWriter{
		w:        w,
		greenFn:  color.New(color.FgGreen).SprintfFunc(),
		yellowFn: color.New(color.FgYellow).SprintfFunc(),
		redFn:    color.New(color.FgRed).SprintfFunc(),
		grayFn:   color.New(color.FgHiBlack).SprintfFunc(),
		boldFn:   color.New(color.Bold).SprintfFunc(),
	}
}

// NewConsoleWriterNoColor creates a ConsoleWriter with color disabled,
// useful for testing or non-TTY output.
func NewConsoleWriterNoColor(w io.Writer) *ConsoleWriter {
	noColor := func(format string, a ...interface{}) string {
		return fmt.Sprintf(format, a...)
	}
	return &ConsoleWriter{
		w:        w,
		greenFn:  noColor,
		yellowFn: noColor,
		redFn:    noColor,
		grayFn:   noColor,
		boldFn:   noColor,
	}
}

// WriteReport writes the full diagnostic report.
func (c *ConsoleWriter) WriteReport(result *engine.EngineResult, profileName string, clusters []engine.ScyllaClusterInfo, scyllaNodes map[engine.ScopeKey][]engine.ScyllaNodeInfo) error {
	c.writeHeader(profileName)
	c.writeTargets(clusters, scyllaNodes)
	c.writeCollectors(result)
	c.writeAnalyzers(result)
	c.writeSummary(result)
	return nil
}

func (c *ConsoleWriter) writeHeader(profileName string) {
	fmt.Fprintf(c.w, "%s\n\n", c.boldFn("ScyllaDB Diagnostics (profile: %s)", profileName))
}

func (c *ConsoleWriter) writeTargets(clusters []engine.ScyllaClusterInfo, scyllaNodes map[engine.ScopeKey][]engine.ScyllaNodeInfo) {
	if len(clusters) == 0 {
		return
	}

	fmt.Fprintf(c.w, "Scylla Clusters:\n")
	for _, cluster := range clusters {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		nodeCount := len(scyllaNodes[clusterKey])
		fmt.Fprintf(c.w, "  %s/%s (%s, %d nodes)\n", cluster.Namespace, cluster.Name, cluster.Kind, nodeCount)
	}
	fmt.Fprintln(c.w)
}

func (c *ConsoleWriter) writeCollectors(result *engine.EngineResult) {
	fmt.Fprintf(c.w, "Collectors:\n")

	for _, collectorID := range result.ResolvedCollectors {
		// ClusterWide results.
		if res, ok := result.Vitals.ClusterWide[collectorID]; ok {
			c.writeCollectorLine(collectorID, "", res)
		}

		// PerScyllaCluster results.
		for _, key := range result.Vitals.ScyllaClusterKeys() {
			if perScyllaCluster, ok := result.Vitals.PerScyllaCluster[key]; ok {
				if res, ok := perScyllaCluster[collectorID]; ok {
					c.writeCollectorLine(collectorID, key.String(), res)
				}
			}
		}

		// PerScyllaNode results.
		for _, key := range result.Vitals.ScyllaNodeKeys() {
			if perNode, ok := result.Vitals.PerScyllaNode[key]; ok {
				if res, ok := perNode[collectorID]; ok {
					c.writeCollectorLine(collectorID, key.String(), res)
				}
			}
		}
	}

	fmt.Fprintln(c.w)
}

func (c *ConsoleWriter) writeCollectorLine(id engine.CollectorID, scopeLabel string, result *engine.CollectorResult) {
	statusStr := c.colorCollectorStatus(result.Status)
	message := result.Message
	if scopeLabel != "" {
		message = scopeLabel + ": " + message
	}
	dur := formatDuration(result.Duration)
	fmt.Fprintf(c.w, "  [%s]  %-35s %s %s\n", statusStr, id, message, c.grayFn("(%s)", dur))
}

// formatDuration formats a duration for display in the console report.
// Sub-second durations are shown as milliseconds; longer durations as seconds.
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func (c *ConsoleWriter) writeAnalyzers(result *engine.EngineResult) {
	fmt.Fprintf(c.w, "Analysis:\n")

	if len(result.ResolvedAnalyzers) == 0 {
		fmt.Fprintf(c.w, "  (no analyzers configured for this profile)\n\n")
		return
	}

	for _, analyzerID := range result.ResolvedAnalyzers {
		byScope, ok := result.AnalyzerResults[analyzerID]
		if !ok {
			continue
		}

		// ClusterWide: single entry under an empty ScopeKey — no scope prefix.
		if res, ok := byScope[engine.ScopeKey{}]; ok && len(byScope) == 1 {
			statusStr := c.colorAnalyzerStatus(res.Status)
			fmt.Fprintf(c.w, "  [%s]  %-35s %s\n", statusStr, analyzerID, res.Message)
			continue
		}

		// PerScyllaCluster: one entry per cluster — print scope prefix on each line.
		// Sort keys for deterministic output.
		keys := make([]engine.ScopeKey, 0, len(byScope))
		for k := range byScope {
			keys = append(keys, k)
		}
		sortScopeKeys(keys)
		for _, scopeKey := range keys {
			res := byScope[scopeKey]
			statusStr := c.colorAnalyzerStatus(res.Status)
			message := scopeKey.String() + ": " + res.Message
			fmt.Fprintf(c.w, "  [%s]  %-35s %s\n", statusStr, analyzerID, message)
		}
	}

	fmt.Fprintln(c.w)
}

func (c *ConsoleWriter) writeSummary(result *engine.EngineResult) {
	var passed, warnings, failed, skipped int

	for _, byScope := range result.AnalyzerResults {
		for _, res := range byScope {
			switch res.Status {
			case engine.AnalyzerPassed:
				passed++
			case engine.AnalyzerWarning:
				warnings++
			case engine.AnalyzerFailed:
				failed++
			case engine.AnalyzerSkipped:
				skipped++
			}
		}
	}

	parts := []string{
		c.greenFn("%d passed", passed),
		c.yellowFn("%d warnings", warnings),
		c.redFn("%d failed", failed),
		c.grayFn("%d skipped", skipped),
	}

	fmt.Fprintf(c.w, "Summary: %s\n", strings.Join(parts, ", "))
}

func (c *ConsoleWriter) colorCollectorStatus(status engine.CollectorStatus) string {
	switch status {
	case engine.CollectorPassed:
		return c.greenFn("PASSED")
	case engine.CollectorFailed:
		return c.redFn("FAILED")
	case engine.CollectorSkipped:
		return c.grayFn("SKIPPED")
	default:
		return status.String()
	}
}

func (c *ConsoleWriter) colorAnalyzerStatus(status engine.AnalyzerStatus) string {
	switch status {
	case engine.AnalyzerPassed:
		return c.greenFn("PASSED")
	case engine.AnalyzerWarning:
		return c.yellowFn("WARNING")
	case engine.AnalyzerFailed:
		return c.redFn("FAILED")
	case engine.AnalyzerSkipped:
		return c.grayFn("SKIPPED")
	default:
		return status.String()
	}
}

func sortScopeKeys(keys []engine.ScopeKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
	})
}
