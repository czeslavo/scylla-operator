package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	scyllaversionedclient "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/signals"
	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	"github.com/scylladb/scylla-operator/pkg/soda/output"
	"github.com/scylladb/scylla-operator/pkg/soda/profiles"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kgenericclioptions "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/templates"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
)

var (
	diagnoseLongDescription = templates.LongDesc(`
		diagnose runs diagnostic checks against ScyllaDB clusters.
		
		This command collects information from the Kubernetes cluster and Scylla
		pods, then analyzes the results to identify potential issues.
		
		This command is experimental and subject to change without notice.
	`)

	diagnoseExample = templates.Examples(`
		# Run full diagnostics on all ScyllaDB clusters in all namespaces.
		scylla-operator diagnose

		# Run diagnostics on a specific cluster.
		scylla-operator diagnose --namespace=scylla --cluster-name=my-cluster

		# Save artifacts to a specific directory.
		scylla-operator diagnose --output-dir=/tmp/diagnostics
	`)
)

// DiagnoseOptions holds the options for the diagnose command.
type DiagnoseOptions struct {
	ConfigFlags *kgenericclioptions.ConfigFlags

	// Targeting flags.
	ClusterName string

	// Profile/override flags.
	ProfileName string
	Enable      []string
	Disable     []string

	// Output flags.
	OutputDir string
	KeepGoing bool

	// Resolved at Complete() time.
	restConfig   *rest.Config
	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface
}

// NewDiagnoseOptions creates a new DiagnoseOptions with default values.
func NewDiagnoseOptions() *DiagnoseOptions {
	return &DiagnoseOptions{
		ConfigFlags: kgenericclioptions.NewConfigFlags(true),
		ProfileName: profiles.FullProfileName,
		KeepGoing:   true,
	}
}

// AddFlags adds diagnose flags to the flagset.
func (o *DiagnoseOptions) AddFlags(flagset *pflag.FlagSet) {
	o.ConfigFlags.AddFlags(flagset)
	flagset.StringVar(&o.ClusterName, "cluster-name", o.ClusterName, "Limit diagnostics to a specific ScyllaCluster/ScyllaDBDatacenter name.")
	flagset.StringVar(&o.ProfileName, "profile", o.ProfileName, "Diagnostic profile to run.")
	flagset.StringSliceVar(&o.Enable, "enable", o.Enable, "Additional analyzer IDs to enable on top of the profile.")
	flagset.StringSliceVar(&o.Disable, "disable", o.Disable, "Analyzer IDs to disable from the profile.")
	flagset.StringVar(&o.OutputDir, "output-dir", o.OutputDir, "Directory to write artifacts. If empty, artifacts are written to a temp directory.")
	flagset.BoolVar(&o.KeepGoing, "keep-going", o.KeepGoing, "Continue running diagnostics even if some collectors fail.")
}

// NewDiagnoseCmd creates the diagnose cobra command.
func NewDiagnoseCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewDiagnoseOptions()

	cmd := &cobra.Command{
		Use:     "diagnose",
		Short:   "Run diagnostic checks against ScyllaDB clusters.",
		Long:    diagnoseLongDescription,
		Example: diagnoseExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Complete(); err != nil {
				return err
			}
			return o.Run(streams, cmd)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	o.AddFlags(cmd.Flags())
	return cmd
}

// Validate checks the DiagnoseOptions for invalid configurations.
func (o *DiagnoseOptions) Validate() error {
	allProfiles := profiles.AllProfiles()
	if _, ok := allProfiles[o.ProfileName]; !ok {
		available := make([]string, 0, len(allProfiles))
		for name := range allProfiles {
			available = append(available, name)
		}
		return fmt.Errorf("unknown profile %q, available profiles: %s", o.ProfileName, strings.Join(available, ", "))
	}

	return nil
}

// Complete builds K8s clients and resolves configuration.
func (o *DiagnoseOptions) Complete() error {
	var err error

	o.restConfig, err = o.ConfigFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("creating REST config: %w", err)
	}

	o.kubeClient, err = kubernetes.NewForConfig(o.restConfig)
	if err != nil {
		return fmt.Errorf("creating Kubernetes client: %w", err)
	}

	o.scyllaClient, err = scyllaversionedclient.NewForConfig(o.restConfig)
	if err != nil {
		return fmt.Errorf("creating Scylla client: %w", err)
	}

	// Set up output directory.
	if o.OutputDir == "" {
		o.OutputDir, err = os.MkdirTemp("", "scylla-diagnose-*")
		if err != nil {
			return fmt.Errorf("creating temp output directory: %w", err)
		}
	}

	return nil
}

