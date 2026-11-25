// Package diagnose orchestrates a SODA run against one ScyllaCluster:
//
//  1. find the ScyllaDB Pods;
//  2. on each, check the runtime contract and run Scylla Doctor inside the scylla container over exec;
//  3. collect Kubernetes objects, logs and vitals, per node and for the cluster;
//  4. analyze in the Scylla Doctor bundle (per-node analysis, Scylla Doctor Cluster) and with SODA's analyzers;
//  5. write the combined report, its HTML rendering and an archive.
//
// A failing node or step is recorded and the run continues.
package diagnose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/scylladb/scylla-operator/pkg/build"
	scyllaversioned "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/contract"
	"github.com/scylladb/scylla-operator/pkg/soda/k8s"
	"github.com/scylladb/scylla-operator/pkg/soda/kube"
	"github.com/scylladb/scylla-operator/pkg/soda/report"
	"github.com/scylladb/scylla-operator/pkg/soda/runtime"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	apimachineryutilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// kubernetesCollectorTimeout bounds one Kubernetes collector, log streams included, so a stuck call can't hang
// the run.
const kubernetesCollectorTimeout = 5 * time.Minute

// DefaultSkippedAnalyzers call scylladb.com during analysis; they are skipped so analysis works without internet.
var DefaultSkippedAnalyzers = []string{"ScyllaUpdateAnalyzer", "DriverVersionAnalyzer"}

type Options struct {
	RestConfig   *rest.Config
	KubeClient   kubernetes.Interface
	ScyllaClient scyllaversioned.Interface

	Namespace   string
	ClusterName string
	DestDir     string

	// Exactly one of BundleDir (SODA runs inside the bundle image) and BundleImage (start an analysis Pod) is set.
	BundleDir          string
	BundlePython       string
	BundleImage        string
	BundlePullSecrets  []string
	BundlePullPolicy   corev1.PullPolicy
	AnalysisPodTimeout time.Duration
	AnalysisMemory     string

	CQLCredentialsSecret string
	NodeTimeout          time.Duration
	SideFilesLimitBytes  int64
	Parallelism          int
	SkippedAnalyzers     []string

	// OperatorNamespace is where the operator runs, for the operator collector.
	OperatorNamespace string
	// LogsTailLines keeps only the newest lines of each container log SODA copies; 0 keeps everything.
	LogsTailLines int64
}

type Result struct {
	DestDir    string
	ReportJSON string
	ReportHTML string
	Failures   int
	Warnings   int
	Incons     int
	NodeStates map[string]string
	RunErrors  []string
}

// Archive layout, relative to DestDir.
const (
	sdVitalsDir    = "scylla-doctor/vitals"
	sdAnalysisDir  = "scylla-doctor/analysis"
	sdClusterDir   = "scylla-doctor/cluster"
	sdRunsDir      = "scylla-doctor/runs"
	sdFilesDir     = "scylla-doctor/files"
	sodaVitalsDir  = "soda/vitals"
	sodaAnalysis   = "soda/analysis"
	kubernetesDir  = "kubernetes"
	reportDir      = "report"
	manifestFile   = "manifest.json"
	reportJSONFile = "report/soda-report.json"
	reportHTMLFile = "report/report.html"
)

type manifest struct {
	FormatVersion   string               `json:"formatVersion"`
	RunID           string               `json:"runID"`
	SODAVersion     string               `json:"sodaVersion"`
	StartedAt       string               `json:"startedAt"`
	FinishedAt      string               `json:"finishedAt"`
	AnalysisRuntime string               `json:"analysisRuntime"`
	Cluster         vitals.ClusterRef    `json:"cluster"`
	Bundle          *vitals.BundleInfo   `json:"bundle,omitempty"`
	Nodes           []vitals.NodeSummary `json:"nodes"`
	Layout          map[string]string    `json:"layout"`
	Errors          []string             `json:"errors,omitempty"`
}

