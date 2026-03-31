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
	Metadata   JSONMetadata                              `json:"metadata"`
	Targets    JSONTargets                               `json:"targets"`
	Collectors JSONCollectors                            `json:"collectors"`
	Analysis   map[engine.AnalyzerID]*JSONAnalyzerResult `json:"analysis"`
}

// JSONMetadata holds report metadata.
type JSONMetadata struct {
	Timestamp   string `json:"timestamp"`
	ToolVersion string `json:"tool_version"`
	Profile     string `json:"profile"`
}

// JSONTargets holds information about targeted clusters.
type JSONTargets struct {
	Clusters []JSONClusterTarget `json:"clusters"`
}

// JSONClusterTarget holds a single cluster target for JSON output.
type JSONClusterTarget struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Kind      string   `json:"kind"`
	Pods      []string `json:"pods"`
}

// JSONCollectors holds all collector results grouped by scope.
type JSONCollectors struct {
	ClusterWide map[engine.CollectorID]*JSONCollectorResult            `json:"cluster_wide"`
	PerCluster  map[string]map[engine.CollectorID]*JSONCollectorResult `json:"per_cluster"`
	PerPod      map[string]map[engine.CollectorID]*JSONCollectorResult `json:"per_pod"`
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
func (j *JSONWriter) WriteReport(result *engine.EngineResult, profileName string, clusters []engine.ClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) error {
	report := j.buildReport(result, profileName, clusters, pods)

	encoder := json.NewEncoder(j.w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encoding JSON report: %w", err)
	}
	return nil
}

func (j *JSONWriter) buildReport(result *engine.EngineResult, profileName string, clusters []engine.ClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) *JSONReport {
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

func (j *JSONWriter) buildTargets(clusters []engine.ClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) JSONTargets {
	targets := JSONTargets{
		Clusters: make([]JSONClusterTarget, 0, len(clusters)),
	}

	for _, cluster := range clusters {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		podNames := make([]string, 0)
		for _, pod := range pods[clusterKey] {
			podNames = append(podNames, pod.Name)
		}

		targets.Clusters = append(targets.Clusters, JSONClusterTarget{
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
		ClusterWide: make(map[engine.CollectorID]*JSONCollectorResult),
		PerCluster:  make(map[string]map[engine.CollectorID]*JSONCollectorResult),
		PerPod:      make(map[string]map[engine.CollectorID]*JSONCollectorResult),
	}

	// ClusterWide.
	for id, res := range result.Vitals.ClusterWide {
		collectors.ClusterWide[id] = toJSONCollectorResult(res)
	}

	// PerCluster.
	for key, perCluster := range result.Vitals.PerCluster {
		keyStr := key.String()
		collectors.PerCluster[keyStr] = make(map[engine.CollectorID]*JSONCollectorResult)
		for id, res := range perCluster {
			collectors.PerCluster[keyStr][id] = toJSONCollectorResult(res)
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

func (j *JSONWriter) buildAnalysis(result *engine.EngineResult) map[engine.AnalyzerID]*JSONAnalyzerResult {
	analysis := make(map[engine.AnalyzerID]*JSONAnalyzerResult, len(result.AnalyzerResults))
	for id, res := range result.AnalyzerResults {
		analysis[id] = &JSONAnalyzerResult{
			Status:  statusToString(res.Status),
			Message: res.Message,
		}
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
