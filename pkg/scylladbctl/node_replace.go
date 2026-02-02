// Copyright (C) 2026 ScyllaDB

package scylladbctl

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/naming"
)

func (c *Client) NodeReplace(ctx context.Context, namespace, name, datacenter string, ordinal int32, options NodeReplaceOptions, eventCh chan<- Event) error {
	sendEvent := func(eventType EventType, message string, data interface{}, err error) {
		if eventCh != nil {
			eventCh <- Event{
				Type:      eventType,
				Timestamp: time.Now(),
				Message:   message,
				Data:      data,
				Error:     err,
			}
		}
	}

	sendEvent(EventTypeProgress, "Starting node replacement procedure", nil, nil)

	sendEvent(EventTypeProgress, "Retrieving ScyllaCluster resource", nil, nil)
	sc, err := c.GetCluster(ctx, namespace, name)
	if err != nil {
		sendEvent(EventTypeError, "Failed to get ScyllaCluster", nil, err)
		return fmt.Errorf("failed to get ScyllaCluster %s/%s: %w", namespace, name, err)
	}

	if sc.Spec.Datacenter.Name != datacenter {
		err := fmt.Errorf("datacenter %s not found in cluster, expected %s", datacenter, sc.Spec.Datacenter.Name)
		sendEvent(EventTypeError, "Invalid datacenter", nil, err)
		return err
	}

	sendEvent(EventTypeProgress, fmt.Sprintf("Looking for node at ordinal %d", ordinal), nil, nil)
	var targetRack *scyllav1.RackSpec
	for i := range sc.Spec.Datacenter.Racks {
		rack := &sc.Spec.Datacenter.Racks[i]
		if ordinal < rack.Members {
			targetRack = rack
			break
		}
		ordinal -= rack.Members
	}

	if targetRack == nil {
		err := fmt.Errorf("ordinal out of range for datacenter %s", datacenter)
		sendEvent(EventTypeError, "Invalid ordinal", nil, err)
		return err
	}

	sendEvent(EventTypeProgress, fmt.Sprintf("Target node is in rack %s with ordinal %d", targetRack.Name, ordinal), nil, nil)

	sendEvent(EventTypeProgress, "Verifying node status", nil, nil)
	isDown, err := c.verifyNodeIsDown(ctx, sc, targetRack, ordinal)
	if err != nil {
		sendEvent(EventTypeError, "Failed to verify node status", nil, err)
		return fmt.Errorf("failed to verify node status: %w", err)
	}

	if !isDown {
		err := fmt.Errorf("node is not in Down status, cannot proceed with replacement")
		sendEvent(EventTypeError, "Node is not down", nil, err)
		return err
	}

	sendEvent(EventTypeProgress, "Node confirmed as Down", nil, nil)

	sendEvent(EventTypeProgress, "Locating member service", nil, nil)
	serviceName := naming.MemberServiceNameForScyllaCluster(*targetRack, sc, int(ordinal))
	svc, err := c.kubeClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		sendEvent(EventTypeError, fmt.Sprintf("Failed to get service %s", serviceName), nil, err)
		return fmt.Errorf("failed to get service %s: %w", serviceName, err)
	}

	sendEvent(EventTypeProgress, fmt.Sprintf("Found member service: %s", serviceName), nil, nil)

	sendEvent(EventTypeProgress, "Applying replace label to service", nil, nil)
	patch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":""}}}`, naming.ReplaceLabel))
	_, err = c.kubeClient.CoreV1().Services(namespace).Patch(
		ctx,
		svc.Name,
		types.MergePatchType,
		patch,
		metav1.PatchOptions{},
	)
	if err != nil {
		sendEvent(EventTypeError, "Failed to apply replace label", nil, err)
		return fmt.Errorf("failed to apply replace label: %w", err)
	}

	sendEvent(EventTypeProgress, "Replace label applied successfully", nil, nil)

	sendEvent(EventTypeProgress, "Waiting for node replacement to complete", nil, nil)
	err = c.waitForNodeReplacement(ctx, sc, targetRack, ordinal, options, sendEvent)
	if err != nil {
		sendEvent(EventTypeError, "Node replacement failed", nil, err)
		return fmt.Errorf("node replacement failed: %w", err)
	}

	sendEvent(EventTypeCompletion, "Node replacement completed successfully", nil, nil)

	return nil
}