func Run(ctx context.Context, o Options) (*Result, error) {
	started := time.Now().UTC()
	runID := apimachineryutilrand.String(8)
	var runErrors []string
	addErr := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		klog.Warning(msg)
		runErrors = append(runErrors, msg)
	}
	path := func(rel ...string) string { return filepath.Join(append([]string{o.DestDir}, rel...)...) }
	for _, d := range []string{sdVitalsDir, sdAnalysisDir, sdClusterDir, sdRunsDir, sodaVitalsDir, sodaAnalysis, kubernetesDir, reportDir} {
		if err := os.MkdirAll(path(d), 0o755); err != nil {
			return nil, err
		}
	}

	// 1. The cluster and its ScyllaDB Pods.
	sc, err := o.ScyllaClient.ScyllaV1().ScyllaClusters(o.Namespace).Get(ctx, o.ClusterName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("can't get ScyllaCluster %s/%s: %w", o.Namespace, o.ClusterName, err)
	}
	clusterRef := vitals.ClusterRef{Kind: "ScyllaCluster", Namespace: sc.Namespace, Name: sc.Name, UID: string(sc.UID)}
	podList, err := o.KubeClient.CoreV1().Pods(o.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			naming.ClusterNameLabel: sc.Name,
			naming.PodTypeLabel:     string(naming.PodTypeScyllaDBNode),
		}).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("can't list ScyllaDB Pods: %w", err)
	}
	pods := podList.Items
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	if len(pods) == 0 {
		return nil, fmt.Errorf("ScyllaCluster %s/%s has no ScyllaDB Pods", o.Namespace, o.ClusterName)
	}
	klog.InfoS("Found ScyllaDB Pods", "Cluster", o.Namespace+"/"+o.ClusterName, "Count", len(pods))

	// 2. The bundle: local, or in an analysis Pod started from the bundle image.
	execer := kube.NewExecer(o.RestConfig, o.KubeClient)
	var rt runtime.Runtime
	if len(o.BundleDir) > 0 {
		rt, err = runtime.NewLocal(o.BundleDir, o.BundlePython)
	} else {
		rt, err = runtime.NewPod(ctx, execer, o.KubeClient, runtime.PodOptions{
			Namespace:       o.Namespace,
			Image:           o.BundleImage,
			RunID:           runID,
			PullSecrets:     o.BundlePullSecrets,
			ImagePullPolicy: o.BundlePullPolicy,
			ActiveDeadline:  o.AnalysisPodTimeout,
			StartTimeout:    5 * time.Minute,
			MemoryLimit:     o.AnalysisMemory,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("can't prepare the Scylla Doctor bundle: %w", err)
	}
	defer func() {
		if err := rt.Close(context.Background()); err != nil {
			klog.ErrorS(err, "Can't clean up the analysis runtime")
		}
	}()
	klog.InfoS("Using the Scylla Doctor bundle", "Runtime", rt.Describe())

	bundleInfo, c, pyz, err := loadBundle(ctx, rt)
	if err != nil {
		return nil, err
	}
	bundleInfo.Image = rt.Image()

	var cqlConfig []byte
	if len(o.CQLCredentialsSecret) > 0 {
		cqlConfig, err = cqlConfigFromSecret(ctx, o.KubeClient, o.Namespace, o.CQLCredentialsSecret)
		if err != nil {
			return nil, err
		}
	}

	// 3. Scylla Doctor on every node, bounded parallelism.
	outputs := make([]*collectors.NodeOutput, len(pods))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, o.Parallelism))
	for i := range pods {
		pod := &pods[i]
		if !scyllaRunning(pod) {
			outputs[i] = &collectors.NodeOutput{Name: pod.Name, Pod: pod, Collection: vitals.NodeCollection{
				State: vitals.NodeCollectionFailed, Reason: "the scylla container is not running"}}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			klog.InfoS("Collecting Scylla Doctor vitals", "Pod", pod.Namespace+"/"+pod.Name)
			outputs[i] = collectors.CollectScyllaDoctor(ctx, execer, pod, collectors.NodeCollectOptions{
				RunID: runID, Pyz: pyz, CQLConfig: cqlConfig, Contract: c, Timeout: o.NodeTimeout,
				SideFilesDir:        path(sdFilesDir, pod.Name),
				SideFilesArchiveDir: filepath.Join(sdFilesDir, pod.Name),
				SideFilesLimitBytes: o.SideFilesLimitBytes,
			})
		}()
	}
	wg.Wait()

	// Kubernetes-side collection shares the namespace's events, listed once.
	env := &k8s.Env{
		KubeClient:        o.KubeClient,
		ScyllaClient:      o.ScyllaClient,
		Namespace:         o.Namespace,
		OperatorNamespace: o.OperatorNamespace,
		LogsTailLines:     o.LogsTailLines,
		ArchiveRoot:       o.DestDir,
	}
	ectx, ecancel := context.WithTimeout(ctx, time.Minute)
	evs, err := o.KubeClient.CoreV1().Events(o.Namespace).List(ectx, metav1.ListOptions{})
	ecancel()
	if err != nil {
		addErr("can't list events in %s: %v", o.Namespace, err)
	} else {
		env.Events = evs.Items
	}

	// 4. Write per-node outputs. Scylla Doctor's vitals are written byte-for-byte as it produced them.
	sodaVitals := map[string]vitals.Vitals{}
	nodeInputs := make([]report.NodeInput, 0, len(outputs))
	analysisFiles := map[string][]byte{}
	var vitalsPaths []string
	for _, out := range outputs {
		if len(out.RawVitals) > 0 && out.Collection.State != vitals.NodeCollectionFailed {
			rel := filepath.Join(sdVitalsDir, out.Name+".vitals.json")
			if err := os.WriteFile(path(rel), out.RawVitals, 0o644); err != nil {
				return nil, err
			}
			vitalsPaths = append(vitalsPaths, rel)
			analysisFiles["vitals/"+out.Name+".vitals.json"] = out.RawVitals
		}
		if out.Stdout != nil || out.Stderr != nil {
			_ = os.MkdirAll(path(sdRunsDir, out.Name), 0o755)
			_ = os.WriteFile(path(sdRunsDir, out.Name, "stdout.log"), out.Stdout, 0o644)
			_ = os.WriteFile(path(sdRunsDir, out.Name, "stderr.log"), out.Stderr, 0o644)
		}

		sv := collectors.SODAVitals(out, c)
		for _, kc := range k8s.NodeCollectors() {
			cctx, cancel := context.WithTimeout(ctx, kubernetesCollectorTimeout)
			sv[kc.Name()] = kc.Collect(cctx, env, out.Pod)
			cancel()
		}
		sodaVitals[out.Name] = sv
		rel := filepath.Join(sodaVitalsDir, out.Name+".vitals.json")
		if err := vitals.WriteJSONFile(path(rel), sv); err != nil {
			return nil, err
		}
		vitalsPaths = append(vitalsPaths, rel)
		sa := analyzers.RunNode(sv)
		if err := vitals.WriteJSONFile(path(sodaAnalysis, out.Name+".analysis.json"), sa); err != nil {
			return nil, err
		}

		summary := vitals.NodeSummary{
			Name:           out.Name,
			Pod:            out.Pod.Namespace + "/" + out.Pod.Name,
			KubernetesNode: out.Pod.Spec.NodeName,
			Datacenter:     out.Pod.Labels["scylla/datacenter"],
			Rack:           out.Pod.Labels["scylla/rack"],
			Collection:     out.Collection,
			Files:          out.SideFiles,
		}
		if out.Vitals != nil {
			summary.Collectors = vitals.CountCollectors(out.Vitals)
		}
		nodeInputs = append(nodeInputs, report.NodeInput{Summary: summary, SODAAnalysis: sa})
	}

	// 5. Cluster-scoped Kubernetes vitals, written like a node's under the name _cluster.
	klog.InfoS("Collecting cluster-scoped Kubernetes vitals")
	clusterVitals := vitals.Vitals{collectors.SODAVersionCollector: collectors.VersionResult()}
	for _, kc := range k8s.ClusterCollectors() {
		cctx, cancel := context.WithTimeout(ctx, kubernetesCollectorTimeout)
		clusterVitals[kc.Name()] = kc.Collect(cctx, env, sc)
		cancel()
	}
	sodaVitals[k8s.ClusterScope] = clusterVitals
	rel := filepath.Join(sodaVitalsDir, k8s.ClusterScope+".vitals.json")
	if err := vitals.WriteJSONFile(path(rel), clusterVitals); err != nil {
		return nil, err
	}
	vitalsPaths = append(vitalsPaths, rel)
	clusterAnalysis := analyzers.RunCluster(clusterVitals)
	if err := vitals.WriteJSONFile(path(sodaAnalysis, k8s.ClusterScope+".analysis.json"), clusterAnalysis); err != nil {
		return nil, err
	}

	// 6. Scylla Doctor analysis in the bundle runtime.
	skips := o.SkippedAnalyzers
	var sdReport *vitals.ClusterReport
	if len(analysisFiles) == 0 {
		addErr("no node produced usable vitals, so Scylla Doctor's analysis was not run")
	} else if err := putFiles(ctx, rt, analysisFiles); err != nil {
		addErr("can't hand the vitals to the analysis runtime: %v", err)
	} else {
		for i := range nodeInputs {
			n := &nodeInputs[i]
			in := "vitals/" + n.Summary.Name + ".vitals.json"
			if _, ok := analysisFiles[in]; !ok {
				continue
			}
			argv := []string{rt.Python(), filepath.Join(rt.BundleDir(), runtime.FullPyz), "--load-vitals", in, "--output", "json"}
			for _, s := range skips {
				argv = append(argv, "-st", s)
			}
			res, err := runAnalysis(ctx, rt, argv)
			if err != nil {
				addErr("per-node analysis of %s: %v", n.Summary.Name, err)
				continue
			}
			var a vitals.Analysis
			if err := json.Unmarshal(res.Stdout, &a); err != nil {
				addErr("per-node analysis of %s didn't return JSON (exit %d): %s", n.Summary.Name, res.ExitCode, lastLine(res.Stderr))
				continue
			}
			_ = os.WriteFile(path(sdAnalysisDir, n.Summary.Name+".analysis.json"), res.Stdout, 0o644)
			n.Summary.Analyzers = vitals.CountAnalyzers(a)
		}

		argv := []string{rt.Python(), filepath.Join(rt.BundleDir(), runtime.ClusterPyz), "--dir", "vitals", "--output", "json",
			"--config-file", filepath.Join(rt.BundleDir(), runtime.ClusterIni)}
		for _, s := range skips {
			argv = append(argv, "-sov", "SkipTest,"+s+",")
		}
		res, err := runAnalysis(ctx, rt, argv)
		switch {
		case err != nil:
			addErr("Scylla Doctor Cluster: %v", err)
		default:
			_ = os.WriteFile(path(sdClusterDir, "report.json"), res.Stdout, 0o644)
			_ = os.WriteFile(path(sdClusterDir, "report.stderr.log"), res.Stderr, 0o644)
			// It exits 1 on any finding, so only the output tells whether it worked.
			r := &vitals.ClusterReport{}
			if err := json.Unmarshal(res.Stdout, r); err != nil {
				addErr("Scylla Doctor Cluster didn't return a report (exit %d): %s %s", res.ExitCode,
					lastLine(res.Stdout), lastLine(res.Stderr))
			} else {
				sdReport = r
			}
		}
	}

	// 7. The combined report, its HTML rendering and the manifest.
	finished := time.Now().UTC()
	ext := vitals.SODAExtension{
		GeneratedAt:         finished.Format(time.RFC3339),
		SODAVersion:         vitals.SODAVersion(build.GitCommit()),
		ScyllaDoctorVersion: bundleInfo.ScyllaDoctorVersion,
		Bundle:              bundleInfo,
		Cluster:             clusterRef,
		SkippedAnalyzers:    skips,
		Errors:              runErrors,
	}
	combined := report.Merge(sdReport, nodeInputs, clusterAnalysis, vitalsPaths, ext)
	if err := vitals.WriteJSONFile(path(reportJSONFile), combined); err != nil {
		return nil, err
	}
	html, err := report.RenderHTML(combined, sodaVitals, c)
	if err != nil {
		return nil, fmt.Errorf("can't render the HTML report: %w", err)
	}
	if err := os.WriteFile(path(reportHTMLFile), html, 0o644); err != nil {
		return nil, err
	}

	m := manifest{
		FormatVersion:   "soda.scylladb.com/archive/v1alpha1",
		RunID:           runID,
		SODAVersion:     vitals.SODAVersion(build.GitCommit()),
		StartedAt:       started.Format(time.RFC3339),
		FinishedAt:      finished.Format(time.RFC3339),
		AnalysisRuntime: rt.Describe(),
		Cluster:         clusterRef,
		Bundle:          bundleInfo,
		Nodes:           combined.SODA.Nodes,
		Errors:          runErrors,
		Layout: map[string]string{
			sdVitalsDir:               "Scylla Doctor per-node vitals, unchanged (<node>.vitals.json)",
			sdAnalysisDir:             "Scylla Doctor per-node analysis (--load-vitals --output json)",
			sdClusterDir:              "Scylla Doctor Cluster report (--output json) and its stderr",
			sdRunsDir:                 "Scylla Doctor's console output per node",
			sdFilesDir:                "files Scylla Doctor wrote next to its vitals, per node (listed in nodes[].files)",
			sodaVitalsDir:             "SODA's per-node vitals, in Scylla Doctor's vitals format",
			sodaAnalysis:              "SODA's per-node analysis, in Scylla Doctor's analysis format",
			"kubernetes/cluster":      "Kubernetes objects and logs of the ScyllaCluster as a whole and of the operator",
			"kubernetes/nodes/<node>": "Kubernetes objects and logs of one ScyllaDB node: Pod, member Service, logs, events, storage, Kubernetes Node",
			reportJSONFile:            "combined report: Scylla Doctor Cluster's format plus a `soda` key",
			reportHTMLFile:            "the combined report for people",
		},
	}
	if err := vitals.WriteJSONFile(path(manifestFile), m); err != nil {
		return nil, err
	}

	f, w, inc := report.Totals(combined)
	states := map[string]string{}
	for _, n := range combined.SODA.Nodes {
		states[n.Name] = n.Collection.State
	}
	return &Result{
		DestDir: o.DestDir, ReportJSON: path(reportJSONFile), ReportHTML: path(reportHTMLFile),
		Failures: f, Warnings: w, Incons: inc, NodeStates: states, RunErrors: runErrors,
	}, nil
}

