// Copyright (C) 2026 ScyllaDB

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl/options"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
)

func NewNodeCommand(o *options.Options, streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage ScyllaDB cluster nodes",
		Long:  `Commands for managing individual nodes in a ScyllaDB cluster.`,
	}

	cmd.AddCommand(NewNodeReplaceCommand(o, streams))

	return cmd
}
