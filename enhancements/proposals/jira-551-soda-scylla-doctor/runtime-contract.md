# ScyllaDB image runtime contract for Scylla Doctor

SODA runs Scylla Doctor inside the scylla container of each ScyllaDB Pod, so the ScyllaDB image is Scylla Doctor's
runtime. This page lists what Scylla Doctor 1.15 uses from it.

The source of truth is
[images/scylla-doctor-bundle/runtime-contract.json](../../../images/scylla-doctor-bundle/runtime-contract.json).
It ships in the Scylla Doctor bundle, so each Scylla Doctor version declares its own needs. SODA turns it into a
read-only shell script and runs it in every scylla container before collecting:
- the result is recorded as `ScyllaDBRuntimeContractCollector` in SODA's vitals;
- `ScyllaDBRuntimeContractAnalyzer` reports gaps;
- the HTML report shows a requirement-by-node table.

A failed check doesn't stop collection; Scylla Doctor reports what it can.

## How it was derived

- **Trace:** an strace of a complete Scylla Doctor 1.15 run in `scylladb/scylla:2026.3.3` (programs executed, files
  opened, connections), attributed per program.
- **Source:** Scylla Doctor's source at tag `scylla-doctor-v1.15`, for the checks that were skipped in the trace.
- **Counts:** "without it" counts come from Scylla Doctor's declared dependencies between checks, cross-checked against
  runs where the scylla container's root was not available.

## Levels

- **required:** without it, Scylla Doctor can't run, or most checks fail. This includes what SODA's own exec
  mechanics need: `sh`, `cat`, `mkdir`, `rm`, `chmod`, `timeout` and a writable `/tmp`.
- **recommended:** without it, the named checks fail or skip.
- **optional:** missing from the image today; installing it enables the named checks. With them installed, 6 of the
  9 checks that skip for missing tools pass. The other 3 also need `CAP_NET_ADMIN`, a `chronyc` package UBI doesn't
  have, or a cloud provider.

## Requirements

| Requirement | Level | Check | Used by | Without it |
|---|---|---|---|---|
| `python` | required | executable: `/opt/scylladb/python3/bin/python3` | Scylla Doctor itself, cqlsh, perftune.py | Scylla Doctor cannot start |
| `python-version` | required | Python >= 3.8 | Scylla Doctor itself | Scylla Doctor cannot start |
| `pyyaml` | required | Python module `yaml` | Scylla Doctor itself (its only non-stdlib import) | Scylla Doctor cannot start |
| `posix-shell` | required | on `PATH`: `sh`, `cat`, `mkdir`, `rm`, `chmod`, `timeout`, `wc` | SODA: staging, bounding the run, collecting side files, cleanup | SODA cannot run Scylla Doctor |
| `tmp-writable` | required | writableDirectory: `/tmp` | SODA: run directory | SODA cannot run Scylla Doctor |
| `home-writable` | required | writableDirectory: `/root` | cqlsh unpacks its bundled packages into $HOME/.shiv | cqlsh fails, and with it 26 checks |
| `scylla-yaml` | required | file: `/etc/scylla/scylla.yaml` | Scylla Doctor's configuration checks, cqlsh address | 29 checks fail |
| `cqlsh` | required | executable: `/opt/scylladb/share/cassandra/bin/cqlsh` | every CQL check, and the REST checks (API address from system.config) | 26 checks fail |
| `scylla` | recommended | executable: `/opt/scylladb/bin/scylla` | ScyllaVersion, ScyllaBinary, nodetool | 3 checks fail |
| `nodetool` | recommended | executable: `/opt/scylladb/bin/nodetool` | NodetoolCFStats | 1 check fails |
| `perftune` | recommended | executable: `/opt/scylladb/scripts/perftune.py` | StorageConfiguration, PerftuneSystemConfiguration, CPUSet | 3 checks fail |
| `hwloc-calc` | recommended | executable: `/opt/scylladb/bin/hwloc-calc` | CPUSet, perftune.py | 1 check fails |
| `seastar-cpu-map` | recommended | executable: `/opt/scylladb/scripts/seastar-cpu-map.sh` | SeastarCPUMap | 1 check fails |
| `scylla-d` | recommended | directory: `/etc/scylla.d` | ScyllaExtraConfigurationFiles, CPUSet | 2 checks fail |
| `sysconfig` | recommended | file: `/etc/sysconfig/scylla-server` | ScyllaSystemConfigurationFiles | 1 check fails |
| `os-release` | recommended | file: `/etc/os-release` | OS | 1 check fails |
| `data-dir` | recommended | directory: `/var/lib/scylla` | ScyllaSSTables | 1 check reports no SSTables |
| `supervisorctl` | recommended | on `PATH`: `supervisorctl` | ServiceManager, ScyllaServices; REST checks fall back to the process list | service manager misdetected as systemd, 1 check fails |
| `rpm` | recommended | on `PATH`: `rpm` | ScyllaVersion (edition detection) | 1 check fails |
| `base-tools` | recommended | on `PATH`: `bash`, `basename`, `dirname`, `readlink`, `realpath`, `cut`, `grep`, `ls`, `env`, `df`, `mount`, `free`, `lscpu`, `dmesg`, `uname`, `sysctl`, `pgrep`, `taskset`, `systemd-detect-virt` | wrappers of ScyllaDB's tools and the OS checks | the checks using the missing tool skip or fail |
| `network-tools` | optional | on `PATH`: `ethtool`, `ip`, `iptables`, `ss`, `lspci`, `chronyc` | NICs, IPAddresses, IPRoutes, InfrastructureProvider, FirewallRules, TCPConnections, LSPCI, ChronyStatus | missing today: 9 checks skip |

