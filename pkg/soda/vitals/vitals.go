// Package vitals holds the data formats SODA shares with Scylla Doctor.
//
// Scylla Doctor's contracts are `status` and `message` of per-node vitals, per-node analysis and the
// Scylla Doctor Cluster report. Collector `data` is not a contract, so SODA never interprets the `data` of
// Scylla Doctor's own collectors; it only reads the `data` of collectors SODA itself writes.
package vitals

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// CollectorStatus is a collector's status in vitals (Scylla Doctor's collectors_base.CollectorStatus).
type CollectorStatus int

const (
	CollectorPassed  CollectorStatus = 0
	CollectorFailed  CollectorStatus = 1
	CollectorSkipped CollectorStatus = 2
)

func (s CollectorStatus) String() string {
	switch s {
	case CollectorPassed:
		return "PASSED"
	case CollectorFailed:
		return "FAILED"
	case CollectorSkipped:
		return "SKIPPED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(s))
	}
}

// AnalyzerStatus is an analyzer's status in analysis output (Scylla Doctor's analyzers_base.AnalyzerStatus).
// Note the numbering differs from CollectorStatus.
type AnalyzerStatus int

const (
	AnalyzerPassed  AnalyzerStatus = 0
	AnalyzerSkipped AnalyzerStatus = 1
	AnalyzerWarning AnalyzerStatus = 2
	AnalyzerFailed  AnalyzerStatus = 3
)

func (s AnalyzerStatus) String() string {
	switch s {
	case AnalyzerPassed:
		return "PASSED"
	case AnalyzerSkipped:
		return "SKIPPED"
	case AnalyzerWarning:
		return "WARNING"
	case AnalyzerFailed:
		return "FAILED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(s))
	}
}

// CollectorResult is one collector's entry in a vitals file: {"Collector": {"status", "data", "message", "mask"}}.
type CollectorResult struct {
	Status  CollectorStatus `json:"status"`
	Data    any             `json:"data"`
	Message string          `json:"message"`
	Mask    []any           `json:"mask"`
}

// Vitals is a vitals file, keyed by collector name.
type Vitals map[string]CollectorResult

// AnalyzerResult is one analyzer's entry in per-node analysis output (`--load-vitals ... --output json`).
type AnalyzerResult struct {
	Description string         `json:"description"`
	Status      AnalyzerStatus `json:"status"`
	Message     string         `json:"message"`
}

// Analysis is per-node analysis output, keyed by analyzer name.
type Analysis map[string]AnalyzerResult

// Findings maps an analyzer name to message -> affected nodes, as in the Scylla Doctor Cluster report.
type Findings map[string]map[string][]string

// ClusterReport is the Scylla Doctor Cluster JSON report (`--output json`).
// SODA's combined report (soda-report.json) uses the same top-level keys and adds an optional `soda` key, so a
// reader of the Scylla Doctor Cluster report can read it unchanged.
type ClusterReport struct {
	Vitals          []string                   `json:"vitals"`
	Failures        Findings                   `json:"failures"`
	Warnings        Findings                   `json:"warnings"`
	Inconsistencies map[string]json.RawMessage `json:"inconsistencies"`
	SODA            *SODAExtension             `json:"soda,omitempty"`
}

// SODAExtension is SODA's backward-compatible addition to the report format.
type SODAExtension struct {
	FormatVersion       string            `json:"formatVersion"`
	GeneratedAt         string            `json:"generatedAt"`
	SODAVersion         string            `json:"sodaVersion"`
	ScyllaDoctorVersion string            `json:"scyllaDoctorVersion,omitempty"`
	Bundle              *BundleInfo       `json:"bundle,omitempty"`
	Cluster             ClusterRef        `json:"cluster"`
	Nodes               []NodeSummary     `json:"nodes"`
	Sources             map[string]string `json:"sources"`
	SkippedAnalyzers    []string          `json:"skippedAnalyzers,omitempty"`
	Errors              []string          `json:"errors,omitempty"`
}

type BundleInfo struct {
	Image               string            `json:"image,omitempty"`
	ScyllaDoctorVersion string            `json:"scyllaDoctorVersion"`
	Files               map[string]string `json:"files"`
}

type ClusterRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid,omitempty"`
}

// NodeSummary describes one ScyllaDB node (Pod) in the run.
type NodeSummary struct {
	Name           string         `json:"name"`
	Pod            string         `json:"pod"`
	KubernetesNode string         `json:"kubernetesNode,omitempty"`
	Datacenter     string         `json:"datacenter,omitempty"`
	Rack           string         `json:"rack,omitempty"`
	Collection     NodeCollection `json:"collection"`
	Collectors     map[string]int `json:"collectors,omitempty"`
	Analyzers      map[string]int `json:"analyzers,omitempty"`
}

// NodeCollection says whether Scylla Doctor's collection on a node can be trusted.
// Exit codes can't tell (Scylla Doctor exits 0 with failed checks), so SODA judges by whether the vitals parse and
// whether the checks everything else depends on passed.
type NodeCollection struct {
	State           string  `json:"state"`
	Reason          string  `json:"reason,omitempty"`
	DurationSeconds float64 `json:"durationSeconds"`
	ExitCode        *int    `json:"exitCode,omitempty"`
}

const (
	NodeCollectionSucceeded = "Succeeded"
	NodeCollectionDegraded  = "Degraded"
	NodeCollectionFailed    = "Failed"
)

// SODA source names in SODAExtension.Sources.
const (
	SourceScyllaDoctor = "scylla-doctor"
	SourceSODA         = "soda"
)

func ReadJSONFile[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, fmt.Errorf("can't parse %q: %w", path, err)
	}
	return v, nil
}

func WriteJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// CountCollectors returns collector counts by status name.
func CountCollectors(v Vitals) map[string]int {
	out := map[string]int{}
	for _, r := range v {
		out[r.Status.String()]++
	}
	return out
}

// CountAnalyzers returns analyzer counts by status name.
func CountAnalyzers(a Analysis) map[string]int {
	out := map[string]int{}
	for _, r := range a {
		out[r.Status.String()]++
	}
	return out
}

// AddFinding records a node under analyzer/message, keeping node lists sorted and unique.
func (f Findings) AddFinding(analyzer, message, node string) {
	if f[analyzer] == nil {
		f[analyzer] = map[string][]string{}
	}
	nodes := f[analyzer][message]
	for _, n := range nodes {
		if n == node {
			return
		}
	}
	nodes = append(nodes, node)
	sort.Strings(nodes)
	f[analyzer][message] = nodes
}

// SODAVersion identifies the SODA build that wrote a file.
func SODAVersion(gitCommit string) string {
	if len(gitCommit) == 0 {
		return "development build"
	}
	return gitCommit
}
