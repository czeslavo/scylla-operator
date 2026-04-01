# Collector/Analyzer Interface Cleanup

This document captures the design problems identified during a review of `pkg/soda` collector and analyzer
implementations and proposes concrete changes to make the interfaces cleaner and easier to implement new
ones.

---

## Problems

### P1 — `CollectorParams` is a "god bag" with scope-dependent nil fields

`CollectorParams` has 7 fields. The engine populates different subsets based on the collector's scope:

| Field          | ClusterWide | PerScyllaCluster | PerScyllaNode |
|----------------|:-----------:|:----------------:|:-------------:|
| `Vitals`       | always      | always           | always        |
| `ScyllaCluster`| **nil**     | set              | set           |
| `ScyllaNode`   | **nil**     | **nil**          | set           |
| `PodExecutor`  | set         | set              | set           |
| `PodLogFetcher`| maybe nil   | maybe nil        | maybe nil     |
| `ResourceLister`| set        | set              | set           |
| `ArtifactWriter`| maybe nil  | maybe nil        | maybe nil     |

This leads to:
- **12 redundant nil checks** (`if params.ScyllaNode == nil`) at the top of every `PerScyllaNode`
  collector's `Collect()` method.
- **Zero checks** for `params.ScyllaCluster` in `PerScyllaCluster` collectors — accessing a nil pointer
  on a misconfigured engine would silently panic instead of returning a clear error.
- **Zero checks** for `params.PodExecutor` in exec-based collectors — same panic risk.
- **Zero checks** for `params.ResourceLister` in manifest collectors — same panic risk.
- Every collector guards `params.ArtifactWriter != nil` before writing artifacts (27 guard sites).

The type system provides no help: a `PerScyllaNode` collector receives the exact same `CollectorParams`
type as a `ClusterWide` collector, even though it is contractually guaranteed to have `ScyllaNode` set.

### P2 — Massive metadata boilerplate per collector

Every collector requires five one-liner methods that are structurally identical across all 31
implementations:

```go
func (c *osInfoCollector) ID() engine.CollectorID          { return OSInfoCollectorID }
func (c *osInfoCollector) Name() string                    { return "OS information" }
func (c *osInfoCollector) Scope() engine.CollectorScope    { return engine.PerScyllaNode }
func (c *osInfoCollector) DependsOn() []engine.CollectorID { return nil }
func (c *osInfoCollector) RBAC() []rbacv1.PolicyRule       { return []rbacv1.PolicyRule{{...}} }
```

155 one-liner methods across 31 collectors, all copy-pasted with only the values changed.

### P3 — Typed accessor functions are copy-pasted 31 times

Every collector defines a `GetXxxResult()` function following the identical pattern:

```go
func GetOSInfoResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*OSInfoResult, error) {
    result, ok := vitals.Get(OSInfoCollectorID, podKey)
    if !ok {
        return nil, fmt.Errorf("OSInfoCollector result not found for %v", podKey)
    }
    if result.Status != engine.CollectorPassed {
        return nil, fmt.Errorf("OSInfoCollector did not pass for %v: %s", podKey, result.Message)
    }
    typed, ok := result.Data.(*OSInfoResult)
    if !ok {
        return nil, fmt.Errorf("unexpected data type %T for OSInfoCollector", result.Data)
    }
    return typed, nil
}
```

The only differences are the type name and the collector ID string. This is ~12 lines × 31 = 372 lines
of identical structure.

### P4 — Common execution patterns are re-implemented in each collector

There are four distinct collector archetypes sharing nearly identical `Collect()` logic:

| Archetype | Count | Repeated pattern |
|-----------|-------|-----------------|
| Shell exec via `PodExecutor` | 12 | `Execute(ctx, ns, name, "scylla", cmd)` → parse → write artifact |
| Manifest listing (operator namespaces) | 7 | loop `operatorNamespaces` → `ListXxx()` → marshal YAML → write artifact |
| Manifest listing (ScyllaCluster children) | 8 | build label selector → `ListXxx(ns, selector)` → marshal YAML → write artifact |
| Log collection | 3 | check fetcher → find pod → iterate containers → current+previous logs → write artifacts |

### P5 — Inconsistent artifact write error handling

- Manifest collectors (`PerScyllaCluster`): artifact write errors are **hard failures** — they return an
  error and abort the collector.
- Exec-based collectors (`PerScyllaNode`): artifact write errors are **silently swallowed** — the parsed
  `Data` is returned even if the artifact file could not be written.
