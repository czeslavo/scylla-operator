# Reference Deployment: OKE

This guide deploys a production-ready ScyllaDB cluster on Oracle Container Engine for Kubernetes (OKE).
By the end, you will have a 3-node ScyllaDB cluster spread across 3 fault domains, with performance tuning and local NVMe storage configured.

## Prerequisites

- An OKE cluster provisioned with a dedicated Dense I/O node pool for ScyllaDB.
  If you do not have one yet, follow [Set up an OKE cluster for ScyllaDB](../../install-operator/prerequisites/set-up-oke-cluster.md).
- Dedicated nodes labeled and tainted per the [ScyllaDB node requirements](../before-you-deploy/index.md).
  The OKE cluster setup guide handles this automatically.
- [`kubectl`](https://kubernetes.io/docs/tasks/tools/) configured and pointed at the cluster.
- The environment variables from the [OKE cluster setup guide](../../install-operator/prerequisites/set-up-oke-cluster.md#set-environment-variables) exported in your shell (at minimum `OCI_REGION`).

## Install cert-manager

:::{include} /_snippets/install-cert-manager.md
:::

## Install ScyllaDB Operator

:::{include} /_snippets/install-operator-gitops.md
:::

## Set Up NodeConfig

NodeConfig prepares the dedicated nodes for ScyllaDB by configuring local disks and kernel parameters.
For background on what NodeConfig does, see [Configure nodes](../before-you-deploy/configure-nodes.md).

Apply the OKE-specific NodeConfig:

:::{code-block} shell
:substitutions:
kubectl apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/examples/oke/nodeconfig.yaml
:::

Wait for the NodeConfig to finish reconciling on all nodes:

:::{code-block} bash
kubectl wait --timeout=10m --for='condition=Progressing=False' nodeconfigs.scylla.scylladb.com/scylladb-nodepool-1
kubectl wait --timeout=10m --for='condition=Degraded=False' nodeconfigs.scylla.scylladb.com/scylladb-nodepool-1
kubectl wait --timeout=10m --for='condition=Available=True' nodeconfigs.scylla.scylladb.com/scylladb-nodepool-1
:::

## Install Local CSI Driver

:::{include} /_snippets/install-local-csi-driver.md
:::

## Deploy a ScyllaDB Cluster

Create a ConfigMap with ScyllaDB configuration (here, enabling authentication and authorization):

:::{code-block} bash
kubectl apply --server-side -f=- <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: scylladb-config
data:
  scylla.yaml: |
    authenticator: PasswordAuthenticator
    authorizer: CassandraAuthorizer
EOF
:::

Create a ScyllaDB cluster with one rack per fault domain.
The resource requests are sized for `VM.DenseIO2.8` (8 OCPUs, 120 GB RAM), leaving headroom for the OS, kubelet, and DaemonSets.
Adjust if you use a different shape.

:::{code-block} bash
:substitutions:
kubectl apply --server-side -f=- <<EOF
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: scylladb
spec:
  repository: {{imageRepository}}
  version: {{scyllaDBImageTag}}
  agentVersion: {{agentVersion}}
  automaticOrphanedNodeCleanup: true
  datacenter:
    name: ${OCI_REGION}
    racks:
    - name: fault-domain-1
      members: 1
      scyllaConfig: scylladb-config
      storage:
        capacity: 100Gi
        storageClassName: scylladb-local-xfs
      resources:
        requests:
          cpu: 6
          memory: 90Gi
        limits:
          cpu: 6
          memory: 90Gi
      agentResources:
        requests:
          cpu: 100m
          memory: 20Mi
        limits:
          cpu: 100m
          memory: 20Mi
      placement:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: scylla.scylladb.com/node-type
                operator: In
                values:
                - scylla
              - key: oci.oraclecloud.com/fault-domain
                operator: In
                values:
                - FAULT-DOMAIN-1
        tolerations:
        - key: scylla-operator.scylladb.com/dedicated
          operator: Equal
          value: scyllaclusters
          effect: NoSchedule
    - name: fault-domain-2
      members: 1
      scyllaConfig: scylladb-config
      storage:
        capacity: 100Gi
        storageClassName: scylladb-local-xfs
      resources:
        requests:
          cpu: 6
          memory: 90Gi
        limits:
          cpu: 6
          memory: 90Gi
      agentResources:
        requests:
          cpu: 100m
          memory: 20Mi
        limits:
          cpu: 100m
          memory: 20Mi
      placement:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: scylla.scylladb.com/node-type
                operator: In
                values:
                - scylla
              - key: oci.oraclecloud.com/fault-domain
                operator: In
                values:
                - FAULT-DOMAIN-2
        tolerations:
        - key: scylla-operator.scylladb.com/dedicated
          operator: Equal
          value: scyllaclusters
          effect: NoSchedule
    - name: fault-domain-3
      members: 1
      scyllaConfig: scylladb-config
      storage:
        capacity: 100Gi
        storageClassName: scylladb-local-xfs
      resources:
        requests:
          cpu: 6
          memory: 90Gi
        limits:
          cpu: 6
          memory: 90Gi
      agentResources:
        requests:
          cpu: 100m
          memory: 20Mi
        limits:
          cpu: 100m
          memory: 20Mi
      placement:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: scylla.scylladb.com/node-type
                operator: In
                values:
                - scylla
              - key: oci.oraclecloud.com/fault-domain
                operator: In
                values:
                - FAULT-DOMAIN-3
        tolerations:
        - key: scylla-operator.scylladb.com/dedicated
          operator: Equal
          value: scyllaclusters
          effect: NoSchedule
EOF
:::

Wait for the cluster to become ready:

:::{code-block} bash
kubectl wait --for='condition=Progressing=False' scyllacluster.scylla.scylladb.com/scylladb
kubectl wait --for='condition=Degraded=False' scyllacluster.scylla.scylladb.com/scylladb
kubectl wait --for='condition=Available=True' scyllacluster.scylla.scylladb.com/scylladb
:::

Verify the cluster status:

:::{code-block} shell
kubectl get scyllaclusters.scylla.scylladb.com/scylladb
:::

Expected output:

```
NAME       READY   MEMBERS   RACKS   AVAILABLE   PROGRESSING   DEGRADED   AGE
scylladb   3       3         3       True        False         False      5m
```

## Verify Performance Tuning

The operator places ScyllaDB pods in `Guaranteed` QoS and runs a per-node `ContainerPerftune` Job for low-latency tuning.
Verify both:

:::{code-block} shell
kubectl get pods -l 'app.kubernetes.io/name=scylla' \
  -o=jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.qosClass}{"\n"}{end}'
:::

Expected output:

```
scylladb-us-sanjose-1-fault-domain-1-0	Guaranteed
scylladb-us-sanjose-1-fault-domain-2-0	Guaranteed
scylladb-us-sanjose-1-fault-domain-3-0	Guaranteed
```

Confirm that the per-node `ContainerPerftune` Jobs completed successfully:

:::{code-block} shell
kubectl get jobs -n scylla-operator-node-tuning \
  -l 'scylla-operator.scylladb.com/node-config-job-type=ContainerPerftune'
:::

Expected output (one completed job per ScyllaDB node):

```
NAME                                          COMPLETIONS   DURATION   AGE
scylladb-nodepool-1-perftune-10.0.1.20       1/1           15s        3m
scylladb-nodepool-1-perftune-10.0.1.21       1/1           14s        3m
scylladb-nodepool-1-perftune-10.0.1.22       1/1           16s        3m
```

## Connect with cqlsh

Run a few CQL commands against the cluster:

:::{code-block} shell
kubectl exec -i pod/scylladb-${OCI_REGION}-fault-domain-1-0 -c scylla -- \
  cqlsh localhost -u cassandra -p cassandra <<EOF
CREATE KEYSPACE IF NOT EXISTS example WITH replication = {'class': 'NetworkTopologyStrategy', '${OCI_REGION}': 3};
USE example;
CREATE TABLE users (id UUID PRIMARY KEY, name TEXT, email TEXT);
INSERT INTO users (id, name, email) VALUES (uuid(), 'Alice', 'alice@example.com');
SELECT * FROM users;
EOF
:::

Expected output:

```
 id                                   | email             | name
--------------------------------------+-------------------+-------
 a1b2c3d4-e5f6-7890-abcd-ef1234567890 | alice@example.com | Alice

(1 rows)
```

## Clean Up

Delete the ScyllaDB cluster first so the underlying PersistentVolumes are released cleanly:

:::{code-block} shell
kubectl delete scyllaclusters.scylla.scylladb.com/scylladb
:::

To tear down the OKE infrastructure, follow the [Clean up](../../install-operator/prerequisites/set-up-oke-cluster.md#clean-up) section of the OKE cluster setup guide.

## Next Steps

- [Set up monitoring](../set-up-monitoring.md) with Prometheus and Grafana dashboards.
- Review the [production checklist](../production-checklist.md) before going live.
- Learn how to [connect your application](../../connect-your-app/index.md) to ScyllaDB.
- Read the [Tuning](../../understand/tuning.md) and [Configure nodes](../before-you-deploy/configure-nodes.md) references to adjust the deployment for your workload.
