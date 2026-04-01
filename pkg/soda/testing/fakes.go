// Package sodatesting provides shared fake implementations for unit testing
// soda diagnostic components. All fakes implement the interfaces defined in
// pkg/soda/engine/types.go.
package sodatesting

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// FakePodExecutor returns preconfigured stdout/stderr per command key.
// The key is built as "namespace/pod/container/command" where command parts
// are joined with spaces.
type FakePodExecutor struct {
	mu        sync.Mutex
	Responses map[string]FakeExecResponse
	Calls     []FakeExecCall
}

// FakeExecResponse holds the preconfigured response for a pod exec call.
type FakeExecResponse struct {
	Stdout string
	Stderr string
	Err    error
}

// FakeExecCall records an individual exec invocation.
type FakeExecCall struct {
	Namespace     string
	PodName       string
	ContainerName string
	Command       []string
}

func (f *FakePodExecutor) Execute(_ context.Context, namespace, podName, containerName string, command []string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	call := FakeExecCall{
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,
		Command:       command,
	}
	f.Calls = append(f.Calls, call)

	key := fmt.Sprintf("%s/%s/%s/%s", namespace, podName, containerName, strings.Join(command, " "))
	if resp, ok := f.Responses[key]; ok {
		return resp.Stdout, resp.Stderr, resp.Err
	}

	return "", "", fmt.Errorf("no fake response configured for key %q", key)
}

// FakeResourceLister implements engine.ResourceLister for testing.
// Fields are set directly; unset fields return nil slices with no error.
type FakeResourceLister struct {
	// Kubernetes core
	Nodes              []corev1.Node
	NodesErr           error
	Pods               map[string][]corev1.Pod // namespace → pods
	PodsErr            error
	ConfigMaps         map[string][]corev1.ConfigMap
	ConfigMapsErr      error
	Services           map[string][]corev1.Service
	ServicesErr        error
	ServiceAccounts    map[string][]corev1.ServiceAccount
	ServiceAccountsErr error

	// Kubernetes apps
	Deployments     map[string][]appsv1.Deployment
	DeploymentsErr  error
	StatefulSets    map[string][]appsv1.StatefulSet
	StatefulSetsErr error
	DaemonSets      map[string][]appsv1.DaemonSet
	DaemonSetsErr   error

	// Scylla
	ScyllaClusters           map[string][]engine.ScyllaClusterInfo
	ScyllaClustersErr        error
	ScyllaDBDatacenters      map[string][]*scyllav1alpha1.ScyllaDBDatacenter
	ScyllaDBDatacentersErr   error
	NodeConfigs              []*scyllav1alpha1.NodeConfig
	NodeConfigsErr           error
	ScyllaOperatorConfigs    []*scyllav1alpha1.ScyllaOperatorConfig
	ScyllaOperatorConfigsErr error
}

var _ engine.ResourceLister = (*FakeResourceLister)(nil)

func (f *FakeResourceLister) ListNodes(_ context.Context) ([]corev1.Node, error) {
	return f.Nodes, f.NodesErr
}

func (f *FakeResourceLister) ListPods(_ context.Context, namespace string, _ labels.Selector) ([]corev1.Pod, error) {
	if f.PodsErr != nil {
		return nil, f.PodsErr
	}
	return f.Pods[namespace], nil
}

func (f *FakeResourceLister) ListConfigMaps(_ context.Context, namespace string) ([]corev1.ConfigMap, error) {
	if f.ConfigMapsErr != nil {
		return nil, f.ConfigMapsErr
	}
	return f.ConfigMaps[namespace], nil
}

func (f *FakeResourceLister) ListServices(_ context.Context, namespace string) ([]corev1.Service, error) {
	if f.ServicesErr != nil {
		return nil, f.ServicesErr
	}
	return f.Services[namespace], nil
}

func (f *FakeResourceLister) ListServiceAccounts(_ context.Context, namespace string) ([]corev1.ServiceAccount, error) {
	if f.ServiceAccountsErr != nil {
		return nil, f.ServiceAccountsErr
	}
	return f.ServiceAccounts[namespace], nil
}