- Log collectors: individual log write errors are also **silently swallowed**.

A new collector author has no clear guidance on which policy to follow.

### P6 — Analyzer scope is not encoded in the type

All 5 analyzers return `engine.AnalyzerPerScyllaCluster` from `Scope()`, meaning `params.ScyllaCluster`
is contractually non-nil — but the `AnalyzerParams` type does not enforce this. A future
`ClusterWideAnalyzer` would receive the same struct with `ScyllaCluster == nil`, relying on documentation
rather than the compiler.

---

## Proposed Changes

### Change 1 — Scope-specific collector interfaces with strongly-typed params

Replace the single `Collector` interface with three scope-specific interfaces. The `CollectorMeta`
interface provides common metadata and is used by the engine for profile resolution and topological
sorting. Each scope-specific interface carries its own `Collect()` method with a params type that only
contains fields guaranteed to be non-nil for that scope.

**New types in `engine/types.go`:**

```go
// CollectorMeta provides identity and dependency metadata shared by all collector types.
// The engine uses this interface for profile resolution and topological sorting.
type CollectorMeta interface {
    ID() CollectorID
    Name() string
    Scope() CollectorScope
    DependsOn() []CollectorID
}

// ClusterWideCollector runs once per diagnostic run.
type ClusterWideCollector interface {
    CollectorMeta
    Collect(ctx context.Context, params ClusterWideCollectorParams) (*CollectorResult, error)
}

// PerScyllaClusterCollector runs once per targeted ScyllaCluster/ScyllaDBDatacenter.
type PerScyllaClusterCollector interface {
    CollectorMeta
    Collect(ctx context.Context, params PerScyllaClusterCollectorParams) (*CollectorResult, error)
}

// PerScyllaNodeCollector runs once per Scylla pod.
type PerScyllaNodeCollector interface {
    CollectorMeta
    Collect(ctx context.Context, params PerScyllaNodeCollectorParams) (*CollectorResult, error)
}

// ClusterWideCollectorParams holds everything a ClusterWide collector needs.
// ResourceLister is always non-nil. PodLogFetcher is nil in offline mode.
// ArtifactWriter is nil when artifact writing is disabled.
type ClusterWideCollectorParams struct {
    Vitals         *Vitals
    ResourceLister ResourceLister
    PodLogFetcher  PodLogFetcher  // nil in offline mode
    ArtifactWriter ArtifactWriter // nil when disabled
}

// PerScyllaClusterCollectorParams holds everything a PerScyllaCluster collector needs.
// ScyllaCluster and ResourceLister are always non-nil.
type PerScyllaClusterCollectorParams struct {
    Vitals         *Vitals
    ScyllaCluster  *ScyllaClusterInfo // always non-nil
    ResourceLister ResourceLister     // always non-nil
    PodLogFetcher  PodLogFetcher      // nil in offline mode
    ArtifactWriter ArtifactWriter     // nil when disabled
}

// PerScyllaNodeCollectorParams holds everything a PerScyllaNode collector needs.
// ScyllaCluster, ScyllaNode, PodExecutor, and ResourceLister are always non-nil.
type PerScyllaNodeCollectorParams struct {
    Vitals         *Vitals
    ScyllaCluster  *ScyllaClusterInfo // always non-nil
    ScyllaNode     *ScyllaNodeInfo    // always non-nil
    PodExecutor    PodExecutor        // always non-nil
    PodLogFetcher  PodLogFetcher      // nil in offline mode
    ResourceLister ResourceLister     // always non-nil
    ArtifactWriter ArtifactWriter     // nil when disabled
}
```

The `ArtifactWriter` and `PodLogFetcher` remain nullable because they are genuinely optional depending on
the run mode — this is a deliberate design choice, not a gap. Their nullable nature is documented in the
struct field comments.

The engine continues to hold all collectors in a single `[]CollectorMeta` slice for profile resolution and
topological sorting. The execution dispatch uses a type-switch:

```go
switch c := collector.(type) {
case ClusterWideCollector:
    result, err = c.Collect(ctx, ClusterWideCollectorParams{...})
case PerScyllaClusterCollector:
    result, err = c.Collect(ctx, PerScyllaClusterCollectorParams{...})
case PerScyllaNodeCollector:
    result, err = c.Collect(ctx, PerScyllaNodeCollectorParams{...})
default:
    panic(fmt.Sprintf("unknown collector type %T", collector))
}
```

