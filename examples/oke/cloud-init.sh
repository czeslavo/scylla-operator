#!/bin/bash
# Cloud-Init script for OKE managed worker nodes that runs the OKE bootstrap
# script with extra kubelet arguments enabling the static CPU manager policy,
# which is required for CPU pinning of ScyllaDB containers (Guaranteed QoS).
#
# Pass this file to `oci ce node-pool create` via the `user_data` key of the
# `--node-metadata` parameter, base64-encoded.
set -euo pipefail

curl --fail -H "Authorization: Bearer Oracle" -L0 \
  http://169.254.169.254/opc/v2/instance/metadata/oke_init_script \
  | base64 --decode > /var/run/oke-init.sh
bash /var/run/oke-init.sh --kubelet-extra-args "--cpu-manager-policy=static"
