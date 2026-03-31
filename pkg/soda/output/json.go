package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// JSONReport is the top-level structure for JSON diagnostic output.
type JSONReport struct {
	Metadata   JSONMetadata                                         `json:"metadata"`
	Targets    JSONTargets                                          `json:"targets"`
	Collectors JSONCollectors                                       `json:"collectors"`
	Analysis   map[engine.AnalyzerID]map[string]*JSONAnalyzerResult `json:"analysis"`
}

// JSONMetadata holds report metadata.
type JSONMetadata struct {
	Timestamp   string `json:"timestamp"`
	ToolVersion string `json:"tool_version"`
	Profile     string `json:"profile"`
}

// JSONTargets holds information about targeted ScyllaClusters.
type JSONTargets struct {
	ScyllaClusters []JSONScyllaClusterTarget `json:"scylla_clusters"`
}

// JSONScyllaClusterTarget holds a single ScyllaCluster target for JSON output.
type JSONScyllaClusterTarget struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Kind      string   `json:"kind"`
	Pods      []string `json:"pods"`
}

// JSONCollectors holds all collector results grouped by scope.
type JSONCollectors struct {
	ClusterWide      map[engine.CollectorID]*JSONCollectorResult            `json:"cluster_wide"`
	PerScyllaCluster map[string]map[engine.CollectorID]*JSONCollectorResult `json:"per_scylla_cluster"`
	PerPod           map[string]map[engine.CollectorID]*JSONCollectorResult `json:"per_pod"`
}

// JSONCollectorResult represents a single collector result in JSON output.
type JSONCollectorResult struct {
	Status    string            `json:"status"`
	Message   string            `json:"message"`
	Data      json.RawMessage   `json:"data,omitempty"`
	Artifacts []engine.Artifact `json:"artifacts,omitempty"`
}

// JSONAnalyzerResult represents a single analyzer result in JSON output.
type JSONAnalyzerResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// JSONWriter writes diagnostic results as JSON.
type JSONWriter struct {
	w           io.Writer
	toolVersion string
}

// NewJSONWriter creates a JSONWriter that writes to w.
func NewJSONWriter(w io.Writer, toolVersion string) *JSONWriter {
	return &JSONWriter{w: w, toolVersion: toolVersion}
}

// WriteReport writes the full diagnostic report as JSON.
func (j *JSONWriter) WriteReport(result *engine.EngineResult, profileName string, clusters []engine.ScyllaClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) error {
	report := j.BuildReport(result, profileName, clusters, pods)

	encoder := json.NewEncoder(j.w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encoding JSON report: %w", err)
	}
	return nil
}

// BuildReport constructs the full JSONReport structure from engine results.
// This is useful for both writing to stdout and persisting as report.json.
func (j *JSONWriter) BuildReport(result *engine.EngineResult, profileName string, clusters []engine.ScyllaClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) *JSONReport {
	report := &JSONReport{
		Metadata: JSONMetadata{
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			ToolVersion: j.toolVersion,
			Profile:     profileName,
		},
		Targets:    j.buildTargets(clusters, pods),
		Collectors: j.buildCollectors(result),
		Analysis:   j.buildAnalysis(result),
	}
	return report
}

func (j *JSONWriter) buildTargets(clusters []engine.ScyllaClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) JSONTargets {
	targets := JSONTargets{
		ScyllaClusters: make([]JSONScyllaClusterTarget, 0, len(clusters)),
	}

	for _, cluster := range clusters {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		podNames := make([]string, 0)
		for _, pod := range pods[clusterKey] {
			podNames = append(podNames, pod.Name)
		}

		targets.ScyllaClusters = append(targets.ScyllaClusters, JSONScyllaClusterTarget{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Kind:      cluster.Kind,
			Pods:      podNames,
		})
	}

	return targets
}

func (j *JSONWriter) buildCollectors(result *engine.EngineResult) JSONCollectors {
	collectors := JSONCollectors{
		ClusterWide:      make(map[engine.CollectorID]*JSONCollectorResult),
		PerScyllaCluster: make(map[string]map[engine.CollectorID]*JSONCollectorResult),
		PerPod:           make(map[string]map[engine.CollectorID]*JSONCollectorResult),
	}

	// ClusterWide.
	for id, res := range result.Vitals.ClusterWide {
		collectors.ClusterWide[id] = toJSONCollectorResult(res)
	}

	// PerScyllaCluster.
	for key, perScyllaCluster := range result.Vitals.PerScyllaCluster {
		keyStr := key.String()
		collectors.PerScyllaCluster[keyStr] = make(map[engine.CollectorID]*JSONCollectorResult)
		for id, res := range perScyllaCluster {
			collectors.PerScyllaCluster[keyStr][id] = toJSONCollectorResult(res)
		}
	}

	// PerPod.
	for key, perPod := range result.Vitals.PerPod {
		keyStr := key.String()
		collectors.PerPod[keyStr] = make(map[engine.CollectorID]*JSONCollectorResult)
		for id, res := range perPod {
			collectors.PerPod[keyStr][id] = toJSONCollectorResult(res)
		}
	}

	return collectors
}

func (j *JSONWriter) buildAnalysis(result *engine.EngineResult) map[engine.AnalyzerID]map[string]*JSONAnalyzerResult {
	analysis := make(map[engine.AnalyzerID]map[string]*JSONAnalyzerResult, len(result.AnalyzerResults))
	for id, byScope := range result.AnalyzerResults {
		inner := make(map[string]*JSONAnalyzerResult, len(byScope))
		for scopeKey, res := range byScope {
			inner[scopeKey.String()] = &JSONAnalyzerResult{
				Status:  statusToString(res.Status),
				Message: res.Message,
			}
		}
		analysis[id] = inner
	}
	return analysis
}

func toJSONCollectorResult(res *engine.CollectorResult) *JSONCollectorResult {
	jr := &JSONCollectorResult{
		Status:    collectorStatusToString(res.Status),
		Message:   res.Message,
		Artifacts: res.Artifacts,
	}

	// Marshal the data field if present.
	if res.Data != nil {
		if data, err := json.Marshal(res.Data); err == nil {
			jr.Data = data
		}
	}

	return jr
}

func collectorStatusToString(s engine.CollectorStatus) string {
	switch s {
	case engine.CollectorPassed:
		return "passed"
	case engine.CollectorFailed:
		return "failed"
	case engine.CollectorSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

func statusToString(s engine.AnalyzerStatus) string {
	switch s {
	case engine.AnalyzerPassed:
		return "passed"
	case engine.AnalyzerWarning:
		return "warning"
	case engine.AnalyzerFailed:
		return "failed"
	case engine.AnalyzerSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}
