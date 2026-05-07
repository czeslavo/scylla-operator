# Before You Deploy

Before deploying a ScyllaDB cluster, prepare your Kubernetes nodes and operator configuration.
These steps ensure ScyllaDB runs with optimal performance and isolation.

**New to ScyllaDB on Kubernetes?** The [reference deployments](../reference-deployments/index.md) cover these steps as part of an end-to-end guide.

## ScyllaDB Node Requirements

ScyllaDB needs dedicated nodes with local NVMe storage, CPU pinning, and kernel tuning:

1. [Set up dedicated node pools](set-up-dedicated-node-pools.md) — provision and label nodes, apply taints.
2. [Configure CPU pinning](configure-cpu-pinning.md) — enable the static CPU manager policy.
3. [Configure nodes](configure-nodes.md) — apply NodeConfig for disk setup and kernel tuning.

## Operator Configuration

- [Configure the Operator](configure-operator.md) — tune operator-level settings.

:::{toctree}
:hidden:

set-up-dedicated-node-pools
configure-cpu-pinning
configure-nodes
configure-operator
:::
