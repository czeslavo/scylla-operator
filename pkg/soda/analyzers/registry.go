package analyzers

import (
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// AllAnalyzers returns the complete list of PoC analyzers.
func AllAnalyzers() []engine.Analyzer {
	return []engine.Analyzer{
		NewScyllaVersionSupportAnalyzer(),
		NewSchemaAgreementAnalyzer(),
		NewOSSupportAnalyzer(),
	}
}

// AllAnalyzersMap returns a map of all analyzers keyed by ID.
func AllAnalyzersMap() map[engine.AnalyzerID]engine.Analyzer {
	m := make(map[engine.AnalyzerID]engine.Analyzer)
	for _, a := range AllAnalyzers() {
		m[a.ID()] = a
	}
	return m
}
