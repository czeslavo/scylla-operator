package profiles

import (
	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// FullProfileName is the name of the built-in profile that runs all collectors and analyzers.
	FullProfileName = "full"
	// HealthProfileName is the name of the built-in profile that collects Scylla runtime health data.
	HealthProfileName = "health"
	// LogsProfileName is the name of the built-in profile that collects container logs.
	LogsProfileName = "logs"
)

// AllProfiles returns the complete map of built-in diagnostic profiles.
func AllProfiles() map[string]engine.Profile {
	return map[string]engine.Profile{
		HealthProfileName: {
			Name:        HealthProfileName,
			Description: "Collect Scylla runtime health data and run all analyzers",
			// REST API and CQL-only collectors — fast, no exec, no manifests, no logs.
			Collectors: []engine.CollectorID{
				collectors.GossipInfoCollectorID,
				collectors.SchemaVersionsCollectorID,
				collectors.SystemPeersLocalCollectorID,
				collectors.SystemTopologyCollectorID,
				collectors.ScyllaVersionCollectorID,
			},
			Analyzers: []engine.AnalyzerID{
				analyzers.ScyllaVersionSupportAnalyzerID,
				analyzers.SchemaAgreementAnalyzerID,
				analyzers.OSSupportAnalyzerID,
				analyzers.GossipHealthAnalyzerID,
				analyzers.TopologyHealthAnalyzerID,
			},
		},
		LogsProfileName: {
			Name:        LogsProfileName,
			Description: "Collect container logs from all Scylla and operator pods",
			Collectors: []engine.CollectorID{
				collectors.ScyllaNodeLogsCollectorID,
				collectors.OperatorPodLogsCollectorID,
				collectors.ScyllaClusterJobLogsCollectorID,
			},
			Analyzers: []engine.AnalyzerID{},
		},
		FullProfileName: {
			Name:        FullProfileName,
			Description: "Run all available diagnostic collectors and analyzers",
			Includes:    []string{HealthProfileName, LogsProfileName},
			// All remaining collectors not covered by health or logs sub-profiles,
			// listed explicitly so that every scope (ClusterWide, PerScyllaCluster,
			// PerScyllaNode) produces artifacts even when no analyzer currently
			// depends on a given collector.
			Collectors: []engine.CollectorID{
				collectors.NodeResourcesCollectorID,
				collectors.OSInfoCollectorID,
				collectors.ScyllaConfigCollectorID,
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
				// PerScyllaCluster manifest collectors — child resources of ScyllaCluster.
				collectors.ScyllaClusterStatefulSetCollectorID,
				collectors.ScyllaClusterServiceCollectorID,
				collectors.ScyllaClusterConfigMapCollectorID,
				collectors.ScyllaClusterPodCollectorID,
				collectors.ScyllaClusterPDBCollectorID,
				collectors.ScyllaClusterServiceAccountCollectorID,
				collectors.ScyllaClusterRoleBindingCollectorID,
				collectors.ScyllaClusterPVCCollectorID,
			},
			// All analyzers are inherited from the health sub-profile via Includes.
			Analyzers: []engine.AnalyzerID{},
		},
	}
}
