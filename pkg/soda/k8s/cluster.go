package k8s

import (
	"context"
	"fmt"
	"sort"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	ScyllaClusterCollectorName = "ScyllaClusterCollector"
	OperatorCollectorName      = "ScyllaOperatorCollector"
)

// ScyllaClusterCollector writes the ScyllaCluster, its ScyllaDBDatacenter and the cluster-wide objects the operator
// owns for it, with their events, and records the cluster's conditions and rack status. Per-node objects (member
// Services, PersistentVolumeClaims) belong to the nodes' directories.
type ScyllaClusterCollector struct{}

func (*ScyllaClusterCollector) Name() string { return ScyllaClusterCollectorName }

func (*ScyllaClusterCollector) Collect(ctx context.Context, env *Env, sc *scyllav1.ScyllaCluster) vitals.CollectorResult {
	f := newFiles(env, ClusterDir())
	var problems []string
	objects := map[string]bool{"ScyllaCluster/" + sc.Name: true}
	if err := f.object("scyllacluster.yaml", "scylla.scylladb.com/v1", "ScyllaCluster", sc); err != nil {
		return failed(fmt.Sprintf("can't write the ScyllaCluster: %v", err))
	}

	sdc, err := env.ScyllaClient.ScyllaV1alpha1().ScyllaDBDatacenters(sc.Namespace).Get(ctx, sc.Name, metav1.GetOptions{})
	switch {
	case err == nil:
		objects["ScyllaDBDatacenter/"+sdc.Name] = true
		if err := f.object("scylladbdatacenter.yaml", "scylla.scylladb.com/v1alpha1", "ScyllaDBDatacenter", sdc); err != nil {
			problems = append(problems, err.Error())
		}
	case !apierrors.IsNotFound(err):
		problems = append(problems, fmt.Sprintf("can't get the ScyllaDBDatacenter: %v", err))
	}

	// Cluster-wide objects the operator labels with the cluster's name. Member Services are named after the Pods
	// and go with the nodes.
	sel := metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name}).String()}
	pods, err := env.KubeClient.CoreV1().Pods(sc.Namespace).List(ctx, sel)
	memberNames := map[string]bool{}
	if err == nil {
		for _, p := range pods.Items {
			memberNames[p.Name] = true
		}
	}

	if l, err := env.KubeClient.AppsV1().StatefulSets(sc.Namespace).List(ctx, sel); err != nil {
		problems = append(problems, fmt.Sprintf("can't list StatefulSets: %v", err))
	} else {
		for i := range l.Items {
			o := &l.Items[i]
			objects["StatefulSet/"+o.Name] = true
			if err := f.object("statefulsets/"+o.Name+".yaml", "apps/v1", "StatefulSet", o); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if l, err := env.KubeClient.CoreV1().Services(sc.Namespace).List(ctx, sel); err != nil {
		problems = append(problems, fmt.Sprintf("can't list Services: %v", err))
	} else {
		for i := range l.Items {
			o := &l.Items[i]
			if memberNames[o.Name] {
				continue
			}
			objects["Service/"+o.Name] = true
			if err := f.object("services/"+o.Name+".yaml", "v1", "Service", o); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if l, err := env.KubeClient.CoreV1().ConfigMaps(sc.Namespace).List(ctx, sel); err != nil {
		problems = append(problems, fmt.Sprintf("can't list ConfigMaps: %v", err))
	} else {
		for i := range l.Items {
			o := &l.Items[i]
			objects["ConfigMap/"+o.Name] = true
			if err := f.object("configmaps/"+o.Name+".yaml", "v1", "ConfigMap", o); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if l, err := env.KubeClient.PolicyV1().PodDisruptionBudgets(sc.Namespace).List(ctx, sel); err != nil {
		problems = append(problems, fmt.Sprintf("can't list PodDisruptionBudgets: %v", err))
	} else {
		for i := range l.Items {
			o := &l.Items[i]
			objects["PodDisruptionBudget/"+o.Name] = true
			if err := f.object("poddisruptionbudgets/"+o.Name+".yaml", "policy/v1", "PodDisruptionBudget", o); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if env.Events != nil {
		if err := f.events("events.yaml", eventsFor(env, objects)); err != nil {
			problems = append(problems, fmt.Sprintf("can't write events: %v", err))
		}
	}

	conditions := []map[string]string{}
	for _, c := range sc.Status.Conditions {
		conditions = append(conditions, map[string]string{
			"type": c.Type, "status": string(c.Status), "reason": c.Reason, "message": c.Message,
		})
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i]["type"] < conditions[j]["type"] })
	racks := map[string]any{}
	for name, r := range sc.Status.Racks {
		racks[name] = map[string]any{
			"version":          r.Version,
			"members":          r.Members,
			"readyMembers":     r.ReadyMembers,
			"availableMembers": r.AvailableMembers,
			"updatedMembers":   r.UpdatedMembers,
		}
	}
	var observed int64
	if sc.Status.ObservedGeneration != nil {
		observed = *sc.Status.ObservedGeneration
	}
	return result(map[string]any{
		"name":               sc.Name,
		"version":            sc.Spec.Version,
		"generation":         sc.Generation,
		"observedGeneration": observed,
		"conditions":         conditions,
		"racks":              racks,
	}, f, problems)
}

// OperatorCollector writes the operator's configuration, Deployment and logs. They live outside the
// ScyllaCluster's namespace, so this needs the optional operator read role; without it the collector is skipped.
type OperatorCollector struct{}

func (*OperatorCollector) Name() string { return OperatorCollectorName }

func (*OperatorCollector) Collect(ctx context.Context, env *Env, _ *scyllav1.ScyllaCluster) vitals.CollectorResult {
	f := newFiles(env, ClusterDir())
	var problems []string
	data := map[string]any{"namespace": env.OperatorNamespace}
	forbidden := 0

	soc, err := env.ScyllaClient.ScyllaV1alpha1().ScyllaOperatorConfigs().Get(ctx, naming.SingletonName, metav1.GetOptions{})
	switch {
	case err == nil:
		if err := f.object("operator/scyllaoperatorconfig.yaml", "scylla.scylladb.com/v1alpha1", "ScyllaOperatorConfig", soc); err != nil {
			problems = append(problems, err.Error())
		}
		data["scyllaUtilsImage"] = soc.Status.ScyllaDBUtilsImage
	case isForbidden(err):
		forbidden++
		problems = append(problems, forbiddenMessage("ScyllaOperatorConfigs (cluster-scoped)", "rbac-cluster-read.yaml"))
	default:
		problems = append(problems, fmt.Sprintf("can't get the ScyllaOperatorConfig: %v", err))
	}

	dep, err := env.KubeClient.AppsV1().Deployments(env.OperatorNamespace).Get(ctx, "scylla-operator", metav1.GetOptions{})
	switch {
	case err == nil:
		if err := f.object("operator/deployment.yaml", "apps/v1", "Deployment", dep); err != nil {
			problems = append(problems, err.Error())
		}
		var images []string
		for _, c := range dep.Spec.Template.Spec.Containers {
			images = append(images, c.Image)
		}
		data["images"] = images
		data["replicas"] = dep.Status.Replicas
		data["readyReplicas"] = dep.Status.ReadyReplicas
	case isForbidden(err):
		forbidden++
		problems = append(problems, forbiddenMessage("the operator's Deployment", "rbac-operator-read.yaml"))
	default:
		problems = append(problems, fmt.Sprintf("can't get the operator Deployment: %v", err))
	}

	pods, err := env.KubeClient.CoreV1().Pods(env.OperatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=scylla-operator",
	})
	switch {
	case err == nil:
		for i := range pods.Items {
			p := &pods.Items[i]
			if err := writeOperatorLog(ctx, env, f, p.Namespace, p.Name); err != nil {
				problems = append(problems, err.Error())
			}
		}
	case isForbidden(err):
		forbidden++
		problems = append(problems, forbiddenMessage("the operator's Pods and logs", "rbac-operator-read.yaml"))
	default:
		problems = append(problems, fmt.Sprintf("can't list the operator Pods: %v", err))
	}

	if forbidden == 3 {
		return skipped("Not allowed to read the operator's configuration, Deployment or logs; grant it with " +
			"examples/soda/rbac-cluster-read.yaml and examples/soda/rbac-operator-read.yaml")
	}
	return result(data, f, problems)
}

func writeOperatorLog(ctx context.Context, env *Env, f *files, namespace, pod string) error {
	return writePodLog(ctx, env, f, namespace, pod, "", false, "operator/logs/"+pod+".log")
}
