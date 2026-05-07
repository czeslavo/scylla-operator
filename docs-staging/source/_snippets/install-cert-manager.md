Install cert-manager:

:::{code-block} shell
:substitutions:
kubectl apply --server-side -f=https://raw.githubusercontent.com/{{repository}}/{{revision}}/examples/third-party/cert-manager.yaml
:::

Wait for cert-manager to become ready:

:::{code-block} shell
kubectl wait --for='condition=established' --timeout=60s crd/certificates.cert-manager.io crd/issuers.cert-manager.io
for deploy in cert-manager{,-cainjector,-webhook}; do
    kubectl -n=cert-manager rollout status --timeout=10m deployment.apps/"${deploy}"
done
:::