// Run executes the diagnostic pipeline.
func (o *DiagnoseOptions) Run(streams genericclioptions.IOStreams, cmd *cobra.Command) error {
	stopCh := signals.StopChannel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stopCh
		cancel()
	}()

	klog.InfoS("Starting diagnostics", "Profile", o.ProfileName, "OutputDir", o.OutputDir)

	// Discover targets.
	clusterLister := &k8sScyllaClusterLister{scyllaClient: o.scyllaClient}
	clusterInfos, err := o.discoverClusters(ctx, clusterLister)
	if err != nil {
		return fmt.Errorf("discovering clusters: %w", err)
	}

	if len(clusterInfos) == 0 {
		fmt.Fprintln(streams.Out, "No ScyllaCluster or ScyllaDBDatacenter resources found.")
		return nil
	}

	klog.InfoS("Discovered clusters", "Count", len(clusterInfos))

	// Discover pods per cluster.
	podLister := &k8sPodLister{kubeClient: o.kubeClient}
	podsByCluster, err := o.discoverPods(ctx, podLister, clusterInfos)
	if err != nil {
		return fmt.Errorf("discovering pods: %w", err)
	}

	// Build engine config.
	enableIDs := make([]engine.AnalyzerID, len(o.Enable))
	for i, s := range o.Enable {
		enableIDs[i] = engine.AnalyzerID(s)
	}
	disableIDs := make([]engine.AnalyzerID, len(o.Disable))
	for i, s := range o.Disable {
		disableIDs[i] = engine.AnalyzerID(s)
	}

	artifactFactory := &fsArtifactWriterFactory{baseDir: o.OutputDir}

	config := engine.EngineConfig{
		AllCollectors: collectors.AllCollectorsMap(),
		AllAnalyzers:  analyzers.AllAnalyzersMap(),
		AllProfiles:   profiles.AllProfiles(),

		ProfileName: o.ProfileName,
		Enable:      enableIDs,
		Disable:     disableIDs,

		ScyllaClusters: clusterInfos,
		Pods:           podsByCluster,

		PodExecutor:         &k8sPodExecutor{restConfig: o.restConfig, kubeClient: o.kubeClient},
		ScyllaClusterLister: clusterLister,
		NodeLister:          &k8sNodeLister{kubeClient: o.kubeClient},
		PodLister:           podLister,

		ArtifactWriterFactory: artifactFactory,
		KeepGoing:             o.KeepGoing,
	}

	eng := engine.NewEngine(config)
	result, err := eng.Run(ctx)
	if err != nil {
		return fmt.Errorf("running diagnostics: %w", err)
	}

	// Always display console output.
	cw := output.NewConsoleWriter(streams.Out)
	if err := cw.WriteReport(result, o.ProfileName, clusterInfos, podsByCluster); err != nil {
		return fmt.Errorf("writing console report: %w", err)
	}

	// Persist vitals.json — the full collector results with data, enabling offline analysis.
	if err := o.writeVitalsJSON(result); err != nil {
		return fmt.Errorf("writing vitals.json: %w", err)
	}

	// Persist report.json — the full diagnostic report including metadata, targets, collectors, and analysis.
	if err := o.writeReportJSON(result, clusterInfos, podsByCluster); err != nil {
		return fmt.Errorf("writing report.json: %w", err)
	}

	fmt.Fprintf(streams.Out, "\nArtifacts written to: %s\n", o.OutputDir)
	klog.InfoS("Diagnostics complete", "ArtifactDir", o.OutputDir)
	return nil
}

