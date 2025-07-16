#!/bin/bash

export RUNNER_PREFIX="czeslavo-test"

#kubectl -n ci-clusters create -f - <<EOF
#apiVersion: ci.scylladb.com/v1alpha1
#kind: KubernetesClusterSet
#metadata:
#  name: "${RUNNER_PREFIX}"
#spec:
#  ttlSeconds: 86400 # 24h
#  networking:
#    type: Shared
#  clusterTemplates:
#    - metadata:
#        name: europe-west1
#      spec:
#        version: "1.31"
#        credentialsSecret:
#          name: "${RUNNER_PREFIX}-europe-west1"
#        type: GKE
#        gke:
#          location: europe-west1
#          nodePools:
#            - name: workers
#              locations:
#                - europe-west1-b
#              config:
#                machineType: e2-standard-8
#                diskType: pd-standard
#                diskSizeGB: 40
#                labels:
#                  pool: workers
#              initialNodeCount: 1
#            - name: scylladb
#              locations:
#                - europe-west1-b
#              config:
#                machineType: c2d-standard-8
#                diskType: pd-balanced
#                diskSizeGB: 40
#                localNVMeSSDBlockConfig:
#                  localSSDCount: 1
#                labels:
#                  pool: scylladb
#                  scylla.scylladb.com/node-type: scylla
#                taints:
#                  - key: scylla-operator.scylladb.com/dedicated
#                    value: scyllaclusters
#                    effect: NO_SCHEDULE
#                kubeletConfig:
#                  cpuManagerPolicy: static
#              initialNodeCount: 1
#    - metadata:
#        name: europe-west3
#      spec:
#        version: "1.31"
#        credentialsSecret:
#          name: "${RUNNER_PREFIX}-europe-west3"
#        type: GKE
#        gke:
#          location: europe-west3
#          nodePools:
#            - name: workers
#              locations:
#                - europe-west3-a
#              config:
#                machineType: e2-standard-8
#                diskType: pd-standard
#                diskSizeGB: 40
#                labels:
#                  pool: workers
#              initialNodeCount: 1
#            - name: scylladb
#              locations:
#                - europe-west3-a
#              config:
#                machineType: c2d-standard-8
#                diskType: pd-balanced
#                diskSizeGB: 40
#                localNVMeSSDBlockConfig:
#                  localSSDCount: 1
#                labels:
#                  pool: scylladb
#                  scylla.scylladb.com/node-type: scylla
#                taints:
#                  - key: scylla-operator.scylladb.com/dedicated
#                    value: scyllaclusters
#                    effect: NO_SCHEDULE
#                kubeletConfig:
#                  cpuManagerPolicy: static
#              initialNodeCount: 1
#    - metadata:
#        name: europe-west4
#      spec:
#        version: "1.31"
#        credentialsSecret:
#          name: "${RUNNER_PREFIX}-europe-west4"
#        type: GKE
#        gke:
#          location: europe-west4
#          nodePools:
#            - name: workers
#              locations:
#                - europe-west4-a
#              config:
#                machineType: e2-standard-8
#                diskType: pd-standard
#                diskSizeGB: 40
#                labels:
#                  pool: workers
#              initialNodeCount: 1
#            - name: scylladb
#              locations:
#                - europe-west4-a
#              config:
#                machineType: c2d-standard-8
#                diskType: pd-balanced
#                diskSizeGB: 40
#                localNVMeSSDBlockConfig:
#                  localSSDCount: 1
#                labels:
#                  pool: scylladb
#                  scylla.scylladb.com/node-type: scylla
#                taints:
#                  - key: scylla-operator.scylladb.com/dedicated
#                    value: scyllaclusters
#                    effect: NO_SCHEDULE
#                kubeletConfig:
#                  cpuManagerPolicy: static
#              initialNodeCount: 1
#EOF
#

timeout -v 15m bash -c 'until kubectl -n ci-clusters wait --for=condition=Degraded=False kubernetesclusterset/"${RUNNER_PREFIX}" --timeout=15m && kubectl -n ci-clusters wait --for=condition=Progressing=False kubernetesclusterset/"${RUNNER_PREFIX}" --timeout=15m; do sleep 1; done'


# $1 - name of the worker cluster
function get_worker_kubeconfig_path {
  echo "/tmp/kubeconfigs/${RUNNER_PREFIX}-${1}.kubeconfig"
}

# $1 - name of the worker cluster
# $2 - path where the kubeconfig should be created
function prepare_worker_kubeconfig {
  kubectl -n ci-clusters get secret/"${RUNNER_PREFIX}-${1}" --template='{{ .data.kubeconfig }}' | base64 -d > "${2}"
  kubectl --kubeconfig="${2}" config set-context --current --namespace 'default-unexisting-namespace'
}

worker_clusters_names=(
  "europe-west1"
  "europe-west3"
  "europe-west4"
)

declare -A WORKER_KUBECONFIGS

mkdir /tmp/kubeconfigs
declare -A WORKER_KUBECONFIGS
for name in "${worker_clusters_names[@]}"; do
  prepare_worker_kubeconfig "${name}" "$(get_worker_kubeconfig_path "${name}")"
  WORKER_KUBECONFIGS["${name}"]="$(get_worker_kubeconfig_path "${name}")"
done

# Prepare a regional GCS bucket for each worker cluster.

# $1 - name of the worker cluster
function storage_bucket_object_name {
  echo "${RUNNER_PREFIX}-${1}-gcs-bucket"
}

gcs_sa_credentials_shared_dir="/tmp/gcs-service-account-credentials"
mkdir -p "${gcs_sa_credentials_shared_dir}"
declare -A WORKER_OBJECT_STORAGE_BUCKETS WORKER_GCS_SERVICE_ACCOUNT_CREDENTIALS_PATHS
for name in "${worker_clusters_names[@]}"; do
  secret_name="${RUNNER_PREFIX}-${name}-gcs-service-account"
  object_name="$( storage_bucket_object_name "${name}" )"
  path="${gcs_sa_credentials_shared_dir}/${name}.json"

  kubectl -n ci-clusters create -f - <<EOF
apiVersion: ci.scylladb.com/v1alpha1
kind: StorageBucket
metadata:
  name: "${object_name}"
spec:
  ttlSeconds: 86400 # 24h
  type: GCS
  gcs:
    location: "${name}" #  We rely on the fact that the names match GCS locations.
  credentialsSecret:
    name: "${secret_name}"
EOF

  # Wait for the bucket to be created and ready.
  timeout -v 5m bash -c "until kubectl -n ci-clusters wait --for=condition=Degraded=False storagebucket/"${object_name}" --timeout=5m && kubectl -n ci-clusters wait --for=condition=Progressing=False storagebucket/"${object_name}" --timeout=5m; do sleep 1; done"

  # Get the bucket name and store it in the associative array.
  bucket_name=$(kubectl -n ci-clusters get storagebuckets/"${object_name}" --template='{{ .status.bucketName }}')
  WORKER_OBJECT_STORAGE_BUCKETS["${name}"]="${bucket_name}"

  # Store the GCS service account credentials in a file and keep the path in the associative array.
  kubectl -n ci-clusters get secret/"${secret_name}" --template='{{ index .data "gcs-service-account.json" }}' | base64 -d > "${path}"
  WORKER_GCS_SERVICE_ACCOUNT_CREDENTIALS_PATHS["${name}"]="${path}"

  echo "Created GCS bucket for worker '${name}'. Bucket '${bucket_name}', credentials stored under '${path}'."
done