**Effect:** All 12 `if params.ScyllaNode == nil` guards are removed from `PerScyllaNode` collectors. The
hidden panic risk for `PodExecutor`, `ResourceLister`, and `ScyllaCluster` is eliminated at the type level.

### Change 2 — `CollectorBase` and `AnalyzerBase` for metadata boilerplate

Provide embeddable base structs that implement the metadata methods, so concrete collectors only implement
`Collect()`:

```go
// CollectorBase implements CollectorMeta and the optional RBACProvider interface.
// Embed it in concrete collector structs.
type CollectorBase struct {
    id        CollectorID
    name      string
    scope     CollectorScope
    deps      []CollectorID
    rbacRules []rbacv1.PolicyRule
}

func NewCollectorBase(id CollectorID, name string, scope CollectorScope,
    deps []CollectorID, rbacRules []rbacv1.PolicyRule) CollectorBase { ... }

func (b *CollectorBase) ID() CollectorID          { return b.id }
func (b *CollectorBase) Name() string              { return b.name }
func (b *CollectorBase) Scope() CollectorScope     { return b.scope }
func (b *CollectorBase) DependsOn() []CollectorID  { return b.deps }
func (b *CollectorBase) RBAC() []rbacv1.PolicyRule { return b.rbacRules }
```

**Effect:** 155 one-liner methods across 31 collectors are replaced by embedding + a single constructor
call. `RBAC()` is handled by `CollectorBase` as well (it implements `RBACProvider`), so the optional
interface check continues to work via normal embedding.

### Change 3 — Generic `GetResult[T]` typed accessor

Replace 31 copy-pasted `GetXxxResult()` functions with a single generic helper:

```go
// GetResult retrieves a typed collector result from the Vitals store.
// It returns an error if the result is not found, did not pass, or has an unexpected data type.
func GetResult[T any](vitals *Vitals, id CollectorID, scopeKey ScopeKey) (*T, error) {
    result, ok := vitals.Get(id, scopeKey)
    if !ok {
        return nil, fmt.Errorf("%s result not found for %v", id, scopeKey)
    }
    if result.Status != CollectorPassed {
        return nil, fmt.Errorf("%s did not pass for %v: %s", id, scopeKey, result.Message)
    }
    typed, ok := result.Data.(*T)
    if !ok {
        return nil, fmt.Errorf("unexpected data type %T for %s", result.Data, id)
    }
    return typed, nil
}
```

Usage:
```go
// Before (31 separate functions, 12 lines each):
result, err := collectors.GetOSInfoResult(params.Vitals, podKey)

// After:
result, err := engine.GetResult[collectors.OSInfoResult](params.Vitals, collectors.OSInfoCollectorID, podKey)
```

The per-collector `GetXxxResult()` functions are kept as thin one-line wrappers to preserve discoverability
and reduce call-site verbosity:

```go
func GetOSInfoResult(vitals *engine.Vitals, key engine.ScopeKey) (*OSInfoResult, error) {
    return engine.GetResult[OSInfoResult](vitals, OSInfoCollectorID, key)
}
```

**Effect:** ~340 lines of boilerplate replaced by one function. Each per-collector wrapper shrinks from
~12 lines to 1 line.

### Change 4 — Common helper functions for repeated execution patterns

Add `pkg/soda/collectors/helpers.go` with three helpers covering the four archetypes:

**4a — `ExecInScyllaPod`** (covers 12 exec-based `PerScyllaNode` collectors):

```go
// ExecInScyllaPod executes a command in the "scylla" container of the given node
// and optionally writes the stdout as an artifact. Artifact write errors are non-fatal.
func ExecInScyllaPod(
    ctx context.Context,
    executor engine.PodExecutor,
    node *engine.ScyllaNodeInfo,
    command []string,
    writer engine.ArtifactWriter,
    artifactName, artifactDesc string,
) (stdout string, artifacts []engine.Artifact, err error)
```

**4b — `collectAndWriteManifests`** (covers 15 manifest collectors):

```go
// collectAndWriteManifests lists resources using listFn, marshals each to YAML,
// and writes them as artifacts. Returns count and artifacts. Artifact write errors
// are non-fatal (the count is still accurate).
func collectAndWriteManifests[T any](
    ctx context.Context,
    writer engine.ArtifactWriter,
    listFn func() ([]T, error),
    namespaceFn func(*T) string,
    nameFn func(*T) string,
    resourceKind string,
) (int, []engine.Artifact, error)
```

**4c — `collectContainerLogs`** (covers 3 log collectors):

