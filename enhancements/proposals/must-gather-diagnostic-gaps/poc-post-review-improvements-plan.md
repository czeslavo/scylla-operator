# PoC Post-Review Improvements Plan

This document captures the actionable improvements identified during a UX and DevEx review of the
`scylla-operator diagnose` PoC implementation. The review compared the implementation against the requirements
in [complete-rewrite-requirements.md](complete-rewrite-requirements.md) and the design in
[complete-rewrite-plan.md](complete-rewrite-plan.md).

Items are grouped by priority and category. Each item includes the specific files affected and a clear
definition of done so that it can be picked up independently.

---

## High Priority

### H1 — Add progress feedback during collection (UX)

**Problem:** `Run()` in `pkg/cmd/operator/diagnose.go` produces no visible output while collectors run.
All progress goes to `klog` at verbose level, which is suppressed by default. For a command that exec's
into every pod, this means users stare at a blank terminal, unsure whether anything is happening or how
long it will take.

**Solution:** Print a progress line to `streams.Out` before each collector invocation inside the engine
(or via a callback/hook from the engine). At minimum:

```
Collecting NodeResourcesCollector (cluster-wide)...
Collecting OSInfoCollector (scylla/my-cluster-0)...
Collecting OSInfoCollector (scylla/my-cluster-1)...
...
Running analysis...
```

**Files to change:**
- `pkg/soda/engine/engine.go` — add an optional `ProgressFunc func(msg string)` field to `EngineConfig`
  that the engine calls before each collector and analyzer invocation.
- `pkg/cmd/operator/diagnose.go` — wire the progress func to write to `streams.Out`.

**Definition of done:** A user running the command against a 3-node cluster sees one progress line per
collector per target before results are displayed. No output if progress func is nil (backwards
compatible).

---

### H2 — Add `Scope()` + `ScopeValue` to `FakeAnalyzer` (DevEx)

**Problem:** `FakeAnalyzer` in `pkg/soda/testing/fakes.go` does not implement the `Scope()` method from
the `engine.Analyzer` interface. The `simpleAnalyzer` used inside `engine_test.go` hardcodes
`AnalyzerClusterWide`. Anyone writing tests for an `AnalyzerPerScyllaCluster` analyzer (all three current
analyzers use this scope) cannot use the shared fake for engine-level tests.

**Files to change:**
- `pkg/soda/testing/fakes.go` — add `ScopeValue engine.AnalyzerScope` field and `Scope()` method to
  `FakeAnalyzer`. Default the zero value to `engine.AnalyzerClusterWide` so existing usages are not
  broken.
- `pkg/soda/engine/engine_test.go` — replace `simpleAnalyzer` usages with `FakeAnalyzer` (from the
  shared fakes package) where the scope matters, adding tests that exercise `AnalyzerPerScyllaCluster`
  behavior through the engine.

**Definition of done:** `FakeAnalyzer` satisfies the `engine.Analyzer` interface (verified by a compile-
time assertion in `fakes.go`). At least one engine test covers `AnalyzerPerScyllaCluster` scope using the
shared fake.

---

### H3 — Add `--dry-run` / pre-run summary (Transparency)

**Problem:** The requirements state: *"Make it transparent what data is being collected and why (what
analyzers need it) before the user runs the tool."* Currently the tool immediately starts collecting with
no preview.

**Solution:** Before executing any collectors, print a pre-run summary to `streams.Out`:

```
ScyllaDB Diagnostics (profile: full)  [dry-run]

Will analyze:
  ScyllaVersionSupportAnalyzer  Scylla version support check
  SchemaAgreementAnalyzer       Schema agreement check
  OSSupportAnalyzer             OS support check

Will collect (auto-resolved from analyzer dependencies):
  NodeResourcesCollector        [ClusterWide]  Kubernetes Node resources
  ScyllaClusterStatusCollector  [PerCluster]   ScyllaDB cluster status
  OSInfoCollector               [PerPod]       OS information
  ScyllaVersionCollector        [PerPod]       Scylla version
  SchemaVersionsCollector       [PerPod]       Schema versions

Targets:
  scylla/my-cluster (ScyllaCluster, 3 pods)
```

