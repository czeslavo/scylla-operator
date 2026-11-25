# SODA formats and their relation to Scylla Doctor's

Scylla Doctor's contracts are `status` and `message` in three outputs: per-node vitals, per-node analysis and the
Scylla Doctor Cluster report. Collector `data` is not a contract. SODA follows that:
- it never interprets the `data` of Scylla Doctor's collectors;
- it reads `data` only from collectors it writes itself;
- it keeps Scylla Doctor's own files byte-identical.

## Vitals

Scylla Doctor's per-node vitals are an object keyed by collector name:

```json
{"CqlshCollector": {"status": 0, "data": {...}, "message": "Data collected", "mask": []}}
```

Collector `status`: 0 passed, 1 failed, 2 skipped. Analyzer `status` uses different numbers: 0 passed, 1 skipped,
2 warning, 3 failed.

**SODA's vitals use exactly this format,** one file per node: `soda/vitals/<node>.vitals.json`.

| Collector | `data` |
|---|---|
| `SODAVersionCollector` | `version`, `formatVersion` (`soda.scylladb.com/vitals/v1alpha1`) |
| `ScyllaDBPodCollector` | the Pod as Kubernetes sees it: phase, readiness, QoS class, Kubernetes node, datacenter, rack, ScyllaDB version label, containers (ready, restarts, last termination reason), the scylla container's requests and limits. Ephemeral containers are left out. |
| `ScyllaDBRuntimeContractCollector` | the contract schema and Scylla Doctor versions, the interpreter, and one result per requirement: `id`, `level`, `satisfied`, `detail`, `usedBy`, `impact` |

**Why separate files and not extra entries in Scylla Doctor's.** Scylla Doctor and Scylla Doctor Cluster refuse
vitals without their `SDVersionCollector`. That was checked with Scylla Doctor 1.15: Scylla Doctor Cluster reports a
version mismatch, and `--load-vitals` raises. Writing a fake `SDVersionCollector` would be wrong, and editing Scylla
Doctor's files would break support tooling (Iliad imports them as they are). So SODA has its own files in the same
format, under the same node names (the Pod name, which is also the stem of Scylla Doctor's vitals file).

**Proposed extension, backward compatible.** A vitals file may hold a source collector other than
`SDVersionCollector`, for example `SODAVersionCollector`, with `{"source": "soda", "version": ..., "formatVersion": ...}`.
Scylla Doctor's tools would:
- skip their version check for such files;
- skip their own analyzers on them;
- include their collectors in the comparison between nodes.

Scylla Doctor Cluster could then compare SODA's collectors between nodes the same way, for example to show a node
whose Pod is not Guaranteed while others are. Until then, SODA merges results itself (see below).

**Cluster-scoped vitals: a proposal, not built.** Some Kubernetes facts belong to the cluster, not a node: the
ScyllaCluster's conditions, the operator's version and configuration, NodeConfigs. A file
`soda/vitals/_cluster.vitals.json` in the same format would hold them, with findings attributed to the node name
`_cluster`. This keeps the report's `{message: [nodes]}` shape.

**Kubernetes objects and logs are not vitals.** Manifests and container logs stay as must-gather files under
`kubernetes/must-gather/`, in the layout `scylla-operator must-gather` already produces. Vitals hold facts that an
analyzer judges and that Scylla Doctor Cluster can compare between nodes. Raw objects already have a format and
tooling, and copying them into vitals would only duplicate them. When a check needs something from them, a SODA
collector extracts just that fact into vitals, as `ScyllaDBPodCollector` does for the Pod.

## Analysis

Scylla Doctor's per-node analysis (`--load-vitals <file> --output json`) is keyed by analyzer name:

```json
{"AuthenticationAnalyzer": {"description": "...", "status": 3, "message": "..."}}
```

SODA's analyzers write the same format to `soda/analysis/<node>.analysis.json`. Like Scylla Doctor's, an analyzer
whose collector failed is failed with "Required X was not successful", and one whose collector was skipped is
skipped.

## Combined report

Scylla Doctor Cluster's JSON report (`--output json`):

```json
{
  "vitals": ["<paths>"],
  "failures": {"<Analyzer>": {"<message>": ["<node>", ...]}},
  "warnings": {"<Analyzer>": {"<message>": ["<node>", ...]}},
  "inconsistencies": {"<Collector>": [["<node A>", "<node B>", {<deepdiff>}], ...]}
}
```

SODA's `report/soda-report.json` has **the same four keys with the same meaning**, so any reader of Scylla Doctor
Cluster's report can read it:
- `failures` and `warnings` hold Scylla Doctor Cluster's findings plus SODA's analyzers' findings, merged by analyzer
  and message.
- `inconsistencies` is Scylla Doctor Cluster's, unchanged.
- `vitals` lists the archive-relative paths of every vitals file used, Scylla Doctor's and SODA's.
- `SODACollectionAnalyzer` reports nodes whose collection failed (no usable vitals) or is incomplete (a check every
  other check depends on didn't pass). Scylla Doctor Cluster can't report those, because the nodes have no usable
  vitals.

It adds one optional key, `soda`:

| Field | Meaning |
|---|---|
| `formatVersion` | `soda.scylladb.com/report/v1alpha1` |
| `generatedAt`, `sodaVersion`, `scyllaDoctorVersion` | when and with what |
| `bundle` | bundle image and the sha256 of each file in it |
| `cluster` | kind, namespace, name, UID |
| `nodes[]` | per node: Pod, Kubernetes node, datacenter, rack, collection state, reason, duration and exit code, and Scylla Doctor's collector and analyzer counts by status |
| `sources` | for every analyzer or collector in the report, `scylla-doctor` or `soda` |
| `skippedAnalyzers` | analyzers turned off for this run (by default the two that call scylladb.com) |
| `errors` | problems during the run that aren't findings about the cluster |

**Proposed changes for Scylla Doctor Cluster, backward compatible:**
- add the `nodes` metadata and passed/skipped counts, so a report alone says how much was checked;
- add `sources` once it accepts non-Scylla-Doctor vitals;
- use an exit code that distinguishes errors from findings.

## Archive

`<dest-dir>.tar.gz`:

```
manifest.json                       run ID, versions, analysis runtime, cluster, bundle, nodes, layout, errors
scylla-operator-diagnose.log        SODA's log
scylla-doctor/vitals/<node>.vitals.json        Scylla Doctor, unchanged
scylla-doctor/analysis/<node>.analysis.json    Scylla Doctor per-node analysis, unchanged
scylla-doctor/cluster/report.json              Scylla Doctor Cluster report, unchanged (+ report.stderr.log)
scylla-doctor/runs/<node>/{stdout,stderr}.log  Scylla Doctor's console output
soda/vitals/<node>.vitals.json                 SODA's vitals (Scylla Doctor's format)
soda/analysis/<node>.analysis.json             SODA's analysis (Scylla Doctor's format)
kubernetes/must-gather/...                     must-gather of the ScyllaCluster's namespace (no Secrets)
report/soda-report.json                        combined report
report/report.html                             combined report for people
```

Support can import `scylla-doctor/vitals/` into existing tooling as it is, because it is a normal Scylla Doctor
vitals directory.