```go
// collectContainerLogs fetches current and previous logs for each container in pods
// and writes them as artifacts. Log fetch errors and artifact write errors are non-fatal.
func collectContainerLogs(
    ctx context.Context,
    fetcher engine.PodLogFetcher,
    writer engine.ArtifactWriter,
    namespace, podName string,
    initContainers, containers []string,
) ([]engine.Artifact, int)
```

**Effect on artifact write error policy:** All three helpers treat artifact write errors as non-fatal
(the parsed `Data` is still returned). This resolves P5 by standardizing on a single policy: artifact
writes are best-effort supplements to the primary collected data.

### Change 5 — Scope-specific analyzer interfaces

Mirror the collector split for analyzers:

```go
// AnalyzerMeta provides identity and dependency metadata shared by all analyzer types.
type AnalyzerMeta interface {
    ID() AnalyzerID
    Name() string
    Scope() AnalyzerScope
    DependsOn() []CollectorID
}

// ClusterWideAnalyzer runs once and receives full Vitals.
type ClusterWideAnalyzer interface {
    AnalyzerMeta
    Analyze(params ClusterWideAnalyzerParams) *AnalyzerResult
}

// PerScyllaClusterAnalyzer runs once per ScyllaCluster with scoped Vitals.
type PerScyllaClusterAnalyzer interface {
    AnalyzerMeta
    Analyze(params PerScyllaClusterAnalyzerParams) *AnalyzerResult
}

// ClusterWideAnalyzerParams holds everything a ClusterWide analyzer needs.
type ClusterWideAnalyzerParams struct {
    Vitals         *Vitals
    ArtifactReader ArtifactReader // nil in live mode
}

// PerScyllaClusterAnalyzerParams holds everything a PerScyllaCluster analyzer needs.
// ScyllaCluster is always non-nil.
type PerScyllaClusterAnalyzerParams struct {
    Vitals         *Vitals
    ScyllaCluster  *ScyllaClusterInfo // always non-nil
    ArtifactReader ArtifactReader     // nil in live mode
}
```

**Effect:** The 5 current analyzers (all `PerScyllaCluster`) implement `PerScyllaClusterAnalyzer` and
receive `PerScyllaClusterAnalyzerParams` where `ScyllaCluster` is guaranteed non-nil. Future
`ClusterWide` analyzers get a params struct without `ScyllaCluster`, avoiding nil-read bugs.

`AnalyzerBase` is added mirroring `CollectorBase`:

```go
type AnalyzerBase struct {
    id    AnalyzerID
    name  string
    scope AnalyzerScope
    deps  []CollectorID
}
```

### Change 6 — Registry and engine updates

- `AllCollectors()` returns `[]CollectorMeta` instead of `[]engine.Collector`.
- `AllAnalyzers()` returns `[]AnalyzerMeta` instead of `[]engine.Analyzer`.
- The engine's `executeCollectors` / `executeAnalyzers` methods use type-switches to dispatch.
- Profile resolution and topological sorting operate on `CollectorMeta` / `AnalyzerMeta`.
- `FakeCollector` in `testing/fakes.go` is replaced with three typed fakes:
  `FakeClusterWideCollector`, `FakePerScyllaClusterCollector`, `FakePerScyllaNodeCollector`.
- `FakeAnalyzer` is similarly split into `FakeClusterWideAnalyzer` / `FakePerScyllaClusterAnalyzer`.

---

## Before / After Examples

### PerScyllaNode collector (OSInfoCollector)

