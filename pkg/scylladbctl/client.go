// Copyright (C) 2026 ScyllaDB

package scylladbctl

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	scyllaversionedclient "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
)

type Client struct {
	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface
	restConfig   *rest.Config
}

func NewClient(kubeClient kubernetes.Interface, scyllaClient scyllaversionedclient.Interface, restConfig *rest.Config) *Client {
	return &Client{
		kubeClient:   kubeClient,
		scyllaClient: scyllaClient,
		restConfig:   restConfig,
	}
}

var _ Dispatcher = &Client{}
