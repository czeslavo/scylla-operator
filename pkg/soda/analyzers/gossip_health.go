package analyzers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const (
	// GossipHealthAnalyzerID is the unique identifier for the GossipHealthAnalyzer.
	GossipHealthAnalyzerID engine.AnalyzerID = "GossipHealthAnalyzer"
)

// gossipHealthAnalyzer checks that all gossip endpoints report is_alive == true.
type gossipHealthAnalyzer struct{}

var _ engine.Analyzer = (*gossipHealthAnalyzer)(nil)

// NewGossipHealthAnalyzer creates a new GossipHealthAnalyzer.
func NewGossipHealthAnalyzer() engine.Analyzer {
	return &gossipHealthAnalyzer{}
}

func (a *gossipHealthAnalyzer) ID() engine.AnalyzerID { return GossipHealthAnalyzerID }
func (a *gossipHealthAnalyzer) Name() string          { return "Gossip health check" }
func (a *gossipHealthAnalyzer) Scope() engine.AnalyzerScope {
	return engine.AnalyzerPerScyllaCluster
}
func (a *gossipHealthAnalyzer) DependsOn() []engine.CollectorID {
	return []engine.CollectorID{collectors.GossipInfoCollectorID}
}

func (a *gossipHealthAnalyzer) Analyze(params engine.AnalyzerParams) *engine.AnalyzerResult {
	// Each pod in the cluster sees all endpoints via gossip. We use the first
	// pod whose GossipInfoCollector succeeded; they should all be equivalent.
	podKeys := params.Vitals.ScyllaNodeKeys()
	if len(podKeys) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No pod gossip data available",
		}
	}

	var gossipResult *collectors.GossipInfoResult
	for _, podKey := range podKeys {
		r, err := collectors.GetGossipInfoResult(params.Vitals, podKey)
		if err == nil {
			gossipResult = r
			break
		}
	}

	if gossipResult == nil {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "No gossip info available from any pod",
		}
	}

	if len(gossipResult.Endpoints) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerWarning,
			Message: "Gossip returned no endpoints",
		}
	}

	var dead []string
	for _, ep := range gossipResult.Endpoints {
		if !ep.IsAlive {
			dead = append(dead, ep.Addrs)
		}
	}

	if len(dead) == 0 {
		return &engine.AnalyzerResult{
			Status:  engine.AnalyzerPassed,
			Message: fmt.Sprintf("All %d gossip endpoints are alive", len(gossipResult.Endpoints)),
		}
	}

	sort.Strings(dead)
	return &engine.AnalyzerResult{
		Status:  engine.AnalyzerFailed,
		Message: fmt.Sprintf("%d of %d gossip endpoints are dead: %s", len(dead), len(gossipResult.Endpoints), strings.Join(dead, ", ")),
	}
}
