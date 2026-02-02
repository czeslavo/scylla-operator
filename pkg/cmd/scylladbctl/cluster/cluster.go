// Copyright (C) 2026 ScyllaDB

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl/options"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
)

func NewClusterCommand(o *options.Options, streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage ScyllaDB clusters",
		Long:  `Commands for managing ScyllaDB clusters, including status checks and node operations.`,
	}

	cmd.AddCommand(NewStatusCommand(o, streams))
	cmd.AddCommand(NewNodeCommand(o, streams))

	return cmd
}
