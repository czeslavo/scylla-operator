package collectors

import (
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// AllCollectors returns the complete list of PoC collectors.
func AllCollectors() []engine.Collector {
	return []engine.Collector{
		NewNodeResourcesCollector(),
		NewOSInfoCollector(),
		NewScyllaVersionCollector(),
		NewSchemaVersionsCollector(),
		NewScyllaConfigCollector(),
		NewSystemPeersLocalCollector(),
		NewGossipInfoCollector(),
		NewSystemTopologyCollector(),
		NewSystemConfigCollector(),
		NewScyllaDConfigCollector(),
		NewDiskUsageCollector(),
		NewRlimitsCollector(),
		// Manifest collectors — Scylla and Kubernetes resources.
		NewScyllaClusterCollector(),
		NewScyllaDBDatacenterCollector(),
		NewNodeManifestCollector(),
		NewNodeConfigCollector(),
		NewScyllaOperatorConfigCollector(),
		NewDeploymentCollector(),
		NewStatefulSetCollector(),
		NewDaemonSetCollector(),
		NewConfigMapCollector(),
		NewServiceCollector(),
		NewServiceAccountCollector(),
		NewPodManifestCollector(),
		// PerScyllaCluster manifest collectors — child resources of ScyllaCluster.
		NewScyllaClusterStatefulSetCollector(),
		NewScyllaClusterServiceCollector(),
		NewScyllaClusterConfigMapCollector(),
		NewScyllaClusterPodCollector(),
		NewScyllaClusterPDBCollector(),
		NewScyllaClusterServiceAccountCollector(),
		NewScyllaClusterRoleBindingCollector(),
		NewScyllaClusterPVCCollector(),
		// Log collectors.
		NewScyllaNodeLogsCollector(),
		NewOperatorPodLogsCollector(),
		NewScyllaClusterJobLogsCollector(),
	}
}

// AllCollectorsMap returns a map of all collectors keyed by ID.
// It panics if two collectors share the same ID, which is a programming error.
func AllCollectorsMap() map[engine.CollectorID]engine.Collector {
	m := make(map[engine.CollectorID]engine.Collector)
	for _, c := range AllCollectors() {
		if _, exists := m[c.ID()]; exists {
			panic(fmt.Sprintf("duplicate collector ID %q: each collector must have a unique ID", c.ID()))
		}
		m[c.ID()] = c
	}
	return m
}

// ResultTypeRegistry returns a map from CollectorID to a zero-value pointer of
// the concrete result type produced by that collector. It is used by the offline
// analysis path to unmarshal the JSON Data field in vitals.json back into the
// correct Go type so that typed accessors (GetXxxResult) work without change.
func ResultTypeRegistry() map[engine.CollectorID]any {
	return map[engine.CollectorID]any{
		NodeResourcesCollectorID:               &NodeResourcesResult{},
		OSInfoCollectorID:                      &OSInfoResult{},
		ScyllaVersionCollectorID:               &ScyllaVersionResult{},
		SchemaVersionsCollectorID:              &SchemaVersionsResult{},
		ScyllaConfigCollectorID:                &ScyllaConfigResult{},
		SystemPeersLocalCollectorID:            &SystemPeersLocalResult{},
		GossipInfoCollectorID:                  &GossipInfoResult{},
		SystemTopologyCollectorID:              &SystemTopologyResult{},
		SystemConfigCollectorID:                &SystemConfigResult{},
		ScyllaDConfigCollectorID:               &ScyllaDConfigResult{},
		DiskUsageCollectorID:                   &DiskUsageResult{},
		RlimitsCollectorID:                     &RlimitsResult{},
		ScyllaClusterCollectorID:               &ScyllaClusterResult{},
		ScyllaDBDatacenterCollectorID:          &ScyllaDBDatacenterResult{},
		NodeManifestCollectorID:                &NodeManifestResult{},
		NodeConfigCollectorID:                  &NodeConfigResult{},
		ScyllaOperatorConfigCollectorID:        &ScyllaOperatorConfigResult{},
		DeploymentCollectorID:                  &DeploymentResult{},
		StatefulSetCollectorID:                 &StatefulSetResult{},
		DaemonSetCollectorID:                   &DaemonSetResult{},
		ConfigMapCollectorID:                   &ConfigMapResult{},
		ServiceCollectorID:                     &ServiceResult{},
		ServiceAccountCollectorID:              &ServiceAccountResult{},
		PodManifestCollectorID:                 &PodManifestResult{},
		ScyllaClusterStatefulSetCollectorID:    &ScyllaClusterStatefulSetResult{},
		ScyllaClusterServiceCollectorID:        &ScyllaClusterServiceResult{},
		ScyllaClusterConfigMapCollectorID:      &ScyllaClusterConfigMapResult{},
		ScyllaClusterPodCollectorID:            &ScyllaClusterPodResult{},
		ScyllaClusterPDBCollectorID:            &ScyllaClusterPDBResult{},
		ScyllaClusterServiceAccountCollectorID: &ScyllaClusterServiceAccountResult{},
		ScyllaClusterRoleBindingCollectorID:    &ScyllaClusterRoleBindingResult{},
		ScyllaClusterPVCCollectorID:            &ScyllaClusterPVCResult{},
		// Log collectors.
		ScyllaNodeLogsCollectorID:       &ScyllaNodeLogsResult{},
		OperatorPodLogsCollectorID:      &OperatorPodLogsResult{},
		ScyllaClusterJobLogsCollectorID: &ScyllaClusterJobLogsResult{},
	}
}
