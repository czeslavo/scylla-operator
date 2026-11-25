package collectors

import (
	"fmt"
	"sort"

	"github.com/scylladb/scylla-operator/pkg/build"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/contract"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	corev1 "k8s.io/api/core/v1"
)

// SODA's own collectors. Their entries use Scylla Doctor's vitals format and live in a separate per-node vitals file
// (soda/vitals/<node>.vitals.json): Scylla Doctor's tools reject vitals without their SDVersionCollector, and
// Scylla Doctor's own files stay byte-identical in the archive.
const (
	SODAVersionCollector             = "SODAVersionCollector"
	ScyllaDBPodCollector             = "ScyllaDBPodCollector"
	ScyllaDBRuntimeContractCollector = "ScyllaDBRuntimeContractCollector"
)

func passed(data any) vitals.CollectorResult {
	return vitals.CollectorResult{Status: vitals.CollectorPassed, Data: data, Message: "Data collected", Mask: []any{}}
}

func failed(message string) vitals.CollectorResult {
	return vitals.CollectorResult{Status: vitals.CollectorFailed, Data: map[string]any{}, Message: message, Mask: []any{}}
}

// SODAVitals builds a node's SODA vitals from what CollectScyllaDoctor gathered and the Pod object.
func SODAVitals(out *NodeOutput, c *contract.Contract) vitals.Vitals {
	v := vitals.Vitals{}
	v[SODAVersionCollector] = passed(map[string]any{
		"version":       vitals.SODAVersion(build.GitCommit()),
		"formatVersion": "soda.scylladb.com/vitals/v1alpha1",
	})
	v[ScyllaDBPodCollector] = passed(podData(out.Pod))

	switch {
	case len(out.ContractError) > 0:
		v[ScyllaDBRuntimeContractCollector] = failed(fmt.Sprintf("can't run the runtime contract check: %s", out.ContractError))
	default:
		r := passed(map[string]any{
			"schemaVersion":       c.SchemaVersion,
			"scyllaDoctorVersion": c.ScyllaDoctorVersion,
			"interpreter":         c.Interpreter,
			"results":             out.ContractResults,
		})
		v[ScyllaDBRuntimeContractCollector] = r
	}
	return v
}

type containerData struct {
	Name                  string `json:"name"`
	Ready                 bool   `json:"ready"`
	RestartCount          int32  `json:"restartCount"`
	State                 string `json:"state"`
	LastTerminationReason string `json:"lastTerminationReason,omitempty"`
	LastTerminationExit   *int32 `json:"lastTerminationExitCode,omitempty"`
	LastTerminationAt     string `json:"lastTerminationAt,omitempty"`
}

// podData describes the ScyllaDB Pod as Kubernetes sees it. Ephemeral containers (kubectl debug) are left out:
// they stay in the Pod spec after they end and say nothing about ScyllaDB.
func podData(pod *corev1.Pod) map[string]any {
	ready := false
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			ready = c.Status == corev1.ConditionTrue
		}
	}
	var containers []containerData
	for _, cs := range pod.Status.ContainerStatuses {
		cd := containerData{Name: cs.Name, Ready: cs.Ready, RestartCount: cs.RestartCount}
		switch {
		case cs.State.Running != nil:
			cd.State = "Running"
		case cs.State.Waiting != nil:
			cd.State = "Waiting: " + cs.State.Waiting.Reason
		case cs.State.Terminated != nil:
			cd.State = "Terminated: " + cs.State.Terminated.Reason
		}
		if t := cs.LastTerminationState.Terminated; t != nil {
			cd.LastTerminationReason = t.Reason
			cd.LastTerminationExit = &t.ExitCode
			cd.LastTerminationAt = t.FinishedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		containers = append(containers, cd)
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })

	resources := map[string]any{}
	for _, c := range pod.Spec.Containers {
		if c.Name == naming.ScyllaContainerName {
			resources = map[string]any{
				"requests": resourceList(c.Resources.Requests),
				"limits":   resourceList(c.Resources.Limits),
			}
		}
	}

	return map[string]any{
		"namespace":       pod.Namespace,
		"name":            pod.Name,
		"kubernetesNode":  pod.Spec.NodeName,
		"phase":           string(pod.Status.Phase),
		"ready":           ready,
		"qosClass":        string(pod.Status.QOSClass),
		"datacenter":      pod.Labels["scylla/datacenter"],
		"rack":            pod.Labels["scylla/rack"],
		"scyllaVersion":   pod.Labels["scylla/scylla-version"],
		"containers":      containers,
		"scyllaResources": resources,
	}
}

func resourceList(rl corev1.ResourceList) map[string]string {
	out := map[string]string{}
	for k, v := range rl {
		out[string(k)] = v.String()
	}
	return out
}
