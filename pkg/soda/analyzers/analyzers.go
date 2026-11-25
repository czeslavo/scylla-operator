// Package analyzers holds SODA's own analyzers. They read SODA's vitals (never the data of Scylla Doctor's
// collectors) and produce results in Scylla Doctor's per-node analysis format, so SODA can merge them into the
// same report.
package analyzers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/contract"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
)

type Analyzer interface {
	Name() string
	Description() string
	DependsOn() []string
	Analyze(v vitals.Vitals) vitals.AnalyzerResult
}

func All() []Analyzer {
	return []Analyzer{
		&RuntimeContractAnalyzer{},
		&PodAnalyzer{},
	}
}

// Run applies every analyzer to one node's SODA vitals. Like Scylla Doctor, an analyzer whose collector didn't pass
// is skipped (collector skipped) or failed (collector failed) with a "Required ..." message.
func Run(v vitals.Vitals) vitals.Analysis {
	out := vitals.Analysis{}
	for _, a := range All() {
		res, ok := checkDependencies(a, v)
		if !ok {
			out[a.Name()] = res
			continue
		}
		res = a.Analyze(v)
		res.Description = a.Description()
		out[a.Name()] = res
	}
	return out
}

func checkDependencies(a Analyzer, v vitals.Vitals) (vitals.AnalyzerResult, bool) {
	for _, dep := range a.DependsOn() {
		c, ok := v[dep]
		switch {
		case !ok:
			return vitals.AnalyzerResult{Description: a.Description(), Status: vitals.AnalyzerSkipped,
				Message: fmt.Sprintf("Required %s is missing", dep)}, false
		case c.Status == vitals.CollectorSkipped:
			return vitals.AnalyzerResult{Description: a.Description(), Status: vitals.AnalyzerSkipped,
				Message: fmt.Sprintf("Required %s was skipped", dep)}, false
		case c.Status == vitals.CollectorFailed:
			return vitals.AnalyzerResult{Description: a.Description(), Status: vitals.AnalyzerFailed,
				Message: fmt.Sprintf("Required %s was not successful", dep)}, false
		}
	}
	return vitals.AnalyzerResult{}, true
}

// decode re-reads a SODA collector's data into a typed value.
func decode[T any](v vitals.Vitals, collector string) (T, error) {
	var out T
	b, err := json.Marshal(v[collector].Data)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

// RuntimeContractAnalyzer reports whether the ScyllaDB image provides what Scylla Doctor needs.
type RuntimeContractAnalyzer struct{}

func (*RuntimeContractAnalyzer) Name() string { return "ScyllaDBRuntimeContractAnalyzer" }
func (*RuntimeContractAnalyzer) Description() string {
	return "ScyllaDB image provides the runtime Scylla Doctor needs"
}
func (*RuntimeContractAnalyzer) DependsOn() []string {
	return []string{collectors.ScyllaDBRuntimeContractCollector}
}

func (a *RuntimeContractAnalyzer) Analyze(v vitals.Vitals) vitals.AnalyzerResult {
	data, err := decode[struct {
		Results []contract.Result `json:"results"`
	}](v, collectors.ScyllaDBRuntimeContractCollector)
	if err != nil {
		return vitals.AnalyzerResult{Status: vitals.AnalyzerFailed, Message: fmt.Sprintf("can't read the contract results: %v", err)}
	}
	missing := map[string][]string{}
	for _, r := range data.Results {
		if !r.Satisfied {
			missing[r.Level] = append(missing[r.Level], fmt.Sprintf("%s (%s)", r.ID, r.Detail))
		}
	}
	switch {
	case len(missing[contract.LevelRequired]) > 0:
		return vitals.AnalyzerResult{Status: vitals.AnalyzerFailed,
			Message: "Required runtime missing from the ScyllaDB image: " + strings.Join(missing[contract.LevelRequired], ", ")}
	case len(missing[contract.LevelRecommended]) > 0:
		return vitals.AnalyzerResult{Status: vitals.AnalyzerWarning,
			Message: "Recommended runtime missing from the ScyllaDB image: " + strings.Join(missing[contract.LevelRecommended], ", ")}
	default:
		msg := "The ScyllaDB image provides every required and recommended runtime item"
		if len(missing[contract.LevelOptional]) > 0 {
			msg += "; optional items missing: " + strings.Join(missing[contract.LevelOptional], ", ")
		}
		return vitals.AnalyzerResult{Status: vitals.AnalyzerPassed, Message: msg}
	}
}

// PodAnalyzer is the example Kubernetes-side analyzer: is the ScyllaDB Pod ready, stable and in the Guaranteed QoS
// class that production deployments need for CPU pinning?
type PodAnalyzer struct{}

func (*PodAnalyzer) Name() string        { return "ScyllaDBPodAnalyzer" }
func (*PodAnalyzer) Description() string { return "ScyllaDB Pod is ready, stable and Guaranteed" }
func (*PodAnalyzer) DependsOn() []string { return []string{collectors.ScyllaDBPodCollector} }

func (a *PodAnalyzer) Analyze(v vitals.Vitals) vitals.AnalyzerResult {
	data, err := decode[struct {
		Ready      bool   `json:"ready"`
		Phase      string `json:"phase"`
		QOSClass   string `json:"qosClass"`
		Containers []struct {
			Name                  string `json:"name"`
			RestartCount          int32  `json:"restartCount"`
			LastTerminationReason string `json:"lastTerminationReason"`
		} `json:"containers"`
	}](v, collectors.ScyllaDBPodCollector)
	if err != nil {
		return vitals.AnalyzerResult{Status: vitals.AnalyzerFailed, Message: fmt.Sprintf("can't read the Pod data: %v", err)}
	}

	var failures, warnings []string
	if !data.Ready {
		failures = append(failures, fmt.Sprintf("Pod is not ready (phase %s)", data.Phase))
	}
	var restarts []string
	for _, c := range data.Containers {
		if c.Name == naming.ScyllaContainerName && c.LastTerminationReason == "OOMKilled" {
			failures = append(failures, "the scylla container was OOMKilled")
		}
		if c.RestartCount > 0 {
			r := fmt.Sprintf("%s restarted %d times", c.Name, c.RestartCount)
			if len(c.LastTerminationReason) > 0 {
				r += fmt.Sprintf(" (last: %s)", c.LastTerminationReason)
			}
			restarts = append(restarts, r)
		}
	}
	sort.Strings(restarts)
	warnings = append(warnings, restarts...)
	if data.QOSClass != "Guaranteed" {
		warnings = append(warnings, fmt.Sprintf("QoS class is %s, not Guaranteed: ScyllaDB can't get dedicated CPUs", data.QOSClass))
	}

	switch {
	case len(failures) > 0:
		return vitals.AnalyzerResult{Status: vitals.AnalyzerFailed, Message: strings.Join(append(failures, warnings...), "; ")}
	case len(warnings) > 0:
		return vitals.AnalyzerResult{Status: vitals.AnalyzerWarning, Message: strings.Join(warnings, "; ")}
	default:
		return vitals.AnalyzerResult{Status: vitals.AnalyzerPassed, Message: "Pod is ready, Guaranteed and has not restarted"}
	}
}
