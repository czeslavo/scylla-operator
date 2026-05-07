# Set Up Dedicated Node Pools

ScyllaDB requires dedicated Kubernetes nodes to guarantee predictable performance.
These nodes should not run any other workloads.

## Requirements

Dedicated ScyllaDB nodes must have:

- **Local NVMe storage** — ScyllaDB stores data on local SSDs for maximum I/O performance.
  Use instance types with directly attached NVMe drives.
- **Sufficient CPU and memory** — ScyllaDB benefits from many cores.
  Plan for at least 2 CPUs reserved for the OS, kubelet, and DaemonSets.

### Recommended Instance Types

::::{tabs}

:::{tab} GKE
Use `n2-highmem` or `z3-highmem` families with local SSDs.
See [GCE recommendations](https://docs.scylladb.com/manual/stable/getting-started/cloud-instance-recommendations.html#google-compute-engine-gce).
:::

:::{tab} EKS
Use storage-optimized instances from the `i` family (e.g., `i3en`, `i4i`, `i8g`).
See [AWS recommendations](https://docs.scylladb.com/manual/stable/getting-started/cloud-instance-recommendations.html#amazon-web-services-aws).
:::

:::{tab} OKE
Use `DenseIO` shapes.
See [OCI recommendations](https://docs.scylladb.com/manual/stable/getting-started/cloud-instance-recommendations.html#oracle-cloud-infrastructure-oci).
:::

::::

## Label the Nodes

Apply the ScyllaDB node label so that pod affinity rules can target these nodes:

:::{code-block} bash
kubectl label nodes <node-name> scylla.scylladb.com/node-type=scylla
:::

When using managed Kubernetes services, apply the label at node pool creation time to avoid manual labeling:

::::{tabs}

:::{tab} GKE
Use `--node-labels 'scylla.scylladb.com/node-type=scylla'`
:::

:::{tab} EKS
Use `--kubernetes-labels 'scylla.scylladb.com/node-type=scylla'`
:::

:::{tab} OKE
Use `--initial-node-labels '[{"key": "scylla.scylladb.com/node-type", "value": "scylla"}]'`
:::

::::

## Taint the Nodes

Apply a taint to prevent non-ScyllaDB workloads from being scheduled on the dedicated nodes:

:::{code-block} bash
kubectl taint nodes -l 'scylla.scylladb.com/node-type=scylla' \
  scylla-operator.scylladb.com/dedicated=scyllaclusters:NoSchedule --overwrite
:::

The ScyllaDB cluster manifest must include a matching toleration for pods to be scheduled on these nodes.
The [reference deployments](../reference-deployments/reference-deployment-oke.md) include this toleration.

## Platform-Specific Guides

For end-to-end node pool creation on a specific platform:

- [Set up an OKE cluster](../../install-operator/prerequisites/set-up-oke-cluster.md) — includes dedicated Dense I/O node pool creation with labels.
