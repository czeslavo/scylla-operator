# Configure Nodes

ScyllaDB Operator uses the `NodeConfig` custom resource to prepare dedicated nodes for ScyllaDB.
NodeConfig handles local disk setup (RAID, filesystems, mounts) and kernel tuning (sysctls) automatically.

## What NodeConfig Does

When you create a `NodeConfig` resource, the operator deploys a DaemonSet on matching nodes that:

1. **Configures local disks** — creates a RAID0 array from NVMe devices, formats it with XFS, and mounts it to a well-known path.
2. **Tunes kernel parameters** — sets sysctls for high-throughput I/O (`fs.aio-max-nr`, `fs.file-max`, `vm.swappiness`, etc.).
3. **Runs performance tuning** — executes `ContainerPerftune` Jobs for IRQ balancing and other low-latency optimizations.

## Apply a NodeConfig

NodeConfig manifests are platform-specific because disk device paths and naming conventions differ.
Apply the manifest for your platform:

::::{tabs}

:::{tab} GKE

```{code-block} shell
:substitutions:
kubectl apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/examples/gke/nodeconfig.yaml
```

:::

:::{tab} EKS

```{code-block} shell
:substitutions:
kubectl apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/examples/eks/nodeconfig.yaml
```

:::

:::{tab} OKE

```{code-block} shell
:substitutions:
kubectl apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/examples/oke/nodeconfig.yaml
```

:::

::::

Wait for NodeConfig to finish reconciling:

:::{code-block} bash
kubectl wait --timeout=10m --for='condition=Progressing=False' nodeconfigs.scylla.scylladb.com/scylladb-nodepool-1
kubectl wait --timeout=10m --for='condition=Degraded=False' nodeconfigs.scylla.scylladb.com/scylladb-nodepool-1
kubectl wait --timeout=10m --for='condition=Available=True' nodeconfigs.scylla.scylladb.com/scylladb-nodepool-1
:::

## NodeConfig Anatomy

A typical NodeConfig includes:

:::{code-block} yaml
apiVersion: scylla.scylladb.com/v1alpha1
kind: NodeConfig
metadata:
  name: scylladb-nodepool-1
spec:
  localDiskSetup:
    raids:
    - name: nvmes
      type: RAID0
      RAID0:
        devices:
          nameRegex: ^/dev/nvme\d+n\d+$
    filesystems:
    - device: /dev/md/nvmes
      type: xfs
    mounts:
    - device: /dev/md/nvmes
      mountPoint: /var/lib/persistent-volumes
      unsupportedOptions:
      - prjquota
  sysctls:
  - name: fs.aio-max-nr
    value: "30000000"
  - name: fs.file-max
    value: "9223372036854775807"
  - name: vm.swappiness
    value: "1"
  placement:
    nodeSelector:
      scylla.scylladb.com/node-type: scylla
    tolerations:
    - effect: NoSchedule
      key: scylla-operator.scylladb.com/dedicated
      operator: Equal
      value: scyllaclusters
:::

Key fields:

- `localDiskSetup.raids` — RAID configuration for local NVMe devices. The `nameRegex` pattern must match the device paths on your platform.
- `localDiskSetup.filesystems` — filesystem type for the RAID device. XFS is required for the Local CSI Driver.
- `localDiskSetup.mounts` — mount point used by the `scylladb-local-xfs` StorageClass.
- `sysctls` — kernel parameters tuned for ScyllaDB's I/O patterns.
- `placement` — targets only the dedicated ScyllaDB nodes.

## Verify NodeConfig

Check that the NodeConfig reports healthy status:

:::{code-block} shell
kubectl get nodeconfigs.scylla.scylladb.com
:::

Expected output:

```
NAME                  AVAILABLE   PROGRESSING   DEGRADED   AGE
scylladb-nodepool-1   True        False         False      5m
```
