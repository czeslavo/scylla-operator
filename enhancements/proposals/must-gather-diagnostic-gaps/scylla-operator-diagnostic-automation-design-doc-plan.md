# Plan: SODA Design Document

This document outlines the structure and content strategy for `scylla-operator-diagnostic-automation-design-doc.md`.

**Target audience:** Cross-team/organizational (operator team, Scylla Doctor team, CX, management).

**Tone:** Professional and diplomatic. Frame the recommendation as what's best for the K8s diagnostic experience, not a rejection of Scylla Doctor. Acknowledge Scylla Doctor as architectural inspiration.

---

## Document Outline

### 1. Title & Metadata

- **Title:** "SODA: Scylla Operator Diagnostic Automation — Design Document"
- **Subtitle:** A Kubernetes-native diagnostic framework for ScyllaDB clusters, integrated as `scylla-operator diagnose`
- **Authors, date, status** (Draft/Proposed/Accepted)

### 2. Executive Summary

~1 paragraph. SODA is a diagnostic framework built natively into the Scylla Operator that replaces `must-gather` with a structured, analyzable, and extensible diagnostic workflow. Inspired by Scylla Doctor's architecture, it brings similar diagnostic depth to Kubernetes while adding K8s-native data collection and analysis.

### 3. Motivation

Explain **why** this effort is needed. Cover these specific points:

1. **must-gather is a shallow data dump** — collects files but performs no analysis. Users receive a tarball with no guidance on what to look for or what's wrong.
2. **must-gather doesn't collect system tables** — CQL system tables (`system.peers`, `system_schema.*`, gossip info) are critical for investigating cluster state issues like rejected cluster joins, but must-gather doesn't touch them.
3. **must-gather doesn't collect the effective scylla.yaml** — users can't see what configuration Scylla is actually running with; only the input ConfigMap is collected, not what the container has merged and is using.
4. **Arbitrary, non-semantic collection scope** — must-gather collects a fixed grab-bag of resources with no organizing principle. Users can't choose to collect "everything about cluster topology" or "everything about storage health." The collection doesn't align with the needs of specific investigation scenarios.
5. **Overlap and gaps between must-gather and Scylla Doctor** — some data is collected redundantly by both tools, while other important diagnostic data falls through the cracks of both. Neither tool covers the full picture for a K8s-hosted ScyllaDB cluster.
6. **Scylla Doctor doesn't work in Kubernetes** — it's designed for bare-metal/VM deployments. ~19 of its 63 collectors are infeasible inside containers, and it has no awareness of Kubernetes resources (CRDs, operators, StatefulSets, PVCs, etc.).
7. **Opportunity for semantic, modular design** — designing the diagnostic tool around semantic areas (topology, configuration, storage, health, etc.) serves two causes:
   - (a) Users can mix & match scope and depth relevant to their specific case, making collected data feel less arbitrary and more aligned with investigation needs.
   - (b) Modular design makes it cheap for the operator team to add modules (especially with LLMs + robust engine), and opens the possibility for CX to develop additional diagnostic modules in the future.

### 4. Goals

- Provide a single `scylla-operator diagnose` command that collects data AND analyzes it, producing actionable findings.
- Collect diagnostic data from Kubernetes resources, Scylla REST API, CQL system tables, and container internals — covering more ground than either must-gather or Scylla Doctor alone.
- Organize collection around semantic diagnostic areas (profiles) so users can tailor scope to their investigation.
- Produce a structured, reproducible archive suitable for offline analysis, sharing with support, and (in the future) LLM-assisted investigation.
- Make adding new collectors and analyzers cheap and straightforward — show example implementations.
- Eventually replace `must-gather` (deprecated for a few releases, then removed).
- Reach feature parity with Scylla Doctor's K8s-relevant checks as the first milestone after PoC.

### 5. Non-Goals

- Not a replacement for Scylla Doctor on bare-metal/VM deployments.
- Not a real-time monitoring or alerting system.
- No web UI or dashboard.
- No auto-remediation — diagnose and report only.
- CX extensibility (third-party modules) is a potential future direction but not an immediate requirement. The focus is on making built-in profiles comprehensive and making internal extensibility cheap for the operator team.

### 6. Approaches Considered

Present three approaches briefly with pros/cons, then recommend one.

#### 6.1 Approach A: Complete Rewrite (Recommended)

Build an independent diagnostic framework natively in Go within the scylla-operator codebase, inspired by Scylla Doctor's collector/analyzer architecture.