func (f *FakeResourceLister) ListDeployments(_ context.Context, namespace string) ([]appsv1.Deployment, error) {
	if f.DeploymentsErr != nil {
		return nil, f.DeploymentsErr
	}
	return f.Deployments[namespace], nil
}

func (f *FakeResourceLister) ListStatefulSets(_ context.Context, namespace string) ([]appsv1.StatefulSet, error) {
	if f.StatefulSetsErr != nil {
		return nil, f.StatefulSetsErr
	}
	return f.StatefulSets[namespace], nil
}

func (f *FakeResourceLister) ListDaemonSets(_ context.Context, namespace string) ([]appsv1.DaemonSet, error) {
	if f.DaemonSetsErr != nil {
		return nil, f.DaemonSetsErr
	}
	return f.DaemonSets[namespace], nil
}

func (f *FakeResourceLister) ListScyllaClusters(_ context.Context, namespace string) ([]engine.ScyllaClusterInfo, error) {
	if f.ScyllaClustersErr != nil {
		return nil, f.ScyllaClustersErr
	}
	if clusters, ok := f.ScyllaClusters[namespace]; ok {
		return clusters, nil
	}
	if namespace == "" {
		var all []engine.ScyllaClusterInfo
		for _, clusters := range f.ScyllaClusters {
			all = append(all, clusters...)
		}
		return all, nil
	}
	return nil, nil
}

func (f *FakeResourceLister) ListScyllaDBDatacenters(_ context.Context, namespace string) ([]*scyllav1alpha1.ScyllaDBDatacenter, error) {
	if f.ScyllaDBDatacentersErr != nil {
		return nil, f.ScyllaDBDatacentersErr
	}
	return f.ScyllaDBDatacenters[namespace], nil
}

func (f *FakeResourceLister) ListNodeConfigs(_ context.Context) ([]*scyllav1alpha1.NodeConfig, error) {
	return f.NodeConfigs, f.NodeConfigsErr
}

func (f *FakeResourceLister) ListScyllaOperatorConfigs(_ context.Context) ([]*scyllav1alpha1.ScyllaOperatorConfig, error) {
	return f.ScyllaOperatorConfigs, f.ScyllaOperatorConfigsErr
}

// FakeCollector is a configurable fake that implements engine.Collector.
// It records invocations and returns preconfigured results.
type FakeCollector struct {
	IDValue        engine.CollectorID
	NameValue      string
	ScopeValue     engine.CollectorScope
	DependsOnValue []engine.CollectorID

	// Result is returned by Collect. If nil, a default PASSED result is returned.
	Result *engine.CollectorResult
	// Err is returned by Collect as the error value.
	Err error

	mu          sync.Mutex
	CallCount   int
	CallParams  []engine.CollectorParams
	CollectFunc func(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error)
}

func (f *FakeCollector) ID() engine.CollectorID          { return f.IDValue }
func (f *FakeCollector) Name() string                    { return f.NameValue }
func (f *FakeCollector) Scope() engine.CollectorScope    { return f.ScopeValue }
func (f *FakeCollector) DependsOn() []engine.CollectorID { return f.DependsOnValue }

func (f *FakeCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	f.mu.Lock()
	f.CallCount++
	f.CallParams = append(f.CallParams, params)
	f.mu.Unlock()

	if f.CollectFunc != nil {
		return f.CollectFunc(ctx, params)
	}

	if f.Err != nil {
		return nil, f.Err
	}
	if f.Result != nil {
		return f.Result, nil
	}
	return &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: fmt.Sprintf("fake %s passed", f.IDValue),
	}, nil
}

// FakeAnalyzer is a configurable fake that implements engine.Analyzer.
// It records invocations and returns preconfigured results.
type FakeAnalyzer struct {
	IDValue        engine.AnalyzerID
	NameValue      string
	ScopeValue     engine.AnalyzerScope
	DependsOnValue []engine.CollectorID

	// Result is returned by Analyze. If nil, a default PASSED result is returned.
	Result *engine.AnalyzerResult

	mu          sync.Mutex
	CallCount   int
	CallParams  []engine.AnalyzerParams
	AnalyzeFunc func(params engine.AnalyzerParams) *engine.AnalyzerResult
}

