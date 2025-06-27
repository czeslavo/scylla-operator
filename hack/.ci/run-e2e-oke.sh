#!/usr/bin/env bash
#
# Copyright (C) 2023 ScyllaDB
#

set -euExo pipefail
shopt -s inherit_errexit

if [ -z "${ARTIFACTS+x}" ]; then
  echo "ARTIFACTS can't be empty" > /dev/stderr
  exit 2
fi

source "$( dirname "${BASH_SOURCE[0]}" )/../lib/kube.sh"
source "$( dirname "${BASH_SOURCE[0]}" )/lib/e2e.sh"
source "$( dirname "${BASH_SOURCE[0]}" )/run-e2e-shared.env.sh"
parent_dir="$( dirname "${BASH_SOURCE[0]}" )"

trap gather-artifacts-on-exit EXIT
trap gracefully-shutdown-e2es INT

SO_NODECONFIG_PATH="${SO_NODECONFIG_PATH=./hack/.ci/manifests/cluster/nodeconfig-oke.yaml}"
export SO_NODECONFIG_PATH
SO_CSI_DRIVER_PATH="${parent_dir}/manifests/namespaces/local-csi-driver/"
export SO_CSI_DRIVER_PATH
SO_SCYLLACLUSTER_STORAGECLASS_NAME="${SO_SCYLLACLUSTER_STORAGECLASS_NAME=scylladb-local-xfs}"
export SO_SCYLLACLUSTER_STORAGECLASS_NAME

# Backwards compatibility. Remove when release repo stops using SO_DISABLE_NODECONFIG.
if [[ "${SO_DISABLE_NODECONFIG:-false}" == "true" ]]; then
  SO_NODECONFIG_PATH=""
  SO_CSI_DRIVER_PATH=""
fi

for i in "${!KUBECONFIGS[@]}"; do
  KUBECONFIG="${KUBECONFIGS[$i]}" DEPLOY_DIR="${ARTIFACTS}/deploy/${i}" timeout --foreground -v 10m "${parent_dir}/../ci-deploy.sh" "${SO_IMAGE}" &
  ci_deploy_bg_pids["${i}"]=$!
done

for pid in "${ci_deploy_bg_pids[@]}"; do
  wait "${pid}"
done

KUBECONFIG="${KUBECONFIGS[0]}" apply-e2e-workarounds
KUBECONFIG="${KUBECONFIGS[0]}" run-e2e

#./hack/.ci/generate-static-oke-kubeconfig.sh \
#  --cluster-id ocid1.cluster.oc1.us-sanjose-1.aaaaaaaaayfitdp6frhii3w4bhxp67b7yzro5vv7updemeediczubxfcb6hq \
#  --output-file ${OKE_PRIVATE_KUBECONFIG} \
#  --kube-endpoint PRIVATE_ENDPOINT
#
#./hack/.ci/generate-static-oke-kubeconfig.sh \
#  --cluster-id ocid1.cluster.oc1.us-sanjose-1.aaaaaaaaayfitdp6frhii3w4bhxp67b7yzro5vv7updemeediczubxfcb6hq \
#  --output-file ${OKE_KUBECONFIG} \
#  --kube-endpoint PUBLIC_ENDPOINT
###
#OKE_KUBECONFIG=~/.kube/oke-kubeconfig.yaml \
#OKE_PRIVATE_KUBECONFIG=~/.kube/private-oke-kubeconfig.yaml \
#SO_SCYLLACLUSTER_CLIENTS_BROADCAST_ADDRESS_TYPE=PodIP \
#SO_SCYLLACLUSTER_NODES_BROADCAST_ADDRESS_TYPE=PodIP \
#SO_SCYLLACLUSTER_NODE_SERVICE_TYPE=Headless \
#SO_SUITE=scylla-operator/conformance/parallel \
#REENTRANT=1 \
#SO_IMAGE=scylladb/scylla-operator:1.17 \
#KUBECONFIG=${OKE_KUBECONFIG} \
#PRIVATE_KUBECONFIGS=${OKE_PRIVATE_KUBECONFIG} \
#ARTIFACTS=./artifacts \
#  ./hack/.ci/run-e2e-oke.sh
