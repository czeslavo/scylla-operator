package collectors

import (
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
	}
}

// AllCollectorsMap returns a map of all collectors keyed by ID.
func AllCollectorsMap() map[engine.CollectorID]engine.Collector {
	m := make(map[engine.CollectorID]engine.Collector)
	for _, c := range AllCollectors() {
		m[c.ID()] = c
	}
	return m
}
