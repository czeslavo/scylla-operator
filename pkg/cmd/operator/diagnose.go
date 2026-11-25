package operator

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	scyllaversioned "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/cmdutil"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/signals"
	"github.com/scylladb/scylla-operator/pkg/soda/diagnose"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apimachineryutilerrors "k8s.io/apimachinery/pkg/util/errors"
	apimachineryutilrand "k8s.io/apimachinery/pkg/util/rand"
	kgenericclioptions "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	diagnoseLongDescription = templates.LongDesc(`
		diagnose runs SODA (Scylla Operator Diagnostic Automation) against one ScyllaCluster.

		It runs Scylla Doctor inside every ScyllaDB node's scylla container over exec, collects Kubernetes objects,
		logs and vitals per node and for the cluster, analyzes everything with Scylla Doctor, Scylla Doctor Cluster and SODA's own
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
	ConfigFlags *kgenericclioptions.ConfigFlags

	DestDir              string
	LogsTailLines        int64
	OperatorNamespace    string
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
	SideFilesLimitBytes  int64
	Parallelism          int
	SkippedAnalyzers     []string
	Archive              bool
	KeepRunningAfter     time.Duration

	restConfig   *rest.Config
	kubeClient   kubernetes.Interface
	scyllaClient scyllaversioned.Interface
	namespace    string
}

func NewDiagnoseOptions(streams genericclioptions.IOStreams) *DiagnoseOptions {
	python := os.Getenv("SCYLLA_DOCTOR_PYTHON")
	if len(python) == 0 {
		python = "python3"
	}
	return &DiagnoseOptions{
		ConfigFlags: kgenericclioptions.NewConfigFlags(true).WithWrapConfigFn(func(c *rest.Config) *rest.Config {
			c.UserAgent = genericclioptions.MakeVersionedUserAgent("scylla-operator-diagnose")
			return c
		}),
		OperatorNamespace:   "scylla-operator",
		BundleDir:           os.Getenv("SCYLLA_DOCTOR_BUNDLE_DIR"),
		BundlePython:        python,
		BundlePullPolicy:    string(corev1.PullIfNotPresent),
		AnalysisMemory:      "1Gi",
		AnalysisPodTimeout:  time.Hour,
		NodeTimeout:         10 * time.Minute,
		SideFilesLimitBytes: 256 * 1024 * 1024,
		Parallelism:         3,
		SkippedAnalyzers:    diagnose.DefaultSkippedAnalyzers,
		Archive:             true,
	}
}

func (o *DiagnoseOptions) AddFlags(flagset *pflag.FlagSet) {
	o.ConfigFlags.AddFlags(flagset)
	flagset.StringVar(&o.DestDir, "dest-dir", o.DestDir, "Directory for the results; it must not exist or be empty. Defaults to scylla-operator-diagnose-<random>.")
	flagset.Int64Var(&o.LogsTailLines, "log-tail-lines", o.LogsTailLines, "Keep only the newest lines of each container log; 0 keeps everything.")
	flagset.StringVar(&o.OperatorNamespace, "operator-namespace", o.OperatorNamespace, "Namespace the operator runs in, for its configuration and logs.")
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
	flagset.Int64Var(&o.SideFilesLimitBytes, "side-files-limit-bytes", o.SideFilesLimitBytes, "Maximum total size of the files Scylla Doctor writes next to its vitals (such as logs) copied per node.")
	flagset.IntVar(&o.Parallelism, "parallelism", o.Parallelism, "How many nodes are collected at the same time.")
	flagset.StringArrayVar(&o.SkippedAnalyzers, "skip-analyzer", o.SkippedAnalyzers, "Scylla Doctor analyzer to skip. Defaults to the ones that call scylladb.com.")
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
	if o.LogsTailLines < 0 {
		errs = append(errs, fmt.Errorf("--log-tail-lines can't be negative"))
	}
	if len(o.DestDir) > 0 {
		if entries, err := os.ReadDir(o.DestDir); err == nil && len(entries) > 0 {
			errs = append(errs, fmt.Errorf("destination directory %q is not empty", o.DestDir))
		}
	}
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
	if o.SideFilesLimitBytes < 0 {
		errs = append(errs, fmt.Errorf("--side-files-limit-bytes can't be negative"))
	}
	if o.Parallelism < 1 {
		errs = append(errs, fmt.Errorf("--parallelism must be at least 1"))
	}
	return apimachineryutilerrors.NewAggregate(errs)
}

func (o *DiagnoseOptions) Complete() error {
	var err error
	o.restConfig, err = o.ConfigFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("can't create RESTConfig: %w", err)
	}
	o.kubeClient, err = kubernetes.NewForConfig(o.restConfig)
	if err != nil {
		return fmt.Errorf("can't build kubernetes clientset: %w", err)
	}
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
	if len(o.DestDir) == 0 {
		o.DestDir = "scylla-operator-diagnose-" + apimachineryutilrand.String(12)
	}
	if err := os.MkdirAll(o.DestDir, 0o770); err != nil {
		return fmt.Errorf("can't create destination directory %q: %w", o.DestDir, err)
	}
	// Log to stderr and to a file that goes into the archive.
	for k, v := range map[string]string{"logtostderr": "false", "alsologtostderr": "true",
		"log_file": filepath.Join(o.DestDir, "scylla-operator-diagnose.log")} {
		if err := flag.Set(k, v); err != nil {
			return fmt.Errorf("can't set %s flag: %w", k, err)
		}
	}
	flag.Parse()
	cmdutil.LogCommandStarting(cmd)
	cliflag.PrintFlags(cmd.Flags())

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
		DestDir:              o.DestDir,
		OperatorNamespace:    o.OperatorNamespace,
		LogsTailLines:        o.LogsTailLines,
		BundleDir:            o.BundleDir,
		BundlePython:         o.BundlePython,
		BundleImage:          o.BundleImage,
		BundlePullSecrets:    o.BundlePullSecrets,
		BundlePullPolicy:     corev1.PullPolicy(o.BundlePullPolicy),
		AnalysisPodTimeout:   o.AnalysisPodTimeout,
		AnalysisMemory:       o.AnalysisMemory,
		CQLCredentialsSecret: o.CQLCredentialsSecret,
		NodeTimeout:          o.NodeTimeout,
		SideFilesLimitBytes:  o.SideFilesLimitBytes,
		Parallelism:          o.Parallelism,
		SkippedAnalyzers:     o.SkippedAnalyzers,
	}

	res, err := diagnose.Run(ctx, opts)
	if err != nil {
		return err
	}
	klog.Flush()

	fmt.Fprintf(originalStreams.Out, "Report: %s\n", res.ReportHTML)
	fmt.Fprintf(originalStreams.Out, "Findings: %d failures, %d warnings, %d differences between nodes\n", res.Failures, res.Warnings, res.Incons)
	nodes := make([]string, 0, len(res.NodeStates))
	for n := range res.NodeStates {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		fmt.Fprintf(originalStreams.Out, "Node %s: %s\n", n, res.NodeStates[n])
	}
	for _, e := range res.RunErrors {
		fmt.Fprintf(originalStreams.Out, "Problem: %s\n", e)
	}

	if o.Archive {
		archive, err := diagnose.CreateArchive(o.DestDir)
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
