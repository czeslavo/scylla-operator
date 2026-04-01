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
			// (ClusterWide, PerScyllaCluster, PerScyllaNode) produces artifacts even
			// when no analyzer currently depends on a given collector.
			Collectors: []engine.CollectorID{
				collectors.NodeResourcesCollectorID,
				collectors.OSInfoCollectorID,
				collectors.SchemaVersionsCollectorID,
				collectors.ScyllaVersionCollectorID,
				collectors.ScyllaConfigCollectorID,
				collectors.SystemPeersLocalCollectorID,
				collectors.GossipInfoCollectorID,
				collectors.SystemTopologyCollectorID,
				collectors.SystemConfigCollectorID,
				collectors.ScyllaDConfigCollectorID,
				collectors.DiskUsageCollectorID,
				collectors.RlimitsCollectorID,
				// Manifest collectors — Scylla and Kubernetes resources.
				collectors.ScyllaClusterCollectorID,
				collectors.ScyllaDBDatacenterCollectorID,
				collectors.NodeManifestCollectorID,
				collectors.NodeConfigCollectorID,
				collectors.ScyllaOperatorConfigCollectorID,
				collectors.DeploymentCollectorID,
				collectors.StatefulSetCollectorID,
				collectors.DaemonSetCollectorID,
				collectors.ConfigMapCollectorID,
				collectors.ServiceCollectorID,
				collectors.ServiceAccountCollectorID,
				collectors.PodManifestCollectorID,
			},
			Analyzers: []engine.AnalyzerID{
				analyzers.ScyllaVersionSupportAnalyzerID,
				analyzers.SchemaAgreementAnalyzerID,
				analyzers.OSSupportAnalyzerID,
				analyzers.GossipHealthAnalyzerID,
				analyzers.TopologyHealthAnalyzerID,
			},
		},
	}
}
