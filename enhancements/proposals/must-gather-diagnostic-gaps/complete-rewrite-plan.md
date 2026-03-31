# Complete Rewrite PoC — Implementation Plan

This document is the implementation plan for the `scylla-operator diagnose`
command, a complete rewrite of ScyllaDB Kubernetes diagnostics that merges the
best of must-gather and Scylla Doctor into a single tool. See
[complete-rewrite-requirements.md](complete-rewrite-requirements.md) for the
full requirements.

## Design decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Registration | Explicit registry file | Simple, no Go init() magic. Adding a collector/analyzer = adding one line. |
| Cluster targeting | `--cluster-name` + `--namespace` flags; auto-discover all if omitted | Most intuitive for users. |
| RBAC metadata | Post-PoC | Each collector will declare RBAC needs later; for now just document. |
| Collector scoping | Scope enum: `ClusterWide`, `PerScyllaCluster`, `PerPod` | Engine iterates the right targets per scope. Collectors don't manage iteration. |
| Vitals format | New K8s-native format (per-pod, per-cluster, cluster-wide sections) | Scylla Doctor compat not needed; our format better fits the multi-scope model. |
| Command name | `scylla-operator diagnose` | Clear verb, sits alongside `must-gather`. |
| Profiles | Analyzer-only definitions; engine auto-resolves required collectors via dependency graph | Users think in "what checks do I want", not "what data do I need". |
| Profile composition | Composable — a profile can include other profiles | Avoids duplication across profiles. |
| Profile overrides | `--enable` / `--disable` flags on top of a profile | Fine-grained control. |
| Concurrency | Sequential execution (topological sort) | Simple, debuggable, sufficient for PoC. Parallelism can be added later. |
| Testability | Interface injection: `PodExecutor`, typed K8s clients behind interfaces | Enables unit testing without K8s cluster or client-go fakes. |
| Test coverage | Full: engine + parsers + all 5 collectors + all 3 analyzers + output | Every step ships with tests. |
| K8s clients | Typed Scylla client + core k8s client (not dynamic/unstructured) | Follows existing codebase patterns, gives type safety. |
| CRD support | Both ScyllaCluster (v1) and ScyllaDBDatacenter (v1alpha1) | Covers all users from the start. |
| Terminal output | `fatih/color` (already in dependency graph as indirect) | Standard Go coloring library, no new deps. |
| Scope keys | Typed `ScopeKey` struct (Namespace + Name fields) | Type safety for map keys, avoids string formatting bugs. |
| Collector results | Typed result struct per collector, `any` in store, typed accessor functions | Compile-time type safety for analyzers; engine stays generic; IDE autocomplete works. |
| Artifact support | Collectors produce structured data AND raw artifact files via `ArtifactWriter` | Full traceability: raw data preserved alongside parsed result. Enables offline re-analysis. |
| Offline analysis | `--from-archive` loads vitals + artifacts, runs analyzers only | Re-analyze collected data without cluster access. |
| Archive mode | `--archive` produces `.tar.gz` instead of directory; console summary always printed | Clean single-file output for sharing. Mutually exclusive with `--from-archive`. |

## PoC scope

### Collectors (5)

| ID | Scope | Collection method | Data shape |
|----|-------|-------------------|------------|
| `NodeResourcesCollector` | `ClusterWide` | K8s API: list Nodes | `{nodes: [{name, capacity, allocatable, labels, conditions}]}` |
| `ScyllaClusterStatusCollector` | `PerScyllaCluster` | Typed Scylla client: get ScyllaCluster/ScyllaDBDatacenter status | `{name, namespace, kind, generation, observedGeneration, members, readyMembers, availableMembers, conditions, racks}` |
| `OSInfoCollector` | `PerPod` | Exec: `uname --all`, `cat /etc/os-release` | `{architecture, kernel_version, os_name, os_version}` |
| `ScyllaVersionCollector` | `PerPod` | Exec: `scylla --version` | `{version, edition}` |
| `SchemaVersionsCollector` | `PerPod` | Exec: `curl -s localhost:10000/storage_proxy/schema_versions` | `[{key, value}]` (JSON array from Scylla REST API) |

### Analyzers (3)

| ID | Depends on | Logic |
|----|------------|-------|
| `ScyllaVersionSupportAnalyzer` | `ScyllaVersionCollector` | Checks version against known-supported ranges. PASSED/WARNING/FAILED. |
| `SchemaAgreementAnalyzer` | `SchemaVersionsCollector` | Checks all pods report exactly one schema version. PASSED/FAILED. |
| `OSSupportAnalyzer` | `OSInfoCollector` | Checks OS is in supported list (RHEL, Ubuntu, etc.). PASSED/WARNING. |

### Profiles (1 for PoC, machinery for more)

```
"full" — includes all 3 analyzers
```

Post-PoC additions: `"quick"`, `"schema"`, `"performance"`, etc.

### Deferred to post-PoC

- Multiple profiles beyond "full"
- RBAC introspection and pre-run validation
- LLM prompt generation
- Custom script collectors
- Rich artifact bundle index file (HTML/Markdown navigable index)
- Parallel collector execution
- `--dry-run` output showing RBAC requirements

## Package layout

All diagnostic logic lives in `pkg/soda/` — self-contained, with no imports
from `pkg/cmd/operator/` or `pkg/gather/`. The only integration point is one
Cobra command file and one line in `cmd.go`.

```
pkg/soda/                                 # Self-contained diagnostic library
├── engine/
│   ├── types.go                          # Core types and interfaces
│   ├── types_test.go                     # Status enum tests
│   ├── resolve.go                        # Profile resolution: analyzer → collector transitive closure
│   ├── resolve_test.go                   # Profile composition, enable/disable, cycle detection
│   ├── engine.go                         # Orchestrator: topo sort, scoped execution, cascade
│   └── engine_test.go                    # Orchestration tests with fake collectors/analyzers
├── collectors/
│   ├── registry.go                       # AllCollectors() — explicit list
│   ├── node_resources.go                 # ClusterWide: K8s Nodes
│   ├── node_resources_test.go
│   ├── scyllacluster_status.go           # PerScyllaCluster: CRD status
│   ├── scyllacluster_status_test.go
│   ├── os_info.go                        # PerPod: uname + os-release
│   ├── os_info_test.go
│   ├── scylla_version.go                 # PerPod: scylla --version
│   ├── scylla_version_test.go
│   ├── schema_versions.go               # PerPod: REST API schema versions
│   └── schema_versions_test.go
├── analyzers/
│   ├── registry.go                       # AllAnalyzers() — explicit list
│   ├── scylla_version_support.go         # Version support check
│   ├── scylla_version_support_test.go
│   ├── schema_agreement.go              # Schema agreement check
│   ├── schema_agreement_test.go
│   ├── os_support.go                    # OS support check
│   └── os_support_test.go
├── profiles/
│   └── profiles.go                       # Built-in profile definitions
├── output/
│   ├── console.go                        # Human-readable colored table
│   ├── console_test.go
│   ├── json.go                           # JSON vitals output
│   └── json_test.go
└── testing/
    └── fakes.go                          # Shared test infrastructure

pkg/cmd/operator/
├── diagnose.go                           # Cobra command: flags, Validate, Complete, Run
└── cmd.go                                # +1 line: cmd.AddCommand(NewDiagnoseCmd(streams))
```

