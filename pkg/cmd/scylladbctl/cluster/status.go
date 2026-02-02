// Copyright (C) 2026 ScyllaDB

package cluster

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl/options"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/scylladbctl"
)

type StatusOptions struct {
	*options.Options
}

func NewStatusOptions(o *options.Options, streams genericclioptions.IOStreams) *StatusOptions {
	return &StatusOptions{
		Options: o,
	}
}

func NewStatusCommand(o *options.Options, streams genericclioptions.IOStreams) *cobra.Command {
	statusOpts := NewStatusOptions(o, streams)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display cluster status",
		Long: `Display the status of all nodes in the ScyllaDB cluster.

This command queries all nodes in the cluster and displays their current status,
including whether they are Up or Down, and their state (Normal, Joining, Leaving, Moving).`,
		Example: `  # Display status of cluster 'my-cluster' in namespace 'scylla'
  scylladbctl cluster status --cluster my-cluster --namespace scylla

  # Use environment variables
  export SCYLLA_CLUSTER_NAME=my-cluster
  export SCYLLA_CLUSTER_NAMESPACE=scylla
  scylladbctl cluster status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := statusOpts.Validate()
			if err != nil {
				return err
			}

			err = statusOpts.Run(cmd.Context(), streams)
			if err != nil {
				return err
			}

			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	return cmd
}

func (o *StatusOptions) Run(ctx context.Context, streams genericclioptions.IOStreams) error {
	eventCh := make(chan scylladbctl.Event, 10)
	defer close(eventCh)

	errCh := make(chan error, 1)
	resultCh := make(chan *scylladbctl.ClusterStatusResult, 1)

	go func() {
		result, err := o.Dispatcher().ClusterStatus(ctx, o.Namespace, o.Cluster, eventCh)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	var result *scylladbctl.ClusterStatusResult
	for {
		select {
		case event := <-eventCh:
			switch event.Type {
			case scylladbctl.EventTypeProgress:
				fmt.Fprintf(streams.Out, "⏳ %s\n", event.Message)
			case scylladbctl.EventTypeError:
				fmt.Fprintf(streams.ErrOut, "❌ %s: %v\n", event.Message, event.Error)
			case scylladbctl.EventTypeCompletion:
				fmt.Fprintf(streams.Out, "✅ %s\n", event.Message)
			}
		case err := <-errCh:
			return fmt.Errorf("failed to get cluster status: %w", err)
		case res := <-resultCh:
			result = res
			goto displayResults
		}
	}

displayResults:
	fmt.Fprintln(streams.Out)
	fmt.Fprintf(streams.Out, "Cluster: %s\n", result.ClusterName)
	fmt.Fprintln(streams.Out)

	w := tabwriter.NewWriter(streams.Out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DATACENTER\tRACK\tORDINAL\tSTATUS\tSTATE\tADDRESS\tHOST ID\tLOAD\tTOKENS")
	fmt.Fprintln(w, "----------\t----\t-------\t------\t-----\t-------\t-------\t----\t------")

	for _, node := range result.Nodes {
		ordinal := "-"
		if node.Ordinal >= 0 {
			ordinal = fmt.Sprintf("%d", node.Ordinal)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			node.Datacenter,
			node.Rack,
			ordinal,
			node.Status,
			node.State,
			node.Address,
			node.HostID,
			node.Load,
			node.Tokens,
		)
	}

	w.Flush()

	return nil
}
