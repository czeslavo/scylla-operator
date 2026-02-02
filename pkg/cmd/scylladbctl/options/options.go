// Copyright (C) 2026 ScyllaDB

package options

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	scyllaversionedclient "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/scylladbctl"
)

type Options struct {
	genericclioptions.ClientConfig
	genericclioptions.IOStreams

	Cluster   string
	Namespace string

	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface
	restConfig   *rest.Config
	dispatcher   scylladbctl.Dispatcher
}

func NewOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		ClientConfig: genericclioptions.ClientConfig{
			QPS:   50,
			Burst: 100,
		},
		IOStreams: streams,
		Cluster:   os.Getenv("SCYLLA_CLUSTER_NAME"),
		Namespace: os.Getenv("SCYLLA_CLUSTER_NAMESPACE"),
	}
}

func (o *Options) AddFlags(cmd *cobra.Command) {
	o.ClientConfig.AddFlags(cmd)

	cmd.PersistentFlags().StringVarP(&o.Cluster, "cluster", "c", o.Cluster, "Name of the ScyllaCluster (can be set via SCYLLA_CLUSTER_NAME env var)")
	cmd.PersistentFlags().StringVarP(&o.Namespace, "namespace", "n", o.Namespace, "Kubernetes namespace (can be set via SCYLLA_CLUSTER_NAMESPACE env var)")
}

func (o *Options) Complete() error {
	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	restConfig.QPS = o.QPS
	restConfig.Burst = o.Burst
	o.restConfig = restConfig

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	o.kubeClient = kubeClient

	scyllaClient, err := scyllaversionedclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create scylla client: %w", err)
	}
	o.scyllaClient = scyllaClient

	o.dispatcher = scylladbctl.NewClient(kubeClient, scyllaClient, restConfig)

	if o.Namespace == "" {
		o.Namespace = "default"
	}

	return nil
}

func (o *Options) Validate() error {
	if o.Cluster == "" {
		return fmt.Errorf("cluster name is required (use --cluster flag or SCYLLA_CLUSTER_NAME env var)")
	}

	if o.Namespace == "" {
		return fmt.Errorf("namespace is required (use --namespace flag or SCYLLA_CLUSTER_NAMESPACE env var)")
	}

	return nil
}

func (o *Options) Dispatcher() scylladbctl.Dispatcher {
	return o.dispatcher
}
