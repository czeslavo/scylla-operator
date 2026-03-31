# Closing the Diagnostic Gap Between must-gather and Scylla Doctor

## Summary

ScyllaDB currently ships two independent diagnostic tools that serve overlapping but incomplete audiences.
**must-gather**, embedded in the Scylla Operator, is a Kubernetes-native artifact collector that talks to the Kubernetes API, dumps resource definitions, pod logs, and runs a handful of commands inside Scylla containers.
**Scylla Doctor**, a standalone Python tool, runs directly on bare-metal/VM Scylla nodes, collects 63 categories of deep system and Scylla-level data, and runs ~45 analyzers that produce actionable PASSED/WARNING/FAILED verdicts.

Neither tool alone covers the full diagnostic surface for Kubernetes-based ScyllaDB deployments.
must-gather lacks depth in Scylla-specific diagnostics (REST API queries, CQL state, hardware/OS inspection, tuning verification, and any form of automated analysis).
Scylla Doctor has zero Kubernetes awareness and several of its collectors explicitly skip or malfunction in container environments.

This document catalogues every gap between the two tools, evaluates three strategic approaches to closing those gaps, and recommends the hybrid Approach C: extend must-gather with missing collectors and build a compatibility layer that translates must-gather archives into Scylla Doctor's vitals format, allowing Scylla Doctor's existing ~45 analyzers to run against Kubernetes-sourced data without modification.

## Motivation

When a Kubernetes user opens a support case today, the workflow looks like this:

1. Run `must-gather` to produce an archive of Kubernetes resources and pod logs.
2. Attach the archive to the support ticket.
3. A support engineer manually inspects YAML files and logs to triage the issue.

There is no automated analysis, no best-practice validation, and no deep Scylla-internal state captured.
A bare-metal user, by contrast, runs Scylla Doctor on each node and immediately receives a diagnostic report with clear verdicts and recommendations.

The result is that Kubernetes users get a materially worse diagnostic experience, and support engineers spend more time triaging Kubernetes cases because the collected data is shallow and unanalyzed.

### Goals

- Catalogue every data collection and analysis gap between must-gather and Scylla Doctor.
- Evaluate three approaches: (A) extending must-gather with Scylla-depth diagnostics and its own analysis, (B) extending Scylla Doctor with Kubernetes intelligence, and (C) a hybrid where must-gather collects data and a converter feeds it into Scylla Doctor's existing analysis pipeline.
- Provide a concrete action plan for each approach.
- Recommend the approach that best serves Kubernetes users, support engineers, and long-term maintainability.

### Non-Goals

