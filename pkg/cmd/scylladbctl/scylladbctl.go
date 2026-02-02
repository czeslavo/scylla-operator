// Copyright (C) 2026 ScyllaDB

package scylladbctl

import (
	"github.com/spf13/cobra"

	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl/cluster"
	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl/options"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
)

func NewScyllaDBCtlCommand(streams genericclioptions.IOStreams) *cobra.Command {
	o := options.NewOptions(streams)

	cmd := &cobra.Command{
		Use:   "scylladbctl",
		Short: "scylladbctl is a kubectl-like tool for managing ScyllaDB clusters on Kubernetes",
		Long: `scylladbctl provides high-level operations for ScyllaDB clusters running on Kubernetes.

It exposes convenient commands for common operations like checking cluster status,
replacing nodes, and managing cluster lifecycle without dealing with low-level
Kubernetes resources directly.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return o.Complete()
		},
	}

	o.AddFlags(cmd)

	cmd.AddCommand(cluster.NewClusterCommand(o, streams))

	return cmd
}
