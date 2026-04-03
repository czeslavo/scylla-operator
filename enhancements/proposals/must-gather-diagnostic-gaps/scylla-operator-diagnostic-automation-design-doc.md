# SODA: Scylla Operator Diagnostic Automation — Design Document

**A Kubernetes-native diagnostic CLI for ScyllaDB clusters, integrated as `scylla-operator diagnose`.**

---

## Table of Contents

1. [TL;DR](#1-tldr)
2. [Motivation](#2-motivation)
3. [Goals](#3-goals)
4. [Non-Goals](#4-non-goals)
5. [Approaches Considered](#5-approaches-considered)
6. [Proposed Architecture](#6-proposed-architecture)
7. [CLI Integration](#7-cli-integration)
8. [Running SODA in Practice](#8-running-soda-in-practice)
9. [Development Experience: Adding Collectors and Analyzers](#9-development-experience-adding-collectors-and-analyzers)
10. [Relationship to must-gather](#10-relationship-to-must-gather)
11. [Relationship to Scylla Doctor](#11-relationship-to-scylla-doctor)
12. [Current PoC Status](#12-current-poc-status)
13. [Milestones](#13-milestones)
14. [Appendix A: Scylla Doctor Collector and Analyzer Mapping](#appendix-a-scylla-doctor-collector-and-analyzer-mapping)
15. [Appendix B: Kubernetes-Specific Collectors and Analyzers](#appendix-b-kubernetes-specific-collectors-and-analyzers)

---

## 1. TL;DR

We propose building SODA (Scylla Operator Diagnostic Automation) — a diagnostic CLI integrated as `scylla-operator diagnose` — to replace `must-gather` and bring Scylla Doctor-level diagnostic depth to Kubernetes. SODA collects data from Kubernetes resources, Scylla REST APIs, CQL system tables, and container internals, then analyzes the results to produce actionable findings with PASS/WARNING/FAIL status. It is inspired by Scylla Doctor's proven collector/analyzer/vitals architecture but built natively in Go within the scylla-operator codebase, giving the operator team full ownership and the best Kubernetes integration.

A working PoC with 35 collectors, 5 analyzers, and a full engine (concurrent execution, dependency tracking, offline re-analysis, archive output) is implemented in `pkg/soda/`.

**Proposed next steps:**

1. Review this proposal and reach consensus on the approach (especially with the CX team).
2. Clean up the PoC, write automated E2E tests.
3. Reach feature parity with Scylla Doctor's Kubernetes-relevant checks (~26 analyzers).
4. Deprecate and eventually remove `must-gather`.

---

## 2. Motivation

### 2.1 must-gather is a shallow data dump

The current `must-gather` tool collects files and Kubernetes manifests but performs no analysis. Users receive a tarball with no guidance on what to look for, what might be wrong, or which findings require attention. Every investigation begins with manual inspection of dozens of files — a time-consuming and error-prone process.

### 2.2 must-gather does not collect system tables

CQL system tables (`system.peers`, `system.local`, `system_schema.*`, Raft group0 state, gossip information) are critical for investigating cluster state issues such as rejected cluster joins, topology inconsistencies, and schema disagreements. must-gather does not collect any of these.

### 2.3 must-gather does not collect the effective scylla.yaml

must-gather collects the input ConfigMap that is mounted into the Scylla container, but not the effective configuration that Scylla is actually running with. The scylla-operator entry point merges multiple configuration sources (ConfigMap, scylla.d overrides, command-line arguments) before starting Scylla. Users cannot see the final, merged `scylla.yaml` — making it impossible to verify that configuration intent matches runtime reality without exec-ing into the pod manually.

### 2.4 Arbitrary, non-semantic collection scope

must-gather collects a fixed, hand-picked set of Kubernetes resources with no organizing principle. Users cannot choose to collect "everything about cluster topology" or "everything about storage health." The collection does not align with the needs of specific investigation scenarios, and the data often feels arbitrary — some potentially important resources are missing while less relevant data is included.

### 2.5 Overlap and gaps between must-gather and Scylla Doctor

There is partial overlap between the diagnostic data collected by must-gather and Scylla Doctor. At the same time, important data falls through the cracks of both tools. Neither tool provides a complete diagnostic picture for a Kubernetes-hosted ScyllaDB cluster:

- must-gather covers Kubernetes manifests but not Scylla internals (CQL, REST API, gossip).
- Scylla Doctor covers Scylla internals but is unaware of Kubernetes resources (CRDs, operators, StatefulSets, PVCs, scheduling constraints).

### 2.6 Scylla Doctor does not work in Kubernetes

Scylla Doctor is designed for bare-metal and VM deployments. It relies on SSH access to nodes, systemd service management, host-level networking, and direct filesystem access — none of which are available inside Kubernetes containers. Approximately 19 of its 63 collectors are infeasible inside containers (requiring `perftune.py`, `journalctl`, `timedatectl`, `lspci`, host NIC enumeration, etc.), and it has no awareness of Kubernetes-specific resources or abstractions.

### 2.7 Opportunity for semantic, modular collection

Designing the diagnostic tool around semantic areas (topology, configuration, storage, health, logs, etc.) serves multiple causes:

1. **User-aligned scope:** Users can mix and match the scope and depth of data collection relevant to their specific case, making the collected data feel less arbitrary and more aligned with investigation needs.
2. **Cheap extensibility:** A modular architecture with well-defined collector and analyzer interfaces makes it cheap for the operator team to add new collectors and analyzers — especially with LLM-assisted development and a robust core engine. Each new collector or analyzer is a small, self-contained unit of ~30-80 lines of Go.

---

## 3. Goals

- **Single command, end-to-end:** Provide a single `scylla-operator diagnose` command that collects data AND analyzes it, producing actionable findings with PASS/WARNING/FAIL status.
- **Broader coverage:** Collect diagnostic data from Kubernetes resources, Scylla REST API, CQL system tables, and container internals — covering more ground than either must-gather or Scylla Doctor alone.
- **Semantic profiles:** Organize collection around semantic diagnostic areas (profiles) so users can tailor scope to their investigation.
- **Structured, reproducible archive:** Produce a structured archive suitable for offline analysis, sharing with support, and (in the future) LLM-assisted investigation.
- **Cheap to extend:** Make adding new collectors and analyzers cheap and straightforward for the operator team. Demonstrate this with example implementations.
- **Replace must-gather:** Eventually replace `must-gather` entirely (deprecated for a few releases, then removed).
- **Feature parity milestone:** Reach feature parity with Scylla Doctor's Kubernetes-relevant checks (~26 analyzers that would make sense to run in K8s) as the first milestone after PoC.

---

## 4. Non-Goals

- **Not for bare-metal/VM:** SODA is not a replacement for Scylla Doctor on bare-metal or VM deployments. Scylla Doctor remains the right tool for those environments.
- **Not real-time monitoring:** SODA is a point-in-time diagnostic tool, not a monitoring or alerting system.
- **No web UI:** Output is CLI-based (console report, JSON, archive). No web dashboard or GUI.
- **No auto-remediation:** SODA diagnoses and reports; it does not fix problems automatically.

---

## 5. Approaches Considered

Three approaches were evaluated for closing the diagnostic gap between Kubernetes-hosted ScyllaDB clusters and the existing tooling.

### 5.1 Approach A: Complete Rewrite (Recommended)

Build an independent diagnostic CLI natively in Go within the scylla-operator codebase, inspired by Scylla Doctor's collector/analyzer architecture.

**Pros:**
- **Best Kubernetes-native experience.** Natural access to the Kubernetes API, CRD types, operator internals, and pod exec — no bridging or conversion layer needed.
- **Full ownership and control.** The operator team can iterate independently without cross-project coupling or release coordination with the Scylla Doctor project.
- **Cheap extensibility.** The PoC has validated that adding a new collector or analyzer is a small, self-contained task (~30-80 lines of Go) thanks to the scope-specific interfaces, base embeddings, and generic typed accessors.
- **Single binary, single workflow.** No multi-tool orchestration — users run one command and get results.
- **Tailored output.** Archive format, README index, and report structure are optimized for Kubernetes diagnostic scenarios and future LLM-assisted analysis.

**Cons:**
- **Dual maintenance for Scylla-native checks.** If a new Scylla-native (non-Kubernetes-specific) collector or analyzer is added to Scylla Doctor, it would need to be independently implemented in SODA as well. However, these additions are infrequent and the per-collector implementation cost is low.

### 5.2 Approach B: Hybrid Pipeline (must-gather + vitals converter + Scylla Doctor)

Adapt must-gather to produce Scylla Doctor-compatible vitals JSON, then feed it into Scylla Doctor for analysis.

**Pros:**
- Reuses Scylla Doctor's existing ~58 analyzers.
- Lower initial analyzer implementation effort.

**Cons:**
- **Two-tool orchestration.** Users must run must-gather, then run a converter, then run Scylla Doctor — a fragile, multi-step workflow.
- **Vitals format coupling.** Changes to Scylla Doctor's internal data model require corresponding updates to the converter.
- **Cross-team release coordination.** Converter changes must stay in sync with Scylla Doctor releases.
- **Many analyzers auto-skip.** ~25 of Scylla Doctor's ~58 analyzers automatically skip in Kubernetes because their dependent collectors are infeasible.
- **Kubernetes-specific analysis still from scratch.** Kubernetes-native checks (CRD validation, scheduling, RBAC, operator health) must still be built independently.
- **Validated in PoC.** This approach was prototyped: 8 of 63 collectors were implemented and validated on GKE (8 PASSED, 3 WARNING, ~47 SKIPPED). The prototype proved the approach feasible but brittle and complex.

### 5.3 Approach C: Extend Scylla Doctor with Kubernetes Support

Add Kubernetes-aware collectors and analyzers directly to Scylla Doctor's Python codebase.

**Pros:**
- Single tool for both VMs/bare-metal and Kubernetes.
- Shared analyzer logic.

**Cons:**
- **Language and ecosystem mismatch.** Scylla Doctor is Python; the operator team works in Go. Cross-language, cross-repo contributions create friction and context-switching overhead.
- **SSH-oriented architecture.** Scylla Doctor's core is built around SSH access to nodes. Adding Kubernetes API access would require significant refactoring of its transport, discovery, and scoping model.
- **Abstraction gap.** Kubernetes introduces concepts that Scylla Doctor has no model for: namespaces, CRDs, pods vs. nodes, StatefulSets, PVCs, operator controllers, RBAC. These would need to be grafted onto an architecture designed for a different world.
- **Ownership and release coordination.** Shared ownership across teams complicates prioritization, review, and release management.
- **Tight integration difficulty.** Sharing CRD types, using the operator's Kubernetes client setup, and integrating with the operator CLI are all much harder from a separate Python codebase.

### 5.4 Recommendation

**Approach A (Complete Rewrite)** is recommended. The PoC has already validated the architecture and demonstrated that the implementation cost is manageable. The operator team gets full ownership, the best Kubernetes integration, and the ability to iterate without cross-project coordination. The main tradeoff — dual implementation of Scylla-native collectors and analyzers — is acceptable given the low per-collector cost, the infrequency of additions, and the robustness of the core engine.

---

## 6. Proposed Architecture

### 6.1 High-Level Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    scylla-operator diagnose                         │
│                                                                     │
│  ┌──────────┐   ┌──────────┐   ┌────────────┐   ┌──────────┐        │
│  │ Topology │──>│ Profile  │──>│ Collectors │──>│ Vitals   │        │
│  │ Discovery│   │Resolution│   │ (concurrent│   │  Store   │        │
│  └──────────┘   └──────────┘   │  by scope) │   └────┬─────┘        │
│                                └──────┬─────┘        │              │
│                                       │              │              │
│                                       │   ┌──────────▼──────────┐   │
│                                       │   │ Analyzers           │   │
│                                       │   │ (read Vitals,       │   │
│                                       │   │  concurrent by scope│   │
│                                       │   │  produce findings)  │   │
│                                       │   └──────────┬──────────┘   │
│                                       │              │              │
│                                       ▼              ▼              │
│                         ┌───────────────────────────────┐           │
│                         │ Output:                       │           │
│                         │   Console Report              │           │
│                         │   report.json                 │           │
│                         │   vitals.json                 │           │
│                         │   README.md                   │           │
│                         │   artifacts/ (raw files)      │           │
│                         │   [.tar.gz archive]           │           │
│                         └───────────────────────────────┘           │
└─────────────────────────────────────────────────────────────────────┘
```

The diagnostic pipeline proceeds in sequential phases:

1. **Topology discovery** — Find all ScyllaCluster/ScyllaDBDatacenter resources and enumerate their Scylla pods.
2. **Profile resolution** — Resolve the selected profile into final sets of collectors and analyzers, applying enable/disable overrides and computing the transitive dependency closure.
3. **Collector execution** — Run collectors concurrently with bounded parallelism, respecting dependency ordering (topological sort) and cascade logic (skip/fail propagation). Each collector stores structured data in the Vitals store and optionally writes raw artifact files (logs, configs, manifests) to the output directory.
4. **Analyzer execution** — Run analyzers concurrently, reading from the Vitals store populated by collectors.
5. **Output generation** — Produce console report, JSON report, vitals file, README index, and artifact archive.

### 6.2 Core Concepts

| Concept | Description |
|---------|-------------|
| **Collector** | Gathers raw diagnostic data from a specific source (Kubernetes API, CQL, REST API, container exec). Each collector has a well-defined scope and produces both structured data (stored in Vitals) and optional raw artifacts (written to the filesystem). |
| **Analyzer** | Examines data from the Vitals store and produces a diagnostic finding with a status (PASS/WARNING/FAIL/SKIPPED) and human-readable message. |
| **Vitals** | The central in-memory data store. Holds all collector results keyed by collector ID and scope (cluster, node). Thread-safe, fully serializable to JSON for archive round-trip. |
| **Artifact** | A raw file produced by a collector alongside its structured data — e.g., log output, config files, YAML manifests. Stored in the filesystem under a structured directory tree. |
| **Profile** | A named set of collectors and analyzers that defines what to run. SODA ships with reasonable built-in profiles for common use cases (e.g., `full`, `health`, `logs`). It is also cheap to allow users to configure custom profiles by specifying exactly which collectors and analyzers they want to run. Profiles can include other profiles for composition. |
| **Engine** | The orchestrator that ties everything together: topology discovery, profile resolution, dependency tracking, concurrent execution, cascade logic, progress reporting. |

### 6.3 Scope Model

Collectors and analyzers operate at one of three scopes, reflecting the natural hierarchy of a Kubernetes-hosted ScyllaDB deployment:

| Scope | Execution | Example |
|-------|-----------|---------|
| **ClusterWide** | Runs once per diagnostic run. | Collect Kubernetes Node manifests, operator Deployments, all ScyllaCluster CRDs. |
| **PerScyllaCluster** | Runs once per ScyllaCluster (or ScyllaDBDatacenter) discovered. | Collect child StatefulSets, Services, PVCs for each cluster. |
| **PerScyllaNode** | Runs once per Scylla pod in each cluster. | Exec `scylla --version`, query CQL system tables, read `/etc/os-release`. |

Each scope has a dedicated collector interface that receives only the context relevant to that scope. A `PerScyllaNode` collector receives the specific pod and cluster information; it never needs to handle iteration over pods — the engine handles that.

Similarly, analyzers are scoped:

| Scope | Description |
|-------|-------------|
| **AnalyzerClusterWide** | Runs once across all vitals. |
| **AnalyzerPerScyllaCluster** | Runs once per ScyllaCluster, receiving vitals filtered to that cluster's nodes. |

### 6.4 Engine Execution Model

The engine orchestrates the diagnostic pipeline in four phases:

1. **Topology discovery** — Query the Kubernetes API for all ScyllaCluster (v1) and ScyllaDBDatacenter (v1alpha1) resources in the target namespace(s), then enumerate Scylla pods for each cluster using label selectors.

2. **Profile resolution** — Resolve the selected profile into final sets of collectors and analyzers. Profiles can include other profiles, and the engine recursively flattens them, applies user overrides (`--enable`/`--disable`), and computes the transitive dependency closure so that all collectors required by the resolved analyzers are included automatically.

3. **Collector execution** — Collectors are topologically sorted by their dependency graph and executed concurrently with bounded parallelism. Each collector stores structured data in the Vitals store and optionally writes raw artifact files to the output directory. If a dependency has failed or been skipped, the dependent collector is automatically cascaded to the same status without execution. The engine emits progress events for real-time CLI output.

4. **Analyzer execution** — After all collectors complete, analyzers run concurrently against the populated Vitals store. Analyzers that depend on failed or skipped collectors are automatically skipped with an appropriate message.

### 6.5 Collector and Analyzer Interfaces

There are well-defined Go interfaces for collectors and analyzers to implement, one per scope. Each interface requires a single method — the collection or analysis logic — while a shared base embedding provides all the metadata boilerplate (ID, name, scope, dependencies). All dependencies that a collector or analyzer needs (Kubernetes clients, pod executors, artifact writers, Vitals references) are injected via a scope-specific params struct, so implementations don't need to set up any infrastructure — they just receive everything they need and focus on the diagnostic logic.

Generic typed accessors allow analyzers to retrieve collector results from the Vitals store without manual type assertions, making cross-collector data access type-safe and concise.

This design keeps each collector and analyzer self-contained at ~30-80 lines of Go. See [Section 9](#9-development-experience-adding-collectors-and-analyzers) for complete examples.

### 6.6 Vitals Store and Serialization

The Vitals store is a thread-safe, scope-organized in-memory data store:

```go
type Vitals struct {
    mu               sync.RWMutex
    ClusterWide      map[CollectorID]*CollectorResult
    PerScyllaCluster map[ScopeKey]map[CollectorID]*CollectorResult
    PerScyllaNode    map[ScopeKey]map[CollectorID]*CollectorResult
}
```

Each `CollectorResult` contains:
- `Status` (PASSED/FAILED/SKIPPED)
- `Data` (typed struct — the collector's structured output)
- `Message` (human-readable summary)
- `Artifacts` (list of raw files written)
- `Duration` (wall-clock time)

**Serialization round-trip:** `Vitals` can be converted to `SerializableVitals` (where `Data` becomes `json.RawMessage`) for persistence to `vitals.json`. On load, a `ResultTypeRegistry` maps each `CollectorID` to its concrete Go type, enabling deserialization back to typed structs for offline analysis.

`SerializableVitals` also embeds a `SerializableClusterTopology` that stores the cluster and node topology discovered during the live run, so that offline re-analysis can reconstruct it without connecting to the cluster.

### 6.7 Archive Format

The output directory follows a structured layout organized by scope:

```
scylla-diagnose-<timestamp>/
├── vitals.json                                          # Full Vitals store (structured data)
├── report.json                                          # Analysis results + metadata
├── README.md                                            # Human+agent-readable index
└── collectors/
    ├── cluster-wide/
    │   ├── NodeResourcesCollector/
    │   │   └── nodes.json
    │   ├── DeploymentCollector/
    │   │   └── deployments.json
    │   └── ...
    ├── per-scylla-cluster/
    │   └── <namespace>/
    │       └── <cluster-name>/
    │           ├── ScyllaClusterStatefulSetCollector/
    │           │   └── statefulsets.json
    │           └── ...
    └── per-scylla-node/
        └── <namespace>/
            └── <pod-name>/
                ├── OSInfoCollector/
                │   ├── uname.log
                │   └── os-release.log
                ├── ScyllaVersionCollector/
                │   └── scylla-version.log
                ├── ScyllaConfigCollector/
                │   └── scylla.yaml
                └── ...
```

The archive can optionally be packed into a `.tar.gz` file via `--archive`.

### 6.8 Offline / From-Archive Mode

SODA supports offline re-analysis via `--from-archive`:

```bash
scylla-operator diagnose --from-archive=./scylla-diagnose-20260401.tar.gz
```

This mode:
1. Extracts the archive (if `.tar.gz`) to a temp directory.
2. Loads `vitals.json` and deserializes it back into typed Vitals using the `ResultTypeRegistry`.
3. Reconstructs the cluster/node topology from the embedded `SerializableClusterTopology`.
4. Runs analyzers against the loaded Vitals without connecting to any cluster.
5. Displays the console report with the analysis results.

This enables:
- Sharing archives with support teams for remote diagnosis.
- Re-analysis with updated analyzers after upgrading the operator.
- Reproducible diagnostics from a fixed point-in-time snapshot.

### 6.9 README.md as an Agent-Friendly Context Source (Future Direction)

The `README.md` generated in the archive is not just a file listing — it is designed to be a structured context document for both humans and AI agents.

Currently, it contains:
- Run summary (profile, cluster count, node count).
- Targets (clusters and their nodes).
- Collector inventory with artifact paths and descriptions.
- Analysis results table (analyzer, scope, status, message).
- Offline re-analysis instructions.

**Future enhancements** could expand README.md into a rich context source specifically designed for LLM-assisted analysis:
- Links to relevant ScyllaDB documentation for each analyzer finding.
- Links to ScyllaDB and Scylla Operator source code at the specific versions running in the cluster (derived from the `ScyllaVersionCollector` and operator Deployment image).
- Guidance on how to approach analyzing the archive: which files to examine first for common issue categories, what patterns to look for, what the expected values are.
- Structured metadata that agents can parse to understand the diagnostic context without reading every artifact.

Combined with the archive contents (vitals.json, raw artifacts), this provides rich context for LLM-assisted diagnostic investigation. The archive is inherently token-efficient: structured JSON data, focused log snippets, and a self-describing index.

### 6.10 Profiles

Profiles define named sets of collectors and analyzers, enabling users to tailor diagnostic scope to their investigation:

| Profile | Description | Contents |
|---------|-------------|----------|
| `full` | Run all available collectors and analyzers | All health + logs collectors, all manifest collectors, all analyzers |
| `health` | Quick health check via Scylla REST API and CQL | GossipInfo, SchemaVersions, SystemPeersLocal, SystemTopology, ScyllaVersion + all analyzers |
| `logs` | Collect container logs only | ScyllaNodeLogs, OperatorPodLogs, ScyllaClusterJobLogs |

Profiles support **composition** via the `Includes` field. The `full` profile includes `health` and `logs`, then adds its own manifest collectors. This avoids duplication and makes it easy to define new profiles that build on existing ones.

Users select profiles via `--profile`:

```bash
scylla-operator diagnose --profile=health     # Quick health check
scylla-operator diagnose --profile=full       # Everything (default)
```

### 6.11 RBAC

Every collector declares the Kubernetes RBAC permissions it requires. The engine aggregates all RBAC rules from the resolved collector set, enabling:

- **`--dry-run` output:** Shows the full set of required permissions before connecting to the cluster, so administrators can provision the correct RBAC ahead of time. The `--dry-run` output could provide complete step-by-step instructions: creating the required ClusterRole/Role, creating a ServiceAccount, binding them, and exporting a kubeconfig for the ServiceAccount — so that users can set up RBAC with minimal effort.
- **RBAC manifest generation:** The aggregated rules can be used to generate ClusterRole or Role manifests directly.

---

## 7. CLI Integration

SODA is integrated into the Scylla Operator binary as a `diagnose` subcommand:

### Usage Examples

```bash
# Full diagnostic run against all ScyllaDB clusters
scylla-operator diagnose

# Target a specific cluster
scylla-operator diagnose --namespace=scylla --cluster-name=my-cluster

# Quick health check only
scylla-operator diagnose --profile=health

# Collect logs for all clusters
scylla-operator diagnose --profile=logs

# Save artifacts to a specific directory
scylla-operator diagnose --output-dir=/tmp/diagnostics

# Pack output into a .tar.gz archive
scylla-operator diagnose --archive

# Dry run — show what would be collected and RBAC required
scylla-operator diagnose --dry-run

# Offline re-analysis from a previous archive
scylla-operator diagnose --from-archive=./scylla-diagnose-20260401.tar.gz

# Enable/disable specific analyzers
scylla-operator diagnose --enable=CustomAnalyzer --disable=OSSupportAnalyzer
```

### Example Console Output

```
ScyllaDB Diagnostics (profile: full)

Scylla Clusters:
  scylla/my-cluster (ScyllaCluster, 3 nodes)

Collectors:
  [PASSED]  NodeResourcesCollector          3 nodes                            (245ms)
  [PASSED]  OSInfoCollector                 scylla/scylla-0: Ubuntu 22.04 x86_64 (312ms)
  [PASSED]  OSInfoCollector                 scylla/scylla-1: Ubuntu 22.04 x86_64 (298ms)
  [PASSED]  OSInfoCollector                 scylla/scylla-2: Ubuntu 22.04 x86_64 (305ms)
  [PASSED]  ScyllaVersionCollector          scylla/scylla-0: 2026.1.0          (156ms)
  [PASSED]  ScyllaVersionCollector          scylla/scylla-1: 2026.1.0          (148ms)
  [PASSED]  ScyllaVersionCollector          scylla/scylla-2: 2026.1.0          (152ms)
  [PASSED]  GossipInfoCollector             scylla/scylla-0: 3 endpoints       (89ms)
  ...

Analysis:
  [PASSED]   ScyllaVersionSupportAnalyzer   scylla/my-cluster: ScyllaDB 2026.1.0 is supported
  [PASSED]   SchemaAgreementAnalyzer        scylla/my-cluster: All 3 nodes agree on schema
  [PASSED]   OSSupportAnalyzer              scylla/my-cluster: All pods run supported OS: Ubuntu 22.04
  [PASSED]   GossipHealthAnalyzer           scylla/my-cluster: All 3 gossip endpoints are UP/NORMAL
  [WARNING]  TopologyHealthAnalyzer         scylla/my-cluster: 1 node has RAFT status: voter

Summary: 4 passed, 1 warnings, 0 failed, 0 skipped

Artifacts written to: /tmp/scylla-diagnose-20260401
```

---

## 8. Running SODA in Practice

### 8.1 Addressing the Trust Problem

The biggest friction we encountered with must-gather was **lack of confidence in what the tool would collect**. Customers and the CX team were hesitant to run it in production — and for good reason. must-gather was an opaque script that required broad admin privileges, with no way to preview what it would access, no way to scope down the collection, and no way to verify that it wouldn't touch resources outside the ScyllaDB deployment. Nobody wants to run a non-transparent tool with admin privileges in their production cluster.

SODA addresses this concern directly:

- **Transparent RBAC.** Every collector declares exactly which Kubernetes permissions it needs. Running `--dry-run` shows the complete RBAC picture — every API group, resource, and verb — before anything touches the cluster.
- **Collector-level control.** Collectors are first-class citizens. Users have full control over which collectors run via profiles and `--enable`/`--disable` flags. If a customer doesn't want SODA to collect container logs or exec into pods, they can select a profile that excludes those collectors, or disable them explicitly.
- **Read-only operations only.** SODA only reads data. The only operation that resembles a "write" is `pods/exec`, used to run read-only commands inside Scylla containers (e.g., `scylla --version`, reading config files, querying CQL system tables). No resources are created, modified, or deleted.
- **Preview before execution.** `--dry-run` provides a complete preview: which collectors will run, what data they will collect, at what scope (cluster-wide, per-cluster, per-node), and what RBAC is required. The customer can review this output with their security team before granting any access.

### 8.2 Obtaining the Binary

SODA is built into the `scylla-operator` binary as the `diagnose` subcommand. There are several ways to obtain it:

**Docker image** (no installation required):
```bash
docker run --rm \
  -v "$HOME/.kube:/kube:ro" \
  -e KUBECONFIG=/kube/config \
  scylladb/scylla-operator:latest \
  diagnose --profile=full --archive
```

**Prebuilt binary** from GitHub releases:
```bash
# Download the scylla-operator binary for your platform from the releases page
curl -L -o scylla-operator https://github.com/scylladb/scylla-operator/releases/download/v1.X.Y/scylla-operator-linux-amd64
chmod +x scylla-operator
./scylla-operator diagnose --profile=full
```

**Build from source:**
```bash
go install github.com/scylladb/scylla-operator/cmd/scylla-operator@latest
scylla-operator diagnose --profile=full
```

In all cases, SODA uses the standard `KUBECONFIG` environment variable or `--kubeconfig` flag to connect to the cluster, following the same conventions as `kubectl`.

### 8.3 KUBECONFIG and Access Control

There are two main approaches to providing SODA with cluster access, depending on the customer's security posture:

**Option 1: Dedicated ServiceAccount with minimal RBAC**

This is the most restrictive and transparent approach. The customer creates a dedicated ServiceAccount with only the permissions that SODA needs for the chosen profile:

```bash
# Step 1: Preview what RBAC is needed for the desired profile
scylla-operator diagnose --dry-run --profile=health

# Step 2: The --dry-run output provides the ClusterRole/Role definition.
#          Create the ServiceAccount, Role, and RoleBinding as instructed.

# Step 3: Export a kubeconfig for the dedicated ServiceAccount
kubectl create token soda-diagnostics -n scylla --duration=1h > /tmp/soda-token
# (or configure a kubeconfig using the SA token)

# Step 4: Run SODA with the dedicated kubeconfig
scylla-operator diagnose --profile=health --kubeconfig=./soda-sa.kubeconfig
```

This way, the customer's security team can audit the exact permissions before granting them, and the ServiceAccount is scoped to exactly what SODA needs — nothing more.

**Option 2: Existing admin kubeconfig**

The simplest path for environments where the operator already has broad access or the customer is comfortable using their existing kubeconfig:

```bash
# Optional: preview what will be collected
scylla-operator diagnose --dry-run --profile=full

# Run with existing kubeconfig
scylla-operator diagnose --profile=full --archive
```

This is reasonable because SODA only performs read-only operations, and the customer can verify exactly what will be collected by reviewing the `--dry-run` output beforehand.

### 8.4 User Stories

#### Story 1: Cluster admin — collecting diagnostics with minimal privileges

> *I'm a K8s cluster admin and I don't want to give SODA admin access to my cluster, but I need to collect diagnostics because my Scylla cluster deployed with Scylla Operator is misbehaving.*

```bash
# 1. Preview what the health profile needs
scylla-operator diagnose --dry-run --profile=health

# 2. Review the RBAC requirements in the output, create a dedicated ServiceAccount
#    with only the listed permissions (the --dry-run output provides the manifests)
kubectl apply -f soda-rbac.yaml

# 3. Create a kubeconfig for the dedicated ServiceAccount
kubectl create token soda-diagnostics -n scylla --duration=1h > /tmp/soda-token

# 4. Run SODA with the scoped-down kubeconfig
scylla-operator diagnose --profile=health --kubeconfig=./soda-sa.kubeconfig --archive

# 5. Collectors that require permissions beyond the SA's scope are automatically
#    skipped with a clear message explaining which permission was missing.
```

#### Story 2: Cluster admin — CX-requested diagnostics

> *I'm a K8s cluster admin and I was asked by Scylla CX to collect diagnostics from my misbehaving cluster.*

```bash
# 1. CX provides the profile to use (e.g., "full") and installation instructions.
#    Install the binary or use the Docker image.

# 2. Preview what will be collected and what RBAC is needed
scylla-operator diagnose --dry-run --profile=full

# 3. Review the output with your security team if needed.
#    Set up RBAC using the provided manifests, or use your existing admin kubeconfig.

# 4. Collect the diagnostic archive
scylla-operator diagnose --profile=full --archive

# 5. Share the resulting .tar.gz file with Scylla CX
#    (the archive contains only the data listed in the --dry-run output)
```

#### Story 3: CX — explaining what SODA collects to a customer

> *I'm a Scylla CX team member and I need to explain to the customer what data SODA will collect from their cluster.*

```bash
# 1. Run --dry-run to generate a complete, human-readable list of collectors,
#    their descriptions, scopes, and required RBAC permissions
scylla-operator diagnose --dry-run --profile=full

# 2. Share the --dry-run output with the customer. It shows:
#    - Every collector that will run
#    - What data each collector gathers (e.g., "Scylla version via scylla --version")
#    - The scope (cluster-wide, per-cluster, per-node)
#    - The exact Kubernetes RBAC permissions required

# 3. If the customer wants to exclude specific collectors, discuss:
#    - Using a more targeted profile (e.g., --profile=health instead of --profile=full)
#    - Disabling individual collectors (e.g., --disable=ScyllaNodeLogsCollector)
```

#### Story 4: CX — guiding a customer through archive collection

> *I'm a Scylla CX team member and I need to explain how to collect a diagnostic archive from the cluster to a customer.*

```bash
# Provide the customer with these instructions:

# 1. Get the SODA binary (pick one)
docker pull scylladb/scylla-operator:latest
# — or —
curl -L -o scylla-operator <release-url> && chmod +x scylla-operator

# 2. Preview what will be collected (optional but recommended)
scylla-operator diagnose --dry-run --profile=full

# 3. Set up RBAC if needed (the --dry-run output provides the manifests)
kubectl apply -f soda-rbac.yaml

# 4. Collect the archive
scylla-operator diagnose --profile=full --archive \
  --namespace=<scylla-namespace> \
  --cluster-name=<cluster-name>

# 5. Send us the resulting .tar.gz file
#    (typically: scylla-diagnose-<timestamp>.tar.gz)
```

#### Story 5: CX — analyzing a customer archive offline

> *I'm a Scylla CX team member and I need to analyze the archive collected by a customer with SODA.*

```bash
# 1. Receive the .tar.gz archive from the customer

# 2. Run offline analysis — no cluster access needed
scylla-operator diagnose --from-archive=./customer-archive.tar.gz

# 3. SODA loads the vitals.json from the archive, reconstructs the cluster
#    topology, and runs all analyzers locally. The console report shows
#    PASS/WARNING/FAIL findings just like a live run.

# 4. For deeper investigation, inspect the archive contents directly:
#    - vitals.json    — structured data from all collectors
#    - report.json    — analysis results and metadata
#    - README.md      — human-readable index of all artifacts
#    - collectors/    — raw artifacts (logs, configs, manifests)
```

#### Story 6: Cluster admin — periodic health check

> *I'm a K8s cluster admin and I want to run regular health checks on my Scylla cluster to catch issues early.*

```bash
# 1. One-time setup: create a dedicated ServiceAccount for the health profile
scylla-operator diagnose --dry-run --profile=health
kubectl apply -f soda-health-rbac.yaml

# 2. Run periodically (e.g., via cron, CI pipeline, or a Kubernetes CronJob)
scylla-operator diagnose --profile=health --kubeconfig=./soda-sa.kubeconfig

# 3. Review findings — any WARNING or FAIL status indicates an issue to investigate.
#    The health profile is lightweight: it checks Scylla version support, schema
#    agreement, gossip health, topology consistency, and OS support without
#    collecting full manifests or logs.
```

---

## 9. Development Experience: Adding Collectors and Analyzers

A key design goal is making it cheap and straightforward to add new collectors and analyzers. This section demonstrates the developer experience with complete examples.

### 8.1 Example: Adding a Simple Collector

The following example shows a complete `PerScyllaNode` collector that retrieves the Scylla version from each pod:

```go
package collectors

import (
    "context"
    "fmt"
    "strings"

    "github.com/scylladb/scylla-operator/pkg/soda/engine"
    rbacv1 "k8s.io/api/rbac/v1"
)

const ScyllaVersionCollectorID engine.CollectorID = "ScyllaVersionCollector"

// Result struct — defines the structured data this collector produces.
type ScyllaVersionResult struct {
    Version string `json:"version"`
    Build   string `json:"build"`
    Raw     string `json:"raw"`
}

// Typed accessor — provides type-safe access from analyzers.
func GetScyllaVersionResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*ScyllaVersionResult, error) {
    return engine.GetResult[ScyllaVersionResult](vitals, ScyllaVersionCollectorID, podKey)
}

// Collector struct — embeds CollectorBase for metadata boilerplate.
type scyllaVersionCollector struct {
    engine.CollectorBase
}

// Constructor — wires up the metadata.
func NewScyllaVersionCollector() engine.PerScyllaNodeCollector {
    return &scyllaVersionCollector{
        CollectorBase: engine.NewCollectorBase(
            ScyllaVersionCollectorID,
            "Scylla version",
            engine.PerScyllaNode,
            nil, // no dependencies
        ),
    }
}

// RBAC declaration — every collector declares its required permissions.
func (c *scyllaVersionCollector) RBAC() []rbacv1.PolicyRule {
    return []rbacv1.PolicyRule{
        {APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
    }
}

// The actual collection logic — the only method you must write.
func (c *scyllaVersionCollector) CollectPerScyllaNode(
    ctx context.Context, params engine.PerScyllaNodeCollectorParams,
) (*engine.CollectorResult, error) {
    stdout, err := ExecInScyllaPod(ctx, params, []string{"scylla", "--version"})
    if err != nil {
        return nil, fmt.Errorf("executing scylla --version: %w", err)
    }

    raw := strings.TrimSpace(stdout)
    result := parseScyllaVersion(raw)

    var artifacts []engine.Artifact
    writeArtifact(params.ArtifactWriter, "scylla-version.log", []byte(stdout),
        "Raw scylla --version output", &artifacts)

    return &engine.CollectorResult{
        Status:    engine.CollectorPassed,
        Data:      result,
        Message:   result.Version,
        Artifacts: artifacts,
    }, nil
}
```

**Registration** (in `collectors/registry.go`):
```go
func AllCollectors() []engine.CollectorMeta {
    return []engine.CollectorMeta{
        // ... existing collectors ...
        NewScyllaVersionCollector(),
    }
}
```

**Add to a profile** (in `profiles/profiles.go`):
```go
Collectors: []engine.CollectorID{
    // ... existing collector IDs ...
    collectors.ScyllaVersionCollectorID,
},
```

That's it. The engine handles iteration over pods, concurrency, cascade, artifact writing, progress reporting, and serialization.

### 8.2 Example: Adding a Simple Analyzer

The following example shows a complete `PerScyllaCluster` analyzer that checks whether all nodes in a ScyllaDB cluster agree on the schema version:

```go
package analyzers

import (
    "fmt"
    "sort"
    "strings"

    "github.com/scylladb/scylla-operator/pkg/soda/collectors"
    "github.com/scylladb/scylla-operator/pkg/soda/engine"
)

const SchemaAgreementAnalyzerID engine.AnalyzerID = "SchemaAgreementAnalyzer"

type schemaAgreementAnalyzer struct {
    engine.AnalyzerBase
}

func NewSchemaAgreementAnalyzer() engine.PerScyllaClusterAnalyzer {
    return &schemaAgreementAnalyzer{
        AnalyzerBase: engine.NewAnalyzerBase(
            SchemaAgreementAnalyzerID,
            "Schema agreement check",
            engine.AnalyzerPerScyllaCluster,
            []engine.CollectorID{collectors.SchemaVersionsCollectorID}, // declares dependency
        ),
    }
}

// The analysis logic — reads from Vitals using typed accessors.
func (a *schemaAgreementAnalyzer) AnalyzePerScyllaCluster(
    params engine.PerScyllaClusterAnalyzerParams,
) *engine.AnalyzerResult {
    allVersions := make(map[string][]string) // schema UUID → list of pod keys
    podsChecked := 0

    for _, podKey := range params.Vitals.ScyllaNodeKeys() {
        schemaResult, err := collectors.GetSchemaVersionsResult(params.Vitals, podKey)
        if err != nil {
            continue // Skip pods where the collector didn't pass.
        }
        podsChecked++
        for _, entry := range schemaResult.Versions {
            allVersions[entry.SchemaVersion] = append(
                allVersions[entry.SchemaVersion], podKey.String())
        }
    }

    if podsChecked == 0 {
        return &engine.AnalyzerResult{
            Status:  engine.AnalyzerWarning,
            Message: "No schema version information available",
        }
    }

    if len(allVersions) == 1 {
        var uuid string
        for k := range allVersions { uuid = k }
        return &engine.AnalyzerResult{
            Status:  engine.AnalyzerPassed,
            Message: fmt.Sprintf("Schema agreement reached: all nodes report version %s", uuid),
        }
    }

    // Multiple schema versions → disagreement.
    versions := make([]string, 0, len(allVersions))
    for v := range allVersions { versions = append(versions, v) }
    sort.Strings(versions)

    details := make([]string, 0, len(versions))
    for _, v := range versions {
        pods := allVersions[v]
        sort.Strings(pods)
        details = append(details, fmt.Sprintf("%s (reported by %s)", v, strings.Join(pods, ", ")))
    }

    return &engine.AnalyzerResult{
        Status:  engine.AnalyzerFailed,
        Message: fmt.Sprintf("Schema disagreement: %d versions found: %s",
            len(allVersions), strings.Join(details, "; ")),
    }
}
```

**Registration** (in `analyzers/registry.go`) and **profile inclusion** follow the same pattern as collectors.

---

## 10. Relationship to must-gather

SODA is the long-term replacement for must-gather. The migration path:

1. **Release alongside must-gather.** SODA ships as `scylla-operator diagnose` in the same binary. Both tools are available.
2. **Deprecate must-gather.** After SODA's `full` profile covers everything must-gather collects (and more), must-gather is marked as deprecated with a warning message directing users to `scylla-operator diagnose`.
3. **Remove must-gather.** After a few releases of deprecation, must-gather is removed from the codebase.

SODA's `full` profile already covers all the Kubernetes manifest collection that must-gather performs, plus Scylla-internal data that must-gather never collected. Importantly, SODA is explicit about what it collects — every collector declares its scope and RBAC requirements, and the tool only accesses namespaces and resources that are ScyllaDB-related. Unlike must-gather, which collected manifests, logs, and potentially secrets from cluster namespaces indiscriminately, SODA does not touch namespaces or resources that are not relevant to ScyllaDB diagnostics.

---

## 11. Relationship to Scylla Doctor

SODA is inspired by Scylla Doctor's proven architecture. The collector/analyzer/vitals model, the concept of typed results with dependency-driven execution, and the idea of structured diagnostic output all draw from Scylla Doctor's design.

**Scylla Doctor remains the right tool for bare-metal and VM deployments.** Its deep integration with systemd, SSH, host-level networking, and Linux tuning utilities (perftune, hwloc, etc.) makes it uniquely suited for environments where those facilities are available.

**SODA is purpose-built for Kubernetes** — where Scylla Doctor's core assumptions do not hold. Containers do not have SSH access, systemd, meaningful host NIC enumeration, or direct hardware introspection. Kubernetes introduces its own abstractions (CRDs, operators, StatefulSets, PVCs, RBAC, scheduling constraints) that require native understanding.

**Feature parity with Scylla Doctor's Kubernetes-relevant checks** (~26 analyzers that would make sense to run in K8s) is the first milestone after PoC. This ensures that users migrating from Scylla Doctor-assisted support workflows to SODA do not lose diagnostic coverage.

**Acknowledged tradeoff:** Scylla-native collectors and analyzers (those that query CQL system tables, REST APIs, or examine Scylla configuration) exist in both projects. When a new Scylla-native check is added to Scylla Doctor, it would need to be independently implemented in SODA. The low per-collector cost (~30-80 lines of Go) and the infrequency of such additions make this manageable. The benefit — full ownership, no cross-project coupling, and the best Kubernetes-native experience — outweighs this cost.

---

## 12. Current PoC Status

The PoC implementation in `pkg/soda/` is substantially complete and has been validated through manual and agent-assisted testing:

### Collectors (35 total)

| Scope | Count | Examples |
|-------|-------|---------|
| **PerScyllaNode** | 12 | OSInfo, ScyllaVersion, SchemaVersions, ScyllaConfig, SystemPeersLocal, GossipInfo, SystemTopology, SystemConfig (CQL), ScyllaDConfig, DiskUsage, Rlimits, ScyllaNodeLogs |
| **PerScyllaCluster** | 10 | ScyllaClusterStatefulSet, ScyllaClusterService, ScyllaClusterConfigMap, ScyllaClusterPod, ScyllaClusterPDB, ScyllaClusterServiceAccount, ScyllaClusterRoleBinding, ScyllaClusterPVC, ScyllaClusterJobLogs |
| **ClusterWide** | 13 | NodeResources, NodeManifest, ScyllaCluster, ScyllaDBDatacenter, NodeConfig, ScyllaOperatorConfig, Deployment, StatefulSet, DaemonSet, ConfigMap, Service, ServiceAccount, PodManifest, OperatorPodLogs |

### Analyzers (5 total)

All `PerScyllaCluster` scope:
- `ScyllaVersionSupportAnalyzer` — Checks Scylla versions against known-supported ranges.
- `SchemaAgreementAnalyzer` — Verifies all nodes agree on schema version.
- `OSSupportAnalyzer` — Checks OS distribution is supported.
- `GossipHealthAnalyzer` — Verifies all gossip endpoints are UP/NORMAL.
- `TopologyHealthAnalyzer` — Checks Raft topology status consistency.

### Profiles (3)

`full`, `health`, `logs` — as described in [Section 6.10](#610-profiles).

### Engine

- Full concurrent execution with `errgroup` and bounded parallelism.
- Topological sort with cycle detection.
- Cascade logic (skip/fail propagation).
- `OfflineRun()` for from-archive mode.
- `SerializableVitals` with full JSON round-trip.
- `SerializableClusterTopology` for offline topology reconstruction.
- Progress event system (`OnCollectorEvent` callback).
- RBAC aggregation.

### Output

- Console writer (colored, with duration).
- JSON report writer.
- README.md index generator.
- vitals.json serialization.

### Archive System

- Filesystem-backed `WriterFactory` and `Reader`.
- `tar.gz` creation and extraction with path traversal protection.

### CLI Integration

- `scylla-operator diagnose` Cobra command with all flags.
- Signal handling and graceful cancellation.
- Dry-run mode.
- Offline re-analysis mode.

### Development Notes

- The PoC was developed with significant help from OpenCode agents in a multi-step workflow (gap analysis, hybrid PoC, complete rewrite requirements, implementation plan, PoC implementation, interface cleanup, post-review improvements).
- The PoC has gone through initial phases of review, but requires additional review and cleanup before release.
- **Automated E2E tests are required before release.** Only manual and agent-assisted testing was performed during the PoC phase. Integration tests covering collector execution, engine orchestration, archive round-trip, and offline mode need to be written.

---

## 13. Milestones

| # | Milestone | Description |
|---|-----------|-------------|
| 0 | **Review and consensus** | Review the proposed approach and reach consensus on the direction, especially with the CX team. Align on scope, priorities, and integration points. |
| 1 | **PoC cleanup and review** | Finalize code quality, address remaining review feedback, clean up interfaces. Ensure all collectors and analyzers follow consistent patterns. |
| 2 | **Automated E2E tests** | Integration tests covering collector execution (with fake K8s clients), engine orchestration, archive round-trip, offline mode, profile resolution edge cases. |
| 3 | **Feature parity with Scylla Doctor (K8s-relevant)** | Implement the remaining ~29 feasible collectors and ~21 applicable analyzers from Scylla Doctor's inventory. See [Appendix A](#appendix-a-scylla-doctor-collector-and-analyzer-mapping) for the full mapping. |
| 4 | **must-gather deprecation** | Release SODA as the primary diagnostic tool. Add deprecation warning to must-gather directing users to `scylla-operator diagnose`. |
| 5 | **must-gather removal** | Remove must-gather from the codebase after a few releases of deprecation. |
| 6 | **Kubernetes-specific analyzers** | Add analyzers that go beyond Scylla Doctor's scope. See [Appendix B](#appendix-b-kubernetes-specific-collectors-and-analyzers) for examples of proposed collectors and analyzers. |

---

## Appendix A: Scylla Doctor Collector and Analyzer Mapping

This appendix provides a complete mapping of all Scylla Doctor collectors and analyzers, assessing Kubernetes feasibility and current SODA implementation status.

### A.1 Scylla Doctor Collectors

#### CQL-Based Collectors

| Scylla Doctor Collector | K8s Feasible | SODA Equivalent | Status | Notes |
|------------------------|:---:|------|--------|-------|
| `CqlshCollector` | Yes | — | Not yet | CQL connectivity test |
| `ClientConnectionCollector` | Yes | — | Not yet | `system.clients` table |
| `ScyllaClusterSchemaDescriptionCollector` | Yes | — | Not yet | `DESC SCHEMA` output |
| `ScyllaClusterSystemKeyspacesCollector` | Yes | — | Not yet | `system_schema.keyspaces` |
| `ScyllaClusterTablesDescriptionCollector` | Yes | — | Not yet | `system_schema.tables` |
| `SystemPeersLocalCollector` | Yes | `SystemPeersLocalCollector` | Implemented | `system.local` + `system.peers` |
| `SystemClusterStatusCollector` | Yes | — | Not yet | `system.cluster_status` |
| `SystemTopologyCollector` | Yes | `SystemTopologyCollector` | Implemented | `system.topology` |
| `SystemConfigCollector` | Yes | `SystemConfigCollector` | Implemented | `system.config` |
| `RaftGroup0Collector` | Yes | — | Not yet | Raft group0 state |
| `ScyllaClusterSchemaCollector` | Yes | `SchemaVersionsCollector` | Implemented | Schema versions via REST API |

#### REST API-Based Collectors

| Scylla Doctor Collector | K8s Feasible | SODA Equivalent | Status | Notes |
|------------------------|:---:|------|--------|-------|
| `ScyllaClusterStatusCollector` | Yes | — | Not yet | Cluster live/dead/joining/leaving |
| `GossipInfoCollector` | Yes | `GossipInfoCollector` | Implemented | Gossip endpoint info |
| `TokenMetadataHostsMappingCollector` | Yes | — | Not yet | Token-to-host mapping |
| `RaftTopologyRPCStatusCollector` | Yes | — | Not yet | Raft topology RPC status |
| `ScyllaTablesUsedDiskCollector` | Yes | — | Not yet | Per-table disk usage |
| `ScyllaTablesCompressionInfoCollector` | Yes | — | Not yet | Per-table compression ratio |

#### Scylla Config Collectors

| Scylla Doctor Collector | K8s Feasible | SODA Equivalent | Status | Notes |
|------------------------|:---:|------|--------|-------|
| `ScyllaConfigurationFileCollector` | Yes | `ScyllaConfigCollector` | Implemented | Effective scylla.yaml |
| `ScyllaConfigurationFileNoParsingCollector` | Yes | `ScyllaConfigCollector` | Implemented | Raw scylla.yaml |
| `ScyllaExtraConfigurationFilesCollector` | Yes | `ScyllaDConfigCollector` | Implemented | `/etc/scylla.d/*` overrides |
| `ScyllaSystemConfigurationFilesCollector` | Yes | — | Not yet | `/etc/sysconfig/scylla-server` |
| `ScyllaVersionCollector` | Yes | `ScyllaVersionCollector` | Implemented | `scylla --version` output |
| `ScyllaLimitNOFILECollector` | Partial | `RlimitsCollector` | Implemented | SODA collects all rlimits via `prlimit` |

#### System/Hardware Collectors

| Scylla Doctor Collector | K8s Feasible | SODA Equivalent | Status | Notes |
|------------------------|:---:|------|--------|-------|
| `CPUSpecificationsCollector` | Yes | — | Not yet | `lscpu` / `/proc/cpuinfo` |
| `RAMCollector` | Yes | — | Not yet | `/proc/meminfo`, `free` |
| `SwapCollector` | Yes | — | Not yet | `/proc/swaps` |
| `ComputerArchitectureCollector` | Yes | `NodeResourcesCollector` | Partial | K8s Node has arch/kernel info |
| `OSCollector` | Yes | `OSInfoCollector` | Implemented | `/etc/os-release`, `uname` |
| `ClockSourceCollector` | Partial | — | Not yet | `sysfs` clocksource — host kernel dependent |
| `SELinuxCollector` | Yes | — | Not yet | `/etc/selinux/config` |
| `SysctlCollector` | Yes | — | Not yet | `sysctl` values — sees host kernel values |
| `HypervisorTypeCollector` | Partial | — | Not yet | `systemd-detect-virt` may not be available |
| `ProcInterruptsCollector` | Partial | — | Not yet | `/proc/interrupts` — host view |
| `RAIDSetupCollector` | Partial | — | Not yet | `/proc/mdstat` |
| `KernelRingBufferCollector` | Partial | — | Not yet | `dmesg` — requires privileges |
| `InfrastructureProviderCollector` | Partial | — | Not yet | Cloud metadata may be inaccessible |
| `NodePlatformCollector` | Yes | — | Not yet | Always "container" in K8s |

#### Tuning/Meta Collectors

| Scylla Doctor Collector | K8s Feasible | SODA Equivalent | Status | Notes |
|------------------------|:---:|------|--------|-------|
| `SeastarCPUMapCollector` | Yes | — | Not yet | `seastar-cpu-map.sh` |
| `NodetoolCFStatsCollector` | Yes | — | Not yet | `nodetool cfstats` |
| `ScyllaSeedsCollector` | Yes | — | Not yet | Seed connectivity |
| `ScyllaBinaryCollector` | Yes | — | Not yet | `which scylla` |
| `PathsCollector` | Yes | — | Not yet | Standard paths |
| `SDVersionCollector` | Yes | — | Not yet | Maps to SODA version |
| `ScyllaSSTablesCollector` | Yes | — | Not yet | SSTable file listing |

#### Infeasible in Kubernetes (Skip)

| Scylla Doctor Collector | Reason |
|------------------------|--------|
| `CPUScalingCollector` | Requires systemd + sysfs cpufreq |
| `CPUSetCollector` | Requires `hwloc-calc`, `perftune.py` |
| `PerftuneSystemConfigurationCollector` | Requires `perftune.py --dry-run` |
| `PerftuneYamlDefaultCollector` | Requires `perftune.py` |
| `CoredumpCollector` | Requires systemd mount service |
| `RsyslogCollector` | N/A in containers |
| `StorageConfigurationCollector` | Requires `perftune.py`, sees PVC paths not devices |
| `ScyllaServicesCollector` | Requires systemd |
| `ServiceManagerCollector` | Detects systemd / supervisord |
| `NTPStatusCollector` | Requires `timedatectl` |
| `NTPServicesCollector` | Requires systemd |
| `ScyllaLogsCollector` | Uses `journalctl` (K8s equivalent: `ScyllaNodeLogsCollector`) |
| `NICsCollector` | Container NICs are not host NICs |
| `IPAddressesCollector` | Container IPs are not meaningful for host-level diagnosis |
| `IPRoutesCollector` | Container routing is not meaningful |
| `FirewallRulesCollector` | Container iptables are meaningless |
| `TCPConnectionsCollector` | Container-level only |
| `MaintenanceEventsCollector` | Cloud metadata inaccessible from pods |
| `LSPCICollector` | `lspci` not available in containers |

**Summary:** Of 63 Scylla Doctor collectors, 42 are feasible in Kubernetes, 2 are partially feasible, and 19 are infeasible. Of the 42 feasible, 13 have SODA equivalents implemented.

### A.2 Scylla Doctor Analyzers

The analysis.md gap analysis identified ~58 Scylla Doctor analyzers. Their Kubernetes feasibility breaks down as follows:

#### Analyzers That Would Make Sense to Run in K8s (~26)

| Analyzer Area | SODA Equivalent | Status |
|--------------|-----------------|--------|
| Schema agreement | `SchemaAgreementAnalyzer` | Implemented |
| Raft health | — | Not yet |
| Raft topology status | `TopologyHealthAnalyzer` | Implemented |
| Gossip consistency | `GossipHealthAnalyzer` | Implemented |
| Topology consistency | — | Not yet |
| Scylla version support | `ScyllaVersionSupportAnalyzer` | Implemented |
| Scylla config: listen address | — | Not yet |
| Scylla config: broadcast address | — | Not yet |
| Scylla config: RPC address | — | Not yet |
| Scylla config: internode compression | — | Not yet |
| Scylla config: deprecated arguments | — | Not yet |
| Scylla config: NIC/disk setup flags | — | Not yet |
| Scylla config: file format | — | Not yet |
| Scylla config: consistency with in-memory | — | Not yet |
| IO setup | — | Not yet |
| Memory tuning | — | Not yet |
| Kernel version | — | Not yet |
| OS support | `OSSupportAnalyzer` | Implemented |
| Architecture support | — | Not yet |
| SSTable format | — | Not yet |
| Seed connectivity | — | Not yet |
| System keyspace replication | — | Not yet |
| Developer mode detection | — | Not yet |
| Raft group0 state | — | Not yet |
| Token ring consistency | — | Not yet |
| Compression ratio | — | Not yet |

**Status:** 5 of ~26 applicable analyzers have SODA equivalents.

#### Analyzers That Would Auto-Skip in K8s (~25)

These depend on infeasible collectors and are not applicable: CPU scaling, CPU set, perftune tuning, NTP, NIC speed, RAID, rsyslog, storage type, swap, XFS filesystem, coredump, systemd services, and related checks.

#### Analyzers That Would Run With Caveats (~3)

- RAM analyzer (sees container memory limits, not host RAM).
- sysctl analyzers (sees host kernel values through `/proc/sys`).
- Storage-RAM ratio (partial view through PVC).

#### Analyzers Not Applicable in K8s (~3)

- SD version match (Scylla Doctor version — irrelevant for SODA).
- NTP status (no `timedatectl` in containers).
- Driver version check (SODA does not use any driver — not applicable).

---

## Appendix B: Kubernetes-Specific Collectors and Analyzers

SODA can include diagnostic modules that Scylla Doctor has no concept of — leveraging Kubernetes-native data sources.

### B.1 Kubernetes-Exclusive Collectors (Already Implemented)

The PoC includes 22 collectors that are purely Kubernetes-native:

| Collector | Scope | Description |
|-----------|-------|-------------|
| `NodeResourcesCollector` | ClusterWide | K8s Node capacity, allocatable resources, conditions |
| `NodeManifestCollector` | ClusterWide | Full K8s Node YAML manifests |
| `ScyllaClusterCollector` | ClusterWide | ScyllaCluster CRD manifests |
| `ScyllaDBDatacenterCollector` | ClusterWide | ScyllaDBDatacenter CRD manifests |
| `NodeConfigCollector` | ClusterWide | NodeConfig CRD manifests |
| `ScyllaOperatorConfigCollector` | ClusterWide | ScyllaOperatorConfig manifests |
| `DeploymentCollector` | ClusterWide | Deployment manifests (operator namespaces) |
| `StatefulSetCollector` | ClusterWide | StatefulSet manifests |
| `DaemonSetCollector` | ClusterWide | DaemonSet manifests |
| `ConfigMapCollector` | ClusterWide | ConfigMap manifests |
| `ServiceCollector` | ClusterWide | Service manifests |
| `ServiceAccountCollector` | ClusterWide | ServiceAccount manifests |
| `PodManifestCollector` | ClusterWide | Pod manifests (operator namespaces) |
| `OperatorPodLogsCollector` | ClusterWide | Operator pod container logs |
| `ScyllaClusterStatefulSetCollector` | PerScyllaCluster | Child StatefulSets of each ScyllaCluster |
| `ScyllaClusterServiceCollector` | PerScyllaCluster | Child Services |
| `ScyllaClusterConfigMapCollector` | PerScyllaCluster | Child ConfigMaps |
| `ScyllaClusterPodCollector` | PerScyllaCluster | Child Pods |
| `ScyllaClusterPDBCollector` | PerScyllaCluster | Child PodDisruptionBudgets |
| `ScyllaClusterServiceAccountCollector` | PerScyllaCluster | Child ServiceAccounts |
| `ScyllaClusterRoleBindingCollector` | PerScyllaCluster | Child RoleBindings |
| `ScyllaClusterPVCCollector` | PerScyllaCluster | Child PersistentVolumeClaims |

### B.2 Proposed Kubernetes-Specific Analyzers

The following are examples of analyzers that would leverage Kubernetes-native data that Scylla Doctor has no access to. This list is not intended to be exhaustive — new Kubernetes-specific analyzers can be added as diagnostic needs are identified:

| Analyzer | Scope | Description |
|----------|-------|-------------|GO_TEST_KIND_E2E_ARGS
| **Node tuning validation** | PerScyllaCluster | Verify that NodeConfig has been applied correctly to all nodes running Scylla pods. Check that sysctl values match expected tuning profile. |
| **Resource requests/limits** | PerScyllaCluster | Check that Scylla pods have appropriate CPU and memory requests/limits set. Flag under-provisioned or unset resources. |
| **PVC health** | PerScyllaCluster | Verify all PVCs are Bound, using the correct storage class, and have sufficient capacity. Flag PVCs in Pending or Lost state. |
| **Operator version compatibility** | ClusterWide | Check operator version against ScyllaCluster API version for known compatibility issues. |
| **CRD configuration validation** | PerScyllaCluster | Detect common misconfigurations in ScyllaCluster/ScyllaDBDatacenter spec (e.g., anti-affinity not set, developer mode enabled in production, insufficient resources for the workload). |
| **ScyllaCluster status conditions** | PerScyllaCluster | Analyze ScyllaCluster status conditions for degraded states, reconciliation errors, or stalled rollouts. |
| **Pod scheduling analysis** | PerScyllaCluster | Check anti-affinity rules, topology spread constraints, and node selectors. Flag pods co-located on the same node or failure domain. |
| **Network policy analysis** | PerScyllaCluster | Detect missing or overly restrictive network policies that might block inter-node or client-to-node communication. |
| **Operator health** | ClusterWide | Check operator Deployment status (available replicas, rollout progress, restart counts, OOMKills). |
| **Image version consistency** | PerScyllaCluster | Verify all Scylla pods are running the same container image. Flag mixed-version deployments. |
