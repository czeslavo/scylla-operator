package output

import (
	"fmt"
	"io"
	"strings"

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
func (c *ConsoleWriter) WriteReport(result *engine.EngineResult, profileName string, clusters []engine.ClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) error {
	c.writeHeader(profileName)
	c.writeTargets(clusters, pods)
	c.writeCollectors(result)
	c.writeAnalyzers(result)
	c.writeSummary(result)
	return nil
}

func (c *ConsoleWriter) writeHeader(profileName string) {
	fmt.Fprintf(c.w, "%s\n\n", c.boldFn("ScyllaDB Diagnostics (profile: %s)", profileName))
}

func (c *ConsoleWriter) writeTargets(clusters []engine.ClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) {
	if len(clusters) == 0 {
		return
	}

	fmt.Fprintf(c.w, "Target clusters:\n")
	for _, cluster := range clusters {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		podCount := len(pods[clusterKey])
		fmt.Fprintf(c.w, "  %s/%s (%s, %d pods)\n", cluster.Namespace, cluster.Name, cluster.Kind, podCount)
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

		// PerCluster results.
		for _, key := range result.Vitals.ClusterKeys() {
			if perCluster, ok := result.Vitals.PerCluster[key]; ok {
				if res, ok := perCluster[collectorID]; ok {
					c.writeCollectorLine(collectorID, key.String(), res)
				}
			}
		}

		// PerPod results.
		for _, key := range result.Vitals.PodKeys() {
			if perPod, ok := result.Vitals.PerPod[key]; ok {
				if res, ok := perPod[collectorID]; ok {
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
	fmt.Fprintf(c.w, "  [%s]  %-35s %s\n", statusStr, id, message)
}

func (c *ConsoleWriter) writeAnalyzers(result *engine.EngineResult) {
	fmt.Fprintf(c.w, "Analysis:\n")

	for _, analyzerID := range result.ResolvedAnalyzers {
		if res, ok := result.AnalyzerResults[analyzerID]; ok {
			statusStr := c.colorAnalyzerStatus(res.Status)
			fmt.Fprintf(c.w, "  [%s]  %-35s %s\n", statusStr, analyzerID, res.Message)
		}
	}

	fmt.Fprintln(c.w)
}

func (c *ConsoleWriter) writeSummary(result *engine.EngineResult) {
	var passed, warnings, failed, skipped int

	for _, res := range result.AnalyzerResults {
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
