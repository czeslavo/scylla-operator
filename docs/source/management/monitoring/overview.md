# ScyllaDB Monitoring overview

## Architecture

ScyllaDB [exposes](https://monitoring.docs.scylladb.com/stable/reference/monitoring-apis.html) its metrics in Prometheus format. 
{{productName}} leverages this capability in Kubernetes environments to provide a comprehensive monitoring solution for your ScyllaDB clusters.
{{productName}} supports deploying and managing a complete monitoring stack for your ScyllaDB clusters using the
[ScyllaDBMonitoring](../../api-reference/groups/scylla.scylladb.com/scylladbmonitorings.rst) custom resource.

:::{note}
ScyllaDBMonitoring CRD is still experimental. The API is currently in version `v1alpha1` and may change in future versions.
:::

The monitoring stack that can be deployed with ScyllaDBMonitoring includes the following components:
- Prometheus for metrics collection and alerting (along with scraping and alerting rules targeting ScyllaDB instances).
- Grafana for metrics visualization (with pre-configured dashboards for ScyllaDB).

:::{include} diagrams/monitoring-overview.mmd
:::

The monitoring stack is expected to be created separately for each ScyllaDB cluster, allowing you to have isolated monitoring environments.

## Prometheus

For deploying and/or configuring Prometheus, {{productName}} relies on the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator).
Depending on configuration, {{productName}} may deploy and manage a Prometheus instance for you, or it can be configured to use
an existing Prometheus instance (managed by the Prometheus Operator) in your cluster.

The following Prometheus Operator resources are created by {{productName}} when you deploy a ScyllaDBMonitoring resource:

- [Prometheus](https://github.com/prometheus-operator/prometheus-operator/blob/e4c727291acc543dab531bc4aaf16637067c1b86/pkg/apis/monitoring/v1/prometheus_types.go#L1085) - the Prometheus instance itself (it may be omitted in External mode).
- [ServiceMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/e4c727291acc543dab531bc4aaf16637067c1b86/pkg/apis/monitoring/v1/servicemonitor_types.go#L41) - the resource that defines how to scrape metrics from ScyllaDB nodes.
- [PrometheusRule](https://github.com/prometheus-operator/prometheus-operator/blob/e4c727291acc543dab531bc4aaf16637067c1b86/pkg/apis/monitoring/v1/prometheusrule_types.go#L37) - the resource that defines alerting rules for Prometheus.

Prometheus version used in the deployment is tied to the version of {{productName}}. You can find the exact version used in the
[config.yaml](https://github.com/scylladb/scylla-operator/blob/master/assets/config/config.yaml) file under `operator.prometheusVersion` key.

As of now, {{productName}} supports two modes of operation for ScyllaDBMonitoring regarding Prometheus deployment:
**Managed** and **External**. You can choose the mode that best fits your needs by setting the `spec.components.prometheus.mode` field in the ScyllaDBMonitoring resource.

### Managed

In the **Managed** mode, {{productName}} will deploy a Prometheus instance for you. This is the default mode.
What this means is that when you create a ScyllaDBMonitoring resource, {{productName}} will create a Prometheus custom 
resource (from the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator)) in the same namespace as the ScyllaDBMonitoring resource. 
This Prometheus instance will be configured to scrape metrics from the ScyllaDB nodes in the cluster that ScyllaDBMonitoring is monitoring and
will also have alerting rules configured for ScyllaDB (using ServiceMonitor and PrometheusRule CRs).
This mode is suitable for most use cases, especially if you don't have an existing Prometheus instance in your cluster.

### External

In the **External** mode, {{productName}} will not deploy a Prometheus CR instance, but it will still create the ServiceMonitor and PrometheusRule resources
that are to be reconciled by an existing Prometheus Operator instance in your cluster. This mode is useful if you already have a Prometheus instance
deployed in your cluster, and you want to use it for monitoring your ScyllaDB clusters.

When using this mode, you need to ensure that the existing Prometheus instance is configured to discover and scrape the 
ServiceMonitor and PrometheusRule resources created by {{productName}}. This requires setting the appropriate
`serviceMonitorSelector` and `ruleSelector` in the Prometheus resource to match the labels used by {{productName}} (or
leaving them empty to select all resources).

Please note that in this mode, ScyllaDBMonitoring has to be configured so that Grafana can access the Prometheus instance.
You can configure Grafana datasources in the `spec.components.grafana.datasources` field of the ScyllaDBMonitoring resource.
Please refer to the [ScyllaDBMonitoring API reference](../../api-reference/groups/scylla.scylladb.com/scylladbmonitorings.rst) for details.

## Grafana

For deploying Grafana, {{productName}} doesn't use any third-party operator. Instead, it manages the Grafana deployment
directly. It makes sure that Grafana is pre-configured with dashboards from [scylla-monitoring](https://github.com/scylladb/scylla-monitoring/).

Grafana image used in the deployment is tied to the version of {{productName}}. You can find the exact image used in the
[config.yaml](https://github.com/scylladb/scylla-operator/blob/master/assets/config/config.yaml) file under `operator.grafanaImage` key.

## Ingress Controller

An Ingress Controller can be used to expose the Grafana outside the Kubernetes cluster.
{{productName}} does not manage the Ingress Controller itself, but it creates an [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) resource to expose Grafana
according to the configuration in the ScyllaDBMonitoring resource. You can use any Ingress Controller of your choice.

You can learn more about exposing Grafana in the [ScyllaDB Monitoring setup](exposing-grafana.md) guide.
