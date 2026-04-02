#!/usr/bin/env bash
# Shared setup for VHS demo recordings.
# Sourced at the beginning of each tape (inside a Hide block).

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Alias 'soda' to the diagnose subcommand.
alias soda="${REPO_ROOT}/scylla-operator diagnose"
# Make alias expansion work in non-interactive bash.
shopt -s expand_aliases

# Clean PS1 for a tidy recording.
export PS1='\[\e[1;32m\]$\[\e[0m\] '

# Point kubectl at the test GKE cluster.
export KUBECONFIG=/tmp/test-cluster-kubeconfig.yaml

# Suppress klog output (it goes to stderr and clutters the recording).
export KLOG_FLAGS="--v=0"
