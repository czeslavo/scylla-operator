// Package k8s provides Kubernetes-backed implementations of the engine
// interfaces (PodExecutor, PodLogFetcher, ResourceLister).
package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"

	scyllaversionedclient "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
)

// PodExecutor implements engine.PodExecutor using Kubernetes SPDY exec.
type PodExecutor struct {
	restConfig *rest.Config
	kubeClient kubernetes.Interface
}

// NewPodExecutor creates a new PodExecutor.
func NewPodExecutor(restConfig *rest.Config, kubeClient kubernetes.Interface) *PodExecutor {
	return &PodExecutor{restConfig: restConfig, kubeClient: kubeClient}
}

func (e *PodExecutor) Execute(ctx context.Context, namespace, podName, containerName string, command []string) (string, string, error) {
	req := e.kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(e.restConfig, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("creating SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("executing command: %w", err)
	}

	return stdout.String(), stderr.String(), nil
}

// PodLogFetcher implements engine.PodLogFetcher using the Kubernetes pods/log API.
type PodLogFetcher struct {
	kubeClient kubernetes.Interface
}

var _ engine.PodLogFetcher = (*PodLogFetcher)(nil)

// NewPodLogFetcher creates a new PodLogFetcher.
func NewPodLogFetcher(kubeClient kubernetes.Interface) *PodLogFetcher {
	return &PodLogFetcher{kubeClient: kubeClient}
}

func (f *PodLogFetcher) GetPodLogs(ctx context.Context, namespace, podName, containerName string, previous bool) ([]byte, error) {
	req := f.kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  containerName,
		Previous:   previous,
		Timestamps: true,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening log stream for %s/%s/%s (previous=%v): %w", namespace, podName, containerName, previous, err)
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("reading log stream for %s/%s/%s (previous=%v): %w", namespace, podName, containerName, previous, err)
	}
	return data, nil
}

// ResourceLister implements engine.ResourceLister using the Kubernetes and
// Scylla API clients.
type ResourceLister struct {
	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface
}

var _ engine.ResourceLister = (*ResourceLister)(nil)

// NewResourceLister creates a new ResourceLister.
func NewResourceLister(kubeClient kubernetes.Interface, scyllaClient scyllaversionedclient.Interface) *ResourceLister {
	return &ResourceLister{kubeClient: kubeClient, scyllaClient: scyllaClient}
}

func (l *ResourceLister) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList, err := l.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	return nodeList.Items, nil
}

func (l *ResourceLister) ListPods(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Pod, error) {
	podList, err := l.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	return podList.Items, nil
}

func (l *ResourceLister) ListConfigMaps(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.ConfigMap, error) {
	list, err := l.kubeClient.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing configmaps: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListServices(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Service, error) {
	list, err := l.kubeClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListServiceAccounts(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.ServiceAccount, error) {
	list, err := l.kubeClient.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing serviceaccounts: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListPersistentVolumeClaims(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.PersistentVolumeClaim, error) {
	list, err := l.kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing persistentvolumeclaims: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListDeployments(ctx context.Context, namespace string, selector labels.Selector) ([]appsv1.Deployment, error) {
	list, err := l.kubeClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListStatefulSets(ctx context.Context, namespace string, selector labels.Selector) ([]appsv1.StatefulSet, error) {
	list, err := l.kubeClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListDaemonSets(ctx context.Context, namespace string, selector labels.Selector) ([]appsv1.DaemonSet, error) {
	list, err := l.kubeClient.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing daemonsets: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListPodDisruptionBudgets(ctx context.Context, namespace string, selector labels.Selector) ([]policyv1.PodDisruptionBudget, error) {
	list, err := l.kubeClient.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing poddisruptionbudgets: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListRoleBindings(ctx context.Context, namespace string, selector labels.Selector) ([]rbacv1.RoleBinding, error) {
	list, err := l.kubeClient.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing rolebindings: %w", err)
	}
	return list.Items, nil
}

func (l *ResourceLister) ListScyllaClusters(ctx context.Context, namespace string) ([]engine.ScyllaClusterInfo, error) {
	var result []engine.ScyllaClusterInfo

	// List ScyllaClusters (v1).
	scList, err := l.scyllaClient.ScyllaV1().ScyllaClusters(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.V(4).InfoS("Error listing ScyllaClusters", "error", err)
		// Don't fail — the CRD might not be installed.
	} else {
		for i := range scList.Items {
			sc := &scList.Items[i]
			result = append(result, engine.ScyllaClusterInfo{
				Name:       sc.Name,
				Namespace:  sc.Namespace,
				Kind:       "ScyllaCluster",
				APIVersion: scyllav1.GroupVersion.String(),
				Object:     sc,
			})
		}
	}

	// List ScyllaDBDatacenters (v1alpha1).
	// Skip ScyllaDBDatacenters that are owned by a ScyllaCluster — we already
	// discovered the parent above so diagnosing the child would be a duplicate.
	sdcList, err := l.scyllaClient.ScyllaV1alpha1().ScyllaDBDatacenters(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.V(4).InfoS("Error listing ScyllaDBDatacenters", "error", err)
	} else {
		for i := range sdcList.Items {
			sdc := &sdcList.Items[i]
			if hasControllerOwnerOfKind(sdc.OwnerReferences, "ScyllaCluster") {
				klog.V(4).InfoS("Skipping ScyllaDBDatacenter owned by ScyllaCluster", "namespace", sdc.Namespace, "name", sdc.Name)
				continue
			}
			result = append(result, engine.ScyllaClusterInfo{
				Name:       sdc.Name,
				Namespace:  sdc.Namespace,
				Kind:       "ScyllaDBDatacenter",
				APIVersion: scyllav1alpha1.GroupVersion.String(),
				Object:     sdc,
			})
		}
	}

	return result, nil
}

func (l *ResourceLister) ListScyllaDBDatacenters(ctx context.Context, namespace string) ([]*scyllav1alpha1.ScyllaDBDatacenter, error) {
	list, err := l.scyllaClient.ScyllaV1alpha1().ScyllaDBDatacenters(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing scylladbdatacenters: %w", err)
	}
	result := make([]*scyllav1alpha1.ScyllaDBDatacenter, len(list.Items))
	for i := range list.Items {
		result[i] = &list.Items[i]
	}
	return result, nil
}

func (l *ResourceLister) ListNodeConfigs(ctx context.Context) ([]*scyllav1alpha1.NodeConfig, error) {
	list, err := l.scyllaClient.ScyllaV1alpha1().NodeConfigs().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodeconfigs: %w", err)
	}
	result := make([]*scyllav1alpha1.NodeConfig, len(list.Items))
	for i := range list.Items {
		result[i] = &list.Items[i]
	}
	return result, nil
}

func (l *ResourceLister) ListScyllaOperatorConfigs(ctx context.Context) ([]*scyllav1alpha1.ScyllaOperatorConfig, error) {
	list, err := l.scyllaClient.ScyllaV1alpha1().ScyllaOperatorConfigs().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing scyllaoperatorconfigs: %w", err)
	}
	result := make([]*scyllav1alpha1.ScyllaOperatorConfig, len(list.Items))
	for i := range list.Items {
		result[i] = &list.Items[i]
	}
	return result, nil
}

// hasControllerOwnerOfKind returns true if any of the ownerReferences has the
// given kind and is marked as the controller.
func hasControllerOwnerOfKind(refs []metav1.OwnerReference, kind string) bool {
	for _, ref := range refs {
		if ref.Kind == kind && ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}
