package profiles

import (
	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// FullProfileName is the name of the built-in profile that enables all analyzers.
	FullProfileName = "full"
)

// AllProfiles returns the complete map of built-in diagnostic profiles.
func AllProfiles() map[string]engine.Profile {
	return map[string]engine.Profile{
		FullProfileName: {
			Name:        FullProfileName,
			Description: "Run all available diagnostic analyzers",
			Analyzers: []engine.AnalyzerID{
				analyzers.ScyllaVersionSupportAnalyzerID,
				analyzers.SchemaAgreementAnalyzerID,
				analyzers.OSSupportAnalyzerID,
			},
		},
	}
}