// writeVitalsJSON serializes the Vitals store (including collector Data) to
// vitals.json in the output directory root.
func (o *DiagnoseOptions) writeVitalsJSON(result *engine.EngineResult) error {
	sv, err := result.Vitals.ToSerializable()
	if err != nil {
		return fmt.Errorf("converting vitals to serializable form: %w", err)
	}

	data, err := json.MarshalIndent(sv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling vitals: %w", err)
	}

	path := filepath.Join(o.OutputDir, "vitals.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	klog.V(2).InfoS("Wrote vitals.json", "path", path)
	return nil
}

// writeReportJSON builds the full JSONReport and writes it to report.json
// in the output directory root.
func (o *DiagnoseOptions) writeReportJSON(result *engine.EngineResult, clusters []engine.ScyllaClusterInfo, pods map[engine.ScopeKey][]engine.PodInfo) error {
	jw := output.NewJSONWriter(nil, "0.1.0-poc")
	report := jw.BuildReport(result, o.ProfileName, clusters, pods)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	path := filepath.Join(o.OutputDir, "report.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	klog.V(2).InfoS("Wrote report.json", "path", path)
	return nil
}

// discoverClusters finds ScyllaCluster and ScyllaDBDatacenter resources.
func (o *DiagnoseOptions) discoverClusters(ctx context.Context, lister engine.ScyllaClusterLister) ([]engine.ScyllaClusterInfo, error) {
	namespace := ""
	if o.ConfigFlags.Namespace != nil && *o.ConfigFlags.Namespace != "" {
		namespace = *o.ConfigFlags.Namespace
	}
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}

	allClusters, err := lister.ListScyllaClusters(ctx, namespace)
	if err != nil {
		return nil, err
	}

	// Filter by --cluster-name if specified.
	if o.ClusterName == "" {
		return allClusters, nil
	}

	var filtered []engine.ScyllaClusterInfo
	for _, c := range allClusters {
		if c.Name == o.ClusterName {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

// discoverPods finds Scylla pods for each cluster.
func (o *DiagnoseOptions) discoverPods(ctx context.Context, lister engine.PodLister, clusterInfos []engine.ScyllaClusterInfo) (map[engine.ScopeKey][]engine.PodInfo, error) {
	result := make(map[engine.ScopeKey][]engine.PodInfo)

	for _, cluster := range clusterInfos {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		selector := labels.SelectorFromSet(labels.Set{
			naming.ClusterNameLabel: cluster.Name,
		})

		pods, err := lister.ListPods(ctx, cluster.Namespace, selector)
		if err != nil {
			return nil, fmt.Errorf("listing pods for %s/%s: %w", cluster.Namespace, cluster.Name, err)
		}

		var podInfos []engine.PodInfo
		for _, pod := range pods {
			podInfos = append(podInfos, engine.PodInfo{
				Name:           pod.Name,
				Namespace:      pod.Namespace,
				ClusterName:    pod.Labels[naming.ClusterNameLabel],
				DatacenterName: pod.Labels[naming.DatacenterNameLabel],
				RackName:       pod.Labels[naming.RackNameLabel],
			})
		}

		result[clusterKey] = podInfos
	}

	return result, nil
}

// --- Production implementations of engine interfaces ---

// k8sPodExecutor implements engine.PodExecutor using Kubernetes exec.
type k8sPodExecutor struct {
	restConfig *rest.Config
	kubeClient kubernetes.Interface
}

func (e *k8sPodExecutor) Execute(ctx context.Context, namespace, podName, containerName string, command []string) (string, string, error) {
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

// k8sScyllaClusterLister implements engine.ScyllaClusterLister using typed Scylla clients.
type k8sScyllaClusterLister struct {
	scyllaClient scyllaversionedclient.Interface
}

func (l *k8sScyllaClusterLister) ListScyllaClusters(ctx context.Context, namespace string) ([]engine.ScyllaClusterInfo, error) {
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

// k8sNodeLister implements engine.NodeLister using the Kubernetes API.
type k8sNodeLister struct {
	kubeClient kubernetes.Interface
}

func (l *k8sNodeLister) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList, err := l.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	return nodeList.Items, nil
}

// k8sPodLister implements engine.PodLister using the Kubernetes API.
type k8sPodLister struct {
	kubeClient kubernetes.Interface
}

func (l *k8sPodLister) ListPods(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Pod, error) {
	podList, err := l.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	return podList.Items, nil
}

// fsArtifactWriterFactory creates filesystem-backed ArtifactWriters.
type fsArtifactWriterFactory struct {
	baseDir string
}

func (f *fsArtifactWriterFactory) NewWriter(collectorID engine.CollectorID, scope engine.CollectorScope, scopeKey engine.ScopeKey) engine.ArtifactWriter {
	var dir string
	if scopeKey.IsEmpty() {
		// ClusterWide scope: no scope key subdirectory.
		dir = filepath.Join(f.baseDir, scope.String(), string(collectorID))
	} else {
		// PerScyllaCluster/PerPod: include namespace/name as path components.
		dir = filepath.Join(f.baseDir, scope.String(), scopeKey.Namespace, scopeKey.Name, string(collectorID))
	}
	return &fsArtifactWriter{dir: dir}
}

// fsArtifactWriter implements engine.ArtifactWriter by writing to the filesystem.
type fsArtifactWriter struct {
	dir string
}

func (w *fsArtifactWriter) WriteArtifact(filename string, content []byte) (string, error) {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return "", fmt.Errorf("creating artifact directory %s: %w", w.dir, err)
	}

	path := filepath.Join(w.dir, filename)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("writing artifact %s: %w", path, err)
	}

	// Return just the filename as the relative path — relative to the
	// collector's own artifact directory (w.dir).
	return filename, nil
}