- Defining the final API or implementation details of any new collectors/analyzers.
- Addressing gaps in Scylla Doctor for bare-metal environments (that is Scylla Doctor's own roadmap).
- Building a unified tool that replaces both must-gather and Scylla Doctor for all deployment models.

## Gap Analysis

### Terminology

| Term | Meaning |
|------|---------|
| **Collected by must-gather** | Data is gathered and written to the output archive |
| **Collected by Scylla Doctor** | Data is gathered into the vitals JSON and/or used by analyzers |
| **MISSING** | Not collected at all by the tool in question |
| **Partial** | Some related data exists but the specific diagnostic is not covered |

### Gap 1: Hardware and System Information

must-gather collects no hardware or OS-level data from inside Scylla pods. The Kubernetes Node object provides kernel version, OS image, and architecture, but nothing about CPU tuning, memory details, or clock sources.

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| CPU model, flags, core count (`lscpu`, `/proc/cpuinfo`) | `CPUSpecificationsCollector` | MISSING |
| CPU scaling governor | `CPUScalingCollector` | MISSING |
| CPU set / pinning configuration | `CPUSetCollector` | MISSING |
| RAM total/free (`/proc/meminfo`) | `RAMCollector` | MISSING |
| Swap configuration | `SwapCollector` | MISSING |
| Clock source (`/sys/devices/system/clocksource/`) | `ClockSourceCollector` | MISSING |
| RAID setup (`/proc/mdstat`) | `RAIDSetupCollector` | MISSING |
| PCI devices (`lspci`) | `LSPCICollector` | MISSING |
| SELinux status | `SELinuxCollector` | MISSING |
| IRQ distribution (`/proc/interrupts`) | `ProcInterruptsCollector` | MISSING |
| Kernel ring buffer (`dmesg`) | `KernelRingBufferCollector` | MISSING |
| Hypervisor type | `HypervisorTypeCollector` | MISSING |
| OS distribution and version | `OSCollector` | Partial (K8s Node `.status.nodeInfo`) |
| Architecture and kernel version | `ComputerArchitectureCollector` | Partial (K8s Node `.status.nodeInfo`) |

**Why this matters in Kubernetes:** Scylla Operator deploys on dedicated, performance-tuned nodes (via `NodeConfig`). CPU governor, clock source, IRQ affinity, and memory configuration are as critical for performance in Kubernetes as on bare-metal. Without this data, support engineers cannot validate that node tuning was applied correctly.

### Gap 2: Network Diagnostics

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| NIC driver, speed (`ethtool`) | `NICsCollector` | MISSING |
| IP addresses per interface | `IPAddressesCollector` | Partial (K8s Pod/Node IPs only) |
| IP routing table | `IPRoutesCollector` | MISSING |
| Firewall rules (`iptables`) | `FirewallRulesCollector` | MISSING |
| Active TCP connections (`ss`) | `TCPConnectionsCollector` | MISSING |
| NTP synchronization status | `NTPStatusCollector` | MISSING |
| NTP service status | `NTPServicesCollector` | MISSING |

**Why this matters in Kubernetes:** Kubernetes adds CNI plugins, Service networking, DNS resolution, and NetworkPolicies on top of the host network stack. NTP drift between nodes causes consistency issues regardless of deployment model. must-gather collects none of these network diagnostics, nor does it collect Kubernetes-specific networking objects (NetworkPolicies, endpoint slices) beyond what happens to be in a collected namespace.

### Gap 3: Storage Deep-Dive

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| Block device type (NVMe vs rotational), filesystem, mount options | `StorageConfigurationCollector` | MISSING (`df -h` only) |
| SSTable file listing | `ScyllaSSTablesCollector` | MISSING |
| Per-table disk usage (REST API) | `ScyllaTablesUsedDiskCollector` | MISSING |
| Per-table compression ratio (REST API) | `ScyllaTablesCompressionInfoCollector` | MISSING |

**Why this matters in Kubernetes:** Storage in Kubernetes involves PVCs, PVs, StorageClasses, and CSI drivers. must-gather does collect StorageClass YAML and PVC/PV definitions as part of namespace resources, but it does not inspect the underlying block device characteristics, filesystem type, or mount options. The only storage diagnostic is a `df -h` command. For performance triage, knowing whether storage is backed by local NVMe (via LocalVolume provisioner) vs network-attached EBS/PD, and whether XFS is used with the correct mount options, is essential.

### Gap 4: Scylla Configuration

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| Parsed `scylla.yaml` with DNS resolution | `ScyllaConfigurationFileCollector` | MISSING |
| Raw `scylla.yaml` | `ScyllaConfigurationFileNoParsingCollector` | MISSING |
| Extra config files (`/etc/scylla.d/*`) | `ScyllaExtraConfigurationFilesCollector` | Partial (`io_properties.yaml` only) |
| System config files (`/etc/sysconfig/`, `/etc/default/`) | `ScyllaSystemConfigurationFilesCollector` | MISSING |
| Scylla version (binary + packages) | `ScyllaVersionCollector` | Partial (container image tag) |
| NOFILE limit | `ScyllaLimitNOFILECollector` | Partial (`prlimit` captures this) |
| In-memory config (CQL `system.config`) | `SystemConfigCollector` | MISSING |

**Why this matters in Kubernetes:** The Scylla Operator generates `scylla.yaml` from the `ScyllaCluster` CRD spec and injects it via ConfigMap. must-gather collects the CRD YAML (the desired state) but not the actual generated configuration files inside the running container (the effective state). When debugging configuration-related issues, the effective state is what matters. The in-memory configuration (from `system.config`) is even more definitive, and it is not collected at all.

### Gap 5: Performance Tuning Verification

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| perftune.py output, sysctl values, IRQ affinity | `PerftuneSystemConfigurationCollector` | MISSING |
| Default perftune.yaml | `PerftuneYamlDefaultCollector` | MISSING |
| Key sysctl values (aio-max-nr, file-max, nr_open) | `SysctlCollector` | MISSING |
| Seastar CPU map | `SeastarCPUMapCollector` | MISSING |
| Coredump configuration | `CoredumpCollector` | MISSING |
| Kubelet CPU manager state | N/A | Collected (via NodeConfig pod) |

**Why this matters in Kubernetes:** The Scylla Operator tunes nodes via the `NodeConfig` DaemonSet (IRQ affinity, sysctl values, CPU isolation). must-gather collects the kubelet CPU manager state but not the actual sysctl values, IRQ affinity settings, or perftune output. These are the first things a support engineer checks when diagnosing performance issues.

### Gap 6: Cluster State via Scylla REST API and CQL

This is the single largest gap. must-gather runs only `nodetool status` and `nodetool gossipinfo` inside Scylla containers. It does not query the Scylla REST API or execute any CQL queries.

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| CQL connectivity test | `CqlshCollector` | MISSING |
| Client connections (`system.clients`) | `ClientConnectionCollector` | MISSING |
| Full schema (`DESC SCHEMA`) | `ScyllaClusterSchemaDescriptionCollector` | MISSING |
| Schema versions (REST) | `ScyllaClusterSchemaCollector` | MISSING |
| Cluster topology (live/down/joining/leaving via REST) | `ScyllaClusterStatusCollector` | Partial (`nodetool status`) |
| System keyspaces | `ScyllaClusterSystemKeyspacesCollector` | MISSING |
| Table descriptions | `ScyllaClusterTablesDescriptionCollector` | MISSING |
| Seed node connectivity | `ScyllaSeedsCollector` | MISSING |
| Gossip failure detector (REST) | `GossipInfoCollector` | Partial (`nodetool gossipinfo`) |
| Token-to-host mapping (REST) | `TokenMetadataHostsMappingCollector` | MISSING |
| `system.peers` + `system.local` | `SystemPeersLocalCollector` | MISSING |
| `system.cluster_status` | `SystemClusterStatusCollector` | MISSING |
| `system.topology` | `SystemTopologyCollector` | MISSING |
| Raft group0 state | `RaftGroup0Collector` | MISSING |
| Raft topology RPC status | `RaftTopologyRPCStatusCollector` | MISSING |
| `nodetool cfstats` | `NodetoolCFStatsCollector` | MISSING |
| Per-table disk usage (REST) | `ScyllaTablesUsedDiskCollector` | MISSING |
| Per-table compression ratio (REST) | `ScyllaTablesCompressionInfoCollector` | MISSING |

**Why this matters in Kubernetes:** Scylla's internal cluster state (schema agreement, Raft consensus, token distribution, gossip health) is entirely independent of the deployment model. These diagnostics are equally critical on Kubernetes. Without them, must-gather archives provide no insight into whether the ScyllaDB cluster itself is healthy, only whether the Kubernetes resources that wrap it look correct.

### Gap 7: Cloud and Infrastructure Awareness

| Data Point | Scylla Doctor Collector | must-gather Status |
|------------|------------------------|--------------------|
| Cloud provider detection (AWS/GCP/Azure/OCI/etc.) | `InfrastructureProviderCollector` | MISSING |
| Instance type | `InfrastructureProviderCollector` | MISSING |
| CPU platform (cloud-specific) | `InfrastructureProviderCollector` | MISSING |
| Platform type (container/VM/baremetal) | `NodePlatformCollector` | MISSING |
| Scheduled maintenance events | `MaintenanceEventsCollector` | MISSING |

**Why this matters in Kubernetes:** K8s Node labels often contain cloud provider and instance type information (e.g., `node.kubernetes.io/instance-type`). must-gather collects Node objects, so this data is indirectly available but not surfaced as a structured diagnostic. Cloud maintenance events are not captured at all.

### Gap 8: Automated Analysis and Recommendations

This is a qualitative gap, not a data gap.

| Capability | Scylla Doctor | must-gather |
|------------|---------------|-------------|
| Automated PASSED/WARNING/FAILED verdicts | ~45 analyzers | None |
| Best-practice validation (CPU governor, NTP, XFS, NVMe, swap, etc.) | Yes | None |
| Version support checking | Yes | None |
| Client driver version warnings | Yes | None |
| Cross-node consistency checking (via `scylla-doctor-cluster`) | Yes | None |
| Actionable remediation text | Yes | None |

**Why this matters:** must-gather is a pure collection tool. Every artifact it produces requires manual interpretation by a support engineer. Scylla Doctor, by contrast, distills raw data into actionable verdicts. For Kubernetes users, the lack of analysis means longer time-to-resolution and a heavier burden on support.

### Gap 9: Kubernetes-Specific Data (Missing from Scylla Doctor)

Scylla Doctor has zero Kubernetes awareness. If Scylla Doctor were to be used in Kubernetes environments, it would need to collect:

| Data Point | must-gather Status | Scylla Doctor Status |
|------------|--------------------|---------------------|
| ScyllaCluster / ScyllaDBDatacenter CRD spec and status | Collected | MISSING |
| ScyllaOperatorConfig | Collected | MISSING |
| NodeConfig CRD | Collected | MISSING |
| Pod status (restart counts, OOMKills, conditions) | Collected | MISSING |
| Container logs (current/previous/terminated) | Collected | MISSING (uses `journalctl`) |
| Kubernetes Events | Collected | MISSING |
| Deployments, StatefulSets, DaemonSets, ReplicaSets | Collected | MISSING |
| Services, EndpointSlices | Collected | MISSING |
| ConfigMaps (operator-generated Scylla configs) | Collected | MISSING |
| PVCs, PVs, StorageClasses | Collected | MISSING |
| Webhook configurations | Collected | MISSING |
| CRD definitions | Collected | MISSING |
| K8s Node objects (capacity, allocatable, conditions, labels) | Collected | MISSING |
| Scylla Operator pod logs | Collected | MISSING |
| Scylla Manager pod logs (in K8s) | Collected | MISSING |
| Node-tuning DaemonSet logs | Collected | MISSING |
| Resource requests/limits | In Pod YAML | MISSING |
| PodDisruptionBudgets | In namespace resources | MISSING |
| NetworkPolicies | In namespace resources | MISSING |
| cert-manager Certificates/Issuers | In namespace resources | MISSING |

### Gap 10: Scylla Doctor Collectors That Break in Containers

Several Scylla Doctor collectors explicitly skip or malfunction in containerized environments:

| Collector | Issue |
|-----------|-------|
| `ScyllaLogsCollector` | Uses `journalctl`, which is not available in containers |
| `ScyllaLimitNOFILECollector` | Reads systemd unit file |
| `PerftuneYamlDefaultCollector` | Requires perftune.py on host |
| `ScyllaServicesCollector` | Checks systemd service status |
| `ServiceManagerCollector` | Detects systemd/supervisord only |
| `FirewallRulesCollector` | Container `iptables` rules do not reflect K8s networking |
| `NICsCollector` | Container network interfaces differ from host NICs |
| `IPAddressesCollector` | Shows container-level IPs, not the node IPs that matter |
| `StorageConfigurationCollector` | Sees PVC mount paths, not underlying block devices |
| `NTPStatusCollector` / `NTPServicesCollector` | NTP runs on the host, not inside containers |

## Proposal

We evaluate three approaches to close the gaps described above.

### Approach A: Extend must-gather "Upward" (Collection + Native Analysis)

In this approach, must-gather remains the single diagnostic entry point for Kubernetes users. It is extended with new collectors that query the Scylla REST API and CQL from outside the container (or via `kubectl exec`), gather system-level data from inside pods, and run its own analysis logic, reimplemented in Go.

#### Advantages

- Single tool, single workflow. Kubernetes users run one command and get everything.
- K8s-native execution model. No need for SSH or direct node access.
- Leverages existing exec-into-pod infrastructure.
- Opportunity for K8s-exclusive diagnostics (PDB, cert-manager, operator reconciliation).
- Go codebase, consistent with the operator.
- No dependency on Scylla Doctor's release cycle.

#### Disadvantages

- **Duplicated logic.** Many collectors would reimplement what Scylla Doctor already does. Divergence risk over time as ScyllaDB evolves (new config options, new system tables, new Raft features).
- **Analysis from scratch.** Scylla Doctor has ~45 mature analyzers with tuned thresholds and field-tested logic. Reimplementing them in Go is significant effort and risks subtle behavioral differences.
- **Ongoing double maintenance.** As ScyllaDB adds features, both codebases need parallel updates to diagnostic logic.

### Approach B: Extend Scylla Doctor "Outward" (Add K8s Intelligence)

In this approach, Scylla Doctor gains Kubernetes awareness. It learns to discover Scylla pods via the K8s API, execute its collectors inside those pods, and produce a unified diagnostic report covering both Scylla and Kubernetes layers. must-gather either becomes a thin wrapper or is replaced.

#### Advantages

- Reuses 63 collectors and ~45 analyzers. No reimplementation.
- Single diagnostic codebase for all deployment models.
- Proven collector-vitals-analyzer pipeline.

#### Disadvantages

- **Fundamental architecture mismatch.** Scylla Doctor is designed for local execution (`subprocess.run()`, local file reads). Making it work remotely via K8s exec requires deep refactoring of the `Executor` class and every collector.
- **Python in a Go ecosystem.** The operator ships as a single Go binary. Adding Python creates packaging and distribution challenges.
- **Release coupling.** Ties operator diagnostics to Scylla Doctor's separate release cycle and team.
- **10+ collectors break in containers** (Gap 10). Each needs case-by-case adaptation.

### Approach C: Hybrid — must-gather Collection + Vitals Converter + Scylla Doctor Analysis (Recommended)

This approach separates concerns cleanly:

1. **must-gather** is extended with new collectors to close the data gaps (Gaps 1-7). It remains the Kubernetes-native collection tool. It produces a richer archive than today.
2. **A converter** (new component, written in Go, part of the operator) translates a must-gather archive into Scylla Doctor's vitals JSON format — one vitals file per Scylla pod.
3. **Scylla Doctor's analyzers** consume these vitals files via `--load-vitals` (and `scylla-doctor-cluster` for cross-node analysis), unchanged. No modifications to Scylla Doctor are required.

The user workflow becomes:

```
must-gather  -->  converter  -->  vitals.json (per pod)  -->  scylla-doctor --load-vitals
                                                          -->  scylla-doctor-cluster (all vitals)
```

#### Design Details: Scylla Doctor's Vitals Format

Scylla Doctor's vitals JSON is a well-defined, stable intermediate format. Understanding it precisely is essential for the converter design.

**Top-level structure:**

```json
{
  "<CollectorClassName>": {
    "status": <int>,
    "data": { <collector-specific dict> },
    "output": [ <list of output entries> ],
    "message": "<string>",
    "mask": [ <list of strings or tuples> ]
  },
  ...
}
```

The dict is flat, keyed by collector class name (e.g., `"CPUSpecificationsCollector"`). Each value is a `CollectorResult` with these fields:

| Field | Type | Description |
|-------|------|-------------|
| `status` | int | `0` = PASSED (data collected successfully), `1` = FAILED, `2` = SKIPPED |
| `data` | dict | Free-form collector-specific data. This is what analyzers read. |
| `output` | list | Debug/verbose output entries (command outputs, file contents). Optional for analysis. |
| `message` | string | Human-readable status message (e.g., `"Data collected"`). |
| `mask` | list | Keys to exclude from cross-node comparison in `scylla-doctor-cluster`. |

**How analyzers consume vitals:** Each analyzer declares a `depends_on` set of collector class names. At runtime, the analyzer receives a `DictView` restricted to only those keys. It accesses data as `vitals["CollectorName"].data["key"]`. If any dependency is missing or has status FAILED/SKIPPED, the analyzer is automatically skipped or failed — no crash, no undefined behavior.

**This is the key insight that makes the converter viable:** Analyzers only check `status` and `data` from their declared dependencies. The converter only needs to produce entries for the collectors it can satisfy. Analyzers whose dependencies are not present will gracefully skip. No Scylla Doctor code changes are needed.

#### Design Details: The Converter

The converter is a new Go function (or subcommand) in the scylla-operator codebase that reads a must-gather archive directory and produces one `vitals.json` file per Scylla pod.

**Input:** A must-gather archive directory with the standard layout:

```
<archive>/
  namespaces/<ns>/pods/<pod-name>/
    scylla.yaml                      # NEW: collected by extended must-gather
    scylla.d/                        # NEW: all /etc/scylla.d/* files
    nodetool-status.log
    nodetool-gossipinfo.log
    nodetool-cfstats.log             # NEW
    df.log
    io_properties.yaml
    scylla-rlimits.log
    lscpu.log                        # NEW
    meminfo.log                      # NEW
    sysctl.log                       # NEW
    rest-api/                        # NEW: REST API responses
      cluster-status.json
      schema-versions.json
      gossip-info.json
      token-host-mapping.json
      raft-topology-rpc-status.json
      table-disk-usage.json
      table-compression-ratio.json
    cql/                             # NEW: CQL query results
      system-peers.json
      system-local.json
      system-config.json
      system-topology.json
      system-cluster-status.json
      desc-schema.txt
      system-clients.json
      raft-group0.json
    ...
  cluster-scoped/
    nodes/<node-name>.yaml
    ...
```

**Output:** Per-pod vitals files:

```
<archive>/
  vitals/
    <ns>-<pod-name>.vitals.json
```

**Conversion strategy per collector:**

The converter maps must-gather artifacts to the `data` dict that each Scylla Doctor collector would have produced. The mapping is per-collector.

The following table shows every Scylla Doctor collector, whether the converter can produce compatible vitals for it, and what must-gather artifact(s) it would read:

| Scylla Doctor Collector | Converter Source | Feasibility | Notes |
|------------------------|-----------------|-------------|-------|
| **CQL-based collectors** | | | |
| `CqlshCollector` | CQL connectivity test result | Yes | must-gather can test CQL via exec |
| `ClientConnectionCollector` | `cql/system-clients.json` | Yes | CQL `SELECT` via exec |
| `ScyllaClusterSchemaDescriptionCollector` | `cql/desc-schema.txt` | Yes | `cqlsh -e "DESC SCHEMA"` via exec |
| `ScyllaClusterSystemKeyspacesCollector` | `cql/system-keyspaces.json` | Yes | CQL `SELECT` via exec |
| `ScyllaClusterTablesDescriptionCollector` | `cql/system-tables.json` | Yes | CQL `SELECT` via exec |
| `SystemPeersLocalCollector` | `cql/system-peers.json`, `cql/system-local.json` | Yes | CQL `SELECT` via exec |
| `SystemClusterStatusCollector` | `cql/system-cluster-status.json` | Yes | CQL `SELECT` via exec |
| `SystemTopologyCollector` | `cql/system-topology.json` | Yes | CQL `SELECT` via exec |
| `SystemConfigCollector` | `cql/system-config.json` | Yes | CQL `SELECT` via exec |
| `RaftGroup0Collector` | `cql/raft-group0.json` | Yes | CQL `SELECT` via exec |
| **REST API-based collectors** | | | |
| `ScyllaClusterSchemaCollector` | `rest-api/schema-versions.json` | Yes | `curl` to REST API via exec |
| `ScyllaClusterStatusCollector` | `rest-api/cluster-status.json` | Yes | `curl` to REST API via exec |
| `GossipInfoCollector` | `rest-api/gossip-info.json` | Yes | `curl` to REST API via exec |
| `TokenMetadataHostsMappingCollector` | `rest-api/token-host-mapping.json` | Yes | `curl` to REST API via exec |
| `RaftTopologyRPCStatusCollector` | `rest-api/raft-topology-rpc-status.json` | Yes | `curl` to REST API via exec |
| `ScyllaTablesUsedDiskCollector` | `rest-api/table-disk-usage.json` | Yes | `curl` to REST API via exec |
| `ScyllaTablesCompressionInfoCollector` | `rest-api/table-compression-ratio.json` | Yes | `curl` to REST API via exec |
| **Scylla config collectors** | | | |
| `ScyllaConfigurationFileCollector` | `scylla.yaml` (parsed by converter with DNS resolution) | Yes | Read file via exec, parse in converter |
| `ScyllaConfigurationFileNoParsingCollector` | `scylla.yaml` (raw) | Yes | Read file via exec |
| `ScyllaExtraConfigurationFilesCollector` | `scylla.d/*` files | Yes | Read files via exec |
| `ScyllaSystemConfigurationFilesCollector` | `/etc/sysconfig/scylla-server` or `/etc/default/scylla-server` via exec | Yes | Read file via exec |
| `ScyllaVersionCollector` | `scylla --version` via exec + container image tag | Yes | Exec `scylla --version` |
| `ScyllaLimitNOFILECollector` | `scylla-rlimits.log` (already collected) | Partial | Can parse `prlimit` output for NOFILE; lacks systemd-specific data. Mark as PASSED with partial data. |
| **System/hardware collectors** | | | |
| `CPUSpecificationsCollector` | `lscpu.log`, `/proc/cpuinfo` via exec | Yes | Exec commands, parse output |
| `RAMCollector` | `meminfo.log` via exec | Yes | Exec `free` and `cat /proc/meminfo` |
| `SwapCollector` | `/proc/swaps` via exec | Yes | Reports container/cgroup view; may show host swap |
| `ComputerArchitectureCollector` | `uname` via exec or K8s Node `.status.nodeInfo` | Yes | |
| `OSCollector` | `/etc/os-release` via exec or K8s Node `.status.nodeInfo` | Yes | |
| `ClockSourceCollector` | `/sys/devices/system/clocksource/clocksource0/current_clocksource` via exec | Partial | File may not be readable in all container runtimes |
| `SELinuxCollector` | `/etc/selinux/config` via exec | Yes | File may not exist (which is fine — means SELinux is off) |
| `SysctlCollector` | `sysctl.log` via exec | Yes | `sysctl` reads host kernel params from within the container |
| `HypervisorTypeCollector` | `systemd-detect-virt` via exec | Partial | May not be available in container |
| `ProcInterruptsCollector` | `/proc/interrupts` via exec | Partial | Shows container-visible interrupts |
| `RAIDSetupCollector` | `/proc/mdstat` via exec | Partial | Shows host RAID if `/proc` is from host |
| `KernelRingBufferCollector` | `dmesg` via NodeConfig pod exec | Partial | Requires host access via NodeConfig |
| **Tuning collectors** | | | |
| `SeastarCPUMapCollector` | `seastar-cpu-map.sh` via exec | Yes | Script is in the Scylla container |
| `NodetoolCFStatsCollector` | `nodetool-cfstats.log` (NEW must-gather artifact) | Yes | |
| `ScyllaSeedsCollector` | Seed list from `scylla.yaml` + TCP connectivity test via exec | Yes | |
| `ScyllaBinaryCollector` | `which scylla` via exec | Yes | |
| `PathsCollector` | Hardcoded or from config | Yes | Standard paths in Scylla container |
| `SDVersionCollector` | Hardcoded to converter version | Yes | Mark as PASSED with converter version |
| `NodePlatformCollector` | Hardcoded to `CONTAINER` | Yes | Always `container` in K8s |
| **Collectors infeasible in K8s** | | | |
| `CPUScalingCollector` | N/A (requires systemd + sysfs `cpufreq`) | Skip | Converter emits `status: SKIPPED`. Analyzers that depend on it will auto-skip. |
| `CPUSetCollector` | N/A (requires `hwloc-calc`, `perftune.py`) | Skip | |
| `PerftuneSystemConfigurationCollector` | N/A (requires `perftune.py --dry-run`) | Skip | |
| `PerftuneYamlDefaultCollector` | N/A (requires `perftune.py`) | Skip | |
| `CoredumpCollector` | N/A (requires systemd mount service) | Skip | |
| `RsyslogCollector` | N/A | Skip | |
| `StorageConfigurationCollector` | N/A (requires `perftune.py`, sees PVC paths not devices) | Skip | See note below on K8s-specific alternatives. |
| `ScyllaServicesCollector` | N/A (requires systemd) | Skip | |
| `ServiceManagerCollector` | N/A (detects systemd/supervisord) | Skip | |
| `NTPStatusCollector` | N/A (requires `timedatectl`) | Skip | |
| `NTPServicesCollector` | N/A (requires systemd) | Skip | |
| `ScyllaLogsCollector` | N/A (uses `journalctl`) | Skip | Logs collected separately by must-gather via K8s API. |
| `NICsCollector` | N/A (container NICs are not the host NICs) | Skip | |
| `IPAddressesCollector` | N/A (container IPs, not node IPs) | Skip | |
| `IPRoutesCollector` | N/A (container routing) | Skip | |
| `FirewallRulesCollector` | N/A (container iptables are meaningless) | Skip | |
| `TCPConnectionsCollector` | N/A (`ss` shows container connections) | Skip | |
| `InfrastructureProviderCollector` | K8s Node labels | Partial | Can extract `node.kubernetes.io/instance-type` and cloud provider from Node labels. Lacks CPU platform. |
| `MaintenanceEventsCollector` | N/A (cloud metadata endpoint inaccessible from pods) | Skip | |
| `ScyllaSSTablesCollector` | SSTable listing via exec | Yes | `find /var/lib/scylla/data -name '*-Data.db'` |
| `LSPCICollector` | N/A (`lspci` not available in container) | Skip | |

**Summary of converter coverage:**

| Category | Convertible | Skipped | Total |
|----------|-------------|---------|-------|
| CQL-based | 11 | 0 | 11 |
| REST API-based | 7 | 0 | 7 |
| Scylla config | 6 | 0 | 6 |
| System/hardware | 11 | 3 | 14 |
| Tuning/meta | 6 | 0 | 6 |
| Infeasible in K8s | 1 | 18 | 19 |
| **Total** | **42** | **21** | **63** |

42 of 63 collectors (67%) can be fully or partially satisfied by the converter. The remaining 21 will be emitted as `status: SKIPPED` with a message indicating data was not available in the Kubernetes environment.

**Analyzer coverage with converted vitals:**

Based on the analyzer-to-collector dependency mapping, here is how each of the ~58 analyzers would behave:

| Analyzer Feasibility | Count | Examples |
|---------------------|-------|---------|
| **Will run normally** | 27 | Schema agreement, Raft health, version support, internode compression, listen/broadcast addresses, developer mode, IO setup, kernel version, memory tuning, OS support, architecture, SSTable format, seed connectivity, configuration consistency |
| **Will auto-skip** (dependency on infeasible collector) | 25 | CPU scaling, CPU set, perftune, NTP, NIC speed, RAID, rsyslog, storage type, swap, XFS filesystem, coredump, services |
| **Will run with caveats** | 4 | RAM analyzer (container memory view), sysctl analyzers (host values visible), storage-RAM ratio (partial), driver version (needs outbound HTTPS) |
| **Not applicable in K8s** | 2 | SD version match (converter version), NTP status (no timedatectl) |

**The 27 analyzers that run normally cover the most critical diagnostics:** schema agreement, Raft consensus health, Raft topology status, gossip consistency, topology consistency, version support, driver versions, Scylla configuration validation (listen address, broadcast address, RPC address, internode compression, deprecated arguments, NIC/disk setup flags, configuration file format, configuration consistency with in-memory state), IO setup, memory tuning, kernel version, OS support, architecture, SSTable format, seed connectivity, system keyspace replication, and developer mode detection.

#### Design Details: What the Converter Does NOT Cover (and How to Address It)

The 21 skipped collectors and 25 auto-skipped analyzers represent host-level and systemd-dependent diagnostics. These gaps fall into categories:

**1. Host-level tuning (CPU governor, perftune, NTP, IRQ affinity):**
These are managed by the `NodeConfig` DaemonSet in the Scylla Operator. Instead of trying to make Scylla Doctor analyze them, must-gather should collect the relevant data from NodeConfig pods and the raw archive should be inspected by support engineers. Additionally, K8s-specific analyzers (not in Scylla Doctor) could be added to must-gather in the future to validate NodeConfig status.

**2. Storage device characteristics (NVMe, filesystem, mount options):**
`StorageConfigurationCollector` requires `perftune.py` and sees PVC paths instead of real devices. The K8s-specific approach is to analyze PV/PVC/StorageClass YAML and correlate with `df` and `mount` output from inside the container. This is a candidate for a future K8s-specific analyzer in must-gather.

**3. Network-level diagnostics (NICs, NTP, firewall, TCP connections):**
Container-level network state does not reflect host-level reality in Kubernetes. These are either inapplicable or require host-level access. NTP can be checked from NodeConfig pods in the future.

#### Design Details: Extended must-gather Output for the Converter

The converter requires must-gather to collect additional artifacts beyond what it collects today. The new must-gather artifacts, all collected via exec into running Scylla containers:

**New exec commands into Scylla containers:**

| Command | Output file | Purpose |
|---------|------------|---------|
| `cat /etc/scylla/scylla.yaml` | `scylla.yaml` | Full Scylla config |
| `ls /etc/scylla.d/ && cat /etc/scylla.d/*` | `scylla.d/<filename>` for each file | Extra config files |
| `cat /etc/sysconfig/scylla-server` or `/etc/default/scylla-server` | `sysconfig-scylla-server` | System config |
| `scylla --version` | `scylla-version.log` | Binary version |
| `nodetool cfstats` | `nodetool-cfstats.log` | Column family stats |
| `lscpu` | `lscpu.log` | CPU specs |
| `cat /proc/cpuinfo` | `cpuinfo.log` | CPU flags |
| `free -k` | `free.log` | Memory info |
| `cat /proc/meminfo` | `meminfo.log` | Detailed memory |
| `cat /proc/swaps` | `swaps.log` | Swap state |
| `sysctl fs.aio-max-nr fs.file-max fs.nr_open` | `sysctl.log` | Key sysctl values |
| `sysctl -a` | `sysctl-all.log` | Full sysctl dump |
| `uname -a` | `uname.log` | Architecture/kernel |
| `cat /etc/os-release` | `os-release.log` | OS info |
| `cat /sys/devices/system/clocksource/clocksource0/current_clocksource` | `clocksource.log` | Clock source |
| `cat /etc/selinux/config` | `selinux-config.log` | SELinux state |
| `seastar-cpu-map.sh -n scylla` | `seastar-cpu-map.log` | CPU map |
| `find /var/lib/scylla/data -name '*-Data.db'` | `sstable-listing.log` | SSTable files |

**New exec commands using `curl` to Scylla REST API (port 10000):**

| Endpoint | Output file |
|----------|------------|
| `GET /storage_service/live` + `dead` + `joining` + `leaving` + `moving` | `rest-api/cluster-status.json` |
| `GET /storage_proxy/schema_versions` | `rest-api/schema-versions.json` |
| `GET /failure_detector/endpoints/` | `rest-api/gossip-info.json` |
| `GET /storage_service/host_id` | `rest-api/token-host-mapping.json` |
| `GET /storage_service/raft_topology/cmd_rpc_status` | `rest-api/raft-topology-rpc-status.json` |
| `GET /column_family/metrics/total_disk_space_used` | `rest-api/table-disk-usage.json` |
| `GET /column_family/metrics/compression_ratio` | `rest-api/table-compression-ratio.json` |

**New exec commands using `cqlsh` for CQL queries:**

| Query | Output file |
|-------|------------|
| `SELECT * FROM system.peers` | `cql/system-peers.json` |
| `SELECT * FROM system.local` | `cql/system-local.json` |
| `SELECT * FROM system.config` | `cql/system-config.json` |
| `SELECT * FROM system.topology` | `cql/system-topology.json` |
| `SELECT * FROM system.cluster_status` | `cql/system-cluster-status.json` |
| `DESC SCHEMA` | `cql/desc-schema.txt` |
| `SELECT * FROM system.clients` | `cql/system-clients.json` |
| `SELECT * FROM system_schema.keyspaces` | `cql/system-keyspaces.json` |
| `SELECT * FROM system_schema.tables` | `cql/system-tables.json` |
| `SELECT * FROM system.scylla_local WHERE key = 'group0_upgrade_state'` | `cql/raft-group0.json` |
| `SELECT * FROM system.raft_state` | `cql/raft-state.json` |

#### Action Plan

| Phase | Work Items | Scope | Deliverable |
|-------|------------|-------|------------|
| **Phase 1: Extend must-gather collection** | Add all new exec commands listed above (REST API queries, CQL queries, system commands, config file reads) to must-gather's `podcollector.go`. This is purely adding more entries to the existing `remoteCommands` slice. | Moderate effort. Follows the exact same pattern as the existing 6 exec commands. No new architecture needed. | Richer must-gather archive. Value even without the converter — support engineers can inspect the raw data. |
| **Phase 2: Build the converter** | Implement the vitals converter as a new subcommand (e.g., `scylla-operator convert-vitals --source-dir=<must-gather-archive> --dest-dir=<output>`). For each Scylla pod found in the archive, parse the collected artifacts and produce a `CollectorResult`-compatible JSON entry for each satisfiable collector. Emit `status: 2 (SKIPPED)` for infeasible collectors. | Moderate-to-significant effort. The converter is a mapping function: for each of the 42 satisfiable collectors, parse the relevant artifact file(s) and produce the expected `data` dict shape. The shapes are well-defined by Scylla Doctor's collector implementations. | Per-pod vitals JSON files that Scylla Doctor can load. |
| **Phase 3: Validate with Scylla Doctor** | Test that `scylla-doctor --load-vitals <converted-vitals>` runs the 27 expected analyzers successfully and produces correct PASSED/WARNING/FAILED results. Also test `scylla-doctor-cluster` with multiple converted vitals files. | Testing effort. May require small fixes to the converter output format. | Validated end-to-end pipeline. |
| **Phase 4: Integrate into user flow** | Add a `--analyze` flag (or similar) to must-gather that automatically runs the converter and, if Scylla Doctor is available, runs analysis. Alternatively, document the manual two-step flow. Produce a summary report alongside the archive. | Design decision + moderate effort. | Streamlined user experience. |
| **Phase 5: K8s-specific analyzers** | Add analyzers that only make sense in Kubernetes and that Scylla Doctor will never have: PDB validation, resource requests vs node capacity, operator version compatibility, webhook health, certificate expiry, StatefulSet rollout status, CRD spec vs effective config drift. These run in must-gather natively (Go), not via Scylla Doctor. | Incremental. No dependency on Scylla Doctor. | K8s-exclusive diagnostic value. |
| **Phase 6: Ongoing maintenance** | As Scylla Doctor adds new collectors/analyzers, evaluate whether must-gather should collect the corresponding data and the converter should support the new collector's data shape. Track Scylla Doctor releases for changes to existing data shapes. | Low ongoing effort. Scylla Doctor's vitals format is stable (it is their serialization contract for `--save-vitals`/`--load-vitals`). | Keeps converter in sync. |

#### Advantages

- **Reuses Scylla Doctor's ~45 analyzers without modification.** The analysis logic (thresholds, heuristics, remediation text) is mature and field-tested. Zero reimplementation.
- **No changes to Scylla Doctor.** The converter produces standard vitals JSON. Scylla Doctor's `--load-vitals` path is the official, supported way to analyze pre-collected data. This is not a hack — it is the intended usage pattern.
- **Clean separation of concerns.** must-gather owns Kubernetes-native collection. The converter is a stateless mapping function. Scylla Doctor owns analysis. Each component evolves independently.
- **Incremental value delivery.** Phase 1 (extended collection) is valuable on its own — support engineers get deeper data even before the converter exists. Phase 2 (converter) unlocks analysis. Phase 5 (K8s analyzers) adds exclusive Kubernetes value.
- **Graceful degradation.** Collectors that cannot be satisfied in K8s are emitted as SKIPPED. Analyzers that depend on them auto-skip. No crashes, no misleading results.
- **Single collection workflow for the user.** The user still runs `must-gather` once. The converter and analysis can run automatically or as a documented second step.
- **Converter is a stable interface.** Scylla Doctor's vitals format is their serialization boundary for offline analysis. It is unlikely to change in backward-incompatible ways because `scylla-doctor-cluster` and `--load-vitals` depend on it.
- **K8s-exclusive analyzers.** Phase 5 adds diagnostics that neither Scylla Doctor nor Approach A's reimplemented analyzers would provide: CRD-level health checks, operator reconciliation validation, and Kubernetes infrastructure analysis.

#### Disadvantages

- **Two-step user flow (if not integrated).** Without the `--analyze` integration (Phase 4), users must run must-gather, then the converter, then Scylla Doctor. This is more complex than a single command.
- **Python runtime dependency for analysis.** Scylla Doctor is Python. If analysis is to run as part of the must-gather workflow, the container image needs Python + Scylla Doctor. Alternatively, analysis can run on the user's machine or the support engineer's machine after receiving the archive.
- **Converter maintenance.** The converter must track the `data` dict shapes of ~42 Scylla Doctor collectors. If Scylla Doctor changes a collector's output format, the converter needs updating. However, the vitals format is a serialization contract, so changes should be backward-compatible.
- **25 analyzers will auto-skip.** Diagnostics that require host-level or systemd data (CPU governor, perftune, NTP, storage device type) will not be analyzed. These gaps must be addressed separately via K8s-specific analyzers (Phase 5) or by collecting host-level data from NodeConfig pods in the future.
- **No analysis of K8s-specific data by Scylla Doctor.** Scylla Doctor does not understand Kubernetes resources. K8s-specific analysis (Phase 5) must be built natively in the operator codebase.

#### Addressing the Python Runtime Question

The converter itself is Go code — it lives in the operator binary. The question is where Scylla Doctor's analysis runs:

| Option | Pros | Cons |
|--------|------|------|
| **User runs Scylla Doctor locally** | No container image changes. User installs scylla-doctor via pip/package. | Extra step for user. Requires Python on user's machine. |
| **Support engineer runs analysis** | User just sends the archive. Engineer has Scylla Doctor installed. | Adds latency to support flow. |
| **must-gather container includes Scylla Doctor** | Fully automated: must-gather collects, converts, analyzes in one command. | Adds Python + Scylla Doctor to operator image. Image size increase. Cross-team packaging dependency. |
| **Separate analysis container** | `docker run scylladb/scylla-doctor:latest --load-vitals vitals.json` | Extra step, but simple. No operator image changes. |

The recommended starting point is the **separate analysis container** option: the converter produces vitals JSON files alongside the must-gather archive, and the user (or support engineer) runs Scylla Doctor in a separate container to analyze them. This keeps the operator image lean and avoids cross-team packaging dependencies, while providing a clear, documented two-container workflow.

## User Stories

### Story 1: Kubernetes User Filing a Support Case

As a Kubernetes user experiencing issues with my ScyllaDB cluster, I want to run a single command that collects all relevant Kubernetes and Scylla-internal diagnostic data, and ideally tells me if anything obvious is misconfigured, so that I can attach the output to a support ticket and get faster resolution.

**Current experience:** I run `must-gather`, which gives me YAML files and logs. I have no idea if the data is sufficient or if there is an obvious misconfiguration I could fix myself.

**Desired experience (Approach C):**
1. I run `must-gather`, which now collects deep Scylla diagnostics alongside K8s resources.
2. The archive includes a `vitals/` directory with per-pod vitals JSON files.
3. I run `scylla-doctor --load-vitals vitals/<pod>.vitals.json` (or use the analysis container) and get a diagnostic report with PASSED/WARNING/FAILED verdicts.
4. I attach both the archive and the diagnostic report to my support ticket.

### Story 2: Support Engineer Triaging a Kubernetes Case

As a support engineer, I want the diagnostic archive from a Kubernetes user to contain Scylla-internal state (schema, Raft status, gossip, table stats), system tuning data (sysctl, CPU governor, NTP), and ideally a pre-computed diagnostic summary, so that I can triage the case quickly without asking the user to run additional commands.

**Current experience:** The must-gather archive contains K8s resource YAML and pod logs. I must ask the user to manually exec into pods and run additional commands, which adds days to resolution time.

**Desired experience (Approach C):** The archive contains all Scylla-internal state. I run `scylla-doctor-cluster` against the vitals directory and get both per-node and cross-node diagnostic reports using the same tool and logic I already use for bare-metal cases.

### Story 3: Self-Service Diagnosis

As a Kubernetes user, I want to periodically run diagnostics against my cluster to validate that my configuration follows best practices, so that I can catch issues before they become incidents.

**Current experience:** must-gather has no analysis capability. I would need to interpret raw YAML files myself.

**Desired experience (Approach C):** I run must-gather, then run the Scylla Doctor analysis container against the output. I get a report telling me if my Scylla configuration follows best practices. The 27 running analyzers cover the most critical checks: schema agreement, Raft health, version support, address configuration, internode compression, and configuration consistency.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Exec-based collection is slow for large clusters | Parallelize exec calls across pods (must-gather already parallelizes). Set reasonable timeouts. |
| Some system-level data requires privileged access | Leverage NodeConfig pods (which have host mounts) for host-level data. Document which diagnostics require NodeConfig to be deployed. |
| REST API / CQL queries may fail on unhealthy clusters | Use `--keep-going` semantics (already the default). Capture errors as diagnostic data. A failed CQL connection is itself informative. The converter emits FAILED status for collectors whose data could not be collected. |
| Scylla Doctor changes the vitals data shape | The vitals format is Scylla Doctor's serialization contract for `--save-vitals`/`--load-vitals`. Breaking changes are unlikely and would also break `scylla-doctor-cluster`. Pin converter to a known-compatible Scylla Doctor version range and track releases. |
| 25 analyzers auto-skip in K8s | Expected and acceptable. These analyzers check host-level concerns (systemd services, CPU governor, perftune) that are managed differently in K8s. Address the most important ones (NTP, storage type) via K8s-specific analyzers in Phase 5. |
| User flow requires two steps (must-gather + scylla-doctor) | Phase 4 addresses this with integration options. Even without integration, the two-step flow is well-documented and not dissimilar from the current scylla-doctor + scylla-doctor-cluster bare-metal workflow. |

## Recommendation

**Approach C (hybrid: must-gather collection + vitals converter + Scylla Doctor analysis) is the recommended path.**

It combines the strengths of Approaches A and B while avoiding their key weaknesses:

- **From Approach A:** must-gather remains the single Kubernetes-native collection tool. The user workflow starts (and can end) with a single `must-gather` command. All collection runs through the Kubernetes API. The codebase stays in Go. K8s-exclusive analyzers are built natively.
- **From Approach B:** Scylla Doctor's ~45 analyzers are reused without modification. No reimplementation of analysis logic. The same diagnostic engine that support engineers use for bare-metal cases works for Kubernetes.
- **Unique to Approach C:** Clean separation at a stable interface boundary (the vitals JSON format). Each team maintains their own component. The converter is a well-scoped mapping function, not a deep architectural change to either tool.

The converter itself is constrained in scope: it is a stateless function that reads files from a must-gather archive and produces JSON dicts matching the shapes defined by Scylla Doctor's collectors. The shapes are observable from the Scylla Doctor source code and stable because they are part of the `--save-vitals`/`--load-vitals` contract.

The main trade-off — that 25 analyzers will auto-skip because their collectors are infeasible in containers — is acceptable. Those analyzers check host-level systemd and hardware concerns that are managed differently in Kubernetes (via NodeConfig). The 27 analyzers that do run cover the most critical Scylla-level diagnostics, and K8s-specific analyzers (Phase 5) will address Kubernetes-unique concerns that no Scylla Doctor analyzer covers.

## Implementation History

- 2025-03-26: Initial gap analysis and evaluation of Approaches A and B.
- 2025-03-26: Expanded with Approach C (hybrid) design, vitals format analysis, converter specification, and per-collector/analyzer feasibility mapping.

## Drawbacks

- The converter introduces a new component to maintain. It must track Scylla Doctor's vitals format, which, while stable, could change across major versions.
- 25 of ~58 analyzers will not run in the Kubernetes context, leaving gaps in host-level diagnostics (CPU tuning, NTP, storage device type, perftune validation). These are partially addressable via K8s-specific analyzers but will never have the same depth as Scylla Doctor's bare-metal checks.
- The user flow, before Phase 4 integration, requires two steps: must-gather + separate Scylla Doctor invocation. This is more complex than a fully integrated single-command experience.

## Alternatives

### Approach A: Extend must-gather with Native Analysis

Reimplement Scylla Doctor's analysis logic in Go inside must-gather. This gives a single-command experience but requires significant effort to reach parity with Scylla Doctor's ~45 analyzers and creates an ongoing double-maintenance burden as both tools must track ScyllaDB changes independently.

### Approach B: Extend Scylla Doctor with Kubernetes Intelligence

Make Scylla Doctor natively Kubernetes-aware. This reuses all existing logic but requires deep architectural changes to Scylla Doctor's execution model (local-only to remote-via-K8s-exec), introduces Python into the operator ecosystem, and creates cross-team release coupling. It is the right choice if the organization wants a single diagnostic tool for all deployment models, but it has the longest time-to-value for Kubernetes users.