## Core types (`pkg/soda/engine/types.go`)

### Collector scope

```go
type CollectorScope int

const (
    ClusterWide      CollectorScope = iota  // Runs once per diagnostic run
    PerScyllaCluster                        // Runs once per targeted ScyllaCluster/ScyllaDBDatacenter
    PerPod                                  // Runs once per Scylla pod
)
```

### Status types

```go
type CollectorStatus int

const (
    CollectorPassed  CollectorStatus = iota
    CollectorFailed
    CollectorSkipped
)

type AnalyzerStatus int

const (
    AnalyzerPassed  AnalyzerStatus = iota
    AnalyzerSkipped
    AnalyzerWarning
    AnalyzerFailed
)
```

### Result types

```go
// Artifact represents a raw file produced by a collector.
type Artifact struct {
    RelativePath string `json:"relative_path"` // Path relative to collector's artifact directory
    Description  string `json:"description"`   // Human-readable description
}

type CollectorResult struct {
    Status    CollectorStatus `json:"status"`
    Data      any             `json:"-"`         // Concrete typed struct (e.g., *OSInfoResult); not serialized directly
    Message   string          `json:"message"`
    Artifacts []Artifact      `json:"artifacts"` // Raw files written by this collector
}

type AnalyzerResult struct {
    Status  AnalyzerStatus `json:"status"`
    Message string         `json:"message"`
}
```

The `Data` field holds a concrete typed struct specific to each collector (e.g.,
`*OSInfoResult`, `*ScyllaVersionResult`). Analyzers access it through typed
accessor functions co-located with each collector — see "Typed accessor
functions" below. The engine treats `Data` as opaque (`any`).

### IDs

```go
type CollectorID string
type AnalyzerID string
```

### Scope keys

Typed struct keys used as map keys in the `Vitals` store, replacing raw
`"namespace/name"` strings for type safety.

```go
// ScopeKey identifies a namespaced resource (cluster or pod).
type ScopeKey struct {
    Namespace string `json:"namespace"`
    Name      string `json:"name"`
}

func (k ScopeKey) String() string { return k.Namespace + "/" + k.Name }
```

### Collector interface

```go
type Collector interface {
    ID() CollectorID
    Name() string                    // Human-readable description
    Scope() CollectorScope
    DependsOn() []CollectorID        // Other collectors this one needs (can be empty)
    Collect(ctx context.Context, params CollectorParams) (*CollectorResult, error)
}
```

### Analyzer interface

```go
type Analyzer interface {
    ID() AnalyzerID
    Name() string                    // Human-readable description
    DependsOn() []CollectorID        // Collector IDs whose results this analyzer reads
    Analyze(params AnalyzerParams) *AnalyzerResult
}
```

### AnalyzerParams (passed to each Analyze call)

```go
type AnalyzerParams struct {
    Vitals         *Vitals         // Full vitals store with all collector results
    ArtifactReader ArtifactReader  // Read raw artifact files from collectors
}
```

### Dependency injection interfaces

These are the seams that enable unit testing without a real K8s cluster:

```go
// PodExecutor runs commands inside pod containers.
type PodExecutor interface {
    Execute(ctx context.Context, namespace, podName, containerName string, command []string) (stdout, stderr string, err error)
}

// ScyllaClusterLister discovers ScyllaCluster and ScyllaDBDatacenter objects.
type ScyllaClusterLister interface {
    ListScyllaClusters(ctx context.Context, namespace string) ([]ClusterInfo, error)
}

// NodeLister lists K8s Node objects.
type NodeLister interface {
    ListNodes(ctx context.Context) ([]corev1.Node, error)
}

// PodLister lists pods matching a selector in a namespace.
type PodLister interface {
    ListPods(ctx context.Context, namespace string, selector labels.Selector) ([]corev1.Pod, error)
}
```

### Artifact interfaces

Collectors write raw files through `ArtifactWriter`; analyzers (and offline
mode) read them back through `ArtifactReader`. The engine assigns the base
directory for each collector invocation — collectors just name files within it.

```go
// ArtifactWriter is passed to collectors via CollectorParams.
// The engine creates one per (collector, scope, scope-key) invocation
// pointing at the correct subdirectory.
type ArtifactWriter interface {
    WriteArtifact(filename string, content []byte) (relativePath string, err error)
}

// ArtifactReader is passed to analyzers via AnalyzerParams.
// Backed by the output directory (live mode) or an extracted archive (offline mode).
type ArtifactReader interface {
    ReadArtifact(collectorID CollectorID, scopeKey ScopeKey, filename string) ([]byte, error)
    ListArtifacts(collectorID CollectorID, scopeKey ScopeKey) ([]Artifact, error)
}
```

### Cluster and pod info

```go
// ClusterInfo represents a discovered ScyllaCluster or ScyllaDBDatacenter.
type ClusterInfo struct {
    Name       string
    Namespace  string
    Kind       string              // "ScyllaCluster" or "ScyllaDBDatacenter"
    APIVersion string              // "scylla.scylladb.com/v1" or "scylla.scylladb.com/v1alpha1"
    Object     any                 // *scyllav1.ScyllaCluster or *scyllav1alpha1.ScyllaDBDatacenter
}

// PodInfo represents a discovered Scylla pod.
type PodInfo struct {
    Name          string
    Namespace     string
    ClusterName   string           // from label scylla/cluster
    DatacenterName string          // from label scylla/datacenter
    RackName      string           // from label scylla/rack
}
```

### CollectorParams (passed to each Collect call)

