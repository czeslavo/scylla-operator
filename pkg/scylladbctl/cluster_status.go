// Copyright (C) 2026 ScyllaDB

package scylladbctl

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/naming"
)

func (c *Client) GetCluster(ctx context.Context, namespace, name string) (*scyllav1.ScyllaCluster, error) {
	return c.scyllaClient.ScyllaV1().ScyllaClusters(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *Client) ClusterStatus(ctx context.Context, namespace, name string, eventCh chan<- Event) (*ClusterStatusResult, error) {
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

	sendEvent(EventTypeProgress, "Retrieving ScyllaCluster resource", nil, nil)

	sc, err := c.GetCluster(ctx, namespace, name)
	if err != nil {
		sendEvent(EventTypeError, "Failed to get ScyllaCluster", nil, err)
		return nil, fmt.Errorf("failed to get ScyllaCluster %s/%s: %w", namespace, name, err)
	}

	sendEvent(EventTypeProgress, fmt.Sprintf("Found cluster with datacenter: %s", sc.Spec.Datacenter.Name), nil, nil)

	sendEvent(EventTypeProgress, "Discovering cluster pods", nil, nil)
	pods, err := c.discoverClusterPods(ctx, sc)
	if err != nil {
		sendEvent(EventTypeError, "Failed to discover cluster pods", nil, err)
		return nil, fmt.Errorf("failed to discover cluster pods: %w", err)
	}

	sendEvent(EventTypeProgress, fmt.Sprintf("Found %d pods in cluster", len(pods)), nil, nil)

	var allNodes []NodeStatus
	for _, pod := range pods {
		sendEvent(EventTypeProgress, fmt.Sprintf("Querying status from pod %s", pod.Name), nil, nil)

		nodes, err := c.getNodeStatusFromPod(ctx, &pod, sc.Spec.Datacenter.Name)
		if err != nil {
			sendEvent(EventTypeError, fmt.Sprintf("Failed to get status from pod %s", pod.Name), nil, err)
			continue
		}

		allNodes = append(allNodes, nodes...)
	}

	result := &ClusterStatusResult{
		ClusterName: name,
		Nodes:       allNodes,
	}

	sendEvent(EventTypeCompletion, fmt.Sprintf("Retrieved status for %d nodes", len(allNodes)), result, nil)

	return result, nil
}

func (c *Client) discoverClusterPods(ctx context.Context, sc *scyllav1.ScyllaCluster) ([]corev1.Pod, error) {
	selector := labels.SelectorFromSet(labels.Set{
		naming.ClusterNameLabel: sc.Name,
	})

	podList, err := c.kubeClient.CoreV1().Pods(sc.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var pods []corev1.Pod
	for _, pod := range podList.Items {
		hasScyllaContainer := false
		for _, container := range pod.Spec.Containers {
			if container.Name == naming.ScyllaContainerName {
				hasScyllaContainer = true
				break
			}
		}

		if hasScyllaContainer && pod.Status.Phase == corev1.PodRunning {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

func (c *Client) getNodeStatusFromPod(ctx context.Context, pod *corev1.Pod, datacenterName string) ([]NodeStatus, error) {
	stdout, stderr, err := c.execInPod(ctx, pod, naming.ScyllaContainerName, []string{"nodetool", "status"})
	if err != nil {
		return nil, fmt.Errorf("failed to execute nodetool status: %w (stderr: %s)", err, stderr)
	}

	nodes, err := parseNodetoolStatus(stdout, datacenterName, pod.Labels)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nodetool status output: %w", err)
	}

	return nodes, nil
}

func (c *Client) execInPod(ctx context.Context, pod *corev1.Pod, containerName string, command []string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	execOptions := ExecOptions{
		Command:       command,
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: containerName,
		CaptureStdout: true,
		CaptureStderr: true,
		Stdin:         nil,
	}

	stdoutStr, stderrStr, err := ExecWithOptions(ctx, c.restConfig, c.kubeClient.CoreV1(), execOptions)
	if err != nil {
		return stdoutStr, stderrStr, err
	}

	stdout.WriteString(stdoutStr)
	stderr.WriteString(stderrStr)

	return stdout.String(), stderr.String(), nil
}

func parseNodetoolStatus(output, datacenterName string, podLabels map[string]string) ([]NodeStatus, error) {
	lines := strings.Split(output, "\n")
	var nodes []NodeStatus

	nodeRegex := regexp.MustCompile(`^([UD])([NLJM])\s+(\S+)\s+(\S+\s*\S*)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)`)

	podName := podLabels[appsv1.StatefulSetPodNameLabel]
	ordinal := int32(-1)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		matches := nodeRegex.FindStringSubmatch(line)
		if len(matches) == 9 {
			status := matches[1]
			state := matches[2]
			address := matches[3]
			load := matches[4]
			tokens := matches[5]
			owns := matches[6]
			hostID := matches[7]
			rackFromOutput := matches[8]

			statusFull := "Up"
			if status == "D" {
				statusFull = "Down"
			}

			stateFull := "Normal"
			switch state {
			case "L":
				stateFull = "Leaving"
			case "J":
				stateFull = "Joining"
			case "M":
				stateFull = "Moving"
			}

			node := NodeStatus{
				Datacenter: datacenterName,
				Rack:       rackFromOutput,
				Ordinal:    ordinal,
				PodName:    podName,
				Status:     statusFull,
				State:      stateFull,
				Address:    address,
				HostID:     hostID,
				Load:       load,
				Tokens:     tokens,
				Owns:       owns,
			}

			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}
