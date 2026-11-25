# SODA built on Scylla Doctor (OPERATOR-551 proof of concept)

## Summary

SODA (Scylla Operator Diagnostic Automation) is `scylla-operator diagnose`. It diagnoses one ScyllaCluster by
running Scylla Doctor on every ScyllaDB node. It adds Kubernetes-side vitals and a must-gather dump, analyzes all of
it with Scylla Doctor, Scylla Doctor Cluster and its own analyzers, and writes an archive with:
- Scylla Doctor's outputs, unchanged;
- a combined report in Scylla Doctor Cluster's format;
- an HTML report.

This is a proof of concept. It runs end to end on kind, both from a workstation and as an in-cluster Job.

Related documents:
- [runtime-contract.md](runtime-contract.md): what Scylla Doctor needs from the ScyllaDB image it runs in.
- [formats.md](formats.md): the vitals, analysis, report and archive formats, and the backward-compatible
  extensions SODA adds.

## How it works

```
 workstation (kubeconfig of the soda ServiceAccount)        or   in-cluster Job (runs in the bundle image)
   scylla-operator diagnose --bundle-image IMAGE                   scylla-operator diagnose --bundle-dir /opt/scylla-doctor
        |                                                                |
        | 1. start an analysis Pod from the bundle image                 | (bundle is local)
        |    (the kubelet pulls it: pull secrets, mirrors)                |
        | 2. read the collection pyz and runtime contract from it        |
        v                                                                v
 for every ScyllaDB Pod, over exec into the scylla container (bounded parallelism, per-step timeouts):
   a. check the runtime contract (`sh -s`, script on stdin, read-only)
   b. stream in scylla_doctor.pyz and, with CQL auth, sd.conf (stdin only, umask 077)
   c. run it on the image's own Python: timeout N /opt/scylladb/python3/bin/python3 scylla_doctor.pyz --save-vitals
   d. read vitals and console output back with separate execs; rm -rf the run directory (also on cancellation)
 Kubernetes side: SODA collectors per node (Pod, runtime contract, SODA version) + must-gather of the namespace
 analysis in the bundle (analysis Pod or locally):
   Scylla Doctor per-node analysis (--load-vitals --output json), Scylla Doctor Cluster (--output json),
   SODA analyzers (Go) on SODA's vitals
 combined report (Scylla Doctor Cluster format + `soda` key) -> report.html -> <dest-dir>.tar.gz
```

The ScyllaDB image is the runtime Scylla Doctor runs on. Its Python, `cqlsh`, the `scylla` and `nodetool` wrappers,
`perftune.py` and about 20 base tools are all used. [runtime-contract.md](runtime-contract.md) lists them. The
contract ships in the bundle, and SODA checks it in every container before collecting, so a gap in the image shows
up as a finding, not as a puzzling run.

## Running it

### 1. Build the Scylla Doctor bundle image

See [images/scylla-doctor-bundle](../../../images/scylla-doctor-bundle). The full build and Scylla Doctor Cluster
are private, so their inputs stay out of git.

```bash
images/scylla-doctor-bundle/prepare-inputs.sh scylla-doctor-1.15.tar.gz scylla-doctor-full-1.15.tar.gz path/to/scylla-doctor-repo
podman build -t registry.example.com/scylla-doctor-bundle:1.15 images/scylla-doctor-bundle
```

### 2. Create SODA's identity

Edit the namespace in [examples/soda/rbac.yaml](../../../examples/soda/rbac.yaml) and apply it. With CQL
authentication, create a Secret with `username` and `password` keys. The role needs
`SELECT ON KEYSPACE system`.

```bash
kubectl -n scylla create secret generic soda-cql --from-file=username=./user --from-file=password=./password
```

### 3a. From a workstation

Apply [rbac-workstation.yaml](../../../examples/soda/rbac-workstation.yaml) too; it lets SODA create and delete its
analysis Pod. Then:

```bash
examples/soda/kubeconfig.sh scylla ./soda.kubeconfig 2h          # as a cluster admin
scylla-operator diagnose --kubeconfig ./soda.kubeconfig --scylla-cluster scylla \
  --bundle-image registry.example.com/scylla-doctor-bundle:1.15 --cql-credentials-secret soda-cql
```

### 3b. In the cluster

Edit the namespace, ScyllaCluster name and images in [examples/soda/job.yaml](../../../examples/soda/job.yaml) and
apply it. The Job's comments show how to copy the archive out.

### Useful flags

`--node-timeout`, `--parallelism`, `--skip-analyzer`, `--skip-must-gather`, `--dest-dir`, `--bundle-pull-secret`,
`--analysis-memory`, `--keep-running-after`. By default, `ScyllaUpdateAnalyzer` and `DriverVersionAnalyzer` are
skipped because they call scylladb.com, so analysis works without internet access.

## Decisions

