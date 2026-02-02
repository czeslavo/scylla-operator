// Copyright (C) 2026 ScyllaDB

package cluster

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl/options"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/scylladbctl"
)

type NodeReplaceOptions struct {
	*options.Options

	Datacenter     string
	Ordinal        int32
	ReplaceOptions scylladbctl.NodeReplaceOptions
}

func NewNodeReplaceOptions(o *options.Options, streams genericclioptions.IOStreams) *NodeReplaceOptions {
	return &NodeReplaceOptions{
		Options:        o,
		ReplaceOptions: scylladbctl.DefaultNodeReplaceOptions(),
	}
}

func NewNodeReplaceCommand(o *options.Options, streams genericclioptions.IOStreams) *cobra.Command {
	replaceOpts := NewNodeReplaceOptions(o, streams)

	cmd := &cobra.Command{
		Use:   "replace",
		Short: "Replace a failed node in the cluster",
		Long: `Replace a failed node in the ScyllaDB cluster.

This command orchestrates the node replacement procedure by:
1. Verifying the target node is in Down status
2. Locating the corresponding Kubernetes service
3. Applying the replace label to trigger replacement
4. Waiting for the pod to be recreated and rejoin the cluster
5. Verifying the new node is Up and Normal

The node is identified by its datacenter name and ordinal index within that datacenter.`,
		Example: `  # Replace node with ordinal 1 in datacenter us-east-1
  scylladbctl cluster node replace --datacenter us-east-1 --ordinal 1

  # Replace with environment variables set
  export SCYLLA_CLUSTER_NAME=my-cluster
  export SCYLLA_CLUSTER_NAMESPACE=scylla
  scylladbctl cluster node replace --datacenter us-east-1 --ordinal 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := replaceOpts.Validate()
			if err != nil {
				return err
			}

			err = replaceOpts.Run(cmd.Context(), streams)
			if err != nil {
				return err
			}

			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().StringVarP(&replaceOpts.Datacenter, "datacenter", "d", "", "Datacenter name (required)")
	cmd.Flags().Int32VarP(&replaceOpts.Ordinal, "ordinal", "o", -1, "Node ordinal index (required)")
	cmd.Flags().DurationVar(&replaceOpts.ReplaceOptions.PollInterval, "poll-interval", replaceOpts.ReplaceOptions.PollInterval, "Polling interval for checking operation status")
	cmd.Flags().DurationVar(&replaceOpts.ReplaceOptions.Timeout, "timeout", replaceOpts.ReplaceOptions.Timeout, "Timeout for the replacement operation")

	cmd.MarkFlagRequired("datacenter")
	cmd.MarkFlagRequired("ordinal")

	return cmd
}

func (o *NodeReplaceOptions) Validate() error {
	if err := o.Options.Validate(); err != nil {
		return err
	}

	if o.Datacenter == "" {
		return fmt.Errorf("datacenter is required")
	}

	if o.Ordinal < 0 {
		return fmt.Errorf("ordinal must be non-negative")
	}

	return nil
}

func (o *NodeReplaceOptions) Run(ctx context.Context, streams genericclioptions.IOStreams) error {
	fmt.Fprintf(streams.Out, "🔄 Starting node replacement for datacenter=%s, ordinal=%d\n\n", o.Datacenter, o.Ordinal)

	eventCh := make(chan scylladbctl.Event, 10)
	defer close(eventCh)

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})

	go func() {
		err := o.Dispatcher().NodeReplace(
			ctx,
			o.Namespace,
			o.Cluster,
			o.Datacenter,
			o.Ordinal,
			o.ReplaceOptions,
			eventCh,
		)
		if err != nil {
			errCh <- err
			return
		}
		close(doneCh)
	}()

	for {
		select {
		case event := <-eventCh:
			switch event.Type {
			case scylladbctl.EventTypeProgress:
				fmt.Fprintf(streams.Out, "⏳ %s\n", event.Message)
			case scylladbctl.EventTypeError:
				fmt.Fprintf(streams.ErrOut, "❌ %s: %v\n", event.Message, event.Error)
			case scylladbctl.EventTypeCompletion:
				fmt.Fprintf(streams.Out, "✅ %s\n\n", event.Message)
			}
		case err := <-errCh:
			return fmt.Errorf("node replacement failed: %w", err)
		case <-doneCh:
			fmt.Fprintf(streams.Out, "🎉 Node replacement completed successfully!\n")
			fmt.Fprintf(streams.Out, "\nNote: You should run a repair using ScyllaDB Manager to ensure data consistency.\n")
			return nil
		}
	}
}
