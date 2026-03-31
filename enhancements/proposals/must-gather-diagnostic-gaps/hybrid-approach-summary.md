# Hybrid Diagnostic Pipeline: must-gather + vitals converter + Scylla Doctor

## Overview

This document summarizes the hybrid diagnostic approach (Approach C from the
[design analysis](analysis.md)) and provides a step-by-step guide for running
the full pipeline against a GKE cluster running a Scylla Operator-managed
ScyllaDB cluster.

The pipeline chains three stages:

```
must-gather (collect) → convert-vitals (translate) → scylla-doctor (analyse)
```

1. **must-gather** — the existing Scylla Operator subcommand, extended with new
   exec-based diagnostic collectors — gathers enriched artifacts from every
   Scylla pod in the cluster.
2. **convert-vitals** — a new Scylla Operator subcommand — reads the
   must-gather output directory and produces one Scylla Doctor-compatible
   `vitals.json` file per pod.
3. **scylla-doctor --load-vitals** — Scylla Doctor's existing offline analysis
   mode — loads the vitals file and runs its ~45 analyzers against the
   Kubernetes-sourced data without modification.

### Why this approach

Scylla Doctor has ~45 mature, field-tested analyzers. Reimplementing them in Go
inside must-gather would be a large effort with ongoing double-maintenance risk.
Instead, the converter translates must-gather artifacts into the format Scylla
Doctor already understands (`--load-vitals` / `--save-vitals` vitals JSON),
letting us reuse its entire analysis pipeline unchanged.

Collectors that are infeasible inside a container (e.g. `CPUScalingCollector`,
`NTPStatusCollector`, anything that needs systemd) are emitted with
`status: 2 (SKIPPED)`. Dependent analyzers cascade to SKIPPED automatically —
no crashes, no misleading FAILED results.

## What the PoC implements

### New must-gather collectors (exec into Scylla containers)

Added to `pkg/gather/collect/podcollector.go` as new `PodRuntimeCollector`
entries:

| Command | Output file | Scylla Doctor collector |
|---------|-------------|------------------------|
| `uname --all` | `uname.log` | `ComputerArchitectureCollector` |
| `cat /etc/os-release` | `os-release.log` | `OSCollector` |
| `/proc/cpuinfo` + `nproc` (via bash) | `cpuinfo.log` | `CPUSpecificationsCollector` |
| `free` | `free.log` | `RAMCollector` |
| `scylla --version` | `scylla-version.log` | `ScyllaVersionCollector` |
| bash glob of `/etc/scylla.d/*` | `scylla.d-contents.log` | `ScyllaExtraConfigurationFilesCollector` |
| `curl -s localhost:10000/storage_proxy/schema_versions` | `scylla-api-schema-versions.log` | `ScyllaClusterSchemaCollector` |

> **Note:** `lscpu` is not available in minimal RHEL-based Scylla container
> images. The PoC synthesises the same `CPU(s):` / `Flags:` output format from
> `/proc/cpuinfo` and `nproc` so the existing parser works unchanged.

### convert-vitals subcommand

Registered as `scylla-operator convert-vitals`. Source code:

- `pkg/cmd/operator/convertvitals.go` — Cobra command (Options/Validate/Complete/Run).
- `pkg/gather/vitals/types.go` — `CollectorResult`, status constants, helpers.
- `pkg/gather/vitals/parsers.go` — 7 parsers (uname, os-release, cpuinfo, free,
  scylla-version, scylla.d, schema-versions) plus INI/YAML/JSON config file helpers.
- `pkg/gather/vitals/converter.go` — Directory walker, pod discovery,
  per-pod artifact-to-vitals conversion, synthetic `SystemConfigCollector`, and
  `fillSkippedCollectors` (emits SKIPPED for all 55 collectors the PoC does not
  produce).

### Produced vitals per pod

| Collector entry | Status | Source |
|----------------|--------|--------|
| `ComputerArchitectureCollector` | PASSED | `uname.log` |
| `OSCollector` | PASSED | `os-release.log` |
| `CPUSpecificationsCollector` | PASSED | `cpuinfo.log` |
| `RAMCollector` | PASSED | `free.log` |
| `ScyllaVersionCollector` | PASSED | `scylla-version.log` |
| `ScyllaExtraConfigurationFilesCollector` | PASSED | `scylla.d-contents.log` |
| `ScyllaClusterSchemaCollector` | PASSED | `scylla-api-schema-versions.log` |
| `SystemConfigCollector` | PASSED | Synthetic (hardcoded K8s defaults) |
| All other 55 collectors | SKIPPED | `"Not available in Kubernetes must-gather collection"` |

