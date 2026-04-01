package collectors

import (
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// AllCollectors returns the complete list of PoC collectors.
func AllCollectors() []engine.Collector {
	return []engine.Collector{
		NewNodeResourcesCollector(),
		NewScyllaClusterStatusCollector(),
		NewOSInfoCollector(),
		NewScyllaVersionCollector(),
		NewSchemaVersionsCollector(),
		NewScyllaConfigCollector(),
		NewSystemPeersLocalCollector(),
		NewGossipInfoCollector(),
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
		NodeResourcesCollectorID:       &NodeResourcesResult{},
		ScyllaClusterStatusCollectorID: &ScyllaClusterStatusResult{},
		OSInfoCollectorID:              &OSInfoResult{},
		ScyllaVersionCollectorID:       &ScyllaVersionResult{},
		SchemaVersionsCollectorID:      &SchemaVersionsResult{},
		ScyllaConfigCollectorID:        &ScyllaConfigResult{},
		SystemPeersLocalCollectorID:    &SystemPeersLocalResult{},
		GossipInfoCollectorID:          &GossipInfoResult{},
	}
}