func putFiles(ctx context.Context, rt runtime.Runtime, files map[string][]byte) error {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return rt.PutFiles(cctx, files)
}

func runAnalysis(ctx context.Context, rt runtime.Runtime, argv []string) (*kube.ExecResult, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return rt.Run(cctx, argv)
}

func loadBundle(ctx context.Context, rt runtime.Runtime) (*vitals.BundleInfo, *contract.Contract, []byte, error) {
	read := func(rel string) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		b, err := rt.ReadBundleFile(cctx, rel)
		if err != nil {
			return nil, fmt.Errorf("can't read %s from the bundle: %w", rel, err)
		}
		return b, nil
	}
	mb, err := read(runtime.BundleManifest)
	if err != nil {
		return nil, nil, nil, err
	}
	info := &vitals.BundleInfo{}
	if err := json.Unmarshal(mb, info); err != nil {
		return nil, nil, nil, fmt.Errorf("can't parse the bundle manifest: %w", err)
	}
	cb, err := read(runtime.RuntimeContract)
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := contract.Parse(cb)
	if err != nil {
		return nil, nil, nil, err
	}
	pyz, err := read(runtime.CollectPyz)
	if err != nil {
		return nil, nil, nil, err
	}
	return info, c, pyz, nil
}

// cqlConfigFromSecret builds Scylla Doctor's [CQL] configuration from a Secret with `username` and `password`.
// The result only ever travels over exec stdin.
func cqlConfigFromSecret(ctx context.Context, kc kubernetes.Interface, namespace, name string) ([]byte, error) {
	s, err := kc.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("can't get CQL credentials Secret %s/%s: %w", namespace, name, err)
	}
	user, pass := string(s.Data["username"]), string(s.Data["password"])
	if len(user) == 0 || len(pass) == 0 {
		return nil, fmt.Errorf("Secret %s/%s needs non-empty `username` and `password` keys", namespace, name)
	}
	if strings.ContainsAny(user+pass, "\n\r") || strings.TrimSpace(user) != user || strings.TrimSpace(pass) != pass {
		return nil, fmt.Errorf("Secret %s/%s: username and password can't contain newlines or surrounding spaces", namespace, name)
	}
	// Scylla Doctor reads it with Python's configparser, which interpolates '%'.
	esc := func(v string) string { return strings.ReplaceAll(v, "%", "%%") }
	return []byte(fmt.Sprintf("[CQL]\nuser = %s\npassword = %s\n", esc(user), esc(pass))), nil
}

func scyllaRunning(pod *corev1.Pod) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == naming.ScyllaContainerName {
			return cs.State.Running != nil
		}
	}
	return false
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}
