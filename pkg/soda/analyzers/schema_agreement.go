package analyzers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// SchemaAgreementAnalyzerID is the unique identifier for the SchemaAgreementAnalyzer.
	SchemaAgreementAnalyzerID engine.AnalyzerID = "SchemaAgreementAnalyzer"
)

// schemaAgreementAnalyzer checks that all pods report the same schema version.
type schemaAgreementAnalyzer struct{}

var _ engine.Analyzer = (*schemaAgreementAnalyzer)(nil)

// NewSchemaAgreementAnalyzer creates a new SchemaAgreementAnalyzer.
func NewSchemaAgreementAnalyzer() engine.Analyzer {
	return &schemaAgreementAnalyzer{}
}

func (a *schemaAgreementAnalyzer) ID() engine.AnalyzerID { return SchemaAgreementAnalyzerID }
func (a *schemaAgreementAnalyzer) Name() string          { return "Schema agreement check" }
func (a *schemaAgreementAnalyzer) Scope() engine.AnalyzerScope {
	return engine.AnalyzerPerScyllaCluster
}
func (a *schemaAgreementAnalyzer) DependsOn() []engine.CollectorID {
	return []engine.CollectorID{collectors.SchemaVersionsCollectorID}
}

func (a *schemaAgreementAnalyzer) Analyze(params engine.AnalyzerParams) *engine.AnalyzerResult {
	// Collect all unique schema version UUIDs across all pods.
	allVersions := make(map[string][]string) // schema UUID → list of pod keys that reported it
	podsChecked := 0

	for _, podKey := range params.Vitals.ScyllaNodeKeys() {
		schemaResult, err := collectors.GetSchemaVersionsResult(params.Vitals, podKey)
		if err != nil {
			// Skip pods where the collector didn't pass.
			continue
		}

		podsChecked++
		for _, entry := range schemaResult.Versions {
			allVersions[entry.SchemaVersion] = append(allVersions[entry.SchemaVersion], podKey.String())
		}
	}

	if podsChecked == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No schema version information available",
		}
	}

	if len(allVersions) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No schema versions reported by any pod",
		}
	}

	if len(allVersions) == 1 {
		// All pods agree on the same schema version.
		var uuid string
		for k := range allVersions {
			uuid = k
		}
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerPassed,
			Message: fmt.Sprintf("Schema agreement reached: all nodes report version %s", uuid),
		}
	}

	// Multiple schema versions → disagreement.
	// Build a sorted list of versions for deterministic output.
	versions := make([]string, 0, len(allVersions))
	for v := range allVersions {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	details := make([]string, 0, len(versions))
	for _, v := range versions {
		pods := allVersions[v]
		sort.Strings(pods)
		details = append(details, fmt.Sprintf("%s (reported by %s)", v, strings.Join(pods, ", ")))
	}

	return &engine.AnalyzerResult{
		Status:  engine.AnalyzerFailed,
		Message: fmt.Sprintf("Schema disagreement: %d versions found: %s", len(allVersions), strings.Join(details, "; ")),
	}
}