**Before** (~80 lines):
```go
type osInfoCollector struct{}
var _ engine.Collector = (*osInfoCollector)(nil)
func NewOSInfoCollector() engine.Collector { return &osInfoCollector{} }
func (c *osInfoCollector) ID() engine.CollectorID          { return OSInfoCollectorID }
func (c *osInfoCollector) Name() string                    { return "OS information" }
func (c *osInfoCollector) Scope() engine.CollectorScope    { return engine.PerScyllaNode }
func (c *osInfoCollector) DependsOn() []engine.CollectorID { return nil }
func (c *osInfoCollector) RBAC() []rbacv1.PolicyRule       { return []rbacv1.PolicyRule{{...}} }
func (c *osInfoCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
    if params.ScyllaNode == nil {      // removed
        return nil, fmt.Errorf("pod info not provided")
    }
    unameOut, _, err := params.PodExecutor.Execute(ctx,
        params.ScyllaNode.Namespace, params.ScyllaNode.Name, scyllaContainerName, []string{"uname", "--all"})
    if err != nil {
        return nil, fmt.Errorf("executing uname: %w", err)
    }
    osReleaseOut, _, err := params.PodExecutor.Execute(ctx,
        params.ScyllaNode.Namespace, params.ScyllaNode.Name, scyllaContainerName, []string{"cat", "/etc/os-release"})
    if err != nil {
        return nil, fmt.Errorf("reading /etc/os-release: %w", err)
    }
    result := parseOSInfo(strings.TrimSpace(unameOut), osReleaseOut)
    var artifacts []engine.Artifact
    if params.ArtifactWriter != nil {
        if relPath, err := params.ArtifactWriter.WriteArtifact("uname.log", []byte(unameOut)); err == nil {
            artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "..."})
        }
        if relPath, err := params.ArtifactWriter.WriteArtifact("os-release.log", []byte(osReleaseOut)); err == nil {
            artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "..."})
        }
    }
    ...
}
```

**After** (~35 lines):
```go
type osInfoCollector struct{ engine.CollectorBase }

var _ engine.PerScyllaNodeCollector = (*osInfoCollector)(nil)

func NewOSInfoCollector() engine.PerScyllaNodeCollector {
    return &osInfoCollector{engine.NewCollectorBase(
        OSInfoCollectorID, "OS information", engine.PerScyllaNode, nil,
        []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}}},
    )}
}

func (c *osInfoCollector) Collect(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
    // ScyllaNode is guaranteed non-nil — no nil check needed
    unameOut, unameArtifacts, err := ExecInScyllaPod(ctx, params.PodExecutor, params.ScyllaNode,
        []string{"uname", "--all"}, params.ArtifactWriter, "uname.log", "Raw uname --all output")
    if err != nil {
        return nil, fmt.Errorf("executing uname: %w", err)
    }
    osReleaseOut, osReleaseArtifacts, err := ExecInScyllaPod(ctx, params.PodExecutor, params.ScyllaNode,
        []string{"cat", "/etc/os-release"}, params.ArtifactWriter, "os-release.log", "Raw /etc/os-release content")
    if err != nil {
        return nil, fmt.Errorf("reading /etc/os-release: %w", err)
    }
    result := parseOSInfo(strings.TrimSpace(unameOut), osReleaseOut)
    return &engine.CollectorResult{
        Status:    engine.CollectorPassed,
        Data:      result,
        Message:   fmt.Sprintf("%s %s %s", result.OSName, result.OSVersion, result.Architecture),
        Artifacts: append(unameArtifacts, osReleaseArtifacts...),
    }, nil
}
```

### PerScyllaCluster manifest collector (ScyllaClusterStatefulSetCollector)

**Before** (~40 lines of Collect() logic):
```go
func (c *scyllaClusterStatefulSetCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
    sc := params.ScyllaCluster  // nil panic risk with old interface
    selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})
    statefulSets, err := params.ResourceLister.ListStatefulSets(ctx, sc.Namespace, selector)
    if err != nil {
        return nil, fmt.Errorf("listing statefulsets in namespace %s: %w", sc.Namespace, err)
    }
    var artifacts []engine.Artifact
    for i := range statefulSets {
        ss := &statefulSets[i]
        if params.ArtifactWriter != nil {
            data, err := yaml.Marshal(ss)
            if err != nil { return nil, fmt.Errorf("marshaling statefulset %s/%s: %w", ss.Namespace, ss.Name, err) }
            relPath, err := params.ArtifactWriter.WriteArtifact(ss.Name+".yaml", data)
            if err != nil { return nil, fmt.Errorf("writing artifact for statefulset %s/%s: %w", ss.Namespace, ss.Name, err) }
            artifacts = append(artifacts, engine.Artifact{...})
        }
    }
    return &engine.CollectorResult{Status: engine.CollectorPassed, Message: fmt.Sprintf("Collected %d StatefulSet(s)...", len(statefulSets)), Data: &ScyllaClusterStatefulSetResult{Count: len(statefulSets)}, Artifacts: artifacts}, nil
}
```

