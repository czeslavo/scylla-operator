package analyzers

import (
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// AllAnalyzers returns the complete list of PoC analyzers.
func AllAnalyzers() []engine.Analyzer {
	return []engine.Analyzer{
		NewScyllaVersionSupportAnalyzer(),
		NewSchemaAgreementAnalyzer(),
		NewOSSupportAnalyzer(),
		NewGossipHealthAnalyzer(),
	}
}

// AllAnalyzersMap returns a map of all analyzers keyed by ID.
// It panics if two analyzers share the same ID, which is a programming error.
func AllAnalyzersMap() map[engine.AnalyzerID]engine.Analyzer {
	m := make(map[engine.AnalyzerID]engine.Analyzer)
	for _, a := range AllAnalyzers() {
		if _, exists := m[a.ID()]; exists {
			panic(fmt.Sprintf("duplicate analyzer ID %q: each analyzer must have a unique ID", a.ID()))
		}
		m[a.ID()] = a
	}
	return m
}
