#!/bin/bash
set -euExo pipefail

declare -A WORKER_KUBECONFIGS=(
  ["europe-west1"]="/tmp/kubeconfigs/czeslavo-test-europe-west1.kubeconfig"
  ["europe-west3"]="/tmp/kubeconfigs/czeslavo-test-europe-west3.kubeconfig"
  ["europe-west4"]="/tmp/kubeconfigs/czeslavo-test-europe-west4.kubeconfig"
)

declare -A WORKER_OBJECT_STORAGE_BUCKETS=(
  ["europe-west1"]="so-c883b704-c3e5-49e9-b344-f19db05a91c0"
  ["europe-west3"]="so-0db7aeea-248f-49b6-bebf-4d1201906a4f"
  ["europe-west4"]="so-22b8371c-303e-4e38-b9b1-41942390b765"
)

declare -A WORKER_GCS_SERVICE_ACCOUNT_CREDENTIALS_PATHS=(
  ["europe-west1"]="/tmp/gcs-service-account-credentials/europe-west1.json"
  ["europe-west3"]="/tmp/gcs-service-account-credentials/europe-west3.json"
  ["europe-west4"]="/tmp/gcs-service-account-credentials/europe-west4.json"
)


# Rebuild tests image
git_sha=$(git rev-parse HEAD)
podman build -t "docker.io/czeslavo/scylla-operator:${git_sha}" .
podman push "docker.io/czeslavo/scylla-operator:${git_sha}"

mkdir -p /tmp/artifacts/

export KUBECONFIG="${WORKER_KUBECONFIGS["europe-west1"]}"
export SO_FOCUS_TESTS="ScyllaDBManagerTask and ScyllaDBCluster integration"
export SO_IMAGE="docker.io/czeslavo/scylla-operator:${git_sha}"
export SO_E2E_PARALLELISM=10
export SO_SUITE=scylla-operator/conformance/multi-datacenter-parallel
export REENTRANT=1
export ARTIFACTS=/tmp/artifacts
export SO_SCYLLACLUSTER_NODE_SERVICE_TYPE=ClusterIP
export SO_SCYLLACLUSTER_NODES_BROADCAST_ADDRESS_TYPE=ServiceClusterIP
export SO_SCYLLACLUSTER_CLIENTS_BROADCAST_ADDRESS_TYPE=ServiceClusterIP
source ./hack/.ci/run-e2e-gke.sh
