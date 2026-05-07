# Configure CPU Pinning

CPU pinning ensures that ScyllaDB threads are bound to specific CPU cores, eliminating context-switch overhead and improving tail latency.

## How It Works

Kubernetes supports CPU pinning through the [static CPU manager policy](https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/#static-policy).
When a pod has `Guaranteed` QoS (requests equal limits for all containers), the kubelet assigns exclusive CPUs to that pod.

ScyllaDB Operator sets resource requests equal to limits in the ScyllaCluster spec, which places pods in Guaranteed QoS automatically.

## Enable the Static CPU Manager Policy

The kubelet on each dedicated ScyllaDB node must be started with:

```
--cpu-manager-policy=static
```

How you configure this depends on your platform:

::::{tabs}

:::{tab} GKE
Not needed — GKE automatically enables the static policy on nodes with local SSDs and appropriate machine types.
:::

:::{tab} EKS
Use a custom launch template with a bootstrap script that passes `--kubelet-extra-args '--cpu-manager-policy=static'`.
:::

:::{tab} OKE
Use a Cloud-Init script that writes a kubelet configuration override before the node joins the cluster.
See the [cloud-init.sh](https://github.com/scylladb/scylla-operator/blob/master/examples/oke/cloud-init.sh) example.
The [OKE cluster setup guide](../../install-operator/prerequisites/set-up-oke-cluster.md) applies this automatically.
:::

::::

:::{warning}
Changing the CPU manager policy on a running node requires draining the node, stopping the kubelet, removing the CPU manager state file (`/var/lib/kubelet/cpu_manager_state`), and restarting the kubelet.
It is much simpler to set the policy at node creation time.
:::

## Verify CPU Pinning

After deploying a ScyllaDB cluster, verify that pods are in Guaranteed QoS:

:::{code-block} shell
kubectl get pods -l 'app.kubernetes.io/name=scylla' \
  -o=jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.qosClass}{"\n"}{end}'
:::

All ScyllaDB pods should report `Guaranteed`.
