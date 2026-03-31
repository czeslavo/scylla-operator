# ScyllaDB Kubernetes Diagnostic Tool - Complete Rewrite Requirements

This document outlines the requirements for a complete rewrite of the existing diagnostic tools for ScyllaDB running in
Kubernetes environments, aiming to merge the best features of both must-gather and Scylla Doctor into a single,
comprehensive, and user-friendly tool.

A working name for the new tool is "soda" (from Scylla Operator Diagnostic Automation) but the final name can be decided later.

## Context

ScyllaDB currently ships two independent diagnostic tools that serve overlapping but incomplete audiences.
**must-gather**, embedded in the Scylla Operator, is a Kubernetes-native artifact collector that talks to the Kubernetes
API, dumps resource definitions, pod logs, and runs a handful of commands inside Scylla containers.
**Scylla Doctor**, a standalone Python tool, runs directly on bare-metal/VM Scylla nodes, collects 63 categories of deep
system and Scylla-level data, and runs ~45 analyzers that produce actionable PASSED/WARNING/FAILED verdicts.

Neither tool alone covers the full diagnostic surface for Kubernetes-based ScyllaDB deployments.
must-gather lacks depth in Scylla-specific diagnostics (REST API queries, CQL state, hardware/OS inspection, tuning
verification, and any form of automated analysis).
Scylla Doctor has zero Kubernetes awareness and several of its collectors explicitly skip or malfunction in container
environments.

Scylla Doctor comes with architecture of a "collector" that gathers data and an "analyzer" that processes it, and the
two are decoupled by a well-defined JSON-based interface.
This kind of architecture allows letting users decide which collectors and analyzers to run, depending on their needs.

The goal of this effort is to merge the best of both worlds into a single tool that can be easily executed by
Kubernetes users and provides comprehensive diagnostics for ScyllaDB running in Kubernetes.

There were concerns regarding must-gather's transparency and user-friendliness. It was noted that the tool's output can
be overwhelming and difficult to navigate, especially for users who are not familiar with Kubernetes.
Must-gather tends to collect not only relevant ScyllaDB-specific data, but also generic Kubernetes cluster information,
which can be excessive and may require users to sift through large amounts of data to find what they need (and also
potentially expose sensitive information).

We should give users the option to choose what collectors will run, documenting each of them, making it transparent what
data is being collected and why (what analyzers need it).

### Requirements

- Provide a single diagnostic workflow for Kubernetes-based ScyllaDB deployments.
- Let users decide which collectors and analyzers to run, depending on their needs (can provide configurable "profiles"
  that group collectors and analyzers by use case, allowing defining custom ones).
- Make it transparent what data is being collected and why (what analyzers need it) before the user runs the tool (e.g.,
  via documentation or an interactive prompt).
- Allow pointing a specific ScyllaDB cluster (or multiple clusters) to the tool, instead of always collecting data from
  all ScyllaDB clusters in the Kubernetes cluster.
- Consider possibility of allowing extending the tool with custom collectors and analyzers, e.g. for users to add their
  own domain-specific checks (e.g., via shell scripts).
- Provide a clear and user-friendly output that lists the results of the analyzers in a concise way, allowing users to
  quickly identify issues and their potential causes.
- The only required input should be the kubeconfig file, and the tool should be able to discover ScyllaDB clusters on
  its own. Scope of access required by the tool should be clearly documented, and the tool should not require more
  permissions than necessary to collect the required data. Permissions levels should depend on the selected collectors
  and analyzers, and the tool should provide guidance on how to set up the necessary permissions for each use case (each
  collector should define permissions it needs - that should allow validating the permissions setup before running the
  tool).
- The tool should cover all the existing Scylla Doctor collectors and analyzers (although some of them may need to be
  adapted to work in Kubernetes environments), and also add new ones that are relevant for Kubernetes-based
  deployments (e.g., related to Kubernetes resource configuration, operator logs, etc.).
- A produced artifact bundle should be self-describing and navigable, allowing users to easily find the collected data
  and analyzer results (e.g., via an index file or a structured directory layout). It should be also possible to
  generate an LLM prompt from the collected data and analyzer results, so that agents like opencode can navigate it and
  provide recommendations to users (linking to ScyllaDB Operator/ScyllaDB source code for the agent to understand the
  context and provide more accurate recommendations).
- The tool should be designed with extensibility in mind, allowing for easy addition of new collectors and analyzers in
  the future without requiring major changes to the core architecture.

### Non-goals

- Making the tool work outside of Kubernetes environments (e.g., on bare-metal or VMs).
- Providing a web-based UI for the tool (the output can be consumed by external tools that provide a UI, but the tool
  itself will be CLI-based).
- Providing real-time monitoring or alerting capabilities (the tool is meant for on-demand diagnostics, not continuous
  monitoring).
- Integrating with external monitoring or logging systems (the tool will focus on collecting data from the Kubernetes
  cluster and ScyllaDB, and providing actionable insights based on that data, rather than integrating with external
  systems).
- Providing automated remediation capabilities (the tool will provide recommendations based on the collected data and
  analyzer results, but it will be up to the users to decide how to act on those recommendations).
