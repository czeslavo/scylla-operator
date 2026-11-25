package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

func testPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "scylla", Name: "node-0", ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "x"}}},
		Spec:       corev1.PodSpec{NodeName: "worker", Containers: []corev1.Container{{Name: "scylla"}}},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			QOSClass:          corev1.PodQOSGuaranteed,
			ContainerStatuses: []corev1.ContainerStatus{{Name: "scylla", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
		},
	}
}

func TestPodCollectorWritesNodeScopedFiles(t *testing.T) {
	pod := testPod()
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "scylla", Name: "node-0"}, Spec: corev1.ServiceSpec{ClusterIP: "10.0.0.7"}}
	env := &Env{
		KubeClient:  fake.NewSimpleClientset(svc),
		ArchiveRoot: t.TempDir(),
		Events: []corev1.Event{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "node-0"}, Reason: "Started"},
			{ObjectMeta: metav1.ObjectMeta{Name: "b"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "node-1"}, Reason: "Other"},
		},
	}

	res := (&PodCollector{}).Collect(context.Background(), env, pod)
	if res.Status != vitals.CollectorPassed {
		t.Fatalf("unexpected status %s: %s", res.Status, res.Message)
	}
	data := res.Data.(map[string]any)
	if data["memberServiceIP"] != "10.0.0.7" {
		t.Errorf("unexpected member Service IP: %v", data["memberServiceIP"])
	}

	refs := data["files"].([]vitals.FileRef)
	got := map[string]vitals.FileRef{}
	for _, r := range refs {
		got[r.Path] = r
	}
	for _, want := range []string{"kubernetes/nodes/node-0/pod.yaml", "kubernetes/nodes/node-0/service.yaml",
		"kubernetes/nodes/node-0/logs/scylla.log", "kubernetes/nodes/node-0/events.yaml"} {
		r, ok := got[want]
		if !ok {
			t.Errorf("missing %s in %v", want, refs)
			continue
		}
		b, err := os.ReadFile(filepath.Join(env.ArchiveRoot, want))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if r.SHA256 != hex.EncodeToString(sum[:]) || r.SizeBytes != int64(len(b)) {
			t.Errorf("%s: file reference doesn't match the file", want)
		}
	}

	var podYAML map[string]any
	b, _ := os.ReadFile(filepath.Join(env.ArchiveRoot, "kubernetes/nodes/node-0/pod.yaml"))
	if err := yaml.Unmarshal(b, &podYAML); err != nil {
		t.Fatal(err)
	}
	if podYAML["kind"] != "Pod" || podYAML["apiVersion"] != "v1" {
		t.Errorf("pod.yaml lacks its type: %v %v", podYAML["apiVersion"], podYAML["kind"])
	}
	if _, ok := podYAML["metadata"].(map[string]any)["managedFields"]; ok {
		t.Errorf("pod.yaml keeps managedFields")
	}

	var events map[string][]any
	b, _ = os.ReadFile(filepath.Join(env.ArchiveRoot, "kubernetes/nodes/node-0/events.yaml"))
	_ = yaml.Unmarshal(b, &events)
	if len(events["items"]) != 1 {
		t.Errorf("expected only the Pod's own event, got %d", len(events["items"]))
	}
}

func TestKubernetesNodeCollectorSkipsWhenForbidden(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("get", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "worker", nil)
	})
	env := &Env{KubeClient: client, ArchiveRoot: t.TempDir()}

	res := (&KubernetesNodeCollector{}).Collect(context.Background(), env, testPod())
	if res.Status != vitals.CollectorSkipped || res.Message != "Not allowed to read Nodes (cluster-scoped); grant it with examples/soda/rbac-cluster-read.yaml" {
		t.Errorf("unexpected result: %s %q", res.Status, res.Message)
	}
}
