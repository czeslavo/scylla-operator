#!/usr/bin/env bash
# Writes a kubeconfig for the soda ServiceAccount, using the current context's server and CA (proof of concept).
#
# usage: kubeconfig.sh NAMESPACE OUTPUT [DURATION]
#   The token is short-lived (default 2h). Run it as a cluster admin; hand OUTPUT to whoever runs SODA.
set -euEo pipefail

if [[ $# -lt 2 ]]; then
  sed -n '2,5p' "$0" >&2
  exit 2
fi
namespace=$1 output=$2 duration=${3:-2h}

server=$(kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.server}')
ca=$(kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
token=$(kubectl -n "${namespace}" create token soda --duration "${duration}")

umask 077
cat > "${output}" <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- name: cluster
  cluster:
    server: ${server}
    certificate-authority-data: ${ca}
users:
- name: soda
  user:
    token: ${token}
contexts:
- name: soda
  context:
    cluster: cluster
    user: soda
    namespace: ${namespace}
current-context: soda
KUBECONFIG
echo "Wrote ${output} (token valid for ${duration})"
