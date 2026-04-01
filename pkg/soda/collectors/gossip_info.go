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
	result, ok := vitals.Get(GossipInfoCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("GossipInfoCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("GossipInfoCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*GossipInfoResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for GossipInfoCollector", result.Data)
	}
	return typed, nil
}

// ReadGossipInfoJSON reads the gossip-info.json artifact.
func ReadGossipInfoJSON(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(GossipInfoCollectorID, podKey, "gossip-info.json")
}

// gossipInfoCollector collects gossip state from the Scylla REST API.
type gossipInfoCollector struct{}

var _ engine.Collector = (*gossipInfoCollector)(nil)

// NewGossipInfoCollector creates a new GossipInfoCollector.
func NewGossipInfoCollector() engine.Collector {
	return &gossipInfoCollector{}
}

func (c *gossipInfoCollector) ID() engine.CollectorID          { return GossipInfoCollectorID }
func (c *gossipInfoCollector) Name() string                    { return "Gossip info" }
func (c *gossipInfoCollector) Scope() engine.CollectorScope    { return engine.PerScyllaNode }
func (c *gossipInfoCollector) DependsOn() []engine.CollectorID { return nil }

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

func (c *gossipInfoCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.ScyllaNode == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	stdout, _, err := params.PodExecutor.Execute(ctx, params.ScyllaNode.Namespace, params.ScyllaNode.Name, scyllaContainerName,
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
	if params.ArtifactWriter != nil {
		if relPath, werr := params.ArtifactWriter.WriteArtifact("gossip-info.json", []byte(raw)); werr == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Raw Scylla REST API gossip endpoint state"})
		}
	}

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
