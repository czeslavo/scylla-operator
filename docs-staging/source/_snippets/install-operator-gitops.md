Install the ScyllaDB Operator:

:::{code-block} shell
:substitutions:
kubectl -n=scylla-operator apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/deploy/operator.yaml
:::

Wait for the operator to become ready:

:::{code-block} shell
kubectl wait --for='condition=established' --timeout=60s crd/scyllaclusters.scylla.scylladb.com crd/nodeconfigs.scylla.scylladb.com crd/scyllaoperatorconfigs.scylla.scylladb.com crd/scylladbmonitorings.scylla.scylladb.com
kubectl -n=scylla-operator rollout status --timeout=10m deployment.apps/{scylla-operator,webhook-server}
:::