When `--dry-run` is set, stop after printing this summary (exit 0, no collection).
When `--dry-run` is not set, print this summary and then proceed with collection (it serves as a
transparency header).

**Files to change:**
- `pkg/cmd/operator/diagnose.go` — add `--dry-run bool` flag to `DiagnoseOptions`; add `DryRun` field;
  call a new `writePlanSummary()` helper at the start of `Run()` before `eng.Run(ctx)`; if dry-run,
  return after printing.
- `pkg/soda/engine/resolve.go` — `ResolveProfile()` already returns resolved collector/analyzer IDs;
  use those in the plan summary. No engine changes needed.
- `pkg/soda/output/console.go` — add a `WritePlan()` method to `ConsoleWriter` that accepts resolved
  collector and analyzer IDs plus their metadata (name, scope) and formats the pre-run summary.

**Definition of done:** `scylla-operator diagnose --dry-run` exits 0 after printing the plan with no
cluster-side operations performed. Running without `--dry-run` prints the same plan header before starting
collection.

---

## Medium Priority

### M1 — Implement `--from-archive` for offline re-analysis (UX)

**Problem:** `vitals.json` is written to disk on every run but there is no code path to load it back.
Users who want to re-analyze previously collected data (e.g., to try different `--enable`/`--disable`
overrides, or to share a bundle with support) must re-run against a live cluster.

**Solution:** Add `--from-archive=<path>` flag. When set:
1. If the path ends in `.tar.gz`, extract to a temporary directory; otherwise use the path as-is.
2. Load `vitals.json` to reconstruct the `Vitals` store.
3. Create a filesystem-backed `ArtifactReader` rooted at the directory.
4. Skip all collectors.
5. Run analyzers against the loaded data using the standard engine path.
6. Print console summary.
7. Clean up temporary directory if extracted from `.tar.gz`.

**Files to change:**
- `pkg/soda/engine/types.go` — the `Vitals.ToSerializable()` and `SerializableVitals` types are already
  present; add a `FromSerializable()` constructor that deserializes `vitals.json` back into a `*Vitals`.
  The deserializer needs a registry mapping `CollectorID → concrete result type` for the `Data` field;
  add a `RegisterResultType(id CollectorID, prototype any)` mechanism or a `ResultTypeRegistry` map
  alongside `AllCollectors()`.
- `pkg/soda/collectors/registry.go` — add `ResultTypeRegistry() map[engine.CollectorID]any` that maps
  each collector ID to its zero-value result struct (used by the deserializer).
- `pkg/cmd/operator/diagnose.go` — add `FromArchive string` field and `--from-archive` flag to
  `DiagnoseOptions`; add `Validate()` checks (mutually exclusive with `--cluster-name`, `--namespace`,
  `--kubeconfig`); implement offline `Run()` branch.
- `pkg/soda/engine/engine.go` — add an `OfflineRun(ctx, vitals, artifactReader)` method (or a flag on
  `EngineConfig`) that skips the collector execution phase and runs analyzers against pre-loaded vitals.

**Definition of done:** `scylla-operator diagnose --from-archive=/tmp/scylla-diagnose-123` produces the
same analysis section and summary as the original live run. Passing a non-existent path produces a clear
error.

---

### M2 — Implement `--archive` for `.tar.gz` output (UX)

**Problem:** The output directory is useful for local inspection but awkward to share (e.g., attach to a
support ticket or send to a colleague).

**Solution:** Add `--archive` flag. When set, write output to a temporary directory, then produce a
`.tar.gz` file and delete the temporary directory. The archive path is printed to stdout at the end.
Console summary is always printed regardless.

**Files to change:**
- `pkg/cmd/operator/diagnose.go` — add `Archive bool` field and `--archive` flag; add `Validate()` check
  that `--archive` and `--from-archive` are mutually exclusive; in `Run()`, when `--archive` is set,
  create a `os.MkdirTemp` directory for collection, then call a helper `createTarGz(srcDir, destPath)`
  and remove `srcDir`; print the archive path to `streams.Out`.

