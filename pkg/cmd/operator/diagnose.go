package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	scyllaversioned "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/gather/collect"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/signals"
	"github.com/scylladb/scylla-operator/pkg/soda/diagnose"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apimachineryutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	diagnoseLongDescription = templates.LongDesc(`
		diagnose runs SODA (Scylla Operator Diagnostic Automation) against one ScyllaCluster.

		It runs Scylla Doctor inside every ScyllaDB node's scylla container over exec, collects Kubernetes-side
		vitals and a must-gather dump, analyzes everything with Scylla Doctor, Scylla Doctor Cluster and SODA's own
		analyzers, and writes an archive with Scylla Doctor's outputs, a combined report and an HTML report.

		Scylla Doctor comes from its bundle image. From a workstation, pass --bundle-image: SODA starts a short-lived
		Pod from it for the analysis. Inside the bundle image (an in-cluster Job), pass --bundle-dir.

		This command is a proof of concept and subject to change without notice.
	`)

	diagnoseExample = templates.Examples(`
		# From a workstation, with a kubeconfig for the SODA ServiceAccount.
		scylla-operator diagnose -n scylla --scylla-cluster scylla --bundle-image registry.example.com/scylla-doctor-bundle:1.15

		# With CQL authentication: a Secret with "username" and "password" keys in the cluster's namespace.
		scylla-operator diagnose -n scylla --scylla-cluster scylla --bundle-image ... --cql-credentials-secret soda-cql

		# Inside the bundle image (in-cluster Job).
		scylla-operator diagnose -n scylla --scylla-cluster scylla --bundle-dir /opt/scylla-doctor --dest-dir /output/run
	`)
)

type DiagnoseOptions struct {
	*GatherBaseOptions

	ClusterName          string
	BundleImage          string
	BundleDir            string
	BundlePython         string
	BundlePullSecrets    []string
	BundlePullPolicy     string
	AnalysisMemory       string
	AnalysisPodTimeout   time.Duration
	CQLCredentialsSecret string
	NodeTimeout          time.Duration
	Parallelism          int
	SkippedAnalyzers     []string
	SkipMustGather       bool
	Archive              bool
	KeepRunningAfter     time.Duration

	scyllaClient scyllaversioned.Interface
	namespace    string
}

func NewDiagnoseOptions(streams genericclioptions.IOStreams) *DiagnoseOptions {
	python := os.Getenv("SCYLLA_DOCTOR_PYTHON")
	if len(python) == 0 {
		python = "python3"
	}
	return &DiagnoseOptions{
		GatherBaseOptions:  NewGatherBaseOptions("scylla-operator-diagnose"),
		BundleDir:          os.Getenv("SCYLLA_DOCTOR_BUNDLE_DIR"),
		BundlePython:       python,
		BundlePullPolicy:   string(corev1.PullIfNotPresent),
		AnalysisMemory:     "1Gi",
		AnalysisPodTimeout: time.Hour,
		NodeTimeout:        10 * time.Minute,
		Parallelism:        3,
		SkippedAnalyzers:   diagnose.DefaultSkippedAnalyzers,
		Archive:            true,
	}
}

func (o *DiagnoseOptions) AddFlags(flagset *pflag.FlagSet) {
	o.GatherBaseOptions.AddFlags(flagset)
	flagset.StringVar(&o.ClusterName, "scylla-cluster", o.ClusterName, "Name of the ScyllaCluster to diagnose, in the namespace given by --namespace. (--cluster is kubeconfig's.)")
	flagset.StringVar(&o.BundleImage, "bundle-image", o.BundleImage, "Scylla Doctor bundle image. SODA starts a short-lived analysis Pod from it.")
	flagset.StringVar(&o.BundleDir, "bundle-dir", o.BundleDir, "Local Scylla Doctor bundle directory, when SODA runs inside the bundle image. Defaults to $SCYLLA_DOCTOR_BUNDLE_DIR.")
	flagset.StringVar(&o.BundlePython, "bundle-python", o.BundlePython, "Python interpreter for analysis with --bundle-dir. Defaults to $SCYLLA_DOCTOR_PYTHON or python3.")
	flagset.StringArrayVar(&o.BundlePullSecrets, "bundle-pull-secret", o.BundlePullSecrets, "Image pull Secret for the bundle image. Can be repeated.")
	flagset.StringVar(&o.BundlePullPolicy, "bundle-pull-policy", o.BundlePullPolicy, "Image pull policy for the bundle image.")
	flagset.StringVar(&o.AnalysisMemory, "analysis-memory", o.AnalysisMemory, "Memory limit of the analysis Pod.")
	flagset.DurationVar(&o.AnalysisPodTimeout, "analysis-pod-timeout", o.AnalysisPodTimeout, "The analysis Pod ends on its own after this long, even if SODA dies.")
	flagset.StringVar(&o.CQLCredentialsSecret, "cql-credentials-secret", o.CQLCredentialsSecret, "Secret with `username` and `password` keys for CQL, when authentication is on.")
	flagset.DurationVar(&o.NodeTimeout, "node-timeout", o.NodeTimeout, "Maximum time Scylla Doctor may run on one node.")
	flagset.IntVar(&o.Parallelism, "parallelism", o.Parallelism, "How many nodes are collected at the same time.")
	flagset.StringArrayVar(&o.SkippedAnalyzers, "skip-analyzer", o.SkippedAnalyzers, "Scylla Doctor analyzer to skip. Defaults to the ones that call scylladb.com.")
	flagset.BoolVar(&o.SkipMustGather, "skip-must-gather", o.SkipMustGather, "Don't collect Kubernetes objects and logs.")
	flagset.BoolVar(&o.Archive, "archive", o.Archive, "Pack the destination directory into <dest-dir>.tar.gz.")
	flagset.DurationVar(&o.KeepRunningAfter, "keep-running-after", o.KeepRunningAfter, "Keep running this long after finishing, so the archive can be copied out of a Job's Pod.")
}

func NewDiagnoseCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewDiagnoseOptions(streams)

	cmd := &cobra.Command{
		Use:     "diagnose",
		Short:   "Diagnose a ScyllaCluster with Scylla Doctor and SODA's analyzers (proof of concept).",
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
		ValidArgs:     []string{},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	o.AddFlags(cmd.Flags())
	return cmd
}

func (o *DiagnoseOptions) Validate() error {
	var errs []error
	errs = append(errs, o.GatherBaseOptions.Validate())
	if len(o.ClusterName) == 0 {
		errs = append(errs, fmt.Errorf("--scylla-cluster is required"))
	}
	switch {
	case len(o.BundleImage) > 0 && len(o.BundleDir) > 0:
		errs = append(errs, fmt.Errorf("--bundle-image and --bundle-dir are mutually exclusive"))
	case len(o.BundleImage) == 0 && len(o.BundleDir) == 0:
		errs = append(errs, fmt.Errorf("one of --bundle-image and --bundle-dir is required"))
	}
	switch corev1.PullPolicy(o.BundlePullPolicy) {
	case corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
	default:
		errs = append(errs, fmt.Errorf("unknown --bundle-pull-policy %q", o.BundlePullPolicy))
	}
	if o.Parallelism < 1 {
		errs = append(errs, fmt.Errorf("--parallelism must be at least 1"))
	}
	return apimachineryutilerrors.NewAggregate(errs)
}

func (o *DiagnoseOptions) Complete() error {
	if err := o.GatherBaseOptions.Complete(); err != nil {
		return err
	}
	var err error
	o.scyllaClient, err = scyllaversioned.NewForConfig(o.restConfig)
	if err != nil {
		return fmt.Errorf("can't build scylla clientset: %w", err)
	}
	o.namespace, _, err = o.ConfigFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("can't determine the namespace: %w", err)
	}
	return nil
}

func (o *DiagnoseOptions) Run(originalStreams genericclioptions.IOStreams, cmd *cobra.Command) error {
	if err := o.GatherBaseOptions.RunInit(originalStreams, cmd); err != nil {
		return err
	}

	stopCh := signals.StopChannel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stopCh
		cancel()
	}()

	opts := diagnose.Options{
		RestConfig:           o.restConfig,
		KubeClient:           o.kubeClient,
		ScyllaClient:         o.scyllaClient,
		Namespace:            o.namespace,
		ClusterName:          o.ClusterName,
		DestDir:              o.cliFlags.DestDir,
		BundleDir:            o.BundleDir,
		BundlePython:         o.BundlePython,
		BundleImage:          o.BundleImage,
		BundlePullSecrets:    o.BundlePullSecrets,
		BundlePullPolicy:     corev1.PullPolicy(o.BundlePullPolicy),
		AnalysisPodTimeout:   o.AnalysisPodTimeout,
		AnalysisMemory:       o.AnalysisMemory,
		CQLCredentialsSecret: o.CQLCredentialsSecret,
		NodeTimeout:          o.NodeTimeout,
		Parallelism:          o.Parallelism,
		SkippedAnalyzers:     o.SkippedAnalyzers,
	}
	if !o.SkipMustGather {
		opts.MustGather = o.mustGather
	}

	res, err := diagnose.Run(ctx, opts)
	if err != nil {
		return err
	}
	klog.Flush()

	fmt.Fprintf(originalStreams.Out, "Report: %s\n", res.ReportHTML)
	fmt.Fprintf(originalStreams.Out, "Findings: %d failures, %d warnings, %d differences between nodes\n", res.Failures, res.Warnings, res.Incons)
	for n, s := range res.NodeStates {
		fmt.Fprintf(originalStreams.Out, "Node %s: %s\n", n, s)
	}
	for _, e := range res.RunErrors {
		fmt.Fprintf(originalStreams.Out, "Problem: %s\n", e)
	}

	if o.Archive {
		archive, err := diagnose.CreateArchive(o.cliFlags.DestDir)
		if err != nil {
			return fmt.Errorf("can't create the archive: %w", err)
		}
		fmt.Fprintf(originalStreams.Out, "Archive: %s\n", archive)
	}

	if o.KeepRunningAfter > 0 {
		klog.InfoS("Keeping the Pod running so the results can be copied out", "Duration", o.KeepRunningAfter)
		select {
		case <-ctx.Done():
		case <-time.After(o.KeepRunningAfter):
		}
	}
	return nil
}

// mustGather collects the ScyllaCluster and everything in its namespace, the way must-gather does.
func (o *DiagnoseOptions) mustGather(ctx context.Context, destDir string) error {
	discoverer := collect.NewResourceDiscoverer(o.cliFlags.IncludeSensitiveResources, o.discoveryClient)
	resources, err := discoverer.DiscoverResources(o.resourcesToExclude...)
	if err != nil {
		return fmt.Errorf("can't discover resources: %w", err)
	}
	collector := collect.NewCollector(
		destDir,
		o.GetPrinters(),
		o.restConfig,
		resources,
		o.kubeClient.CoreV1(),
		o.dynamicClient,
		true,
		true,
		o.cliFlags.LogsLimitBytes,
	)
	ri, err := findResource(resources, schema.GroupResource{Group: "scylla.scylladb.com", Resource: "scyllaclusters"})
	if err != nil {
		return err
	}
	return collector.CollectResourceObject(ctx, ri, o.namespace, o.ClusterName)
}
