// Package runtime gives SODA access to the Scylla Doctor bundle: its files, and a place to run the analysis.
//
// In-cluster, SODA runs inside the bundle image and uses it directly (Local). From a workstation, SODA starts a
// short-lived Pod from the bundle image (Pod); the kubelet pulls the image, so pull secrets and registry mirrors
// apply, and nothing has to be installed on the workstation.
package runtime

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/scylladb/scylla-operator/pkg/soda/kube"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	// Bundle layout (images/scylla-doctor-bundle/Containerfile).
	CollectPyz      = "collect/scylla_doctor.pyz"
	FullPyz         = "analyze/scylla_doctor_full.pyz"
	ClusterPyz      = "analyze/scylla_doctor_cluster.pyz"
	ClusterIni      = "analyze/scylla_doctor_cluster.ini"
	RuntimeContract = "runtime-contract.json"
	BundleManifest  = "bundle.json"
)

type Runtime interface {
	// Describe says where the analysis runs, for logs and the archive manifest.
	Describe() string
	// Image is the bundle image, or "" when it is not known (local bundle).
	Image() string
	BundleDir() string
	WorkDir() string
	Python() string
	ReadBundleFile(ctx context.Context, rel string) ([]byte, error)
	// PutFiles writes files, given by paths relative to WorkDir().
	PutFiles(ctx context.Context, files map[string][]byte) error
	// Run runs argv in WorkDir(). A non-zero exit is reported in the result.
	Run(ctx context.Context, argv []string) (*kube.ExecResult, error)
	Close(ctx context.Context) error
}

// Local uses a bundle on the local filesystem: SODA runs inside the bundle image.
type Local struct {
	bundleDir string
	python    string
	workDir   string
}

var _ Runtime = &Local{}

func NewLocal(bundleDir, python string) (*Local, error) {
	if _, err := os.Stat(filepath.Join(bundleDir, BundleManifest)); err != nil {
		return nil, fmt.Errorf("%q is not a Scylla Doctor bundle: %w", bundleDir, err)
	}
	workDir, err := os.MkdirTemp("", "soda-analysis-")
	if err != nil {
		return nil, err
	}
	return &Local{bundleDir: bundleDir, python: python, workDir: workDir}, nil
}

func (l *Local) Describe() string  { return fmt.Sprintf("local bundle %s", l.bundleDir) }
func (l *Local) Image() string     { return os.Getenv("SODA_BUNDLE_IMAGE") }
func (l *Local) BundleDir() string { return l.bundleDir }
func (l *Local) WorkDir() string   { return l.workDir }
func (l *Local) Python() string    { return l.python }

func (l *Local) ReadBundleFile(_ context.Context, rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(l.bundleDir, rel))
}

