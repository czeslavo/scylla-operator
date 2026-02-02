# scylladbctl

A kubectl-like CLI tool for managing ScyllaDB clusters on Kubernetes.

## Overview

`scylladbctl` provides high-level operations for ScyllaDB clusters running on Kubernetes. It exposes convenient commands for common operations like checking cluster status, replacing nodes, and managing cluster lifecycle without dealing with low-level Kubernetes resources directly.

## Architecture

The tool consists of three layers:

1. **Dispatcher Layer** (`pkg/scylladbctl/`) - Core business logic for cluster operations
   - Event-driven architecture with real-time progress streaming
   - Uses Kubernetes API and kubectl exec for reliable operations
   - Supports ScyllaCluster v1 API

2. **CLI Layer** (`pkg/cmd/scylladbctl/`) - User-facing commands
   - Built with Cobra framework
   - Consistent with existing operator CLI patterns
   - Supports environment variables for configuration

3. **Main Entry Point** (`cmd/scylladbctl/`) - Binary entry point

## Installation

Build from source:

```bash
go build -o bin/scylladbctl ./cmd/scylladbctl/
```

## Usage

### Configuration

Set environment variables for convenience:

```bash
export SCYLLA_CLUSTER_NAME=my-cluster
export SCYLLA_CLUSTER_NAMESPACE=scylla
```

Or use flags:

```bash
scylladbctl --cluster my-cluster --namespace scylla [command]
```

### Commands

#### Cluster Status

Display the status of all nodes in the cluster:

```bash
scylladbctl cluster status
```

This command:
- Queries all nodes using `nodetool status`
- Aggregates results from all pods
- Displays status (Up/Down) and state (Normal/Joining/Leaving/Moving) for each node

Example output:

```
⏳ Retrieving ScyllaCluster resource
⏳ Found cluster with datacenter: us-east-1
⏳ Discovering cluster pods
⏳ Found 3 pods in cluster
✅ Retrieved status for 3 nodes

Cluster: my-cluster

DATACENTER   RACK      ORDINAL   STATUS   STATE    ADDRESS      HOST ID       LOAD      TOKENS
----------   ----      -------   ------   -----    -------      -------       ----      ------
us-east-1    rack-0    -         Up       Normal   10.0.1.5     a1b2c3d4...   1.5 GB    256
us-east-1    rack-0    -         Up       Normal   10.0.1.6     e5f6g7h8...   1.4 GB    256
us-east-1    rack-0    -         Down     Normal   10.0.1.7     i9j0k1l2...   1.6 GB    256
```

#### Node Replacement

Replace a failed node in the cluster:

```bash
scylladbctl cluster node replace --datacenter us-east-1 --ordinal 1
```

This command orchestrates the node replacement procedure by:
1. Verifying the target node is in Down status
2. Locating the corresponding Kubernetes service
3. Applying the `scylla/replace=""` label to trigger replacement
4. Waiting for the pod to be recreated and rejoin the cluster
5. Verifying the new node is Up and Normal

Options:
- `--datacenter, -d`: Datacenter name (required)
- `--ordinal, -o`: Node ordinal index (required)
- `--poll-interval`: Polling interval for checking operation status (default: 5s)
- `--timeout`: Timeout for the replacement operation (default: 30m)

Example output:

```
🔄 Starting node replacement for datacenter=us-east-1, ordinal=1

⏳ Starting node replacement procedure
⏳ Retrieving ScyllaCluster resource
⏳ Looking for node at ordinal 1
⏳ Target node is in rack rack-0 with ordinal 1
⏳ Verifying node status
⏳ Node confirmed as Down
⏳ Locating member service
⏳ Found member service: my-cluster-us-east-1-rack-0-1
⏳ Applying replace label to service
⏳ Replace label applied successfully
⏳ Waiting for node replacement to complete
⏳ Waiting for old pod my-cluster-us-east-1-rack-0-1 to be deleted
⏳ Old pod deleted, waiting for new pod to be created
⏳ Pod status: Pending
⏳ Pod status: Running
⏳ Pod running but scylla container not ready yet
⏳ New pod is running, verifying node status
⏳ Node status: Up/Joining
⏳ Node is Up and Normal
✅ Node replacement completed successfully!

🎉 Node replacement completed successfully!

Note: You should run a repair using ScyllaDB Manager to ensure data consistency.
```

## Global Flags

- `--cluster, -c`: Name of the ScyllaCluster (env: `SCYLLA_CLUSTER_NAME`)
- `--namespace, -n`: Kubernetes namespace (env: `SCYLLA_CLUSTER_NAMESPACE`)
- `--kubeconfig`: Path to the kubeconfig file
- `--qps`: Maximum allowed number of queries per second (default: 50)
- `--burst`: Allows extra queries to accumulate when a client is exceeding its rate (default: 100)

## Future Enhancements

Potential additions to the tool:

- **HTTP Server**: REST API accepting the same queries/commands as the CLI
- **Interactive UI**: React-based web interface communicating with the local HTTP server
- **Additional Commands**:
  - `scylladbctl cluster backup status`: Display backup status
  - `scylladbctl cluster view`: Visual representation of the cluster
  - `scylladbctl cluster node status`: Detailed status of a specific node
  - `scylladbctl cluster node remove`: Remove a node from the middle of a rack
  - `scylladbctl gather`: Collect debug information

## Implementation Details

### Event Streaming

All long-running operations send timestamped events through channels:
- `EventTypeProgress`: Progress updates during execution
- `EventTypeStatus`: Status information
- `EventTypeError`: Error occurred during execution
- `EventTypeCompletion`: Operation completed successfully

The CLI consumes these events in real-time, providing immediate feedback to users.

### Node Operations

Node replacement follows the official ScyllaDB Operator documentation:
- Uses `nodetool status` to verify node state
- Applies Kubernetes labels to trigger operator actions
- Polls pod status and cluster state until completion
- Provides detailed progress updates throughout the process

### Reliability

- Uses `kubectl exec` for pod operations (no network connectivity requirements)
- Handles context cancellation gracefully
- Provides configurable timeouts and polling intervals
- Reports errors with detailed context
