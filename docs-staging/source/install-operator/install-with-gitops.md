# Install with GitOps

Install ScyllaDB Operator and its dependencies by applying raw manifests from the project repository.
This method works with any GitOps tool (Argo CD, Flux, etc.) or plain `kubectl apply`.

## Prerequisites

- A Kubernetes cluster meeting the [prerequisites](prerequisites/index.md).
- [`kubectl`](https://kubernetes.io/docs/tasks/tools/) configured to communicate with the cluster.

## Install cert-manager

ScyllaDB Operator requires [cert-manager](https://cert-manager.io/) for TLS certificate management.
If you already have cert-manager running in your cluster, skip this step.

:::{include} /_snippets/install-cert-manager.md
:::

## Install ScyllaDB Operator

:::{include} /_snippets/install-operator-gitops.md
:::

## Install Local CSI Driver

If your ScyllaDB nodes use local NVMe disks (recommended for production), install the Local CSI Driver to automatically provision PersistentVolumes:

:::{include} /_snippets/install-local-csi-driver.md
:::

:::{note}
The Local CSI Driver requires that a [NodeConfig](../deploy-scylladb/before-you-deploy/configure-nodes.md) resource has already prepared the local disks on the target nodes.
When following a platform-specific reference deployment, NodeConfig setup is covered there.
:::

## Next Steps

- [Deploy your first ScyllaDB cluster](../deploy-scylladb/deploy-your-first-cluster.md).
- Follow a platform-specific reference deployment: [OKE](../deploy-scylladb/reference-deployments/reference-deployment-oke.md).
