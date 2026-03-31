package analyzers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// OSSupportAnalyzerID is the unique identifier for the OSSupportAnalyzer.
	OSSupportAnalyzerID engine.AnalyzerID = "OSSupportAnalyzer"
)

// supportedOSNames is the list of OS distributions known to be supported for ScyllaDB.
var supportedOSNames = map[string]bool{
	"red hat enterprise linux": true,
	"ubuntu":                   true,
	"debian gnu/linux":         true,
	"centos linux":             true,
	"centos stream":            true,
	"amazon linux":             true,
	"rocky linux":              true,
}

// osSupportAnalyzer checks that the OS running on Scylla pods is a supported distribution.
type osSupportAnalyzer struct{}

var _ engine.Analyzer = (*osSupportAnalyzer)(nil)

// NewOSSupportAnalyzer creates a new OSSupportAnalyzer.
func NewOSSupportAnalyzer() engine.Analyzer {
	return &osSupportAnalyzer{}
}

func (a *osSupportAnalyzer) ID() engine.AnalyzerID       { return OSSupportAnalyzerID }
func (a *osSupportAnalyzer) Name() string                { return "OS support check" }
func (a *osSupportAnalyzer) Scope() engine.AnalyzerScope { return engine.AnalyzerPerScyllaCluster }
func (a *osSupportAnalyzer) DependsOn() []engine.CollectorID {
	return []engine.CollectorID{collectors.OSInfoCollectorID}
}

func (a *osSupportAnalyzer) Analyze(params engine.AnalyzerParams) *engine.AnalyzerResult {
	var supported []string
	var unknown []string
	podsChecked := 0

	for _, podKey := range params.Vitals.PodKeys() {
		osResult, err := collectors.GetOSInfoResult(params.Vitals, podKey)
		if err != nil {
			// Skip pods where the collector didn't pass.
			continue
		}

		podsChecked++
		osName := osResult.OSName
		if osName == "" {
			unknown = append(unknown, fmt.Sprintf("unknown OS (%s)", podKey))
			continue
		}

		if isOSSupported(osName) {
			supported = append(supported, fmt.Sprintf("%s %s (%s)", osName, osResult.OSVersion, podKey))
		} else {
			unknown = append(unknown, fmt.Sprintf("%s %s (%s)", osName, osResult.OSVersion, podKey))
		}
	}

	if podsChecked == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No OS information available",
		}
	}

	if len(unknown) > 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: fmt.Sprintf("Unknown/unverified OS distribution(s): %s", strings.Join(unknown, ", ")),
		}
	}

	// All checked pods run supported OS.
	uniqueOS := dedupSupportedOS(supported)
	return &engine.AnalyzerResult{
		Status:  engine.AnalyzerPassed,
		Message: fmt.Sprintf("All pods run supported OS: %s", strings.Join(uniqueOS, ", ")),
	}
}

// isOSSupported checks if an OS name matches a known supported distribution.
func isOSSupported(osName string) bool {
	return supportedOSNames[strings.ToLower(osName)]
}

// dedupSupportedOS extracts unique "OSName OSVersion" pairs from the supported list
// (which includes pod key suffixes) and returns sorted unique names.
func dedupSupportedOS(entries []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, entry := range entries {
		// entry format: "Ubuntu 22.04 (ns/pod-0)" — extract the OS part before the parenthesis.
		idx := strings.LastIndex(entry, " (")
		osStr := entry
		if idx > 0 {
			osStr = entry[:idx]
		}
		if !seen[osStr] {
			seen[osStr] = true
			result = append(result, osStr)
		}
	}
	sort.Strings(result)
	return result
}