func (l *Local) PutFiles(_ context.Context, files map[string][]byte) error {
	for rel, b := range files {
		p := filepath.Join(l.workDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (l *Local) Run(ctx context.Context, argv []string) (*kube.ExecResult, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = l.workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := &kube.ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		res.ExitCode = -1
		return res, err
	}
	return res, nil
}

func (l *Local) Close(context.Context) error {
	return os.RemoveAll(l.workDir)
}

// Pod runs a helper Pod from the bundle image and works in it over exec.
type Pod struct {
	execer     *kube.Execer
	kubeClient kubernetes.Interface
	namespace  string
	name       string
	image      string
}

var _ Runtime = &Pod{}

const (
	podContainer = "scylla-doctor"
	podWorkDir   = "/tmp/soda-analysis"
	podBundleDir = "/opt/scylla-doctor"
	podPython    = "/usr/bin/python3.12"
)

type PodOptions struct {
	Namespace       string
	Image           string
	RunID           string
	PullSecrets     []string
	ActiveDeadline  time.Duration
	StartTimeout    time.Duration
	MemoryLimit     string
	ImagePullPolicy corev1.PullPolicy
}

// NewPod creates the helper Pod and waits until it runs. Callers must Close it on every exit path; the Pod also
// carries activeDeadlineSeconds so it ends on its own if SODA dies.
func NewPod(ctx context.Context, execer *kube.Execer, kubeClient kubernetes.Interface, o PodOptions) (*Pod, error) {
	deadline := int64(o.ActiveDeadline.Seconds())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "soda-analysis-" + o.RunID,
			Namespace: o.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "soda",
				"app.kubernetes.io/component": "analysis",
				"soda.scylladb.com/run":       o.RunID,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         &deadline,
			AutomountServiceAccountToken:  ptr.To(false),
			TerminationGracePeriodSeconds: ptr.To[int64](1),
			EnableServiceLinks:            ptr.To(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   ptr.To(true),
				RunAsUser:      ptr.To[int64](65532),
				RunAsGroup:     ptr.To[int64](65532),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:            podContainer,
				Image:           o.Image,
				ImagePullPolicy: o.ImagePullPolicy,
				Command:         []string{"sleep", fmt.Sprintf("%d", deadline)},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse(o.MemoryLimit),
					},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					ReadOnlyRootFilesystem:   ptr.To(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
			}},
			Volumes: []corev1.Volume{{
				Name:         "tmp",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			}},
		},
	}
	for _, s := range o.PullSecrets {
		pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: s})
	}

	created, err := kubeClient.CoreV1().Pods(o.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("can't create analysis Pod: %w", err)
	}
	p := &Pod{execer: execer, kubeClient: kubeClient, namespace: o.Namespace, name: created.Name, image: o.Image}
	klog.InfoS("Created analysis Pod", "Pod", o.Namespace+"/"+created.Name, "Image", o.Image)

	waitCtx, cancel := context.WithTimeout(ctx, o.StartTimeout)
	defer cancel()
	for {
		cur, err := kubeClient.CoreV1().Pods(o.Namespace).Get(waitCtx, created.Name, metav1.GetOptions{})
		if err == nil {
			if cur.Status.Phase == corev1.PodRunning {
				return p, nil
			}
			if cur.Status.Phase == corev1.PodFailed || cur.Status.Phase == corev1.PodSucceeded {
				err = fmt.Errorf("analysis Pod ended in phase %s", cur.Status.Phase)
			}
			for _, cs := range cur.Status.ContainerStatuses {
				if w := cs.State.Waiting; w != nil && (w.Reason == "ErrImagePull" || w.Reason == "ImagePullBackOff" || w.Reason == "InvalidImageName") {
					err = fmt.Errorf("can't pull bundle image %q: %s: %s", o.Image, w.Reason, w.Message)
				}
			}
		}
		if err != nil && !apierrors.IsNotFound(err) {
			_ = p.Close(context.Background())
			return nil, err
		}
		select {
		case <-waitCtx.Done():
			_ = p.Close(context.Background())
			return nil, fmt.Errorf("analysis Pod didn't start within %s", o.StartTimeout)
		case <-time.After(time.Second):
		}
	}
}

func (p *Pod) Describe() string  { return fmt.Sprintf("Pod %s/%s", p.namespace, p.name) }
func (p *Pod) Image() string     { return p.image }
func (p *Pod) BundleDir() string { return podBundleDir }
func (p *Pod) WorkDir() string   { return podWorkDir }
func (p *Pod) Python() string    { return podPython }

func (p *Pod) exec(ctx context.Context, argv []string, stdin []byte) (*kube.ExecResult, error) {
	var in *bytes.Reader
	if stdin != nil {
		in = bytes.NewReader(stdin)
	}
	if in == nil {
		return p.execer.Exec(ctx, p.namespace, p.name, podContainer, argv, nil)
	}
	return p.execer.Exec(ctx, p.namespace, p.name, podContainer, argv, in)
}

func (p *Pod) ReadBundleFile(ctx context.Context, rel string) ([]byte, error) {
	res, err := p.exec(ctx, []string{"cat", filepath.Join(podBundleDir, rel)}, nil)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("can't read %s from the bundle: %s", rel, bytes.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

func (p *Pod) PutFiles(ctx context.Context, files map[string][]byte) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), ModTime: time.Now()}); err != nil {
			return err
		}
		if _, err := tw.Write(files[n]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	res, err := p.exec(ctx, []string{"sh", "-c", fmt.Sprintf("mkdir -p %s && tar -x -f - -C %s", podWorkDir, podWorkDir)}, buf.Bytes())
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("can't unpack files in the analysis Pod: %s", bytes.TrimSpace(res.Stderr))
	}
	return nil
}

func (p *Pod) Run(ctx context.Context, argv []string) (*kube.ExecResult, error) {
	// exec has no working directory option; use the shell's.
	script := `cd "$0" && exec "$@"`
	return p.exec(ctx, append([]string{"sh", "-c", script, podWorkDir}, argv...), nil)
}

// Close deletes the Pod. It uses its own timeout, so a cancelled run still cleans up.
func (p *Pod) Close(_ context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := p.kubeClient.CoreV1().Pods(p.namespace).Delete(ctx, p.name, metav1.DeleteOptions{GracePeriodSeconds: ptr.To[int64](0)})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("can't delete analysis Pod %s/%s: %w", p.namespace, p.name, err)
	}
	klog.InfoS("Deleted analysis Pod", "Pod", p.namespace+"/"+p.name)
	return nil
}
