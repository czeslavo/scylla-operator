package report

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/contract"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
)

//go:embed report.html.tmpl
var htmlTemplate string

type finding struct {
	Analyzer string
	Source   string
	Message  string
	Nodes    []string
}

type inconsistencyPair struct {
	A, B string
	Diff string
}

type inconsistency struct {
	Collector string
	Source    string
	Pairs     []inconsistencyPair
}

type contractCell struct {
	Satisfied bool
	Detail    string
	Known     bool
}

type contractRow struct {
	ID, Level, UsedBy, Impact string
	Cells                     []contractCell
}

type podRow struct {
	Name, KubernetesNode, Phase, QOSClass, Restarts string
	Ready                                           bool
}

type htmlData struct {
	Report          *vitals.ClusterReport
	Ext             *vitals.SODAExtension
	Verdict         string
	VerdictClass    string
	NFailures       int
	NWarnings       int
	NInconsistent   int
	Failures        []finding
	Warnings        []finding
	Inconsistencies []inconsistency
	NodeNames       []string
	Contract        []contractRow
	ContractNote    string
	Pods            []podRow
}

// RenderHTML renders the combined report. sodaVitals are SODA's per-node vitals (by node name); only SODA's own
// collectors' data is read from them.
func RenderHTML(rep *vitals.ClusterReport, sodaVitals map[string]vitals.Vitals, c *contract.Contract) ([]byte, error) {
	d := htmlData{Report: rep, Ext: rep.SODA}
	d.NFailures, d.NWarnings, d.NInconsistent = Totals(rep)
	switch {
	case d.NFailures > 0:
		d.Verdict, d.VerdictClass = "Needs attention", "bad"
	case d.NWarnings > 0 || d.NInconsistent > 0:
		d.Verdict, d.VerdictClass = "Review the warnings", "warn"
	default:
		d.Verdict, d.VerdictClass = "No findings", "good"
	}
	d.Failures = findings(rep.Failures, rep.SODA)
	d.Warnings = findings(rep.Warnings, rep.SODA)
	d.Inconsistencies = inconsistencies(rep)

	for _, n := range rep.SODA.Nodes {
		d.NodeNames = append(d.NodeNames, n.Name)
	}
	if c != nil {
		d.ContractNote = fmt.Sprintf("Scylla Doctor %s, interpreter %s", c.ScyllaDoctorVersion, c.Interpreter)
		d.Contract = contractRows(c, d.NodeNames, sodaVitals)
	}
	d.Pods = podRows(d.NodeNames, sodaVitals)

	funcs := template.FuncMap{
		"join":   strings.Join,
		"source": sourceLabel,
		"count":  func(m map[string]int, k string) int { return m[k] },
	}
	t, err := template.New("report").Funcs(funcs).Parse(htmlTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sourceLabel(s string) string {
	if s == vitals.SourceSODA {
		return "SODA"
	}
	return "Scylla Doctor"
}

func findings(f vitals.Findings, ext *vitals.SODAExtension) []finding {
	var out []finding
	for a, msgs := range f {
		for m, nodes := range msgs {
			out = append(out, finding{Analyzer: a, Source: ext.Sources[a], Message: m, Nodes: nodes})
		}
	}
	// SODA's findings first (they explain gaps in the rest), then by how many nodes are affected.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Source == vitals.SourceSODA) != (out[j].Source == vitals.SourceSODA) {
			return out[i].Source == vitals.SourceSODA
		}
		if len(out[i].Nodes) != len(out[j].Nodes) {
			return len(out[i].Nodes) > len(out[j].Nodes)
		}
		if out[i].Analyzer != out[j].Analyzer {
			return out[i].Analyzer < out[j].Analyzer
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func inconsistencies(rep *vitals.ClusterReport) []inconsistency {
	var out []inconsistency
	for c, raw := range rep.Inconsistencies {
		inc := inconsistency{Collector: c, Source: rep.SODA.Sources[c]}
		var pairs []json.RawMessage
		if err := json.Unmarshal(raw, &pairs); err == nil {
			for _, p := range pairs {
				var triple []json.RawMessage
				if err := json.Unmarshal(p, &triple); err != nil || len(triple) < 3 {
					continue
				}
				var a, b string
				_ = json.Unmarshal(triple[0], &a)
				_ = json.Unmarshal(triple[1], &b)
				var diff bytes.Buffer
				_ = json.Indent(&diff, triple[2], "", "  ")
				s := diff.String()
				if len(s) > 4000 {
					s = s[:4000] + "\n…"
				}
				inc.Pairs = append(inc.Pairs, inconsistencyPair{A: a, B: b, Diff: s})
			}
		}
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Collector < out[j].Collector })
	return out
}

func contractRows(c *contract.Contract, nodes []string, sv map[string]vitals.Vitals) []contractRow {
	perNode := map[string]map[string]contract.Result{}
	for _, n := range nodes {
		perNode[n] = map[string]contract.Result{}
		v, ok := sv[n]
		if !ok || v[collectors.ScyllaDBRuntimeContractCollector].Status != vitals.CollectorPassed {
			continue
		}
		b, _ := json.Marshal(v[collectors.ScyllaDBRuntimeContractCollector].Data)
		var data struct {
			Results []contract.Result `json:"results"`
		}
		if json.Unmarshal(b, &data) == nil {
			for _, r := range data.Results {
				perNode[n][r.ID] = r
			}
		}
	}
	var rows []contractRow
	for _, req := range c.Requirements {
		row := contractRow{ID: req.ID, Level: req.Level, UsedBy: req.UsedBy, Impact: req.Impact}
		for _, n := range nodes {
			r, ok := perNode[n][req.ID]
			row.Cells = append(row.Cells, contractCell{Satisfied: r.Satisfied, Detail: r.Detail, Known: ok})
		}
		rows = append(rows, row)
	}
	return rows
}

func podRows(nodes []string, sv map[string]vitals.Vitals) []podRow {
	var rows []podRow
	for _, n := range nodes {
		v, ok := sv[n]
		if !ok {
			continue
		}
		b, _ := json.Marshal(v[collectors.ScyllaDBPodCollector].Data)
		var data struct {
			KubernetesNode string `json:"kubernetesNode"`
			Phase          string `json:"phase"`
			Ready          bool   `json:"ready"`
			QOSClass       string `json:"qosClass"`
			Containers     []struct {
				Name         string `json:"name"`
				RestartCount int32  `json:"restartCount"`
			} `json:"containers"`
		}
		if json.Unmarshal(b, &data) != nil {
			continue
		}
		var restarts []string
		for _, c := range data.Containers {
			if c.RestartCount > 0 {
				restarts = append(restarts, fmt.Sprintf("%s: %d", c.Name, c.RestartCount))
			}
		}
		r := "none"
		if len(restarts) > 0 {
			r = strings.Join(restarts, ", ")
		}
		rows = append(rows, podRow{Name: n, KubernetesNode: data.KubernetesNode, Phase: data.Phase, QOSClass: data.QOSClass,
			Ready: data.Ready, Restarts: r})
	}
	return rows
}