| Service | Endpoint | Used by |
|---|---|---|
| `cql` | rpc_address:9042 (scylla.yaml) | cqlsh |
| `rest-api` | api_address:10000 (system.config; the operator sets 127.0.0.1) | REST checks, nodetool |
| `supervisord` | 127.0.0.1:9001 (/etc/supervisord.conf) | supervisorctl |
| `scylla-process` | a process named scylla in the container's PID namespace | SeastarCPUMap, service fallback |

## Notes

- **What is verified and what is inferred.** Each requirement was either seen in the trace or read in the source.
  The "without it" impacts are inferred from Scylla Doctor's dependencies between checks, except for the cases
  observed in the spike: no scylla container root (all ScyllaDB tools, configuration and data), a failed `cqlsh`
  login (26 checks) and missing `free`, `pgrep` and `sysctl`. Removing `rpm`, `supervisorctl` or a writable `$HOME`
  alone was never tried. That the wrappers use absolute paths was confirmed for `scylla` and `hwloc-calc`.

- **The interpreter is `scylla-python3`.** Scylla Doctor's own Python needs are small: Python 3.8 or newer and PyYAML.
  But `cqlsh` and `perftune.py` run on `scylla-python3` too, so the image must keep it even if Scylla Doctor brought
  its own Python.
- **`supervisorctl` decides the service manager.** Without it, Scylla Doctor falls back to `systemctl` and reports
  systemd. The REST checks fall back to finding the `scylla` process in `/proc`. This matters for an image without
  supervisord.
- **Paths are fixed.** The ScyllaDB tools' wrappers find their libraries and binaries by absolute path under
  `/opt/scylladb`, so they only work from inside the scylla container's own root.
- **The services are runtime, not image, properties.** The operator sets `api_address` to `127.0.0.1`; Scylla Doctor
  reads it from `system.config` through `cqlsh`.
- **Some checks never run in a container** (12 in Scylla Doctor 1.15): they need the host's journal, systemd,
  `CAP_SYSLOG` and so on. They aren't part of this contract.