func (f *FakeAnalyzer) ID() engine.AnalyzerID           { return f.IDValue }
func (f *FakeAnalyzer) Name() string                    { return f.NameValue }
func (f *FakeAnalyzer) Scope() engine.AnalyzerScope     { return f.ScopeValue }
func (f *FakeAnalyzer) DependsOn() []engine.CollectorID { return f.DependsOnValue }

func (f *FakeAnalyzer) Analyze(params engine.AnalyzerParams) *engine.AnalyzerResult {
	f.mu.Lock()
	f.CallCount++
	f.CallParams = append(f.CallParams, params)
	f.mu.Unlock()

	if f.AnalyzeFunc != nil {
		return f.AnalyzeFunc(params)
	}

	if f.Result != nil {
		return f.Result
	}
	return &engine.AnalyzerResult{
		Status:  engine.AnalyzerPassed,
		Message: fmt.Sprintf("fake %s passed", f.IDValue),
	}
}

// FakeArtifactWriter captures written artifacts in memory.
type FakeArtifactWriter struct {
	mu        sync.Mutex
	Artifacts map[string][]byte // filename → content
}

// NewFakeArtifactWriter creates a new FakeArtifactWriter.
func NewFakeArtifactWriter() *FakeArtifactWriter {
	return &FakeArtifactWriter{
		Artifacts: make(map[string][]byte),
	}
}

func (f *FakeArtifactWriter) WriteArtifact(filename string, content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Make a copy to avoid mutation.
	copied := make([]byte, len(content))
	copy(copied, content)
	f.Artifacts[filename] = copied

	// Return just the filename as the relative path, consistent with the
	// production fsArtifactWriter implementation.
	return filename, nil
}

// FakeArtifactReader returns preconfigured content from a nested map.
type FakeArtifactReader struct {
	// Data maps collectorID → scopeKey → filename → content.
	Data map[engine.CollectorID]map[engine.ScopeKey]map[string][]byte
}

// NewFakeArtifactReader creates a new FakeArtifactReader with an initialized data map.
func NewFakeArtifactReader() *FakeArtifactReader {
	return &FakeArtifactReader{
		Data: make(map[engine.CollectorID]map[engine.ScopeKey]map[string][]byte),
	}
}

// AddArtifact adds a single artifact to the reader.
func (f *FakeArtifactReader) AddArtifact(collectorID engine.CollectorID, scopeKey engine.ScopeKey, filename string, content []byte) {
	if f.Data[collectorID] == nil {
		f.Data[collectorID] = make(map[engine.ScopeKey]map[string][]byte)
	}
	if f.Data[collectorID][scopeKey] == nil {
		f.Data[collectorID][scopeKey] = make(map[string][]byte)
	}
	f.Data[collectorID][scopeKey][filename] = content
}

func (f *FakeArtifactReader) ReadArtifact(collectorID engine.CollectorID, scopeKey engine.ScopeKey, filename string) ([]byte, error) {
	if byScope, ok := f.Data[collectorID]; ok {
		if byFile, ok := byScope[scopeKey]; ok {
			if content, ok := byFile[filename]; ok {
				return content, nil
			}
		}
	}
	return nil, fmt.Errorf("artifact not found: %s/%s/%s", collectorID, scopeKey, filename)
}

func (f *FakeArtifactReader) ListArtifacts(collectorID engine.CollectorID, scopeKey engine.ScopeKey) ([]engine.Artifact, error) {
	if byScope, ok := f.Data[collectorID]; ok {
		if byFile, ok := byScope[scopeKey]; ok {
			var artifacts []engine.Artifact
			// Sort filenames for deterministic output.
			filenames := make([]string, 0, len(byFile))
			for filename := range byFile {
				filenames = append(filenames, filename)
			}
			sort.Strings(filenames)
			for _, filename := range filenames {
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: filename,
					Description:  fmt.Sprintf("fake artifact: %s", filename),
				})
			}
			return artifacts, nil
		}
	}
	return nil, nil
}
