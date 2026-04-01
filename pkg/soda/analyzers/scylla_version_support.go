package analyzers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// ScyllaVersionSupportAnalyzerID is the unique identifier for the ScyllaVersionSupportAnalyzer.
	ScyllaVersionSupportAnalyzerID engine.AnalyzerID = "ScyllaVersionSupportAnalyzer"
)

// scyllaVersionSupportAnalyzer checks Scylla versions against known-supported ranges.
type scyllaVersionSupportAnalyzer struct {
	engine.AnalyzerBase
}

var _ engine.PerScyllaClusterAnalyzer = (*scyllaVersionSupportAnalyzer)(nil)

// NewScyllaVersionSupportAnalyzer creates a new ScyllaVersionSupportAnalyzer.
func NewScyllaVersionSupportAnalyzer() engine.PerScyllaClusterAnalyzer {
	return &scyllaVersionSupportAnalyzer{
		AnalyzerBase: engine.NewAnalyzerBase(
			ScyllaVersionSupportAnalyzerID,
			"Scylla version support check",
			engine.AnalyzerPerScyllaCluster,
			[]engine.CollectorID{collectors.ScyllaVersionCollectorID},
		),
	}
}

func (a *scyllaVersionSupportAnalyzer) AnalyzePerScyllaCluster(params engine.PerScyllaClusterAnalyzerParams) *engine.AnalyzerResult {
	var versions []string
	var unsupported []string
	var warnings []string

	for _, podKey := range params.Vitals.ScyllaNodeKeys() {
		versionResult, err := collectors.GetScyllaVersionResult(params.Vitals, podKey)
		if err != nil {
			// Skip pods where the collector didn't pass.
			continue
		}

		version := versionResult.Version
		if version == "" {
			continue
		}

		versions = append(versions, version)
		support := checkVersionSupport(version)
		switch support {
		case versionUnsupported:
			unsupported = append(unsupported, fmt.Sprintf("%s (%s)", version, podKey))
		case versionWarning:
			warnings = append(warnings, fmt.Sprintf("%s (%s)", version, podKey))
		}
	}

	if len(versions) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No Scylla version information available",
		}
	}

	if len(unsupported) > 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerFailed,
			Message: fmt.Sprintf("Unsupported Scylla version(s): %s", strings.Join(unsupported, ", ")),
		}
	}

	if len(warnings) > 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: fmt.Sprintf("Scylla version(s) nearing end of support: %s", strings.Join(warnings, ", ")),
		}
	}

	// Deduplicate versions for the message.
	uniqueVersions := dedupStrings(versions)
	return &engine.AnalyzerResult{
		Status:  engine.AnalyzerPassed,
		Message: fmt.Sprintf("ScyllaDB %s is supported", strings.Join(uniqueVersions, ", ")),
	}
}

type versionSupportLevel int

const (
	versionSupported   versionSupportLevel = iota
	versionWarning                         // Nearing end of support
	versionUnsupported                     // End of life
)

// checkVersionSupport checks a Scylla version string against known-supported ranges.
// This is a simplified check for the PoC. A production implementation would
// fetch support data from a canonical source.
func checkVersionSupport(version string) versionSupportLevel {
	major, minor, ok := parseMajorMinor(version)
	if !ok {
		return versionWarning // Unknown format
	}

	// Enterprise versions (year-based: 2024.x, 2025.x, 2026.x)
	if major >= 2024 {
		// Enterprise: latest two years are supported.
		if major >= 2025 {
			return versionSupported
		}
		// 2024.x is nearing end of support.
		return versionWarning
	}

	// Open-source versions (semantic: 5.x, 6.x)
	if major >= 6 {
		return versionSupported
	}
	if major == 5 && minor >= 4 {
		return versionWarning // 5.4 is the last supported 5.x
	}

	return versionUnsupported
}

// parseMajorMinor extracts the major and minor version numbers from a version string.
func parseMajorMinor(version string) (major, minor int, ok bool) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func dedupStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
