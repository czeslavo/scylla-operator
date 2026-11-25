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
	"github.com/scylladb/scylla-operator/pkg/soda/k8s"
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
	NodeInfo, Storage                               string
	Files                                           int
}

type clusterView struct {
	Collected      bool
	Message        string
	Version        string
	Conditions     []conditionView
	Racks          []rackRow
	Operator       string
	Files          int
	SkippedReasons []string
}

type conditionView struct {
	Type, Status string
	Healthy      bool
}

type rackRow struct {
	Name                      string
	Members, Ready, Available int
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
	Cluster         clusterView
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
	d.Cluster = clusterViewOf(sodaVitals[ClusterScope])

	funcs := template.FuncMap{
		"join": strings.Join,
		"nodes": func(ns []string) string {
			out := make([]string, 0, len(ns))
			for _, n := range ns {
				if n == ClusterScope {
					n = "the cluster"
				}
				out = append(out, n)
			}
			return strings.Join(out, ", ")
		},
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

func decodeData(v vitals.Vitals, collector string, out any) bool {
	r, ok := v[collector]
	if !ok || r.Status != vitals.CollectorPassed {
		return false
	}
	b, err := json.Marshal(r.Data)
	return err == nil && json.Unmarshal(b, out) == nil
}

func podRows(nodes []string, sv map[string]vitals.Vitals) []podRow {
	var rows []podRow
	for _, n := range nodes {
		v, ok := sv[n]
		if !ok {
			continue
		}
		var pod struct {
			KubernetesNode string `json:"kubernetesNode"`
			Phase          string `json:"phase"`
			Ready          bool   `json:"ready"`
			QOSClass       string `json:"qosClass"`
			Containers     []struct {
				Name         string `json:"name"`
				RestartCount int32  `json:"restartCount"`
			} `json:"containers"`
			Files []vitals.FileRef `json:"files"`
		}
		if !decodeData(v, k8s.PodCollectorName, &pod) {
			continue
		}
		var restarts []string
		for _, c := range pod.Containers {
			if c.RestartCount > 0 {
				restarts = append(restarts, fmt.Sprintf("%s: %d", c.Name, c.RestartCount))
			}
		}
		row := podRow{Name: n, KubernetesNode: pod.KubernetesNode, Phase: pod.Phase, QOSClass: pod.QOSClass,
			Ready: pod.Ready, Restarts: "none", Files: len(pod.Files)}
		if len(restarts) > 0 {
			row.Restarts = strings.Join(restarts, ", ")
		}

		var node struct {
			KubeletVersion string           `json:"kubeletVersion"`
			InstanceType   string           `json:"instanceType"`
			Files          []vitals.FileRef `json:"files"`
		}
		if decodeData(v, k8s.KubernetesNodeCollectorName, &node) {
			row.NodeInfo = strings.TrimSpace(node.KubeletVersion + " " + node.InstanceType)
			row.Files += len(node.Files)
		} else if r, ok := v[k8s.KubernetesNodeCollectorName]; ok {
			row.NodeInfo = "not collected: " + r.Message
		}

		var storage struct {
			Volumes []struct {
				StorageClass string `json:"storageClass"`
				Capacity     string `json:"capacity"`
			} `json:"volumes"`
			Files []vitals.FileRef `json:"files"`
		}
		if decodeData(v, k8s.StorageCollectorName, &storage) {
			var parts []string
			for _, vol := range storage.Volumes {
				parts = append(parts, strings.TrimSpace(vol.Capacity+" "+vol.StorageClass))
			}
			row.Storage = strings.Join(parts, ", ")
			row.Files += len(storage.Files)
		}
		rows = append(rows, row)
	}
	return rows
}

func clusterViewOf(v vitals.Vitals) clusterView {
	cv := clusterView{}
	if v == nil {
		return cv
	}
	var sc struct {
		Version    string              `json:"version"`
		Conditions []map[string]string `json:"conditions"`
		Racks      map[string]struct {
			Members          int  `json:"members"`
			ReadyMembers     int  `json:"readyMembers"`
			AvailableMembers *int `json:"availableMembers"`
		} `json:"racks"`
		Files []vitals.FileRef `json:"files"`
	}
	if decodeData(v, k8s.ScyllaClusterCollectorName, &sc) {
		cv.Collected = true
		cv.Version, cv.Files = sc.Version, len(sc.Files)
		// The aggregate conditions only; the per-controller ones stay in the vitals and the YAML.
		for _, c := range sc.Conditions {
			switch c["type"] {
			case "Available":
				cv.Conditions = append(cv.Conditions, conditionView{c["type"], c["status"], c["status"] == "True"})
			case "Progressing", "Degraded":
				cv.Conditions = append(cv.Conditions, conditionView{c["type"], c["status"], c["status"] == "False"})
			}
		}
		for name, r := range sc.Racks {
			row := rackRow{Name: name, Members: r.Members, Ready: r.ReadyMembers}
			if r.AvailableMembers != nil {
				row.Available = *r.AvailableMembers
			}
			cv.Racks = append(cv.Racks, row)
		}
		sort.Slice(cv.Racks, func(i, j int) bool { return cv.Racks[i].Name < cv.Racks[j].Name })
	} else if r, ok := v[k8s.ScyllaClusterCollectorName]; ok {
		cv.Message = r.Message
	}

	var op struct {
		Images        []string         `json:"images"`
		ReadyReplicas int32            `json:"readyReplicas"`
		Replicas      int32            `json:"replicas"`
		Problems      []string         `json:"problems"`
		Files         []vitals.FileRef `json:"files"`
	}
	if decodeData(v, k8s.OperatorCollectorName, &op) {
		if len(op.Images) > 0 {
			cv.Operator = fmt.Sprintf("%s, %d/%d ready", strings.Join(op.Images, ", "), op.ReadyReplicas, op.Replicas)
		}
		cv.Files += len(op.Files)
		cv.SkippedReasons = append(cv.SkippedReasons, op.Problems...)
	} else if r, ok := v[k8s.OperatorCollectorName]; ok {
		cv.SkippedReasons = append(cv.SkippedReasons, r.Message)
	}
	return cv
}
