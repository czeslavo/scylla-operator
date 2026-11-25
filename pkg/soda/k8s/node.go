package k8s

import (
	"context"
	"fmt"
	"sort"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	PodCollectorName            = "ScyllaDBPodCollector"
	StorageCollectorName        = "ScyllaDBStorageCollector"
	KubernetesNodeCollectorName = "KubernetesNodeCollector"
)

// PodCollector writes the ScyllaDB Pod, its member Service, its containers' logs and its events, and records the
// Pod's state.
type PodCollector struct{}

func (*PodCollector) Name() string { return PodCollectorName }

func (*PodCollector) Collect(ctx context.Context, env *Env, pod *corev1.Pod) vitals.CollectorResult {
	f := newFiles(env, NodeDir(pod.Name))
	var problems []string
	if err := f.object("pod.yaml", "v1", "Pod", pod); err != nil {
		return failed(fmt.Sprintf("can't write the Pod: %v", err))
	}
	data := podData(pod)

	// The operator gives every node a Service with the Pod's name; its ClusterIP is the node's broadcast address.
	svc, err := env.KubeClient.CoreV1().Services(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	switch {
	case err == nil:
		if err := f.object("service.yaml", "v1", "Service", svc); err != nil {
			problems = append(problems, fmt.Sprintf("can't write the member Service: %v", err))
		}
		data["memberServiceIP"] = svc.Spec.ClusterIP
	case apierrors.IsNotFound(err):
		problems = append(problems, "no member Service with the Pod's name")
	default:
		problems = append(problems, fmt.Sprintf("can't get the member Service: %v", err))
	}

	// Logs: current, and previous for containers that restarted.
	statuses := map[string]corev1.ContainerStatus{}
	for _, cs := range append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...) {
		statuses[cs.Name] = cs
	}
	var containers []string
	for _, c := range pod.Spec.InitContainers {
		containers = append(containers, c.Name)
	}
	for _, c := range pod.Spec.Containers {
		containers = append(containers, c.Name)
	}
	for _, c := range containers {
		cs, started := statuses[c]
		if !started || (cs.State.Waiting != nil && cs.RestartCount == 0) {
			continue
		}
		if err := writeLog(ctx, env, f, pod, c, false); err != nil {
			problems = append(problems, err.Error())
		}
		if cs.RestartCount > 0 {
			if err := writeLog(ctx, env, f, pod, c, true); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}

	if env.Events != nil {
		if err := f.events("events.yaml", eventsFor(env, map[string]bool{"Pod/" + pod.Name: true})); err != nil {
			problems = append(problems, fmt.Sprintf("can't write events: %v", err))
		}
	}
	return result(data, f, problems)
}

func writeLog(ctx context.Context, env *Env, f *files, pod *corev1.Pod, container string, previous bool) error {
	name := container + ".log"
	if previous {
		name = container + ".previous.log"
	}
	return writePodLog(ctx, env, f, pod.Namespace, pod.Name, container, previous, "logs/"+name)
}

// writePodLog streams a container's log into the archive. With --log-tail-lines it keeps the newest lines: the
// API's limitBytes would keep the oldest and drop the ones a diagnosis needs most.
func writePodLog(ctx context.Context, env *Env, f *files, namespace, pod, container string, previous bool, rel string) error {
	opts := &corev1.PodLogOptions{Container: container, Previous: previous, Timestamps: true}
	if env.LogsTailLines > 0 {
		opts.TailLines = &env.LogsTailLines
	}
	stream, err := env.KubeClient.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
	if err != nil {
		return fmt.Errorf("can't get logs for %s: %v", rel, err)
	}
	defer stream.Close()
	if err := f.copy(rel, stream); err != nil {
		return fmt.Errorf("can't write %s: %v", rel, err)
	}
	return nil
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

// StorageCollector writes the node's data volume claim and, when allowed, its PersistentVolume.
type StorageCollector struct{}

func (*StorageCollector) Name() string { return StorageCollectorName }

func (*StorageCollector) Collect(ctx context.Context, env *Env, pod *corev1.Pod) vitals.CollectorResult {
	f := newFiles(env, NodeDir(pod.Name))
	var problems []string
	var volumes []map[string]any
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		pvc, err := env.KubeClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, v.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if err != nil {
			problems = append(problems, fmt.Sprintf("can't get PersistentVolumeClaim %s: %v", v.PersistentVolumeClaim.ClaimName, err))
			continue
		}
		if err := f.object("pvc-"+pvc.Name+".yaml", "v1", "PersistentVolumeClaim", pvc); err != nil {
			problems = append(problems, err.Error())
		}
		d := map[string]any{
			"volume":       v.Name,
			"claim":        pvc.Name,
			"phase":        string(pvc.Status.Phase),
			"accessModes":  pvc.Spec.AccessModes,
			"requested":    pvc.Spec.Resources.Requests.Storage().String(),
			"capacity":     pvc.Status.Capacity.Storage().String(),
			"storageClass": ptrString(pvc.Spec.StorageClassName),
		}
		if pvName := pvc.Spec.VolumeName; len(pvName) > 0 {
			pv, err := env.KubeClient.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
			switch {
			case err == nil:
				if err := f.object("pv-"+pv.Name+".yaml", "v1", "PersistentVolume", pv); err != nil {
					problems = append(problems, err.Error())
				}
				d["reclaimPolicy"] = string(pv.Spec.PersistentVolumeReclaimPolicy)
				if pv.Spec.CSI != nil {
					d["csiDriver"] = pv.Spec.CSI.Driver
				}
			case isForbidden(err):
				problems = append(problems, forbiddenMessage("PersistentVolumes (cluster-scoped)", "rbac-cluster-read.yaml"))
			default:
				problems = append(problems, fmt.Sprintf("can't get PersistentVolume %s: %v", pvName, err))
			}
		}
		volumes = append(volumes, d)
	}
	if len(volumes) == 0 && len(problems) == 0 {
		return skipped("The Pod has no PersistentVolumeClaims")
	}
	return result(map[string]any{"volumes": volumes}, f, problems)
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// KubernetesNodeCollector writes the Kubernetes Node the ScyllaDB Pod runs on. Nodes are cluster-scoped, so this
// needs the optional cluster read role.
type KubernetesNodeCollector struct{}

func (*KubernetesNodeCollector) Name() string { return KubernetesNodeCollectorName }

func (*KubernetesNodeCollector) Collect(ctx context.Context, env *Env, pod *corev1.Pod) vitals.CollectorResult {
	if len(pod.Spec.NodeName) == 0 {
		return skipped("The Pod is not scheduled")
	}
	node, err := env.KubeClient.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	switch {
	case isForbidden(err):
		return skipped(forbiddenMessage("Nodes (cluster-scoped)", "rbac-cluster-read.yaml"))
	case err != nil:
		return failed(fmt.Sprintf("can't get Node %s: %v", pod.Spec.NodeName, err))
	}
	f := newFiles(env, NodeDir(pod.Name))
	var problems []string
	if err := f.object("kubernetes-node.yaml", "v1", "Node", node); err != nil {
		problems = append(problems, err.Error())
	}
	conditions := map[string]string{}
	for _, c := range node.Status.Conditions {
		conditions[string(c.Type)] = string(c.Status)
	}
	info := node.Status.NodeInfo
	return result(map[string]any{
		"name":             node.Name,
		"kubeletVersion":   info.KubeletVersion,
		"containerRuntime": info.ContainerRuntimeVersion,
		"osImage":          info.OSImage,
		"kernelVersion":    info.KernelVersion,
		"architecture":     info.Architecture,
		"instanceType":     node.Labels[corev1.LabelInstanceTypeStable],
		"zone":             node.Labels[corev1.LabelTopologyZone],
		"capacity":         resourceList(node.Status.Capacity),
		"allocatable":      resourceList(node.Status.Allocatable),
		"conditions":       conditions,
		"taints":           len(node.Spec.Taints),
	}, f, problems)
}