**After** (~10 lines of Collect() logic):
```go
func (c *scyllaClusterStatefulSetCollector) Collect(ctx context.Context, params engine.PerScyllaClusterCollectorParams) (*engine.CollectorResult, error) {
    sc := params.ScyllaCluster // guaranteed non-nil
    selector := labels.SelectorFromSet(labels.Set{naming.ClusterNameLabel: sc.Name})
    count, artifacts, err := collectAndWriteManifests(ctx, params.ArtifactWriter,
        func() ([]appsv1.StatefulSet, error) { return params.ResourceLister.ListStatefulSets(ctx, sc.Namespace, selector) },
        func(ss *appsv1.StatefulSet) string { return ss.Namespace },
        func(ss *appsv1.StatefulSet) string { return ss.Name },
        "StatefulSet",
    )
    if err != nil {
        return nil, fmt.Errorf("listing statefulsets in namespace %s: %w", sc.Namespace, err)
    }
    return &engine.CollectorResult{
        Status:    engine.CollectorPassed,
        Message:   fmt.Sprintf("Collected %d StatefulSet(s) for ScyllaCluster %s/%s", count, sc.Namespace, sc.Name),
        Data:      &ScyllaClusterStatefulSetResult{Count: count},
        Artifacts: artifacts,
    }, nil
}
```

### Typed accessor (GetOSInfoResult)

**Before** (~12 lines):
```go
func GetOSInfoResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*OSInfoResult, error) {
    result, ok := vitals.Get(OSInfoCollectorID, podKey)
    if !ok {
        return nil, fmt.Errorf("OSInfoCollector result not found for %v", podKey)
    }
    if result.Status != engine.CollectorPassed {
        return nil, fmt.Errorf("OSInfoCollector did not pass for %v: %s", podKey, result.Message)
    }
    typed, ok := result.Data.(*OSInfoResult)
    if !ok {
        return nil, fmt.Errorf("unexpected data type %T for OSInfoCollector", result.Data)
    }
    return typed, nil
}
```

**After** (1 line):
```go
func GetOSInfoResult(vitals *engine.Vitals, key engine.ScopeKey) (*OSInfoResult, error) {
    return engine.GetResult[OSInfoResult](vitals, OSInfoCollectorID, key)
}
```

---

## Summary of Impact

| Metric | Before | After | Reduction |
|--------|--------|-------|-----------|
| Nil checks for `ScyllaNode` | 12 | 0 | 100% |
| Hidden nil-panic sites (`ScyllaCluster`, `PodExecutor`, `ResourceLister`) | 30+ | 0 | 100% |
| One-liner metadata methods | 155 | 0 | 100% |
| Lines in `GetXxxResult()` accessor functions | ~372 | ~31 | 92% |
| Lines in a typical `PerScyllaNode` collector `Collect()` | ~40 | ~15 | 63% |
| Lines in a typical manifest `Collect()` | ~35 | ~10 | 71% |
| Artifact write error handling policy | inconsistent | uniform (non-fatal) | — |

---

## Implementation Steps

1. **Engine types** (`engine/types.go`): Add `CollectorMeta`, `ClusterWideCollector`,
   `PerScyllaClusterCollector`, `PerScyllaNodeCollector`, the three `*CollectorParams` structs,
   `AnalyzerMeta`, `ClusterWideAnalyzer`, `PerScyllaClusterAnalyzer`, the two `*AnalyzerParams` structs,
   and the generic `GetResult[T]` function.

2. **Base structs** (`engine/types.go`): Add `CollectorBase`, `NewCollectorBase`, `AnalyzerBase`,
   `NewAnalyzerBase`.

3. **Helpers** (`collectors/helpers.go`): Add `ExecInScyllaPod`, `collectAndWriteManifests`,
   `collectContainerLogs`.

4. **Engine dispatch** (`engine/engine.go`): Replace `CollectorParams` construction with
   scope-specific params structs. Replace calls to `c.Collect(ctx, params)` with type-switch dispatch.
   Same for analyzers.

5. **Migrate collectors**: Update all 31 collectors to implement the appropriate scope-specific
   interface, embed `CollectorBase`, and use the helper functions.

6. **Migrate analyzers**: Update all 5 analyzers to implement `PerScyllaClusterAnalyzer`, embed
   `AnalyzerBase`, and shrink `GetXxxResult()` wrappers to one line.

7. **Registry/resolve** (`collectors/registry.go`, `engine/resolve.go`): Change `AllCollectors()` to
   return `[]CollectorMeta`. Update `AllAnalyzers()` similarly. Update profile resolution and topo-sort
   to use the new types.

8. **Fakes and tests** (`testing/fakes.go`, `*_test.go`): Replace `FakeCollector` with three typed
   fakes. Replace `FakeAnalyzer` with two typed fakes. Update all test params construction.