**Pros:**
- Best K8s-native experience — natural access to the K8s API, CRD types, operator internals.
- Full ownership and control — operator team can iterate independently without cross-project coupling or release coordination.
- Adding collectors/analyzers is cheap — the PoC proves this with a robust engine + scope-specific interfaces + base embeddings. With LLM assistance and the existing framework, new modules can be added quickly.
- Single binary, single workflow — no multi-tool orchestration.
- Tailored output — archive format, README index, and report structure optimized for K8s diagnostic scenarios and future LLM analysis.

**Cons:**
- If a new Scylla-native (non-K8s-specific) collector or analyzer is added to Scylla Doctor, it would need to be independently implemented in SODA as well. However, these additions are infrequent and the implementation cost is low given the framework.

#### 6.2 Approach B: Hybrid Pipeline (must-gather + vitals converter + Scylla Doctor)

Adapt must-gather to produce Scylla Doctor-compatible vitals JSON, then feed it into Scylla Doctor for analysis.

**Pros:**
- Reuses Scylla Doctor's existing ~58 analyzers.
- Lower initial analyzer implementation effort.

**Cons:**
- Two separate tools to orchestrate — worse UX.
- Vitals format coupling — changes to Scylla Doctor's internal data model require converter updates.
- Cross-team release coordination required.
- Many Scylla Doctor analyzers auto-skip in K8s anyway (~25 of ~58).
- K8s-specific analysis still needs to be built from scratch.
- Validated in PoC: 8/63 collectors implemented, proved feasible but brittle and complex.

#### 6.3 Approach C: Extend Scylla Doctor with K8s Support

Add Kubernetes-aware collectors and analyzers directly to Scylla Doctor's Python codebase.

**Pros:**
- Single tool for both bare-metal and K8s.
- Shared analyzer logic.

