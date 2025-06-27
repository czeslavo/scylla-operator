#!/usr/bin/env bash
set -euo pipefail

# Usage:
# ./generate_static_kubeconfig.sh \
#     --cluster-id ocid1.cluster.oc1... \
#     --output-file /path/to/kubeconfig \
#     [--kube-endpoint <ENDPOINT>]

print_usage() {
  echo "Usage: $0 --cluster-id <OCID> --output-file <FILE> [--kube-endpoint <ENDPOINT>]"
  exit 1
}

# Parse arguments
CLUSTER_ID=""
OUTPUT_FILE=""
KUBE_ENDPOINT="PUBLIC_ENDPOINT"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster-id)
      CLUSTER_ID="$2"
      shift 2
      ;;
    --output-file)
      OUTPUT_FILE="$2"
      shift 2
      ;;
    --kube-endpoint)
      KUBE_ENDPOINT="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1"
      print_usage
      ;;
  esac
done

if [[ -z "$CLUSTER_ID" || -z "$OUTPUT_FILE" ]]; then
  echo "Missing required arguments."
  print_usage
fi

echo "[INFO] Generating dynamic kubeconfig to extract cluster details..."

# Generate a temporary dynamic kubeconfig
TEMP_KUBECONFIG=$(mktemp)

oci ce cluster create-kubeconfig \
  --cluster-id "$CLUSTER_ID" \
  --file "$TEMP_KUBECONFIG" \
  --token-version 2.0.0 \
  --kube-endpoint "$KUBE_ENDPOINT"

echo "[INFO] Parsing cluster server URL and CA data..."

SERVER=$(yq '.clusters[0].cluster.server' "$TEMP_KUBECONFIG")
CA=$(yq '.clusters[0].cluster["certificate-authority-data"]' "$TEMP_KUBECONFIG")

# Alternatively use jq if you have yq v4+ that supports JSON conversion
# SERVER=$(yq e '.clusters[0].cluster.server' "$TEMP_KUBECONFIG")
# CA=$(yq e '.clusters[0].cluster.certificate-authority-data' "$TEMP_KUBECONFIG")

echo "[INFO] Generating static token..."

TOKEN=$(oci ce cluster generate-token \
    --cluster-id "$CLUSTER_ID" \
    | jq '.status.token')

echo "[INFO] Writing static kubeconfig to $OUTPUT_FILE"

cat > "$OUTPUT_FILE" <<EOF
apiVersion: v1
kind: Config
clusters:
- name: cluster-oke
  cluster:
    certificate-authority-data: $CA
    server: $SERVER
contexts:
- name: oke-context
  context:
    cluster: cluster-oke
    user: oke-user
current-context: oke-context
users:
- name: oke-user
  user:
    token: $TOKEN
EOF

rm -f "$TEMP_KUBECONFIG"

echo "[INFO] Static kubeconfig created successfully: $OUTPUT_FILE"
