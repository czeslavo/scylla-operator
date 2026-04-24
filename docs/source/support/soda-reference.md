# soda — Diagnostic Reference

Auto-generated documentation for all collectors, analyzers, and profiles.

## Profiles

### `full`

Run all available diagnostic collectors and analyzers

**Includes:** health, logs

**Collectors:** 35 &nbsp; **Analyzers:** 5

### `health`

Collect Scylla runtime health data and run all analyzers

**Collectors:** 6 &nbsp; **Analyzers:** 5

### `logs`

Collect container logs from all Scylla and operator pods

**Collectors:** 3 &nbsp; **Analyzers:** 0

## Collectors

Total: 35

| ID | Name | Description | Scope | Profiles | RBAC |
|---|---|---|---|---|---|
| `NodeResourcesCollector` | Kubernetes Node resources | Collects Kubernetes Node resources including capacity, allocatable resources, labels, and conditions. | ClusterWide | full | core/nodes: get,list |
| `OSInfoCollector` | OS information | Reads /etc/os-release inside each Scylla pod to identify the OS distribution and version. | PerScyllaNode | full, health | core/pods/exec: create |
| `ScyllaVersionCollector` | Scylla version | Runs 'scylla --version' inside each Scylla pod to capture the exact Scylla version and build. | PerScyllaNode | full, health | core/pods/exec: create |
| `SchemaVersionsCollector` | Schema versions | Queries the Scylla REST API for the schema version reported by each node, used for schema agreement analysis. | PerScyllaNode | full, health | core/pods/exec: create |
| `ScyllaConfigCollector` | Scylla configuration | Reads /etc/scylla/scylla.yaml from each Scylla pod to capture the active configuration. | PerScyllaNode | full | core/pods/exec: create |
| `SystemPeersLocalCollector` | System local and peers | Queries system.local and system.peers CQL tables via cqlsh to capture cluster membership and topology from each node's perspective. | PerScyllaNode | full, health | core/pods/exec: create |
| `GossipInfoCollector` | Gossip info | Queries the Scylla REST API for gossip endpoint state including liveness, generation, and application state. | PerScyllaNode | full, health | core/pods/exec: create |
| `SystemTopologyCollector` | System topology | Queries the system.topology CQL table via cqlsh to capture node state, shard count, and upgrade status. | PerScyllaNode | full, health | core/pods/exec: create |
| `SystemConfigCollector` | System config | Queries the system.config CQL table via cqlsh to capture runtime configuration parameters and their sources. | PerScyllaNode | full | core/pods/exec: create |
| `ScyllaDConfigCollector` | Scylla drop-in config directory | Lists and reads all files under /etc/scylla.d/ to capture drop-in configuration overrides. | PerScyllaNode | full | core/pods/exec: create |
| `DiskUsageCollector` | Disk usage | Runs 'df -h' inside each Scylla pod to capture filesystem disk usage. | PerScyllaNode | full | core/pods/exec: create |
| `RlimitsCollector` | Scylla process resource limits | Reads /proc/<pid>/limits inside each Scylla pod to capture process resource limits (open files, memory, etc.). | PerScyllaNode | full | core/pods/exec: create |
| `ScyllaClusterCollector` | ScyllaCluster manifests | Collects ScyllaCluster custom resource manifests from all namespaces. | ClusterWide | full | scylla.scylladb.com/scyllaclusters: get,list |
| `ScyllaDBDatacenterCollector` | ScyllaDBDatacenter manifests | Collects ScyllaDBDatacenter custom resource manifests from all namespaces. | ClusterWide | full | scylla.scylladb.com/scylladbdatacenters: get,list |
| `NodeManifestCollector` | Node manifests | Collects Kubernetes Node manifests including capacity, allocatable resources, and conditions. | ClusterWide | full | core/nodes: get,list |
| `NodeConfigCollector` | NodeConfig manifests | Collects ScyllaDB NodeConfig custom resource manifests. | ClusterWide | full | scylla.scylladb.com/nodeconfigs: get,list |
| `ScyllaOperatorConfigCollector` | ScyllaOperatorConfig manifests | Collects ScyllaOperatorConfig custom resource manifests. | ClusterWide | full | scylla.scylladb.com/scyllaoperatorconfigs: get,list |
| `DeploymentCollector` | Deployment manifests | Collects Deployment manifests from ScyllaDB operator namespaces. | ClusterWide | full | apps/deployments: get,list |
| `StatefulSetCollector` | StatefulSet manifests | Collects StatefulSet manifests from ScyllaDB operator namespaces. | ClusterWide | full | apps/statefulsets: get,list |
| `DaemonSetCollector` | DaemonSet manifests | Collects DaemonSet manifests from ScyllaDB operator namespaces. | ClusterWide | full | apps/daemonsets: get,list |
| `ConfigMapCollector` | ConfigMap manifests | Collects ConfigMap manifests from ScyllaDB operator namespaces for configuration analysis. | ClusterWide | full | core/configmaps: get,list |
| `ServiceCollector` | Service manifests | Collects Service manifests from ScyllaDB operator namespaces. | ClusterWide | full | core/services: get,list |
| `ServiceAccountCollector` | ServiceAccount manifests | Collects ServiceAccount manifests from ScyllaDB operator namespaces. | ClusterWide | full | core/serviceaccounts: get,list |
| `PodManifestCollector` | Pod manifests (operator namespaces) | Collects Pod manifests from ScyllaDB operator namespaces. | ClusterWide | full | core/pods: get,list |
| `ScyllaClusterStatefulSetCollector` | ScyllaCluster StatefulSet manifests | Collects StatefulSet manifests owned by a ScyllaCluster. | PerScyllaCluster | full | apps/statefulsets: get,list |
| `ScyllaClusterServiceCollector` | ScyllaCluster Service manifests | Collects Service manifests owned by a ScyllaCluster. | PerScyllaCluster | full | core/services: get,list |
| `ScyllaClusterConfigMapCollector` | ScyllaCluster ConfigMap manifests | Collects ConfigMap manifests owned by a ScyllaCluster. | PerScyllaCluster | full | core/configmaps: get,list |
| `ScyllaClusterPodCollector` | ScyllaCluster Pod manifests | Collects Pod manifests owned by a ScyllaCluster. | PerScyllaCluster | full | core/pods: get,list |
| `ScyllaClusterPDBCollector` | ScyllaCluster PodDisruptionBudget manifests | Collects PodDisruptionBudget manifests owned by a ScyllaCluster. | PerScyllaCluster | full | policy/poddisruptionbudgets: get,list |
| `ScyllaClusterServiceAccountCollector` | ScyllaCluster ServiceAccount manifests | Collects ServiceAccount manifests owned by a ScyllaCluster. | PerScyllaCluster | full | core/serviceaccounts: get,list |
| `ScyllaClusterRoleBindingCollector` | ScyllaCluster RoleBinding manifests | Collects RoleBinding manifests owned by a ScyllaCluster. | PerScyllaCluster | full | rbac.authorization.k8s.io/rolebindings: get,list |
| `ScyllaClusterPVCCollector` | ScyllaCluster PersistentVolumeClaim manifests | Collects PersistentVolumeClaim manifests owned by a ScyllaCluster. | PerScyllaCluster | full | core/persistentvolumeclaims: get,list |
| `ScyllaNodeLogsCollector` | Scylla node container logs | Collects current and previous container logs from each Scylla node pod. | PerScyllaNode | full, logs | core/pods: get,list; core/pods/log: get |
| `OperatorPodLogsCollector` | Operator pod logs | Collects current and previous container logs from ScyllaDB operator pods. | ClusterWide | full, logs | core/pods: get,list; core/pods/log: get |
| `ScyllaClusterJobLogsCollector` | ScyllaCluster cleanup job pod logs | Collects current and previous container logs from cleanup job pods belonging to a ScyllaCluster. | PerScyllaCluster | full, logs | core/pods: get,list; core/pods/log: get |

