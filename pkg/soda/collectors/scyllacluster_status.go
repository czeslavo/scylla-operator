package collectors

import (
	"context"
	"fmt"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	"sigs.k8s.io/yaml"
)

const (
	// ScyllaClusterStatusCollectorID is the unique identifier for the ScyllaClusterStatusCollector.
	ScyllaClusterStatusCollectorID engine.CollectorID = "ScyllaClusterStatusCollector"
)

// ScyllaClusterStatusResult holds the parsed cluster status data.
type ScyllaClusterStatusResult struct {
	Name               string                 `json:"name"`
	Namespace          string                 `json:"namespace"`
	Kind               string                 `json:"kind"` // "ScyllaCluster" or "ScyllaDBDatacenter"
	Generation         int64                  `json:"generation"`
	ObservedGeneration int64                  `json:"observed_generation"`
	Members            int32                  `json:"members"`
	ReadyMembers       int32                  `json:"ready_members"`
	AvailableMembers   int32                  `json:"available_members"`
	Conditions         []ClusterConditionInfo `json:"conditions"`
	Racks              []RackStatusInfo       `json:"racks"`
}

// ClusterConditionInfo holds a single cluster condition.
type ClusterConditionInfo struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// RackStatusInfo holds per-rack status.
type RackStatusInfo struct {
	Name         string `json:"name"`
	Members      int32  `json:"members"`
	ReadyMembers int32  `json:"ready_members"`
}

// GetScyllaClusterStatusResult is the typed accessor for ScyllaClusterStatusCollector results.
func GetScyllaClusterStatusResult(vitals *engine.Vitals, scopeKey engine.ScopeKey) (*ScyllaClusterStatusResult, error) {
	result, ok := vitals.Get(ScyllaClusterStatusCollectorID, scopeKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaClusterStatusCollector result not found for %v", scopeKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaClusterStatusCollector did not pass for %v: %s", scopeKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaClusterStatusResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaClusterStatusCollector", result.Data)
	}
	return typed, nil
}

// ReadManifestYAML reads the raw manifest.yaml artifact.
func ReadManifestYAML(reader engine.ArtifactReader, scopeKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(ScyllaClusterStatusCollectorID, scopeKey, "manifest.yaml")
}

// scyllaClusterStatusCollector collects ScyllaCluster/ScyllaDBDatacenter status.
type scyllaClusterStatusCollector struct{}

// NewScyllaClusterStatusCollector creates a new ScyllaClusterStatusCollector.
func NewScyllaClusterStatusCollector() engine.Collector {
	return &scyllaClusterStatusCollector{}
}

func (c *scyllaClusterStatusCollector) ID() engine.CollectorID          { return ScyllaClusterStatusCollectorID }
func (c *scyllaClusterStatusCollector) Name() string                    { return "ScyllaDB cluster status" }
func (c *scyllaClusterStatusCollector) Scope() engine.CollectorScope    { return engine.PerScyllaCluster }
func (c *scyllaClusterStatusCollector) DependsOn() []engine.CollectorID { return nil }

func (c *scyllaClusterStatusCollector) Collect(_ context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.Cluster == nil {
		return nil, fmt.Errorf("cluster info not provided")
	}

	var statusResult *ScyllaClusterStatusResult

	switch obj := params.Cluster.Object.(type) {
	case *scyllav1.ScyllaCluster:
		statusResult = extractScyllaClusterStatus(obj)
	case *scyllav1alpha1.ScyllaDBDatacenter:
		statusResult = extractScyllaDBDatacenterStatus(obj)
	default:
		return nil, fmt.Errorf("unsupported cluster object type: %T", params.Cluster.Object)
	}

	// Write artifact.
	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		yamlBytes, err := yaml.Marshal(params.Cluster.Object)
		if err != nil {
			return nil, fmt.Errorf("marshaling cluster manifest to YAML: %w", err)
		}
		relPath, err := params.ArtifactWriter.WriteArtifact("manifest.yaml", yamlBytes)
		if err != nil {
			return nil, fmt.Errorf("writing manifest.yaml artifact: %w", err)
		}
		artifacts = append(artifacts, engine.Artifact{
			RelativePath: relPath,
			Description:  fmt.Sprintf("%s manifest YAML", statusResult.Kind),
		})
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      statusResult,
		Message:   fmt.Sprintf("%s/%s: %d/%d members ready", statusResult.Namespace, statusResult.Name, statusResult.ReadyMembers, statusResult.Members),
		Artifacts: artifacts,
	}, nil
}

func extractScyllaClusterStatus(sc *scyllav1.ScyllaCluster) *ScyllaClusterStatusResult {
	result := &ScyllaClusterStatusResult{
		Name:       sc.Name,
		Namespace:  sc.Namespace,
		Kind:       "ScyllaCluster",
		Generation: sc.Generation,
	}

	if sc.Status.ObservedGeneration != nil {
		result.ObservedGeneration = *sc.Status.ObservedGeneration
	}
	if sc.Status.Members != nil {
		result.Members = *sc.Status.Members
	}
	if sc.Status.ReadyMembers != nil {
		result.ReadyMembers = *sc.Status.ReadyMembers
	}
	if sc.Status.AvailableMembers != nil {
		result.AvailableMembers = *sc.Status.AvailableMembers
	}

	for _, cond := range sc.Status.Conditions {
		result.Conditions = append(result.Conditions, ClusterConditionInfo{
			Type:    string(cond.Type),
			Status:  string(cond.Status),
			Message: cond.Message,
		})
	}

	for rackName, rackStatus := range sc.Status.Racks {
		result.Racks = append(result.Racks, RackStatusInfo{
			Name:         rackName,
			Members:      rackStatus.Members,
			ReadyMembers: rackStatus.ReadyMembers,
		})
	}

	return result
}

func extractScyllaDBDatacenterStatus(sdc *scyllav1alpha1.ScyllaDBDatacenter) *ScyllaClusterStatusResult {
	result := &ScyllaClusterStatusResult{
		Name:       sdc.Name,
		Namespace:  sdc.Namespace,
		Kind:       "ScyllaDBDatacenter",
		Generation: sdc.Generation,
	}

	if sdc.Status.ObservedGeneration != nil {
		result.ObservedGeneration = *sdc.Status.ObservedGeneration
	}
	if sdc.Status.Nodes != nil {
		result.Members = *sdc.Status.Nodes
	}
	if sdc.Status.ReadyNodes != nil {
		result.ReadyMembers = *sdc.Status.ReadyNodes
	}
	if sdc.Status.AvailableNodes != nil {
		result.AvailableMembers = *sdc.Status.AvailableNodes
	}

	for _, cond := range sdc.Status.Conditions {
		result.Conditions = append(result.Conditions, ClusterConditionInfo{
			Type:    string(cond.Type),
			Status:  string(cond.Status),
			Message: cond.Message,
		})
	}

	for _, rackStatus := range sdc.Status.Racks {
		rack := RackStatusInfo{
			Name: rackStatus.Name,
		}
		if rackStatus.Nodes != nil {
			rack.Members = *rackStatus.Nodes
		}
		if rackStatus.ReadyNodes != nil {
			rack.ReadyMembers = *rackStatus.ReadyNodes
		}
		result.Racks = append(result.Racks, rack)
	}

	return result
}
