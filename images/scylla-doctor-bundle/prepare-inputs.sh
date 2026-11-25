#!/usr/bin/env bash
# Collects the Scylla Doctor build inputs into ./inputs for the bundle image (proof of concept).
#
# usage: prepare-inputs.sh BASIC_TARBALL FULL_TARBALL SCYLLA_DOCTOR_REPO
#   BASIC_TARBALL       scylla-doctor-<version>.tar.gz (public, collection only)
#   FULL_TARBALL        scylla-doctor-full-<version>.tar.gz (private, with analyzers)
#   SCYLLA_DOCTOR_REPO  checkout of the private scylladb/scylla-doctor repository at tag scylla-doctor-v<version>;
#                       Scylla Doctor Cluster is built from it with `make -C scylla-doctor-cluster portable`.
set -euEo pipefail

if [[ $# -ne 3 ]]; then
  sed -n '2,9p' "$0" >&2
  exit 2
fi
basic=$1 full=$2 repo=$3
here=$(cd "$(dirname "$0")" && pwd)

mkdir -p "${here}/inputs"
cp -- "${basic}" "${full}" "${here}/inputs/"
if [[ ! -f "${repo}/scylla-doctor-cluster/scylla_doctor_cluster.pyz" ]]; then
  make -C "${repo}/scylla-doctor-cluster" portable
fi
cp -- "${repo}/scylla-doctor-cluster/scylla_doctor_cluster.pyz" "${repo}/scylla-doctor-cluster/scylla_doctor_cluster.ini" \
  "${here}/inputs/"
ls -l "${here}/inputs"
