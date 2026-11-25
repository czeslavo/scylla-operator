// Package kube has the Kubernetes plumbing SODA needs: exec into containers and a short-lived helper Pod.
package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// Execer runs commands in containers over the API server's exec subresource.
type Execer struct {
	restConfig *rest.Config
	kubeClient kubernetes.Interface
}

func NewExecer(restConfig *rest.Config, kubeClient kubernetes.Interface) *Execer {
	return &Execer{restConfig: restConfig, kubeClient: kubeClient}
}

// ExecResult holds a command's output. ExitCode is -1 when the command didn't report one.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Exec runs command in the container. Arguments end up in the API request and in audit logs, so secrets must go
// over stdin only. A non-zero exit is returned in ExecResult, not as an error; err means the exec itself failed.
// Every call must carry a deadline: an exec into a GKE Pod once hung for 31 minutes after the command finished.
func (e *Execer) Exec(ctx context.Context, namespace, pod, container string, command []string, stdin io.Reader) (*ExecResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, fmt.Errorf("exec without a deadline is not allowed")
	}

	req := e.kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	wsExec, err := remotecommand.NewWebSocketExecutor(e.restConfig, http.MethodGet, req.URL().String())
	if err != nil {
		return nil, fmt.Errorf("can't create websocket executor: %w", err)
	}
	spdyExec, err := remotecommand.NewSPDYExecutor(e.restConfig, http.MethodPost, req.URL())
	if err != nil {
		return nil, fmt.Errorf("can't create spdy executor: %w", err)
	}
	// Fall back to SPDY only when the websocket upgrade itself failed, so a command never runs twice.
	executor, err := remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("can't create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	res := &ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}
	if err != nil {
		var exitErr utilexec.ExitError
		if errors.As(err, &exitErr) && exitErr.Exited() {
			res.ExitCode = exitErr.ExitStatus()
			return res, nil
		}
		res.ExitCode = -1
		return res, fmt.Errorf("can't exec %q in %s/%s/%s: %w", command[0], namespace, pod, container, err)
	}
	return res, nil
}
