package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// GossipInfoCollectorID is the unique identifier for the GossipInfoCollector.
	GossipInfoCollectorID engine.CollectorID = "GossipInfoCollector"
)

// GossipApplicationState holds a single application state entry from gossip.
type GossipApplicationState struct {
	ApplicationState int    `json:"application_state"`
	Value            string `json:"value"`
	Version          int    `json:"version"`
}

// GossipEndpoint holds gossip state for a single endpoint (node).
type GossipEndpoint struct {
	Addrs            string                   `json:"addrs"`
	Generation       int64                    `json:"generation"`
	Version          int64                    `json:"version"`
	UpdateTime       int64                    `json:"update_time"`
	IsAlive          bool                     `json:"is_alive"`
	ApplicationState []GossipApplicationState `json:"application_state"`
}

// GossipInfoResult holds the gossip state for all known endpoints.
type GossipInfoResult struct {
	Endpoints []GossipEndpoint `json:"endpoints"`
}

// GetGossipInfoResult is the typed accessor for GossipInfoCollector results.
func GetGossipInfoResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*GossipInfoResult, error) {
	return engine.GetResult[GossipInfoResult](vitals, GossipInfoCollectorID, podKey)
}

// ReadGossipInfoJSON reads the gossip-info.json artifact.
func ReadGossipInfoJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(GossipInfoCollectorID, podKey, "gossip-info.json")
}

// gossipInfoCollector collects gossip state from the Scylla REST API.
type gossipInfoCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*gossipInfoCollector)(nil)

// NewGossipInfoCollector creates a new GossipInfoCollector.
func NewGossipInfoCollector() engine.PerScyllaNodeCollector {
	return &gossipInfoCollector{CollectorBase: engine.NewCollectorBase(GossipInfoCollectorID, "Gossip info", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to curl the Scylla REST API at localhost:10000)
func (c *gossipInfoCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *gossipInfoCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	stdout, err := ExecInScyllaPod(ctx, params,
		[]string{"curl", "-s", "http://localhost:10000/failure_detector/endpoints"})
	if err != nil {
		return nil, fmt.Errorf("querying gossip info: %w", err)
	}

	raw := strings.TrimSpace(stdout)
	result, err := parseGossipInfo(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing gossip info response: %w", err)
	}

	// Write artifact.
	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "gossip-info.json", []byte(raw), "Raw Scylla REST API gossip endpoint state", &artifacts)

	alive := 0
	for _, ep := range result.Endpoints {
		if ep.IsAlive {
			alive++
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("%d endpoints, %d alive", len(result.Endpoints), alive),
		Artifacts: artifacts,
	}, nil
}

// parseGossipInfo parses the JSON array returned by /failure_detector/endpoints.
func parseGossipInfo(raw string) (*GossipInfoResult, error) {
	if raw == "" {
		return &GossipInfoResult{}, nil
	}

	var endpoints []GossipEndpoint
	if err := json.Unmarshal([]byte(raw), &endpoints); err != nil {
		return nil, fmt.Errorf("unmarshaling gossip info JSON: %w", err)
	}

	return &GossipInfoResult{Endpoints: endpoints}, nil
}