**Definition of done:** `scylla-operator diagnose --archive` produces a single `.tar.gz` file in the
current directory (or alongside `--output-dir`). The archive can be extracted and passed to
`--from-archive` for offline re-analysis (round-trip test).

---

### M3 — Add duplicate-ID detection in registries (DevEx)

**Problem:** `AllCollectorsMap()` and `AllAnalyzersMap()` use a simple loop that silently overwrites on
duplicate keys. If a developer adds a collector with an ID that already exists, the first one is silently
dropped with no indication of the problem.

**Files to change:**
- `pkg/soda/collectors/registry.go` — change `AllCollectorsMap()` to panic (or return an error) if
  two collectors share the same `ID()`. A `init()`-time check or a `MustAllCollectorsMap()` helper that
  panics on duplicate is acceptable.
- `pkg/soda/analyzers/registry.go` — same treatment for `AllAnalyzersMap()`.

**Definition of done:** Adding a second collector with an existing ID causes a panic or compile-time error
before the binary even runs. A test in `collectors/registry_test.go` asserts that all IDs in
`AllCollectors()` are unique.

---

### M4 — Add interface compliance checks (DevEx)

**Problem:** There are no `var _ engine.Collector = (*nodeResourcesCollector)(nil)` style assertions.
A method signature mismatch on a collector or analyzer struct is only caught when the type is actually
used, which may be distant from where the mistake was made.

**Files to change:** Each collector file (`node_resources.go`, `scyllacluster_status.go`, `os_info.go`,
`scylla_version.go`, `schema_versions.go`) and each analyzer file (`scylla_version_support.go`,
`os_support.go`, `schema_agreement.go`) — add one line near the top:
```go
var _ engine.Collector = (*nodeResourcesCollector)(nil)
var _ engine.Analyzer  = (*scyllaVersionSupportAnalyzer)(nil)
```

Also add checks for the fake types in `pkg/soda/testing/fakes.go`:
```go
var _ engine.Collector      = (*FakeCollector)(nil)
var _ engine.Analyzer       = (*FakeAnalyzer)(nil)
var _ engine.ArtifactWriter = (*FakeArtifactWriter)(nil)
var _ engine.ArtifactReader = (*FakeArtifactReader)(nil)
```

**Definition of done:** Every concrete type that implements a soda engine interface has a compile-time
assertion. The build fails immediately if a method is missing or has a wrong signature.

---

### M5 — Add per-collector timing to `CollectorResult` and output (Transparency)

**Problem:** For a command that execs into many pods, users and support engineers have no visibility into
which collectors are slow or whether a specific pod is timing out. Neither the console output nor the JSON
report includes timing data.

**Solution:** Add a `Duration time.Duration` field to `CollectorResult` (and to
`SerializableCollectorResult` / `JSONCollectorResult`). The engine records wall-clock time around each
`collector.Collect()` call and stores it in the result. The console writer shows duration for collectors
that exceed a threshold (e.g., > 1 second):

```
  [PASSED]  OSInfoCollector   scylla/pod-0: RHEL 9.7 x86_64   (2.3s)
```

**Files to change:**
- `pkg/soda/engine/types.go` — add `Duration time.Duration json:"duration_ms"` to
  `CollectorResult` and `SerializableCollectorResult`.
- `pkg/soda/engine/engine.go` — wrap each `collector.Collect()` call with `time.Now()` / `time.Since()`.
- `pkg/soda/output/console.go` — optionally append duration to collector lines.
- `pkg/soda/output/json.go` — include `duration_ms` in `JSONCollectorResult`.

**Definition of done:** `report.json` includes `duration_ms` for every collector result. Console output
shows duration for slow collectors.

---

## Low Priority

### L2 — Align artifact directory naming with the plan (Transparency)

