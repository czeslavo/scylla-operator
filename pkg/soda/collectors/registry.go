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
