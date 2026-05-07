# Prerequisites

Before installing ScyllaDB Operator, ensure your environment meets the following requirements.

## Kubernetes Cluster

ScyllaDB Operator requires a [supported Kubernetes environment](../../reference/releases.md).
Issues on unsupported environments are unlikely to be addressed.

If you do not have a cluster yet, follow one of the platform-specific guides:

- [Set up a GKE cluster](set-up-gke-cluster.md) — Google Kubernetes Engine.
- [Set up an EKS cluster](set-up-eks-cluster.md) — Amazon Elastic Kubernetes Service.
- [Set up an OKE cluster](set-up-oke-cluster.md) — Oracle Container Engine for Kubernetes.
- [Set up an OpenShift cluster](set-up-openshift-cluster.md) — Red Hat OpenShift.

## cert-manager

ScyllaDB Operator uses [cert-manager](https://cert-manager.io/) to manage TLS certificates for webhook servers.
cert-manager must be installed before the operator.
See [Install with GitOps](../install-with-gitops.md) for installation steps.

## Prometheus Operator (Optional)

If you plan to use ScyllaDB Operator's [monitoring integration](../../deploy-scylladb/set-up-monitoring.md), the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) must be installed in the cluster.
This is not required for the operator itself to function.

:::{toctree}
:hidden:

set-up-gke-cluster
set-up-eks-cluster
set-up-oke-cluster
set-up-openshift-cluster
:::