| Decision | Why |
|---|---|
| Scylla Doctor runs inside the scylla container over exec, on the image's Python and tools | It is the simplest mode with complete results. An ephemeral container that chroots into the scylla container matched it in the spike, but needs SYS_NICE and leaves terminated containers in the Pod spec. A sidecar or an ephemeral container without chroot sees the wrong filesystem, and most checks fail or report the wrong facts. |
| Scylla Doctor is delivered by a bundle image, not installed at runtime and not baked into the ScyllaDB image | It is versioned and pinned like other operator images. The kubelet pulls it, so registry handling isn't SODA's problem. |
| Analysis runs in the bundle image: an analysis Pod from a workstation, locally in the Job | Python, deepdiff and memory for Scylla Doctor Cluster come with the bundle, so the workstation needs nothing installed. |
| The in-cluster mode is a plain Job | It's enough for the PoC. The operator can later create the same Job from a request object that names only the ScyllaCluster, with the image pinned by the operator release and no user-chosen commands. |
| SODA's vitals and analysis use Scylla Doctor's formats but live in separate files | Scylla Doctor's tools reject vitals without their own version collector (checked). Scylla Doctor's files stay byte-identical for support tooling. |
| The combined report is Scylla Doctor Cluster's JSON plus an optional `soda` key | Existing readers keep working. See [formats.md](formats.md). |
| A node is judged by its vitals, not by exit codes | Scylla Doctor exits 0 with failed checks, and Scylla Doctor Cluster exits 1 on any finding. SODA checks that the vitals parse and that `ScyllaConfigurationFile`, `Cqlsh` and `SystemConfig` passed. |
| Failures are recorded, not fatal | A failed node, analysis step or must-gather becomes a finding or a "problem during the run"; the rest continues. |
| Secrets only over stdin | Exec arguments end up in API requests and audit logs. `sd.conf` is written with umask 077 and removed even when the run is cancelled. |
| Output goes to files in the container and is read back with separate execs | A broken or hanging exec stream (seen once on GKE) then loses nothing: SODA reads the exit code and vitals back, and fails the node only when Scylla Doctor didn't finish. Kind can't reproduce the hang, so this path is untested. |
| Cleanup stops an unfinished run | Closing an exec stream doesn't stop the command. Cleanup stops Scylla Doctor by its PID when it hasn't written an exit code, then removes the run directory. |
| ScyllaCluster (v1) only | Enough for the PoC. ScyllaDBDatacenter and ScyllaDBCluster need their own lookup of the member Pods. |
| must-gather is reused with the `view` role | It collects what `view` can read in the namespace and logs the rest (for example Roles, Leases, monitoring CRs) as forbidden. |

## What was validated (kind, ScyllaDB 2026.3.3, three nodes with CQL authentication)

- **Workstation mode as the `soda` ServiceAccount:** all three nodes collected, 40 s end to end, a 3.1 MB archive.
  Node 1's Scylla Doctor statuses are identical to the spike's exec baseline: 50 passed, 21 skipped, 3 failed.
- **In-cluster Job:** the same results; the archive was copied out of the Pod with `kubectl cp`.
- **The combined report:**
  - 15 failures, 10 warnings and 7 differences between nodes from Scylla Doctor Cluster;
  - SODA's Pod analyzer warns about Burstable QoS on all nodes;
  - the runtime contract passes, with only the six optional network tools missing.
  Node 0 has extra tools from an earlier spike run, which accounts for the differences between nodes.
- **Hygiene:** no CQL password in the run directory or the archive; no `/tmp/soda-*` left in the scylla containers;
  the analysis Pod is deleted; Secrets are not in the must-gather dump.
- **Wrong CQL password:** every node is reported as Degraded, and SODA's first failure names the cause ("calling cqlsh
  failed, please check cql authentication configuration"). The run still completes.
- **Interrupted mid-collection (SIGINT):**
  - The run directory and the Scylla Doctor process were gone at once, and the analysis Pod was deleted.
  - A `cqlsh` query already in flight ran to completion on its own within about 30 s, with the password in its
    arguments until then.
  - SODA still wrote a partial archive recording the failed nodes.
- **RBAC:** derived by running as the ServiceAccount. Exec needs both `get` (websocket) and `create` (SPDY) on
  `pods/exec`.

## Not done, and open questions

- **Resource impact:** Scylla Doctor runs in the scylla container's cgroup, on ScyllaDB's CPUs and memory limit. That
  isn't measured on a Guaranteed Pod with the static CPU manager; do it on GKE before periodic runs are considered.
- **Operator orchestration and periodic runs with opt-in upload:** not built. The Job shows the shape.
- **Distribution:** the full Scylla Doctor build and Scylla Doctor Cluster are private (DOCTOR-159), so who may pull
  the bundle needs deciding.
- **Multi-datacenter clusters, ScyllaDBDatacenter and ScyllaDBCluster:** not covered.
- **Collection scope:** only the ScyllaCluster's namespace, because the role is namespaced. The operator's logs and
  configuration, ScyllaOperatorConfig, NodeConfigs and Kubernetes nodes are not collected. They need a cluster-scoped
  read role, or the existing `must-gather` run by an admin.
- **Production-like clusters:** nothing ran on GKE or OpenShift in this PoC; everything above is kind.
- **Cluster-scoped Kubernetes vitals:** SODA's collectors are per node. A proposal for cluster-scoped ones is in
  [formats.md](formats.md).
- **Air gap:** analysis skips the two analyzers that call scylladb.com; collection makes no external calls beyond
  the cloud metadata probes Scylla Doctor does.

## Asks for the Scylla Doctor team

1. Own the runtime contract in the bundle and test every release against every ScyllaDB image the pinning operator
   release supports, by running inside the image as SODA does.
2. Don't pass the CQL password on `cqlsh`'s command line inside the container; use a cqlshrc or credentials file.
3. Make exit codes distinguish "the tool failed" from "the node has findings".
4. Accept non-Scylla-Doctor vitals sources in Scylla Doctor Cluster, or define a merge step. See [formats.md](formats.md).
5. Mask per-node values in `ScyllaExtraConfigurationFiles` (`docker.conf` holds each node's address) and make the
   `NitroDetailedStats` skip message deterministic: both produce noise in the differences between nodes.
6. Publish the bundle (or its inputs) from release engineering.

## Asks for the ScyllaDB image owners

Treat [runtime-contract.md](runtime-contract.md) as part of the image's interface, especially before splitting the
image into one binary per image. `scylla-python3`, `cqlsh`, the `scylla`/`nodetool` wrappers, `perftune.py`,
`supervisorctl`, `rpm` and the base tools are all used.