```go
type CollectorParams struct {
    // Always available:
    Vitals *Vitals                  // Results from upstream collectors (filtered to DependsOn)

    // Available based on scope:
    Cluster *ClusterInfo            // Non-nil for PerScyllaCluster and PerPod
    Pod     *PodInfo                // Non-nil for PerPod

    // Dependency-injected capabilities:
    PodExecutor          PodExecutor
    ScyllaClusterLister  ScyllaClusterLister
    NodeLister           NodeLister
    PodLister            PodLister
    ArtifactWriter       ArtifactWriter      // Write raw artifact files
}
```

### Vitals store

```go
// Vitals is the central data store. It holds collector results keyed by scope.
type Vitals struct {
    ClusterWide  map[CollectorID]*CollectorResult                `json:"cluster_wide"`
    PerCluster   map[ScopeKey]map[CollectorID]*CollectorResult   `json:"per_cluster"`
    PerPod       map[ScopeKey]map[CollectorID]*CollectorResult   `json:"per_pod"`
}

// Get retrieves a collector result, searching across all scopes.
// For PerCluster/PerPod results, it searches in the context-appropriate map
// based on the current execution context (cluster key or pod key).
func (v *Vitals) Get(id CollectorID, scopeKey ScopeKey) (*CollectorResult, bool)

// PodKeys returns all pod-scope keys in the store.
func (v *Vitals) PodKeys() []ScopeKey

// ClusterKeys returns all cluster-scope keys in the store.
func (v *Vitals) ClusterKeys() []ScopeKey
```

### Profile

```go
type Profile struct {
    Name        string
    Description string
    Includes    []string         // Names of other profiles to compose
    Analyzers   []AnalyzerID     // Analyzer IDs this profile enables
}
```

### Typed collector result structs

Each collector defines a concrete result struct with plain Go types and explicit
JSON tags. These structs are stored in `CollectorResult.Data` as `any` — the
engine treats them as opaque, but analyzers access them through typed accessor
functions (see below).

#### NodeResourcesResult

```go
// In collectors/node_resources.go
type NodeResourcesResult struct {
    Nodes []NodeInfo `json:"nodes"`
}

type NodeInfo struct {
    Name        string                `json:"name"`
    Capacity    map[string]string     `json:"capacity"`     // e.g. {"cpu": "4", "memory": "16Gi"}
    Allocatable map[string]string     `json:"allocatable"`
    Labels      map[string]string     `json:"labels"`
    Conditions  []NodeConditionInfo   `json:"conditions"`
}

type NodeConditionInfo struct {
    Type    string `json:"type"`    // e.g. "Ready", "MemoryPressure"
    Status  string `json:"status"`  // "True", "False", "Unknown"
    Message string `json:"message"`
}
```

**Artifact files:** `nodes.yaml` — raw YAML dump of all Node objects.

#### ScyllaClusterStatusResult

```go
// In collectors/scyllacluster_status.go
type ScyllaClusterStatusResult struct {
    Name               string                   `json:"name"`
    Namespace          string                   `json:"namespace"`
    Kind               string                   `json:"kind"`      // "ScyllaCluster" or "ScyllaDBDatacenter"
    Generation         int64                    `json:"generation"`
    ObservedGeneration int64                    `json:"observed_generation"`
    Members            int32                    `json:"members"`
    ReadyMembers       int32                    `json:"ready_members"`
    AvailableMembers   int32                    `json:"available_members"`
    Conditions         []ClusterConditionInfo   `json:"conditions"`
    Racks              []RackStatusInfo         `json:"racks"`
}

type ClusterConditionInfo struct {
    Type    string `json:"type"`
    Status  string `json:"status"`
    Message string `json:"message"`
}

type RackStatusInfo struct {
    Name         string `json:"name"`
    Members      int32  `json:"members"`
    ReadyMembers int32  `json:"ready_members"`
}
```

**Artifact files:** `manifest.yaml` — full ScyllaCluster or ScyllaDBDatacenter
YAML manifest.

#### OSInfoResult

```go
// In collectors/os_info.go
type OSInfoResult struct {
    Architecture  string            `json:"architecture"`    // e.g. "x86_64"
    KernelVersion string            `json:"kernel_version"`  // e.g. "5.15.0-1041-gke"
    OSName        string            `json:"os_name"`         // e.g. "Red Hat Enterprise Linux"
    OSVersion     string            `json:"os_version"`      // e.g. "9.7"
    OSReleaseFull map[string]string `json:"os_release_full"` // Full parsed /etc/os-release key-value pairs
}
```

**Artifact files:** `uname.log` — raw `uname --all` output; `os-release.log` —
raw `/etc/os-release` content.

#### ScyllaVersionResult

```go
// In collectors/scylla_version.go
type ScyllaVersionResult struct {
    Version string `json:"version"` // e.g. "2026.1.0" or "6.2.2"
    Build   string `json:"build"`   // Build identifier if present
    Raw     string `json:"raw"`     // Full raw output from scylla --version
}
```

**Artifact files:** `scylla-version.log` — raw `scylla --version` output.

#### SchemaVersionsResult

```go
// In collectors/schema_versions.go
type SchemaVersionsResult struct {
    Versions []SchemaVersionEntry `json:"versions"`
}

type SchemaVersionEntry struct {
    SchemaVersion string   `json:"schema_version"` // UUID
    Hosts         []string `json:"hosts"`          // IP addresses reporting this version
}
```

**Artifact files:** `schema-versions.json` — raw JSON response from the Scylla
REST API.

### Typed accessor functions

Each collector exposes typed accessor functions co-located with its result
struct. These are the **only** place where the `any → concrete type` assertion
happens, keeping that knowledge in the collector package and out of analyzers.

```go
// In collectors/os_info.go — typed result accessor:
func GetOSInfoResult(vitals *Vitals, podKey ScopeKey) (*OSInfoResult, error) {
    result, ok := vitals.Get(OSInfoCollectorID, podKey)
    if !ok {
        return nil, fmt.Errorf("OSInfoCollector result not found for %v", podKey)
    }
    if result.Status != CollectorPassed {
        return nil, fmt.Errorf("OSInfoCollector did not pass for %v: %s", podKey, result.Message)
    }
    typed, ok := result.Data.(*OSInfoResult)
    if !ok {
        return nil, fmt.Errorf("unexpected data type %T for OSInfoCollector", result.Data)
    }
    return typed, nil
}

// In collectors/os_info.go — artifact read helpers:
func ReadUnameOutput(reader ArtifactReader, podKey ScopeKey) ([]byte, error) {
    return reader.ReadArtifact(OSInfoCollectorID, podKey, "uname.log")
}

func ReadOSReleaseOutput(reader ArtifactReader, podKey ScopeKey) ([]byte, error) {
    return reader.ReadArtifact(OSInfoCollectorID, podKey, "os-release.log")
}
```