func (c *Client) verifyNodeIsDown(ctx context.Context, sc *scyllav1.ScyllaCluster, rack *scyllav1.RackSpec, ordinal int32) (bool, error) {
	pods, err := c.discoverClusterPods(ctx, sc)
	if err != nil || len(pods) == 0 {
		return false, fmt.Errorf("no running pods found to check status")
	}

	stdout, stderr, err := c.execInPod(ctx, &pods[0], naming.ScyllaContainerName, []string{"nodetool", "status"})
	if err != nil {
		return false, fmt.Errorf("failed to execute nodetool status: %w (stderr: %s)", err, stderr)
	}

	nodes, err := parseNodetoolStatus(stdout, sc.Spec.Datacenter.Name, pods[0].Labels)
	if err != nil {
		return false, fmt.Errorf("failed to parse nodetool output: %w", err)
	}

	for _, node := range nodes {
		if node.Rack == rack.Name && node.Ordinal == ordinal {
			return node.Status == "Down", nil
		}
	}

	return true, nil
}

func (c *Client) waitForNodeReplacement(
	ctx context.Context,
	sc *scyllav1.ScyllaCluster,
	rack *scyllav1.RackSpec,
	ordinal int32,
	options NodeReplaceOptions,
	sendEvent func(EventType, string, interface{}, error),
) error {
	podName := naming.PodNameForScyllaCluster(*rack, sc, int(ordinal))

	sendEvent(EventTypeProgress, fmt.Sprintf("Waiting for old pod %s to be deleted", podName), nil, nil)
	err := wait.PollUntilContextTimeout(ctx, options.PollInterval, options.Timeout, true, func(ctx context.Context) (bool, error) {
		_, err := c.kubeClient.CoreV1().Pods(sc.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timeout waiting for old pod to be deleted: %w", err)
	}

	sendEvent(EventTypeProgress, "Old pod deleted, waiting for new pod to be created", nil, nil)

	err = wait.PollUntilContextTimeout(ctx, options.PollInterval, options.Timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := c.kubeClient.CoreV1().Pods(sc.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		if pod.Status.Phase != corev1.PodRunning {
			sendEvent(EventTypeProgress, fmt.Sprintf("Pod status: %s", pod.Status.Phase), nil, nil)
			return false, nil
		}

		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == naming.ScyllaContainerName {
				if containerStatus.Ready {
					return true, nil
				}
				sendEvent(EventTypeProgress, "Pod running but scylla container not ready yet", nil, nil)
				return false, nil
			}
		}

		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timeout waiting for new pod to be ready: %w", err)
	}

	sendEvent(EventTypeProgress, "New pod is running, verifying node status", nil, nil)

	err = wait.PollUntilContextTimeout(ctx, options.PollInterval, options.Timeout, true, func(ctx context.Context) (bool, error) {
		pods, err := c.discoverClusterPods(ctx, sc)
		if err != nil || len(pods) == 0 {
			return false, nil
		}

		stdout, _, err := c.execInPod(ctx, &pods[0], naming.ScyllaContainerName, []string{"nodetool", "status"})
		if err != nil {
			return false, nil
		}

		nodes, err := parseNodetoolStatus(stdout, sc.Spec.Datacenter.Name, pods[0].Labels)
		if err != nil {
			return false, nil
		}

		for _, node := range nodes {
			if node.Rack == rack.Name && node.Ordinal == ordinal {
				if node.Status == "Up" && node.State == "Normal" {
					sendEvent(EventTypeProgress, "Node is Up and Normal", nil, nil)
					return true, nil
				}
				sendEvent(EventTypeProgress, fmt.Sprintf("Node status: %s/%s", node.Status, node.State), nil, nil)
				return false, nil
			}
		}

		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timeout waiting for node to become Up/Normal: %w", err)
	}

	return nil
}
