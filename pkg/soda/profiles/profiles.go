package profiles

import (
	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
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
			Description: "Run all available diagnostic collectors and analyzers",
			// All known collectors are listed explicitly so that every scope
			// (ClusterWide, PerScyllaCluster, PerPod) produces artifacts even
			// when no analyzer currently depends on a given collector.
			Collectors: []engine.CollectorID{
				collectors.NodeResourcesCollectorID,
				collectors.ScyllaClusterStatusCollectorID,
				collectors.OSInfoCollectorID,
				collectors.SchemaVersionsCollectorID,
				collectors.ScyllaVersionCollectorID,
				collectors.ScyllaConfigCollectorID,
			},
			Analyzers: []engine.AnalyzerID{
				analyzers.ScyllaVersionSupportAnalyzerID,
				analyzers.SchemaAgreementAnalyzerID,
				analyzers.OSSupportAnalyzerID,
			},
		},
	}
}