**Problem:** `fsArtifactWriterFactory.NewWriter()` in `pkg/cmd/operator/diagnose.go` produces paths like:
```
<output-dir>/ClusterWide/<CollectorID>/...
<output-dir>/PerPod/<namespace>/<name>/<CollectorID>/...
```
The plan (`complete-rewrite-plan.md`) specifies:
```
<output-dir>/collectors/cluster-wide/<CollectorID>/...
<output-dir>/collectors/per-pod/<namespace>/<name>/<CollectorID>/...
```
The differences: `ClusterWide` vs `cluster-wide`, `PerPod` vs `per-pod`, and the absence of a
`collectors/` top-level prefix.

**Files to change:**
- `pkg/cmd/operator/diagnose.go` — update `fsArtifactWriterFactory.NewWriter()` to use kebab-case scope
  names under a `collectors/` prefix.
- `pkg/soda/engine/types.go` — optionally add a `KebabString()` method to `CollectorScope` or handle the
  mapping in the factory.

**Definition of done:** The output directory structure matches the plan exactly. Existing tests that
assert artifact paths are updated accordingly.

---

### L3 — Add placeholder for RBAC metadata on the `Collector` interface (DevEx/Transparency)

**Problem:** The requirements say each collector should declare the RBAC permissions it needs. There is
currently no place to put this information — neither as a method on the interface nor as a comment
convention.

**Solution:** Add an optional `RBAC() []rbacv1.PolicyRule` method to the `Collector` interface (or, to
avoid making it mandatory, define a separate `RBACCollector` interface and use a type assertion). This
unlocks future `--dry-run` output that lists required permissions per collector.

**Files to change:**
- `pkg/soda/engine/types.go` — add an optional `RBACProvider` interface:
  ```go
  // RBACProvider is an optional interface that collectors can implement to declare
  // the Kubernetes RBAC rules they require.
  type RBACProvider interface {
      RBAC() []rbacv1.PolicyRule
  }
  ```
- Each collector file — add a `RBAC()` implementation (can return `nil` initially) and a comment
  documenting the actual permissions needed.

**Definition of done:** Each collector file has an `RBAC()` method (even if it returns `nil`) with a
documentation comment listing the required K8s permissions. The interface is defined but not yet enforced.

---

### L4 — Add a generated index file to the output directory (Transparency)

**Problem:** The requirements state the artifact bundle should be *"self-describing and navigable"*. There
is no index file, no README, and no structured manifest explaining the directory layout.

**Solution:** At the end of `Run()`, write a `README.md` (or `index.json`) to the output directory root
that lists:
- The profile used
- The clusters and pods that were targeted
- The collectors that ran and the artifact files each produced
- The analyzer results summary
- Instructions for offline re-analysis (`--from-archive`)

**Files to change:**
- `pkg/soda/output/` — add an `index.go` file with a `WriteIndex()` function.
- `pkg/cmd/operator/diagnose.go` — call `WriteIndex()` at the end of `Run()`.

**Definition of done:** The output directory contains a `README.md` that a human (or LLM) can read to
understand the contents without prior knowledge of the tool.

---

## Implementation Order Recommendation

The items above are designed to be independent commits. A suggested sequence for a follow-up
implementation sprint:

1. **H2** — FakeAnalyzer `Scope()` (pure test fix, zero risk, unblocks engine tests)
2. **M4** — Interface compile-time assertions (zero-risk defensive measure, catches bugs early)
3. **M3** — Duplicate-ID detection in registries (zero-risk safety net)
4. **H1** — Progress feedback (improves UX immediately, small engine change)
5. **H3** — `--dry-run` / pre-run summary (transparency requirement, builds on H1)
6. **M5** — Per-collector timing (builds naturally on top of H1's engine changes)
7. **L2** — Artifact directory naming alignment (do before M1/M2 to avoid migrating archive format)
8. **M1** — `--from-archive` offline mode (largest change, depends on stable vitals.json format)
9. **M2** — `--archive` flag (straightforward once M1 is done)
10. **L3** — RBAC metadata placeholder (sets up future `--dry-run --show-rbac` feature)
11. **L4** — Output directory index file (polish, do last)