Each collector follows this pattern — one `Get<Name>Result()` function for the
typed struct, plus one `Read<File>()` helper per artifact file. Filename
knowledge stays in the collector that produces the file, never duplicated in
analyzers.

## Artifact system

### Directory layout

All artifact files are organized under a well-known directory structure. The
engine assigns the base directory for each collector invocation; collectors just
name files within it.

```
<output-dir>/
  vitals.json                                              # Serialized Vitals store
  collectors/
    cluster-wide/
      NodeResourcesCollector/
        nodes.yaml
    per-cluster/
      <namespace>/<cluster-name>/
        ScyllaClusterStatusCollector/
          manifest.yaml
    per-pod/
      <namespace>/<pod-name>/
        OSInfoCollector/
          uname.log
          os-release.log
        ScyllaVersionCollector/
          scylla-version.log
        SchemaVersionsCollector/
          schema-versions.json
```

### Engine artifact directory assignment

When the engine invokes a collector, it creates an `ArtifactWriter` rooted at
the correct subdirectory for that invocation:

- `ClusterWide` collectors → `collectors/cluster-wide/<CollectorID>/`
- `PerScyllaCluster` collectors → `collectors/per-cluster/<namespace>/<name>/<CollectorID>/`
- `PerPod` collectors → `collectors/per-pod/<namespace>/<name>/<CollectorID>/`

The collector calls `params.ArtifactWriter.WriteArtifact("filename.ext",
content)` and receives back the relative path that was written. The engine
records the returned `Artifact` entries in `CollectorResult.Artifacts`.

### Artifact files per collector

| Collector | Artifacts | Description |
|-----------|-----------|-------------|
| `NodeResourcesCollector` | `nodes.yaml` | YAML dump of all Node objects |
| `ScyllaClusterStatusCollector` | `manifest.yaml` | Full ScyllaCluster/ScyllaDBDatacenter YAML manifest |
| `OSInfoCollector` | `uname.log`, `os-release.log` | Raw command output |
| `ScyllaVersionCollector` | `scylla-version.log` | Raw `scylla --version` output |
| `SchemaVersionsCollector` | `schema-versions.json` | Raw REST API JSON response |

## Offline analysis mode

### `--from-archive` flag

The `--from-archive=<path>` flag enables offline analysis — running analyzers
against previously collected data without cluster access.

**Behavior:**

1. If `<path>` is a `.tar.gz` file, extract to a temporary directory.
2. Load `vitals.json` from the directory to reconstruct the `Vitals` store.
3. Create a filesystem-backed `ArtifactReader` rooted at the directory.
4. Skip all collectors — use the loaded vitals as-is.
5. Run analyzers against the loaded data.
6. Print console summary to stdout (same format as live mode).
7. Clean up the temporary directory (if `.tar.gz` was extracted).
8. No output files are generated in offline mode.

**Mutual exclusivity:**

- `--from-archive` is mutually exclusive with `--archive` (validation error).
- `--from-archive` is mutually exclusive with cluster-targeting flags
  (`--kubeconfig`, `--cluster-name`, `--namespace`) since collectors are skipped
  and no cluster connection is needed (validation error).

### `vitals.json` requirements

The `vitals.json` file must contain enough metadata to reconstruct the full
`Vitals` store, including:

- All `CollectorResult` entries with their scope keys
- The typed `Data` field serialized as JSON (using the result struct's JSON tags)
- The `Artifacts` list for each collector result (so `ArtifactReader` knows what
  files are available)

For deserialization, the JSON decoder needs to know which concrete type to
unmarshal `Data` into for each collector. This is handled by including the
collector ID alongside the result — the deserializer uses a registry mapping
`CollectorID → concrete result type`.

## Archive output

### `--archive` flag

When `--archive` is set:

1. Run collectors and analyzers normally.
2. Print console summary to stdout (always printed, same as without `--archive`).
3. Write output to a **temporary directory** with the standard layout.
4. Create a `.tar.gz` archive from the temporary directory.
5. Clean up the temporary directory — **only the tarball remains**.
6. Print the archive file path to stdout.

Without `--archive`, output is written to the directory specified by
`--output-dir` (or an auto-generated directory) and left as-is.

The console summary is **always printed** regardless of `--archive`. The flag
only controls whether file output is a directory or a `.tar.gz`.

## Engine orchestration (`pkg/soda/engine/engine.go`)

### Execution flow

```
1. Resolve profile
   → Flatten includes (detect cycles)
   → Apply --enable / --disable overrides
   → Final set of AnalyzerIDs

2. Resolve collectors (transitive closure)
   → For each enabled analyzer, walk DependsOn
   → For each required collector, walk its DependsOn (collector-to-collector deps)
   → Final set of CollectorIDs

3. Topological sort collectors
   → Order by dependency: if A depends on B, B runs first
   → Detect cycles (error)

4. Pre-run summary (always printed to console)
   → List enabled analyzers with descriptions
   → List auto-resolved collectors with scope and description
   → List target clusters and pods
   → If --dry-run, stop here

5. Execute collectors (sequential, in topo order)
   → Group by scope:
     a. ClusterWide collectors: run once, store in Vitals.ClusterWide
     b. PerScyllaCluster collectors: for each target cluster, run collector, store in Vitals.PerCluster[key]
     c. PerPod collectors: for each target pod, run collector, store in Vitals.PerPod[key]
   → On error:
     - Store CollectorResult with Status=Failed, Message=error text
     - Continue (keep-going semantics)
   → Cascade:
     - If a collector's dependency has Failed/Skipped status, this collector gets Skipped

6. Execute analyzers (sequential)
   → For each enabled analyzer:
     a. Check all DependsOn collectors have at least one Passed result
     b. If any dependency is missing/Failed → AnalyzerFailed
     c. If any dependency is Skipped → AnalyzerSkipped
     d. Otherwise, call Analyze(params) with AnalyzerParams containing the full
        vitals store and an ArtifactReader
   → For cross-scope analysis (e.g., SchemaAgreementAnalyzer reads PerPod data
     from ALL pods), the analyzer receives the complete Vitals store and iterates
     the PerPod map itself.

7. Output results
   → Console: always print colored table summary to stdout
   → Write vitals.json + artifact files to output directory
   → If --archive: tar.gz the output directory, clean up temp dir
   → If --from-archive: skip file output entirely
```

### Dependency cascade rules

Same semantics as Scylla Doctor:

- If a collector's dependency was **Skipped** → this collector is **Skipped** (with message: "Required {dep} was skipped")
- If a collector's dependency was **Failed** (and none Skipped) → this collector is **Failed** (with message: "Required {dep} failed")
- For analyzers: same logic, checking against collector results

### Cross-scope dependencies

Analyzers can depend on collectors of any scope. The engine passes the full
`Vitals` store, and the analyzer navigates to the appropriate scope map.

Collectors can depend on other collectors of the same or broader scope:
- A `PerPod` collector can depend on a `ClusterWide` or `PerScyllaCluster` collector
- A `PerScyllaCluster` collector can depend on a `ClusterWide` collector
- A `ClusterWide` collector cannot depend on a narrower-scoped collector

The engine validates these constraints during the resolve phase.

## Profile resolution (`pkg/soda/engine/resolve.go`)

```go
func ResolveProfile(
    profileName string,
    allProfiles map[string]Profile,
    enable []AnalyzerID,
    disable []AnalyzerID,
    allAnalyzers map[AnalyzerID]Analyzer,
    allCollectors map[CollectorID]Collector,
) (resolvedCollectors []CollectorID, resolvedAnalyzers []AnalyzerID, err error)
```

Steps:
1. Flatten the profile's `Includes` recursively (detect cycles via visited set).
2. Merge all `Analyzers` from the flattened profile set.
3. Add `enable` items, remove `disable` items.
4. Validate all analyzer IDs exist in the registry.
5. For each analyzer, walk `DependsOn()` → collector IDs.
6. For each collector, walk its `DependsOn()` → more collector IDs (transitive closure).
7. Validate all collector IDs exist in the registry.
8. Return deduplicated, sorted lists.

## CLI integration (`pkg/cmd/operator/diagnose.go`)

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--kubeconfig` | string | (from env) | Path to kubeconfig file (inherited from ConfigFlags) |
| `--namespace` | string | (all) | Namespace to search for ScyllaDB clusters |
| `--cluster-name` | string | (all) | Name of a specific ScyllaCluster/ScyllaDBDatacenter to diagnose |
| `--profile` | string | `"full"` | Diagnostic profile to use |
| `--enable` | []string | `[]` | Additional analyzer IDs to enable on top of the profile |
| `--disable` | []string | `[]` | Analyzer IDs to disable from the profile |
| `--output-dir` | string | (auto) | Directory to write JSON results; auto-generated if empty |
| `--keep-going` | bool | `true` | Continue on collector errors |
| `--archive` | bool | `false` | Produce `.tar.gz` instead of output directory. Console summary still printed. Mutually exclusive with `--from-archive`. |
| `--from-archive` | string | `""` | Path to directory or `.tar.gz` for offline analysis. Skips collectors, runs analyzers only. Mutually exclusive with `--archive`, `--kubeconfig`, `--cluster-name`, `--namespace`. |

### Run() flow

**Live mode** (no `--from-archive`):

1. Build typed Scylla client + core K8s client + REST config from ConfigFlags.
2. Create production implementations of `PodExecutor`, `ScyllaClusterLister`, `NodeLister`, `PodLister`.
3. Discover target clusters (filtered by `--namespace` and `--cluster-name`).
4. Discover pods for each target cluster (label selector, filter for running Scylla container).
5. Create output directory (temp if `--archive`, otherwise `--output-dir` or auto-generated).
6. Build `Engine` with registry + targets + filesystem-backed `ArtifactWriter`.
7. Call `engine.Run(ctx)` — collects, analyzes, writes vitals.json + artifact files.
8. Print console summary to stdout (always).
9. If `--archive`: create `.tar.gz` from output directory, clean up temp directory, print archive path.

**Offline mode** (`--from-archive`):

1. If path is `.tar.gz`, extract to temp directory.
2. Load `vitals.json` from the directory, reconstruct `Vitals` store.
3. Create filesystem-backed `ArtifactReader` rooted at the directory.
4. Run analyzers only against loaded vitals and artifacts.
5. Print console summary to stdout.
6. Clean up temp directory (if `.tar.gz` was extracted).

## Output format

### Console output

```
ScyllaDB Diagnostics (profile: full)

Target clusters:
  scylla/my-cluster (ScyllaCluster, 3 pods)

Collectors:
  [PASSED]  NodeResourcesCollector           Collected 3 nodes
  [PASSED]  ScyllaClusterStatusCollector      scylla/my-cluster: 3/3 members ready
  [PASSED]  OSInfoCollector                   scylla/my-cluster-us-east1-0: RHEL 9.7 x86_64
  [PASSED]  OSInfoCollector                   scylla/my-cluster-us-east1-1: RHEL 9.7 x86_64
  [PASSED]  OSInfoCollector                   scylla/my-cluster-us-east1-2: RHEL 9.7 x86_64
  [PASSED]  ScyllaVersionCollector            scylla/my-cluster-us-east1-0: 2026.1.0
  ...
  [PASSED]  SchemaVersionsCollector           scylla/my-cluster-us-east1-0: 1 schema version

Analysis:
  [PASSED]  ScyllaVersionSupportAnalyzer      ScyllaDB 2026.1.0 is supported
  [PASSED]  SchemaAgreementAnalyzer           All 3 pods report the same schema version
  [PASSED]  OSSupportAnalyzer                 RHEL 9 is a supported OS

Summary: 3 passed, 0 warnings, 0 failed, 0 skipped
```

Status colors: PASSED=green, WARNING=yellow, FAILED=red, SKIPPED=gray.

### JSON output

```json
{
  "metadata": {
    "timestamp": "2026-03-31T12:00:00Z",
    "tool_version": "0.1.0-poc",
    "profile": "full",
    "kubernetes_context": "gke_project_zone_cluster"
  },
  "targets": {
    "clusters": [
      {"name": "my-cluster", "namespace": "scylla", "kind": "ScyllaCluster", "pods": ["my-cluster-us-east1-0", ...]}
    ]
  },
  "collectors": {
    "cluster_wide": {
      "NodeResourcesCollector": {"status": "passed", "data": {...}, "message": "..."}
    },
    "per_cluster": {
      "scylla/my-cluster": {
        "ScyllaClusterStatusCollector": {"status": "passed", "data": {...}, "message": "..."}
      }
    },
    "per_pod": {
      "scylla/my-cluster-us-east1-0": {
        "OSInfoCollector": {"status": "passed", "data": {...}, "message": "..."},
        "ScyllaVersionCollector": {"status": "passed", "data": {...}, "message": "..."},
        "SchemaVersionsCollector": {"status": "passed", "data": {...}, "message": "..."}
      }
    }
  },
  "analysis": {
    "ScyllaVersionSupportAnalyzer": {"status": "passed", "message": "ScyllaDB 2026.1.0 is supported"},
    "SchemaAgreementAnalyzer": {"status": "passed", "message": "All 3 pods report the same schema version"},
    "OSSupportAnalyzer": {"status": "passed", "message": "RHEL 9 is a supported OS"}
  }
}
```

## Testing strategy

### Dependency injection interfaces

Collectors receive narrow interfaces instead of concrete K8s client types.
Test implementations return canned responses. No client-go fakes, no envtest.

```go
// Production: wraps remotecommand.NewWebSocketExecutor
type k8sPodExecutor struct { restConfig *rest.Config; coreClient corev1client.CoreV1Interface }

// Test: returns preconfigured responses
type fakePodExecutor struct { responses map[string]fakeExecResponse }
```

### Shared test infrastructure (`pkg/soda/testing/fakes.go`)

```go
// FakePodExecutor — returns preconfigured stdout/stderr per command
type FakePodExecutor struct { ... }

// FakeNodeLister — returns a preconfigured list of Nodes
type FakeNodeLister struct { ... }

// FakeScyllaClusterLister — returns preconfigured ClusterInfo list
type FakeScyllaClusterLister struct { ... }

// FakePodLister — returns preconfigured pod lists
type FakePodLister struct { ... }

// FakeCollector — returns a preconfigured CollectorResult (for engine tests)
type FakeCollector struct { ... }

// FakeAnalyzer — returns a preconfigured AnalyzerResult (for engine tests)
type FakeAnalyzer struct { ... }

// FakeArtifactWriter — captures written artifacts in memory map[filename][]byte
type FakeArtifactWriter struct { ... }

// FakeArtifactReader — returns preconfigured content from map[collectorID][scopeKey][filename][]byte
type FakeArtifactReader struct { ... }
```

### Test matrix

| Component | Test file | What is tested | Dependencies |
|-----------|-----------|----------------|--------------|
| **Engine: topo sort** | `engine_test.go` | Collectors run in dependency order | FakeCollector recording call order |
| **Engine: cascade skip** | `engine_test.go` | Collector SKIPPED → dependent analyzer SKIPPED | FakeCollector returning SKIPPED |
| **Engine: cascade fail** | `engine_test.go` | Collector FAILED → dependent analyzer FAILED | FakeCollector returning FAILED |
| **Engine: scope iteration** | `engine_test.go` | ClusterWide runs 1x, PerCluster Nx, PerPod Mx | FakeCollector counting invocations |
| **Engine: cross-scope deps** | `engine_test.go` | PerPod collector depends on ClusterWide → works | FakeCollectors in different scopes |
| **Engine: invalid cross-scope** | `engine_test.go` | ClusterWide depends on PerPod → error | FakeCollectors |
| **Engine: artifact writing** | `engine_test.go` | Collector writes artifacts → correct directory layout, artifacts recorded in result | FakeCollector + FakeArtifactWriter |
| **Resolve: basic** | `resolve_test.go` | Profile → analyzer set → collector transitive closure | FakeAnalyzers + FakeCollectors |
| **Resolve: composition** | `resolve_test.go` | Profile A includes B includes C → merged set | Profile definitions |
| **Resolve: enable/disable** | `resolve_test.go` | --enable adds, --disable removes | Profile + overrides |
| **Resolve: cycle detection** | `resolve_test.go` | Profile A includes B includes A → error | Circular profile definitions |
| **Resolve: missing analyzer** | `resolve_test.go` | Unknown analyzer ID → error | Registry |
| **Collector: NodeResources** | `node_resources_test.go` | FakeNodeLister returns 3 nodes → correct typed result + artifact written | FakeNodeLister + FakeArtifactWriter |
| **Collector: ScyllaClusterStatus** | `scyllacluster_status_test.go` | ClusterInfo with status → correct typed result + manifest.yaml written | Direct struct construction + FakeArtifactWriter |
| **Collector: OSInfo** | `os_info_test.go` | FakeExecutor returns uname+os-release → correct typed result + artifacts written | FakePodExecutor + FakeArtifactWriter |
| **Collector: ScyllaVersion** | `scylla_version_test.go` | FakeExecutor returns version string → correct typed result + artifact written | FakePodExecutor + FakeArtifactWriter |
| **Collector: SchemaVersions** | `schema_versions_test.go` | FakeExecutor returns JSON → correct typed result + artifact written | FakePodExecutor + FakeArtifactWriter |
| **Analyzer: ScyllaVersionSupport** | `scylla_version_support_test.go` | Known/unknown versions → PASSED/WARNING/FAILED | Direct vitals construction with typed results |
| **Analyzer: SchemaAgreement** | `schema_agreement_test.go` | 1 version → PASSED, 2 versions → FAILED | Direct vitals construction with typed results |
| **Analyzer: OSSupport** | `os_support_test.go` | RHEL 9 → PASSED, unknown → WARNING | Direct vitals construction with typed results |
| **Output: console** | `console_test.go` | Results → expected table format | Capture to bytes.Buffer |
| **Output: JSON** | `json_test.go` | Results → valid JSON matching schema | Marshal + unmarshal |

## Implementation steps

Each step is a self-contained commit. Every step after step 2 ships with tests.

### Step 1: Core types and interfaces

**Files:** `pkg/soda/engine/types.go`

Create all the types listed in the "Core types" section above:
- `CollectorScope`, `CollectorStatus`, `AnalyzerStatus` enums with String() methods
- `CollectorID`, `AnalyzerID` string types
- `ScopeKey` struct with `Namespace`, `Name` fields and `String()` method
- `Artifact` struct with `RelativePath`, `Description` and JSON tags
- `CollectorResult` struct (`Status`, `Data any`, `Message`, `Artifacts []Artifact`)
- `AnalyzerResult` struct
- `Collector`, `Analyzer` interfaces (`Analyze` takes `AnalyzerParams`)
- `PodExecutor`, `ScyllaClusterLister`, `NodeLister`, `PodLister` interfaces
- `ArtifactWriter`, `ArtifactReader` interfaces
- `ClusterInfo`, `PodInfo` structs
- `CollectorParams` struct (includes `ArtifactWriter`)
- `AnalyzerParams` struct (includes `Vitals` + `ArtifactReader`)
- `Vitals` struct with `ScopeKey`-keyed maps, `Get()`, `PodKeys()`, `ClusterKeys()` methods
- `Profile` struct

**Tests:** `pkg/soda/engine/types_test.go`
- Status String() methods
- ScopeKey.String()
- Vitals.Get() across scopes with ScopeKey
- Vitals.PodKeys() / ClusterKeys()

**Commit message:** `feat(soda): add core types and interfaces for diagnostic engine`

### Step 2: Test fakes

**Files:** `pkg/soda/testing/fakes.go`

Create all fake implementations:
- `FakePodExecutor` — map from `(namespace, pod, container, command)` → `(stdout, stderr, error)`
- `FakeNodeLister` — returns preconfigured `[]corev1.Node`
- `FakeScyllaClusterLister` — returns preconfigured `[]ClusterInfo`
- `FakePodLister` — returns preconfigured `[]corev1.Pod`
- `FakeCollector` — configurable ID, scope, depends-on, result; records call count and params
- `FakeAnalyzer` — configurable ID, depends-on, result; records call count
- `FakeArtifactWriter` — captures written artifacts in memory `map[string][]byte`; records calls
- `FakeArtifactReader` — returns preconfigured content from `map[CollectorID]map[ScopeKey]map[string][]byte`

**Commit message:** `feat(soda): add shared test fakes for diagnostic engine`

### Step 3: Profile resolution

**Files:** `pkg/soda/engine/resolve.go`, `pkg/soda/engine/resolve_test.go`

Implement `ResolveProfile()`:
- Profile flattening with cycle detection
- Enable/disable overrides
- Transitive collector resolution from analyzer dependencies
- Cross-scope dependency validation (ClusterWide cannot depend on narrower scope)

**Tests:**
- Basic resolution: profile → analyzers → collectors
- Composition: profile includes chain
- Enable/disable overrides
- Cycle detection (profiles and collector deps)
- Unknown IDs → error
- Cross-scope violation → error

**Commit message:** `feat(soda): implement profile resolution with transitive dependency closure`

### Step 4: Engine orchestration

**Files:** `pkg/soda/engine/engine.go`, `pkg/soda/engine/engine_test.go`

Implement `Engine`:
- Constructor: takes registry of all collectors/analyzers, list of target clusters/pods, injected interfaces
- `Run(ctx)`: resolve profile → topo sort → execute collectors by scope → execute analyzers → return results
- Topological sort with cycle detection
- Dependency cascade (skip/fail propagation)
- Keep-going error handling
- ArtifactWriter creation per collector invocation (assigns correct base directory per scope/key/collector)
- ArtifactReader creation for analyzer invocations

**Tests** (all using FakeCollector/FakeAnalyzer):
- Topological ordering verification
- Cascade: skip propagation
- Cascade: fail propagation
- Scope iteration counts (ClusterWide=1, PerCluster=N, PerPod=M)
- Cross-scope dependency access
- Empty registry → no errors
- All collectors fail → all analyzers fail
- Artifact writing: collectors receive ArtifactWriter, artifacts recorded in CollectorResult

**Commit message:** `feat(soda): implement diagnostic engine with topo sort and cascade`

### Step 5: Collectors + tests

**Files:** `pkg/soda/collectors/*.go`, `pkg/soda/collectors/*_test.go`, `pkg/soda/collectors/registry.go`

Implement all 5 collectors. Each collector:
- Returns a **typed result struct** (e.g., `*NodeResourcesResult`) stored in `CollectorResult.Data`
- Writes **raw artifact files** via `params.ArtifactWriter`
- Exposes a **typed accessor function** `Get<Name>Result(vitals, scopeKey)` for analyzers
- Exposes **artifact read helpers** `Read<File>(reader, scopeKey)` for each artifact

Collectors:

1. `NodeResourcesCollector` — calls `NodeLister.ListNodes()`, returns `*NodeResourcesResult`, writes `nodes.yaml`
2. `ScyllaClusterStatusCollector` — reads `ClusterInfo.Object`, type-switches on ScyllaCluster vs ScyllaDBDatacenter, returns `*ScyllaClusterStatusResult`, writes `manifest.yaml`
3. `OSInfoCollector` — exec `uname --all` and `cat /etc/os-release`, returns `*OSInfoResult`, writes `uname.log` and `os-release.log`
4. `ScyllaVersionCollector` — exec `scylla --version`, returns `*ScyllaVersionResult`, writes `scylla-version.log`
5. `SchemaVersionsCollector` — exec REST API curl, returns `*SchemaVersionsResult`, writes `schema-versions.json`

Registry: `AllCollectors()` returns all 5.

**Tests** (each collector has its own test file):
- Happy path: fake returns valid data → correct typed result struct + artifacts written to FakeArtifactWriter
- Parse variants: different OS releases, version formats, multi-schema JSON
- Error handling: fake returns error → CollectorResult with Failed status
- Empty output: fake returns empty string → appropriate handling
- Typed accessor function: verify `Get<Name>Result()` returns correct typed struct from vitals

**Commit message:** `feat(soda): implement 5 PoC collectors with tests`

### Step 6: Analyzers + tests

**Files:** `pkg/soda/analyzers/*.go`, `pkg/soda/analyzers/*_test.go`, `pkg/soda/analyzers/registry.go`

Implement all 3 analyzers. Each analyzer:
- Receives `AnalyzerParams` (containing `Vitals` + `ArtifactReader`)
- Uses **typed accessor functions** from collector packages (e.g., `collectors.GetOSInfoResult()`) instead of `map[string]any` lookups
- Returns `*AnalyzerResult` with status and message

Analyzers:

1. `ScyllaVersionSupportAnalyzer` — uses `GetScyllaVersionResult()` to get typed version data, checks against hardcoded supported ranges
2. `SchemaAgreementAnalyzer` — iterates all PerPod scope keys, uses `GetSchemaVersionsResult()` for each, checks for agreement
3. `OSSupportAnalyzer` — uses `GetOSInfoResult()` to get typed OS data, checks OS name/version against supported list

Registry: `AllAnalyzers()` returns all 3.

**Tests** (each analyzer has its own test file):
- Known supported version → PASSED
- Unknown version → WARNING
- Known end-of-life version → FAILED
- Single schema version across pods → PASSED
- Multiple schema versions → FAILED
- Supported OS → PASSED
- Unknown OS → WARNING
- Tests construct vitals directly with typed result structs

**Commit message:** `feat(soda): implement 3 PoC analyzers with tests`

### Step 7: Output formatters + tests

**Files:** `pkg/soda/output/*.go`, `pkg/soda/output/*_test.go`

Implement:
1. `ConsoleWriter` — colored table output to `io.Writer` using `fatih/color`
2. `JSONWriter` — JSON serialization of full results to `io.Writer`

**Tests:**
- Console: verify expected lines/format by capturing to `bytes.Buffer`
- JSON: marshal → unmarshal round-trip, verify structure matches schema

**Commit message:** `feat(soda): implement console and JSON output formatters`

### Step 8: Profiles

**Files:** `pkg/soda/profiles/profiles.go`

Define the `"full"` profile containing all 3 analyzer IDs.

**Commit message:** `feat(soda): add built-in diagnostic profiles`

### Step 9: Cobra command integration

**Files:** `pkg/cmd/operator/diagnose.go`, edit `pkg/cmd/operator/cmd.go`

Implement `DiagnoseOptions`:
- Embeds kubeconfig flags (from `kgenericclioptions.ConfigFlags`)
- Own flags: `--namespace`, `--cluster-name`, `--profile`, `--enable`, `--disable`, `--output-dir`, `--keep-going`, `--archive`, `--from-archive`
- `Validate()`:
  - Check basic flag validity
  - `--archive` and `--from-archive` are mutually exclusive
  - `--from-archive` is mutually exclusive with `--kubeconfig`, `--cluster-name`, `--namespace`
- `Complete()`: build K8s clients (unless `--from-archive`), create production implementations of injected interfaces, discover targets
- `Run()`:
  - If `--from-archive`: load vitals from directory/tar.gz, create filesystem `ArtifactReader`, run analyzers only, print console summary
  - Otherwise: create Engine with `ArtifactWriter` backed by output directory, run full pipeline
  - If `--archive`: create tar.gz from output directory, clean up temp directory
  - Always print console summary to stdout

Register in `cmd.go`: `cmd.AddCommand(NewDiagnoseCmd(streams))`

**Commit message:** `feat(soda): add 'diagnose' subcommand to scylla-operator`

### Step 10: Build verification and polish

- `go build ./cmd/...` — verify it compiles
- `go test ./pkg/soda/...` — verify all tests pass
- `go vet ./pkg/soda/...` — verify no vet warnings
- Fix any issues found

**Commit message:** `fix(soda): address build and test issues` (if needed)

## Dependency graph (visual)

```
                    ┌──────────────────────────────┐
                    │       diagnose (CLI)          │
                    │   pkg/cmd/operator/diagnose   │
                    └──────────────┬───────────────┘
                                   │ uses
                    ┌──────────────▼───────────────┐
                    │          Engine               │
                    │   pkg/soda/engine             │
                    │   (resolve, topo sort, run)   │
                    └──┬───────────┬────────────┬──┘
                       │           │            │
            ┌──────────▼──┐  ┌────▼─────┐  ┌───▼──────────┐
            │  Collectors  │  │ Analyzers│  │   Output     │
            │  pkg/soda/   │  │ pkg/soda/│  │  pkg/soda/   │
            │  collectors  │  │ analyzers│  │  output      │
            └───┬──────┬───┘  └──┬───┬──┘  └──────────────┘
                │      │         │   │
          writes│ stores│   reads│   │reads
                │      │         │   │
          ┌─────▼──┐ ┌─▼────────▼┐  │
          │Artifact│ │   Vitals   │  │
          │ Files  │ │ (typed     │  │
          │ (disk) │ │  results)  │  │
          └────────┘ └────────────┘  │
                │                    │
                └────────────────────┘
                  (ArtifactReader)

    Dependency injection (testing seam):
    ┌─────────────┐  ┌───────────────┐  ┌────────────┐  ┌───────────┐
    │ PodExecutor  │  │ NodeLister    │  │ PodLister  │  │ Scylla    │
    │ (interface)  │  │ (interface)   │  │ (interface)│  │ Cluster   │
    └──────┬──────┘  └───────┬───────┘  └─────┬──────┘  │ Lister    │
           │                 │                │          │ (iface)   │
    ┌──────▼──────┐  ┌───────▼───────┐  ┌─────▼──────┐  └─────┬─────┘
    │ k8sExec     │  │ k8sNodeLister │  │ k8sPodList │  ┌─────▼─────┐
    │ (prod)      │  │ (prod)        │  │ (prod)     │  │ k8sScylla │
    ├─────────────┤  ├───────────────┤  ├────────────┤  │ Lister    │
    │ fakeExec    │  │ fakeNodeList  │  │ fakePodList│  │ (prod)    │
    │ (test)      │  │ (test)        │  │ (test)     │  ├───────────┤
    └─────────────┘  └───────────────┘  └────────────┘  │ fakeLister│
                                                        │ (test)    │
                                                        └───────────┘

    Artifact interfaces (testing seam):
    ┌────────────────┐  ┌────────────────┐
    │ ArtifactWriter │  │ ArtifactReader │
    │ (interface)    │  │ (interface)    │
    └───────┬────────┘  └───────┬────────┘
    ┌───────▼────────┐  ┌───────▼────────┐
    │ fsWriter       │  │ fsReader       │
    │ (prod: disk)   │  │ (prod: disk)   │
    ├────────────────┤  ├────────────────┤
    │ fakeWriter     │  │ fakeReader     │
    │ (test: memory) │  │ (test: memory) │
    └────────────────┘  └────────────────┘
```

## Post-PoC roadmap

These items are out of scope for the PoC but the architecture explicitly
supports them:

| Feature | How the architecture supports it |
|---------|----------------------------------|
| More collectors (~63 total) | Add files to `pkg/soda/collectors/`, add line to registry. No engine changes. |
| More analyzers (~45 total) | Add files to `pkg/soda/analyzers/`, add line to registry. No engine changes. |
| More profiles | Add entries to `pkg/soda/profiles/profiles.go`. No engine changes. |
| RBAC introspection | Add `RequiredRBAC() []rbacv1.PolicyRule` to Collector interface. Engine validates before run. |
| Parallel execution | Replace sequential loop in engine.go with `errgroup`. Collectors/analyzers unchanged. |
| `--dry-run` | Engine already resolves everything before execution. Add early return after summary. |
| Custom script collectors | New collector type that shells out. Registers like any other collector. |
| LLM prompt generation | New output formatter in `pkg/soda/output/`. Reads same Vitals/AnalyzerResult data. |
| Rich artifact bundle index | New output formatter that writes an index.html/index.md alongside the artifact directory. Basic artifact collection already in PoC. |
| Standalone binary | Move `pkg/soda/` to its own module. Write a `cmd/soda/main.go` that constructs the Cobra command directly. |