## Analyzers

Total: 5

| ID | Name | Description | Scope | Profiles | Dependencies |
|---|---|---|---|---|---|
| `ScyllaVersionSupportAnalyzer` | Scylla version support check | Checks each node's Scylla version against known-supported release ranges and flags end-of-life or unrecognized versions. | PerScyllaCluster | full, health | `ScyllaVersionCollector` |
| `SchemaAgreementAnalyzer` | Schema agreement check | Verifies all nodes in the cluster report the same schema version; flags schema disagreement that may indicate a stuck migration. | PerScyllaCluster | full, health | `SchemaVersionsCollector` |
| `OSSupportAnalyzer` | OS support check | Checks that the OS distribution running on each Scylla pod is on the list of supported platforms. | PerScyllaCluster | full, health | `OSInfoCollector` |
| `GossipHealthAnalyzer` | Gossip health check | Verifies all gossip endpoints report is_alive == true; flags any dead nodes detected by the gossip protocol. | PerScyllaCluster | full, health | `GossipInfoCollector` |
| `TopologyHealthAnalyzer` | Topology health check | Checks the system.topology table to verify all nodes are in 'normal' state with upgrade_state 'done'; flags nodes that are joining, leaving, or mid-upgrade. | PerScyllaCluster | full, health | `SystemTopologyCollector` |

