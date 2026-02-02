// Copyright (C) 2026 ScyllaDB

package scylladbctl

import (
	"context"
	"time"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
)

type EventType string

const (
	EventTypeProgress   EventType = "progress"
	EventTypeStatus     EventType = "status"
	EventTypeError      EventType = "error"
	EventTypeCompletion EventType = "completion"
)

type Event struct {
	Type      EventType
	Timestamp time.Time
	Message   string
	Data      interface{}
	Error     error
}

type NodeStatus struct {
	Datacenter string
	Rack       string
	Ordinal    int32
	PodName    string
	Status     string
	State      string
	Address    string
	HostID     string
	Load       string
	Tokens     string
	Owns       string
}

type ClusterStatusResult struct {
	ClusterName string
	Nodes       []NodeStatus
}

type NodeReplaceOptions struct {
	PollInterval time.Duration
	Timeout      time.Duration
}

func DefaultNodeReplaceOptions() NodeReplaceOptions {
	return NodeReplaceOptions{
		PollInterval: 5 * time.Second,
		Timeout:      30 * time.Minute,
	}
}

type Dispatcher interface {
	ClusterStatus(ctx context.Context, namespace, name string, eventCh chan<- Event) (*ClusterStatusResult, error)
	NodeReplace(ctx context.Context, namespace, name, datacenter string, ordinal int32, options NodeReplaceOptions, eventCh chan<- Event) error
	GetCluster(ctx context.Context, namespace, name string) (*scyllav1.ScyllaCluster, error)
}