### PoC validation results (real GKE cluster)

Tested against a GKE cluster running ScyllaDB 2026.1.0 on RHEL 9.7 / AMD EPYC
7B13 / 8 vCPUs / 32 GB RAM:

- **0 FAILED** results (previously ~45 FAILED before SKIPPED entries were added)
- **8 PASSED** analyzers: CPUInstructionSet, ComputerArchitecture, KernelVersion,
  OSSupport, ScyllaClusterSchema, ScyllaSupport, ScyllaUpdate, SystemConfig
- **3 WARNING**: DeveloperMode (enabled), MemoryTuning (not configured),
  RAMAnalyzer (31.34 GiB < 32 GiB)
- **~47 SKIPPED**: All show clean `"Required XxxCollector was skipped"` messages

## Running the full pipeline

### Prerequisites

- A running GKE (or other K8s) cluster with Scylla Operator deployed and at
  least one `ScyllaCluster` (or `ScyllaDBDatacenter`) with running Scylla pods.
- `kubectl` configured with a context that has access to the cluster.
- The scylla-operator binary built from the `scylla-doctor-parity` branch (or
  the PoC changes cherry-picked into your branch).
- Python 3 with [scylla-doctor](https://pypi.org/project/scylla-doctor/)
  installed (`pip install scylla-doctor`).

### Step 0: Build the scylla-operator binary

```bash
cd <scylla-operator-repo>
go build -o ./scylla-operator ./cmd/...
```

This produces the `scylla-operator` binary that includes both the
`must-gather` and `convert-vitals` subcommands.

### Step 1: Collect artifacts with must-gather

```bash
./scylla-operator must-gather \
    --kubeconfig="$KUBECONFIG" \
    --dest-dir=/tmp/must-gather-output
```

This will:
- Discover all Scylla-related namespaces (operator, manager, ScyllaCluster
  namespaces).
- Collect Kubernetes resource YAMLs, pod logs, and Events.
- Exec into each running Scylla container and run all diagnostic commands
  (including the 7 new ones added by the PoC).

The output is written to `/tmp/must-gather-output/` with the standard layout:

```
/tmp/must-gather-output/
  namespaces/
    <namespace>/
      pods/
        <pod-name>/
          uname.log
          os-release.log
          cpuinfo.log
          free.log
          scylla-version.log
          scylla.d-contents.log
          scylla-api-schema-versions.log
          nodetool-status.log
          nodetool-gossipinfo.log
          df.log
          io_properties.yaml
          scylla-rlimits.log
          scylla/                    # container logs
            scylla.current
          ...
      ...
  cluster-scoped/
    ...
```

### Step 2: Convert artifacts to Scylla Doctor vitals

```bash
./scylla-operator convert-vitals \
    --must-gather-dir=/tmp/must-gather-output \
    --output-dir=/tmp/vitals-output
```

This walks the must-gather directory, finds every pod directory that contains
at least one diagnostic artifact, parses the collected files, and writes a
`vitals.json` per pod:

```
/tmp/vitals-output/
  namespaces/
    <namespace>/
      pods/
        <pod-name>/
          vitals.json
```

If `--output-dir` is omitted, vitals files are written alongside the
must-gather artifacts (inside each pod's directory).

### Step 3: Run Scylla Doctor analysis

#### Per-node analysis

Run Scylla Doctor against each pod's vitals file. The `-sov SDVersionAnalyzer,run,no`
flag is required because the vitals were not produced by Scylla Doctor itself,
so the version check would fail:

```bash
for vitals_file in /tmp/vitals-output/namespaces/*/pods/*/vitals.json; do
    pod_name=$(basename "$(dirname "$vitals_file")")
    echo "=== Analysing pod: $pod_name ==="
    scylla-doctor \
        --load-vitals "$vitals_file" \
        -sov SDVersionAnalyzer,run,no
    echo ""
done
```

#### Cluster-wide analysis (cross-node comparison)

If you have multiple Scylla pods, you can use `scylla-doctor-cluster` to run
cross-node analysis. Collect all vitals files into a single directory first:

```bash
# Flatten vitals into a single directory for scylla-doctor-cluster
mkdir -p /tmp/vitals-flat
for f in /tmp/vitals-output/namespaces/*/pods/*/vitals.json; do
    pod=$(basename "$(dirname "$f")")
    cp "$f" "/tmp/vitals-flat/${pod}.vitals.json"
done

scylla-doctor-cluster \
    --load-vitals-dir /tmp/vitals-flat \
    -sov SDVersionAnalyzer,run,no
```

### Step 4: Interpret results

The output follows Scylla Doctor's standard format:

- **PASSED** — the check passed; the configuration matches best practices.
- **WARNING** — a non-critical issue was detected (e.g. developer mode enabled,
  RAM slightly below recommendation).
- **FAILED** — a critical issue was detected.
- **SKIPPED** — the required collector data was not available. In the
  Kubernetes context this is expected for host-level checks (CPU governor, NTP,
  systemd services, etc.) and is not a cause for concern.

Focus on PASSED, WARNING, and FAILED results. SKIPPED results for collectors
marked `"Not available in Kubernetes must-gather collection"` are expected and
can be filtered from the output with:

```bash
scylla-doctor \
    --load-vitals "$vitals_file" \
    -sov SDVersionAnalyzer,run,no \
    --print-filter "^((?!SKIPPED).)*$"
```

## Complete example session

```bash
# Build
go build -o ./scylla-operator ./cmd/...

# Collect
./scylla-operator must-gather \
    --kubeconfig="$KUBECONFIG" \
    --dest-dir=/tmp/diag

# Convert
./scylla-operator convert-vitals \
    --must-gather-dir=/tmp/diag \
    --output-dir=/tmp/diag-vitals

# Analyse (single pod)
scylla-doctor \
    --load-vitals /tmp/diag-vitals/namespaces/scylla/pods/scylla-us-east1-us-east1-b-0/vitals.json \
    -sov SDVersionAnalyzer,run,no

# Analyse (all pods, cluster-wide)
mkdir -p /tmp/diag-vitals-flat
for f in /tmp/diag-vitals/namespaces/*/pods/*/vitals.json; do
    pod=$(basename "$(dirname "$f")")
    cp "$f" "/tmp/diag-vitals-flat/${pod}.vitals.json"
done
scylla-doctor-cluster \
    --load-vitals-dir /tmp/diag-vitals-flat \
    -sov SDVersionAnalyzer,run,no
```

## PoC commit history

| Commit | Description |
|--------|-------------|
| `bd1b35fef` | Design document (gap analysis, approach comparison, recommendation) |
| `3b6a30d44` | Add 7 diagnostic exec commands to `podcollector.go` |
| `4d0fbf57f` | Scaffold `convert-vitals` Cobra subcommand and `pkg/gather/vitals` package |
| `5ad382765` | Implement full converter logic — 7 parsers, directory walker, vitals JSON output |
| `5b66b572c` | Add 14 unit tests covering all parsers and converter flow |
| `9ff756165` | Fix container compatibility — replace `lscpu` with `/proc/cpuinfo`+`nproc`, harden scylla.d glob |
| `c2354dc6b` | Emit SKIPPED entries for all 55 non-produced collectors (0 FAILED in E2E) |

## Current coverage and future work

The PoC demonstrates the approach with 8 of 63 collectors. The design document's
Phase 2-6 roadmap covers expanding to ~42 collectors (67% of Scylla Doctor's
total), which would enable ~27 analyzers to run with full data.

Key areas for expansion:

| Category | Examples | Estimated new collectors |
|----------|----------|------------------------|
| CQL-based | `system.peers`, `system.config`, `DESC SCHEMA`, Raft state | ~11 |
| REST API | cluster status, gossip info, token mapping, table stats | ~7 |
| Scylla config | `scylla.yaml` (parsed), system config files | ~4 |
| System/hardware | sysctl, swap, clock source, SELinux, SSTable listing | ~8 |
| Tuning/meta | Seastar CPU map, paths, NOFILE limit, binary path | ~4 |

Collectors that will always be SKIPPED in Kubernetes (systemd, perftune,
host-level NIC/NTP/firewall) — ~18 total — are documented in the design
analysis as candidates for K8s-specific analyzers built natively in the
operator.
