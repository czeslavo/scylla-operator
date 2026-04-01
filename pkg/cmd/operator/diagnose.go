package operator

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	OutputDir   string
	DryRun      bool
	KeepGoing   bool
	FromArchive string // Path to a previous output directory (or .tar.gz archive) for offline re-analysis.
	Archive     bool   // When true, pack the output directory into a .tar.gz file after collection.

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
	flagset.BoolVar(&o.DryRun, "dry-run", o.DryRun, "Print what would be collected and analyzed without connecting to the cluster.")
	flagset.BoolVar(&o.KeepGoing, "keep-going", o.KeepGoing, "Continue running diagnostics even if some collectors fail.")
	flagset.StringVar(&o.FromArchive, "from-archive", o.FromArchive, "Path to a previous output directory (or .tar.gz archive) to re-analyze offline without connecting to the cluster.")
	flagset.BoolVar(&o.Archive, "archive", o.Archive, "Pack the artifact output directory into a .tar.gz file. The archive path is printed to stdout.")
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

	if o.FromArchive != "" {
		if o.ClusterName != "" {
			return fmt.Errorf("--from-archive and --cluster-name are mutually exclusive")
		}
		kubeconfig := ""
		if o.ConfigFlags.KubeConfig != nil {
			kubeconfig = *o.ConfigFlags.KubeConfig
		}
		if kubeconfig != "" {
			return fmt.Errorf("--from-archive and --kubeconfig are mutually exclusive")
		}
		if o.Archive {
			return fmt.Errorf("--from-archive and --archive are mutually exclusive")
		}
	}

	return nil
}