**Cons:**
- Scylla Doctor is Python; operator team works in Go. Cross-language, cross-repo contributions create friction.
- Scylla Doctor's architecture is designed around SSH access to nodes, not K8s API access.
- Would require significant refactoring of Scylla Doctor to support K8s abstractions (namespaces, CRDs, pods vs. nodes, etc.).
- Ownership and release coordination challenges.
- Harder to integrate tightly with operator (e.g., sharing CRD types, using operator's K8s client setup).

#### 6.4 Recommendation

**Approach A (Complete Rewrite)** is recommended. The PoC has already validated the architecture and demonstrated that the implementation cost is manageable. The operator team gets full control, better K8s integration, and can iterate faster. The main tradeoff (duplicating Scylla-native collectors/analyzers) is acceptable given the low per-module cost and the infrequency of additions.

### 7. Proposed Architecture

This is the main technical section. Cover:

#### 7.1 High-Level Architecture Diagram

ASCII or description: CLI command -> Engine -> discovers topology -> resolves profiles -> runs Collectors (concurrent, scope-aware) -> stores Vitals -> runs Analyzers (concurrent, scope-aware, reading from Vitals) -> produces Report + Archive.

#### 7.2 Core Concepts

- **Collectors** — gather raw diagnostic data from various sources (K8s API, CQL, REST API, exec into containers). Scoped: ClusterWide, PerScyllaCluster, PerScyllaNode.
- **Analyzers** — examine collected data (Vitals) and produce diagnostic findings (PASS/WARN/FAIL/SKIPPED). Scoped similarly.
- **Vitals** — the in-memory store of all collected data, keyed by collector ID and scope (cluster name, node name). Fully serializable to JSON for archive round-trip.
- **Artifacts** — raw files produced by collectors alongside structured data (logs, config files, manifests). Stored in the archive filesystem.
- **Profiles** — named sets of collectors and analyzers that define what to run. Users select a profile (e.g., `full`, `health`, `logs`) or can combine them.
- **Engine** — orchestrates the entire workflow: topology discovery, profile resolution, concurrent execution with dependency tracking, cascade logic (skip/fail propagation), progress reporting.

#### 7.3 Scope Model

Explain the three scopes and how the engine iterates:
- `ClusterWide` — runs once per diagnostic run.
- `PerScyllaCluster` — runs once per ScyllaCluster/ScyllaDBDatacenter discovered.
- `PerScyllaNode` — runs once per Scylla pod in each cluster.

Show how scope-specific interfaces (e.g., `PerScyllaNodeCollector`) receive only the context they need (node name, pod reference, etc.).

#### 7.4 Engine Execution Model

1. Topology discovery (find all ScyllaClusters, enumerate pods).
2. Profile resolution (merge requested profiles, deduplicate collectors/analyzers).
3. Collector execution — concurrent within scope, bounded parallelism, cascade on failure.
4. Analyzer execution — concurrent, read from Vitals, produce findings.
5. Report generation — console output, JSON, archive with index.

Cover: `errgroup` concurrency, `OnCollectorEvent` progress callbacks, `AnalyzerResult` (status + message + detail), RBAC provider interface.

#### 7.5 Collector & Analyzer Interfaces

Show the actual Go interfaces (or simplified versions) for:
- `ClusterWideCollector`, `PerScyllaClusterCollector`, `PerScyllaNodeCollector`
- `ClusterWideAnalyzer`, `PerScyllaClusterAnalyzer`
- `CollectorBase` / `AnalyzerBase` embeddings
- `GetResult[T]` generic accessor

**Include a simple example collector and a simple example analyzer** to demonstrate how cheap it is to add new ones. This is the DevEx showcase.

#### 7.6 Vitals Store & Serialization

- In-memory structure: nested maps keyed by scope.
- `SerializableVitals` — full JSON round-trip enabling offline mode.
- `SerializableClusterTopology` — reconstructs topology from archive.
- Type registry for deserialization.

#### 7.7 Archive Format

Describe the output directory structure:
```
soda-archive-<timestamp>/
  vitals.json              # Serialized Vitals (all structured data)
  report.json              # Analysis results
  README.md                # Human+LLM-readable index
  artifacts/
    cluster-wide/
      <collector-id>/      # Raw artifacts
    per-scylla-cluster/
      <namespace>/<cluster>/
        <collector-id>/
    per-scylla-node/
      <namespace>/<cluster>/<node>/
        <collector-id>/
```

#### 7.8 Offline / From-Archive Mode

- `scylla-operator diagnose --from-archive=<path>` loads vitals.json and re-runs analyzers without K8s access.
- Enables: sharing archives with support, re-analysis with updated analyzers, future LLM-assisted analysis.

#### 7.9 README.md as LLM Context Source (Future Direction)

Describe the vision:
- README.md is not just a file listing — it's a structured context document for both humans and AI agents.
- Could include: links to relevant ScyllaDB documentation, links to Scylla Operator and ScyllaDB source code (at the specific versions running in the cluster), explanation of how to approach analyzing the archive, what each section contains and what to look for.
- Combined with the archive contents, this provides rich context for LLM-assisted diagnostic analysis.

#### 7.10 Profiles

List the current profiles and their contents:
- `full` — all collectors + all analyzers (default)
- `health` — REST API + CQL collectors + all analyzers (quick health check)
- `logs` — log collectors only (for log-focused investigation)

Discuss how profiles map to semantic areas and how users can combine them via `--profile=health,logs`.

#### 7.11 RBAC

- Collectors can declare required RBAC rules via an optional interface.
- The engine can aggregate and report required permissions.
- Supports dry-run mode to show what permissions would be needed.

### 8. CLI Integration

Show usage examples:

```bash
# Full diagnostic run
scylla-operator diagnose --kubeconfig=... --profile=full

# Health check only
scylla-operator diagnose --profile=health

# Collect logs for a specific cluster
scylla-operator diagnose --profile=logs --scylla-cluster=my-cluster

# Offline re-analysis
scylla-operator diagnose --from-archive=./soda-archive-20250401/

# Dry run (show what would be collected, RBAC needed)
scylla-operator diagnose --dry-run --profile=full
```

Show example console output (colored findings, PASS/WARN/FAIL with detail).

### 9. Development Experience: Adding Collectors & Analyzers

This section serves as a showcase of how cheap it is to add new modules.

#### 9.1 Example: Adding a Simple Collector

Walk through implementing a hypothetical collector (e.g., a new CQL system table collector or a K8s resource collector). Show:
- Struct with `CollectorBase` embedding
- Implementing the scope-specific interface
- Registering in the registry
- Adding to a profile

Should be ~30-50 lines of Go.

#### 9.2 Example: Adding a Simple Analyzer

Walk through implementing a hypothetical analyzer. Show:
- Struct with `AnalyzerBase` embedding
- Using `GetResult[T]` to access collector data
- Returning `AnalyzerResult` with status and message
- Registering and adding to a profile

Should be ~30-50 lines of Go.

### 10. Relationship to must-gather

- SODA is the long-term replacement for must-gather.
- Migration plan: release SODA alongside must-gather, deprecate must-gather, remove after a few releases.
- SODA's `full` profile covers everything must-gather collects (and much more).

### 11. Relationship to Scylla Doctor

Frame diplomatically:
- SODA is inspired by Scylla Doctor's proven architecture (collectors, analyzers, vitals).
- Scylla Doctor remains the right tool for bare-metal/VM deployments.
- SODA is purpose-built for the Kubernetes context where Scylla Doctor's assumptions (SSH access, systemd, host-level networking) don't apply.
- Reaching feature parity with Scylla Doctor's K8s-relevant checks (~27 analyzers) is the first milestone after PoC.
- **Acknowledged tradeoff:** Scylla-native (non-K8s) collectors/analyzers added to Scylla Doctor will need independent implementation in SODA. The low per-module cost and robust framework make this manageable.

### 12. Current PoC Status

Summarize what exists today:
- **34 collectors** across 3 scopes (12 PerScyllaNode, 10 PerScyllaCluster, 14 ClusterWide — including exec-based, K8s API, and log collectors).
- **5 analyzers** (ScyllaVersionSupport, SchemaAgreement, OSSupport, GossipHealth, TopologyHealth).
- **3 profiles** (full, health, logs).
- **Full engine** with concurrent execution, cascade logic, offline mode, progress events, RBAC aggregation.
- **Archive system** with tar.gz, vitals serialization round-trip, README.md index.
- **CLI integration** as `scylla-operator diagnose` Cobra command.
- Note: PoC was developed with significant help from OpenCode agents (in a multi-step workflow), went through initial review phases, but requires further review and cleanup before release.
- E2E automated tests are required before release — only manual and agentic testing was performed during the PoC phase.

### 13. Milestones

1. **PoC cleanup & review** — finalize code quality, address remaining review feedback, clean up interfaces.
2. **Automated E2E tests** — integration tests covering collector execution, engine orchestration, archive round-trip, offline mode.
3. **Feature parity with Scylla Doctor (K8s-relevant)** — implement the remaining ~29 feasible collectors and ~22 applicable analyzers.
4. **must-gather deprecation** — release SODA as the primary diagnostic tool, deprecate must-gather.
5. **must-gather removal** — after a few releases of deprecation.
6. **K8s-specific analyzers** — add analyzers that Scylla Doctor doesn't have (e.g., node tuning validation, resource requests/limits checks, PVC health, operator version compatibility, CRD configuration validation).

### 14. Appendix A: Scylla Doctor Collector/Analyzer Mapping

Full table of ALL Scylla Doctor collectors (63) with columns:
- Collector name
- Category (CQL, REST, Config, Hardware, Tuning, Network, etc.)
- K8s feasibility (Yes / Partial / Skip)
- SODA equivalent (if exists)
- SODA status (Implemented / Not yet / N/A)
- Notes

Full table of ALL Scylla Doctor analyzers (~58) with columns:
- Analyzer area
- K8s feasibility (Runs normally / Runs with caveats / Auto-skips / N/A)
- SODA equivalent (if exists)
- SODA status
- Notes

### 15. Appendix B: K8s-Specific Collectors & Analyzers (Beyond Scylla Doctor)

Table of K8s-specific modules that Scylla Doctor has no concept of:
- Already implemented (22 K8s-exclusive collectors in PoC)
- Planned/proposed K8s-specific analyzers:
  - Node tuning validation (NodeConfig applied correctly, sysctl values match expected)
  - Resource requests/limits checks (memory/CPU appropriately set for Scylla pods)
  - PVC health (bound, correct storage class, sufficient capacity)
  - Operator version compatibility (operator version vs. ScyllaCluster API version)
  - CRD configuration validation (common misconfigurations in ScyllaCluster spec)
  - ScyllaCluster status condition analysis (degraded conditions, reconciliation errors)
  - Pod scheduling analysis (anti-affinity, topology spread, node selector issues)
  - Network policy analysis (missing or overly restrictive network policies)

---

## Writing Strategy

1. **Start with sections 1-6** (context, motivation, goals, approaches) — these set the stage and justify the decision.
2. **Then section 7** (architecture) — the technical core. Use actual code snippets from the PoC where helpful.
3. **Then sections 8-12** (CLI, DevEx, relationships, status) — practical and organizational.
4. **Finally sections 13-15** (milestones, appendices) — reference material.

Total estimated length: ~1500-2000 lines of markdown.