// Complete builds K8s clients and resolves configuration.
func (o *DiagnoseOptions) Complete() error {
	var err error

	// Skip cluster connectivity when running in offline mode.
	if o.FromArchive == "" {
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

	// --dry-run: resolve and print the plan without touching the cluster.
	if o.DryRun {
		return o.printDryRunSummary(streams.Out)
	}

	// --from-archive: offline re-analysis against a previous output directory.
	if o.FromArchive != "" {
		return o.runOffline(ctx, streams)
	}

	// Discover targets.
	resourceLister := &k8sResourceLister{kubeClient: o.kubeClient, scyllaClient: o.scyllaClient}
	clusterInfos, err := o.discoverClusters(ctx, resourceLister)
	if err != nil {
		return fmt.Errorf("discovering clusters: %w", err)
	}

	if len(clusterInfos) == 0 {
		fmt.Fprintln(streams.Out, "No ScyllaCluster or ScyllaDBDatacenter resources found.")
		return nil
	}

	klog.InfoS("Discovered clusters", "Count", len(clusterInfos))

	// Discover pods per cluster.
	podsByCluster, err := o.discoverPods(ctx, resourceLister, clusterInfos)
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
		ScyllaNodes:    podsByCluster,

		PodExecutor:    &k8sPodExecutor{restConfig: o.restConfig, kubeClient: o.kubeClient},
		ResourceLister: resourceLister,

		ArtifactWriterFactory: artifactFactory,
		OnCollectorEvent:      makeProgressPrinter(streams.ErrOut),
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
	if err := o.writeVitalsJSON(result, clusterInfos, podsByCluster); err != nil {
		return fmt.Errorf("writing vitals.json: %w", err)
	}

	// Persist report.json — the full diagnostic report including metadata, targets, collectors, and analysis.
	if err := o.writeReportJSON(result, clusterInfos, podsByCluster); err != nil {
		return fmt.Errorf("writing report.json: %w", err)
	}

	// Write README.md — a self-describing index of the output directory.
	if err := o.writeIndexFile(result, clusterInfos, podsByCluster); err != nil {
		// Non-fatal: log and continue so the rest of the output is still usable.
		klog.V(2).InfoS("Failed to write README.md", "error", err)
	}

	fmt.Fprintf(streams.Out, "\nArtifacts written to: %s\n", o.OutputDir)
	klog.InfoS("Diagnostics complete", "ArtifactDir", o.OutputDir)

	// --archive: pack the output directory into a .tar.gz and clean up.
	if o.Archive {
		archiveName := filepath.Base(o.OutputDir) + ".tar.gz"
		archivePath, err := filepath.Abs(archiveName)
		if err != nil {
			return fmt.Errorf("resolving archive path: %w", err)
		}

		if err := createTarGz(o.OutputDir, archivePath); err != nil {
			return fmt.Errorf("creating archive %s: %w", archivePath, err)
		}

		// Remove the unpacked output directory — the archive is self-contained.
		if err := os.RemoveAll(o.OutputDir); err != nil {
			klog.V(2).InfoS("Failed to remove temp output directory after archiving", "dir", o.OutputDir, "error", err)
		}

		fmt.Fprintf(streams.Out, "Archive written to: %s\n", archivePath)
		fmt.Fprintf(streams.Out, "To re-analyze offline: scylla-operator diagnose --from-archive=%s\n", archivePath)
	}

	return nil
}

// writeVitalsJSON serializes the Vitals store (including collector Data) to
// vitals.json in the output directory root. The clusterInfos and podsByCluster
// arguments are embedded in a Topology field so that offline re-analysis can
// reconstruct the cluster/pod topology without connecting to the cluster.
func (o *DiagnoseOptions) writeVitalsJSON(result *engine.EngineResult, clusterInfos []engine.ScyllaClusterInfo, podsByCluster map[engine.ScopeKey][]engine.ScyllaNodeInfo) error {
	sv, err := result.Vitals.ToSerializable()
	if err != nil {
		return fmt.Errorf("converting vitals to serializable form: %w", err)
	}

	// Embed the cluster/Scylla-node topology so it survives the serialization round-trip.
	topo := &engine.SerializableClusterTopology{
		Clusters:    make([]engine.SerializableClusterInfo, 0, len(clusterInfos)),
		ScyllaNodes: make(map[engine.ScopeKey][]engine.SerializableScyllaNodeInfo, len(podsByCluster)),
	}
	for _, ci := range clusterInfos {
		topo.Clusters = append(topo.Clusters, engine.SerializableClusterInfo{
			Namespace:  ci.Namespace,
			Name:       ci.Name,
			Kind:       ci.Kind,
			APIVersion: ci.APIVersion,
		})
	}
	for key, pods := range podsByCluster {
		snodes := make([]engine.SerializableScyllaNodeInfo, 0, len(pods))
		for _, pod := range pods {
			snodes = append(snodes, engine.SerializableScyllaNodeInfo{
				Namespace:      pod.Namespace,
				Name:           pod.Name,
				ClusterName:    pod.ClusterName,
				DatacenterName: pod.DatacenterName,
				RackName:       pod.RackName,
			})
		}
		topo.ScyllaNodes[key] = snodes
	}
	sv.Topology = topo

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
func (o *DiagnoseOptions) writeReportJSON(result *engine.EngineResult, clusters []engine.ScyllaClusterInfo, pods map[engine.ScopeKey][]engine.ScyllaNodeInfo) error {
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

// writeIndexFile writes a README.md to the output directory that describes the
// contents of the artifact bundle and provides instructions for offline re-analysis.
func (o *DiagnoseOptions) writeIndexFile(result *engine.EngineResult, clusters []engine.ScyllaClusterInfo, pods map[engine.ScopeKey][]engine.ScyllaNodeInfo) error {
	allCollectorMap := collectors.AllCollectorsMap()
	allAnalyzerMap := analyzers.AllAnalyzersMap()

	collectorNames := make(map[engine.CollectorID]string, len(allCollectorMap))
	for id, c := range allCollectorMap {
		collectorNames[id] = c.Name()
	}
	analyzerNames := make(map[engine.AnalyzerID]string, len(allAnalyzerMap))
	for id, a := range allAnalyzerMap {
		analyzerNames[id] = a.Name()
	}

	params := output.IndexParams{
		ProfileName:    o.ProfileName,
		Clusters:       clusters,
		ScyllaNodes:    pods,
		Result:         result,
		CollectorNames: collectorNames,
		AnalyzerNames:  analyzerNames,
		OutputDir:      o.OutputDir,
	}

	if err := output.WriteIndexFile(o.OutputDir, params); err != nil {
		return err
	}

	klog.V(2).InfoS("Wrote README.md", "path", filepath.Join(o.OutputDir, "README.md"))
	return nil
}

// printDryRunSummary resolves the profile and prints what would be collected
// and analyzed, without making any connection to the cluster.
func (o *DiagnoseOptions) printDryRunSummary(w io.Writer) error {
	allCollectorMap := collectors.AllCollectorsMap()
	allAnalyzerMap := analyzers.AllAnalyzersMap()
	allProfileMap := profiles.AllProfiles()

	enableIDs := make([]engine.AnalyzerID, len(o.Enable))
	for i, s := range o.Enable {
		enableIDs[i] = engine.AnalyzerID(s)
	}
	disableIDs := make([]engine.AnalyzerID, len(o.Disable))
	for i, s := range o.Disable {
		disableIDs[i] = engine.AnalyzerID(s)
	}

	resolvedCollectors, resolvedAnalyzers, err := engine.ResolveProfile(
		o.ProfileName, allProfileMap, enableIDs, disableIDs, allAnalyzerMap, allCollectorMap,
	)
	if err != nil {
		return fmt.Errorf("resolving profile: %w", err)
	}

	fmt.Fprintf(w, "Dry run — nothing will be collected or written.\n\n")
	fmt.Fprintf(w, "Profile:  %s\n", o.ProfileName)
	if o.ClusterName != "" {
		fmt.Fprintf(w, "Target:   cluster %q (all namespaces unless --namespace is set)\n", o.ClusterName)
	} else {
		fmt.Fprintf(w, "Target:   all ScyllaDB clusters\n")
	}

	fmt.Fprintf(w, "\nCollectors (%d):\n", len(resolvedCollectors))
	for _, id := range resolvedCollectors {
		c := allCollectorMap[id]
		fmt.Fprintf(w, "  %-14s  %s\n", "["+c.Scope().String()+"]", c.Name())
	}

	fmt.Fprintf(w, "\nAnalyzers (%d):\n", len(resolvedAnalyzers))
	for _, id := range resolvedAnalyzers {
		a := allAnalyzerMap[id]
		fmt.Fprintf(w, "  %s\n", a.Name())
	}

	return nil
}

// discoverClusters finds ScyllaCluster and ScyllaDBDatacenter resources.
func (o *DiagnoseOptions) discoverClusters(ctx context.Context, lister engine.ResourceLister) ([]engine.ScyllaClusterInfo, error) {
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
func (o *DiagnoseOptions) discoverPods(ctx context.Context, lister engine.ResourceLister, clusterInfos []engine.ScyllaClusterInfo) (map[engine.ScopeKey][]engine.ScyllaNodeInfo, error) {
	result := make(map[engine.ScopeKey][]engine.ScyllaNodeInfo)

	for _, cluster := range clusterInfos {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		selector := labels.SelectorFromSet(labels.Set{
			naming.ClusterNameLabel: cluster.Name,
		})

		pods, err := lister.ListPods(ctx, cluster.Namespace, selector)
		if err != nil {
			return nil, fmt.Errorf("listing pods for %s/%s: %w", cluster.Namespace, cluster.Name, err)
		}

		var podInfos []engine.ScyllaNodeInfo
		for _, pod := range pods {
			podInfos = append(podInfos, engine.ScyllaNodeInfo{
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

// k8sResourceLister implements engine.ResourceLister using the Kubernetes and Scylla API clients.
type k8sResourceLister struct {
	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface
}

var _ engine.ResourceLister = (*k8sResourceLister)(nil)

func (l *k8sResourceLister) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList, err := l.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	return nodeList.Items, nil
}

func (l *k8sResourceLister) ListPods(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Pod, error) {
	podList, err := l.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	return podList.Items, nil
}

func (l *k8sResourceLister) ListConfigMaps(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.ConfigMap, error) {
	list, err := l.kubeClient.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing configmaps: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListServices(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Service, error) {
	list, err := l.kubeClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListServiceAccounts(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.ServiceAccount, error) {
	list, err := l.kubeClient.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing serviceaccounts: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListPersistentVolumeClaims(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.PersistentVolumeClaim, error) {
	list, err := l.kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing persistentvolumeclaims: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListDeployments(ctx context.Context, namespace string, selector labels.Selector) ([]appsv1.Deployment, error) {
	list, err := l.kubeClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListStatefulSets(ctx context.Context, namespace string, selector labels.Selector) ([]appsv1.StatefulSet, error) {
	list, err := l.kubeClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListDaemonSets(ctx context.Context, namespace string, selector labels.Selector) ([]appsv1.DaemonSet, error) {
	list, err := l.kubeClient.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing daemonsets: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListPodDisruptionBudgets(ctx context.Context, namespace string, selector labels.Selector) ([]policyv1.PodDisruptionBudget, error) {
	list, err := l.kubeClient.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing poddisruptionbudgets: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListRoleBindings(ctx context.Context, namespace string, selector labels.Selector) ([]rbacv1.RoleBinding, error) {
	list, err := l.kubeClient.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing rolebindings: %w", err)
	}
	return list.Items, nil
}

func (l *k8sResourceLister) ListScyllaClusters(ctx context.Context, namespace string) ([]engine.ScyllaClusterInfo, error) {
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

func (l *k8sResourceLister) ListScyllaDBDatacenters(ctx context.Context, namespace string) ([]*scyllav1alpha1.ScyllaDBDatacenter, error) {
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

func (l *k8sResourceLister) ListNodeConfigs(ctx context.Context) ([]*scyllav1alpha1.NodeConfig, error) {
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

func (l *k8sResourceLister) ListScyllaOperatorConfigs(ctx context.Context) ([]*scyllav1alpha1.ScyllaOperatorConfig, error) {
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

// fsArtifactWriterFactory creates filesystem-backed ArtifactWriters.
type fsArtifactWriterFactory struct {
	baseDir string
}

// collectorScopeDirName maps CollectorScope values to the kebab-case directory
// names used under the collectors/ prefix in the output directory.
var collectorScopeDirName = map[engine.CollectorScope]string{
	engine.ClusterWide:      "cluster-wide",
	engine.PerScyllaCluster: "per-scylla-cluster",
	engine.PerScyllaNode:    "per-scylla-node",
}

func (f *fsArtifactWriterFactory) NewWriter(collectorID engine.CollectorID, scope engine.CollectorScope, scopeKey engine.ScopeKey) engine.ArtifactWriter {
	scopeDir := collectorScopeDirName[scope]
	var dir string
	if scopeKey.IsEmpty() {
		// ClusterWide scope: no scope key subdirectory.
		dir = filepath.Join(f.baseDir, "collectors", scopeDir, string(collectorID))
	} else {
		// PerScyllaCluster/PerScyllaNode: include namespace/name as path components.
		dir = filepath.Join(f.baseDir, "collectors", scopeDir, scopeKey.Namespace, scopeKey.Name, string(collectorID))
	}
	return &fsArtifactWriter{dir: dir}
}

// fsArtifactWriter implements engine.ArtifactWriter by writing to the filesystem.
type fsArtifactWriter struct {
	dir string
}

func (w *fsArtifactWriter) WriteArtifact(filename string, content []byte) (string, error) {
	path := filepath.Join(w.dir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating artifact directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("writing artifact %s: %w", path, err)
	}

	// Return just the filename as the relative path — relative to the
	// collector's own artifact directory (w.dir).
	return filename, nil
}

// makeProgressPrinter returns an OnCollectorEvent callback that prints a single
// progress line to w for each collector start and finish event.
//
// Format examples:
//
//	collecting: Kubernetes Node resources (cluster-wide) ...
//	collected:  Kubernetes Node resources (cluster-wide) PASSED
//	collecting: OS information (pod scylla/scylla-0) ...
//	collected:  OS information (pod scylla/scylla-0) FAILED: collector error: ...
func makeProgressPrinter(w io.Writer) func(engine.CollectorEvent) {
	return func(ev engine.CollectorEvent) {
		var scopeLabel string
		switch ev.Scope {
		case engine.ClusterWide:
			scopeLabel = "cluster-wide"
		case engine.PerScyllaCluster:
			scopeLabel = fmt.Sprintf("cluster %s", ev.ScopeKey)
		case engine.PerScyllaNode:
			scopeLabel = fmt.Sprintf("scylla-node %s", ev.ScopeKey)
		}

		switch ev.Kind {
		case engine.CollectorEventStarted:
			fmt.Fprintf(w, "collecting: %s (%s) ...\n", ev.CollectorName, scopeLabel)
		case engine.CollectorEventFinished:
			status := ev.Result.Status.String()
			if ev.Result.Message != "" && ev.Result.Status != engine.CollectorPassed {
				fmt.Fprintf(w, "collected:  %s (%s) %s: %s\n", ev.CollectorName, scopeLabel, status, ev.Result.Message)
			} else {
				fmt.Fprintf(w, "collected:  %s (%s) %s\n", ev.CollectorName, scopeLabel, status)
			}
		}
	}
}

// runOffline loads vitals.json from the archive path, reconstructs the Vitals
// store, and runs analyzers against it without connecting to the cluster.
// If the archive path ends in ".tar.gz" it is first extracted to a temp directory.
func (o *DiagnoseOptions) runOffline(ctx context.Context, streams genericclioptions.IOStreams) error {
	archiveDir := o.FromArchive

	// If the path is a .tar.gz, extract it to a temp directory first.
	var tempDir string
	if strings.HasSuffix(o.FromArchive, ".tar.gz") {
		var err error
		tempDir, err = os.MkdirTemp("", "scylla-diagnose-offline-*")
		if err != nil {
			return fmt.Errorf("creating temp directory for extraction: %w", err)
		}
		defer os.RemoveAll(tempDir)

		if err := extractTarGz(o.FromArchive, tempDir); err != nil {
			return fmt.Errorf("extracting archive %s: %w", o.FromArchive, err)
		}
		archiveDir = tempDir
	}

	// Load vitals.json.
	vitalsPath := filepath.Join(archiveDir, "vitals.json")
	vitalsData, err := os.ReadFile(vitalsPath)
	if err != nil {
		return fmt.Errorf("reading vitals.json from %s: %w", vitalsPath, err)
	}

	var sv engine.SerializableVitals
	if err := json.Unmarshal(vitalsData, &sv); err != nil {
		return fmt.Errorf("parsing vitals.json: %w", err)
	}

	vitals, err := engine.FromSerializable(&sv, collectors.ResultTypeRegistry())
	if err != nil {
		return fmt.Errorf("deserializing vitals: %w", err)
	}

	// Build engine config — no Kubernetes clients needed.
	enableIDs := make([]engine.AnalyzerID, len(o.Enable))
	for i, s := range o.Enable {
		enableIDs[i] = engine.AnalyzerID(s)
	}
	disableIDs := make([]engine.AnalyzerID, len(o.Disable))
	for i, s := range o.Disable {
		disableIDs[i] = engine.AnalyzerID(s)
	}

	// Reconstruct the cluster/pod topology from the vitals store so that the
	// per-cluster analyzer dispatch works correctly.
	clusterInfos, podsByCluster := clusterTopologyFromVitals(&sv, vitals)

	config := engine.EngineConfig{
		AllCollectors: collectors.AllCollectorsMap(),
		AllAnalyzers:  analyzers.AllAnalyzersMap(),
		AllProfiles:   profiles.AllProfiles(),

		ProfileName: o.ProfileName,
		Enable:      enableIDs,
		Disable:     disableIDs,

		ScyllaClusters: clusterInfos,
		ScyllaNodes:    podsByCluster,
	}

	eng := engine.NewEngine(config)
	artifactReader := &fsArtifactReader{baseDir: archiveDir}
	result, err := eng.OfflineRun(ctx, vitals, artifactReader)
	if err != nil {
		return fmt.Errorf("running offline analysis: %w", err)
	}

	// Display console output.
	cw := output.NewConsoleWriter(streams.Out)
	if err := cw.WriteReport(result, o.ProfileName, clusterInfos, podsByCluster); err != nil {
		return fmt.Errorf("writing console report: %w", err)
	}

	return nil
}

// clusterTopologyFromVitals reconstructs the ScyllaClusterInfo and pod topology
// needed for offline analyzer dispatch. It prefers the Topology embedded in
// SerializableVitals (populated during a live run) for accuracy. When Topology
// is absent (older archives), it falls back to inferring one cluster per distinct
// namespace from the PerScyllaNode keys — a best-effort approximation sufficient for
// single-cluster namespaces.
func clusterTopologyFromVitals(sv *engine.SerializableVitals, vitals *engine.Vitals) ([]engine.ScyllaClusterInfo, map[engine.ScopeKey][]engine.ScyllaNodeInfo) {
	// Preferred path: use the stored topology from the live run.
	if sv.Topology != nil && len(sv.Topology.Clusters) > 0 {
		clusterInfos := make([]engine.ScyllaClusterInfo, 0, len(sv.Topology.Clusters))
		for _, ci := range sv.Topology.Clusters {
			clusterInfos = append(clusterInfos, engine.ScyllaClusterInfo{
				Namespace:  ci.Namespace,
				Name:       ci.Name,
				Kind:       ci.Kind,
				APIVersion: ci.APIVersion,
			})
		}
		podsByCluster := make(map[engine.ScopeKey][]engine.ScyllaNodeInfo, len(sv.Topology.ScyllaNodes))
		for key, snodes := range sv.Topology.ScyllaNodes {
			nodes := make([]engine.ScyllaNodeInfo, 0, len(snodes))
			for _, sn := range snodes {
				nodes = append(nodes, engine.ScyllaNodeInfo{
					Namespace:      sn.Namespace,
					Name:           sn.Name,
					ClusterName:    sn.ClusterName,
					DatacenterName: sn.DatacenterName,
					RackName:       sn.RackName,
				})
			}
			podsByCluster[key] = nodes
		}
		return clusterInfos, podsByCluster
	}

	// Fallback: infer topology from PerScyllaCluster keys (if any) and PerScyllaNode keys.
	// This handles archives produced before topology was stored in vitals.json.
	clusterKeys := vitals.ScyllaClusterKeys()
	clusterInfos := make([]engine.ScyllaClusterInfo, 0, len(clusterKeys))
	for _, key := range clusterKeys {
		clusterInfos = append(clusterInfos, engine.ScyllaClusterInfo{
			Namespace: key.Namespace,
			Name:      key.Name,
		})
	}

	podKeys := vitals.ScyllaNodeKeys()

	// If there are no PerScyllaCluster keys, synthesize one cluster per distinct
	// namespace seen in the PerScyllaNode keys.  This is the common case when only
	// PerScyllaNode-scope collectors ran (e.g. the default full profile today).
	if len(clusterKeys) == 0 && len(podKeys) > 0 {
		seen := make(map[string]bool)
		for _, podKey := range podKeys {
			if !seen[podKey.Namespace] {
				seen[podKey.Namespace] = true
				syntheticKey := engine.ScopeKey{Namespace: podKey.Namespace, Name: podKey.Namespace}
				clusterKeys = append(clusterKeys, syntheticKey)
				clusterInfos = append(clusterInfos, engine.ScyllaClusterInfo{
					Namespace: podKey.Namespace,
					Name:      podKey.Namespace, // best-effort; real name not available
				})
			}
		}
	}

	podsByCluster := make(map[engine.ScopeKey][]engine.ScyllaNodeInfo)
	for _, podKey := range podKeys {
		// Pods are associated with clusters that share the same namespace.
		// If multiple clusters share a namespace, pods go to the first match.
		for _, clusterKey := range clusterKeys {
			if clusterKey.Namespace == podKey.Namespace {
				podsByCluster[clusterKey] = append(podsByCluster[clusterKey], engine.ScyllaNodeInfo{
					Namespace: podKey.Namespace,
					Name:      podKey.Name,
				})
				break
			}
		}
	}

	return clusterInfos, podsByCluster
}

// createTarGz creates a .tar.gz archive at destPath containing all files under
// srcDir. The archive entries use paths relative to srcDir.
func createTarGz(srcDir, destPath string) error {
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Compute the path inside the archive relative to srcDir.
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("creating tar header for %s: %w", path, err)
		}
		header.Name = filepath.ToSlash(rel)

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing tar header for %s: %w", path, err)
		}

		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening file %s: %w", path, err)
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("writing file %s to archive: %w", path, err)
		}

		return nil
	})
}

// extractTarGz extracts a .tar.gz archive to the given destination directory.
func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Sanitise path to prevent directory traversal attacks.
		target := filepath.Join(dest, filepath.Clean("/"+header.Name))
		if !strings.HasPrefix(target, dest+string(os.PathSeparator)) && target != dest {
			return fmt.Errorf("tar entry %q would escape destination directory", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", target, err)
			}
			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("writing file %s: %w", target, err)
			}
			out.Close()
		}
	}
	return nil
}

// fsArtifactReader implements engine.ArtifactReader by reading from the
// filesystem layout produced by fsArtifactWriterFactory:
//
//	<baseDir>/collectors/cluster-wide/<collectorID>/<filename>
//	<baseDir>/collectors/per-scylla-cluster/<ns>/<name>/<collectorID>/<filename>
//	<baseDir>/collectors/per-pod/<ns>/<name>/<collectorID>/<filename>
//
// It does not need to know the scope up-front — it probes the three possible
// locations and returns the first file found. For ListArtifacts, it reads the
// Artifacts slice from vitals.json (already loaded into the Vitals store) so
// this type only needs to implement ReadArtifact for direct content reads.
type fsArtifactReader struct {
	baseDir string
}

var _ engine.ArtifactReader = (*fsArtifactReader)(nil)

// artifactDir returns the directory path for a given collector ID and scope key
// by probing the three possible scope directories.
func (r *fsArtifactReader) artifactDir(collectorID engine.CollectorID, scopeKey engine.ScopeKey) string {
	if scopeKey.IsEmpty() {
		return filepath.Join(r.baseDir, "collectors", "cluster-wide", string(collectorID))
	}
	// Try per-scylla-node first (most specific), then per-scylla-cluster.
	perNode := filepath.Join(r.baseDir, "collectors", "per-scylla-node", scopeKey.Namespace, scopeKey.Name, string(collectorID))
	if _, err := os.Stat(perNode); err == nil {
		return perNode
	}
	return filepath.Join(r.baseDir, "collectors", "per-scylla-cluster", scopeKey.Namespace, scopeKey.Name, string(collectorID))
}

func (r *fsArtifactReader) ReadArtifact(collectorID engine.CollectorID, scopeKey engine.ScopeKey, filename string) ([]byte, error) {
	path := filepath.Join(r.artifactDir(collectorID, scopeKey), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading artifact %s: %w", path, err)
	}
	return data, nil
}

func (r *fsArtifactReader) ListArtifacts(collectorID engine.CollectorID, scopeKey engine.ScopeKey) ([]engine.Artifact, error) {
	dir := r.artifactDir(collectorID, scopeKey)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing artifacts in %s: %w", dir, err)
	}

	var artifacts []engine.Artifact
	for _, entry := range entries {
		if !entry.IsDir() {
			artifacts = append(artifacts, engine.Artifact{
				RelativePath: entry.Name(),
			})
		}
	}
	return artifacts, nil
}
