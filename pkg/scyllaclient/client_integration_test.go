package scyllaclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	configassets "github.com/scylladb/scylla-operator/assets/config"
	"github.com/testcontainers/testcontainers-go"
	scyllacontainers "github.com/testcontainers/testcontainers-go/modules/scylladb"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestClient(t *testing.T) {
	scyllaContainer := setupScyllaContainer(t)
	host, client := setupScyllaClient(t, scyllaContainer)

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	var statusAndState NodeStatusAndStateInfoSlice
	for {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for node status and state info")
		case <-ticker.C:
			var err error
			statusAndState, err = client.NodesStatusAndStateInfo(t.Context(), host)
			if err == nil {
				goto success
			}
			t.Logf("retrying to get node status and state info: %v", err)
		}
	}
success:

	if len(statusAndState) != 1 {
		t.Fatalf("expected 1 node, got %d", len(statusAndState))
	}
	node := statusAndState[0]
	if node.State != NodeStateNormal {
		t.Fatalf("expected node state to be NORMAL, got %s", statusAndState[0].State)
	}
	if node.Status != NodeStatusUp {
		t.Fatalf("expected node status to be UP, got %s", statusAndState[0].Status)
	}
}

func setupScyllaContainer(t *testing.T) *scyllacontainers.Container {
	scyllaImage := fmt.Sprintf("scylladb/scylla:%s", configassets.Project.Operator.ScyllaDBVersion)
	scyllaContainer, err := scyllacontainers.Run(t.Context(), scyllaImage,
		scyllacontainers.WithCustomCommands(
			"--api-address=0.0.0.0",
		),
		testcontainers.WithExposedPorts("10000"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("10000/tcp")),
	)
	if err != nil {
		t.Fatalf("failed to start ScyllaDB container: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		if err := scyllaContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate ScyllaDB container: %v", err)
		}
	})

	return scyllaContainer
}

func setupScyllaClient(t *testing.T, container *scyllacontainers.Container) (string, *Client) {
	host, err := container.Host(t.Context())
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}

	mappedPort, err := container.MappedPort(t.Context(), "10000")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	cfg := DefaultConfig("", host)
	cfg.Port = mappedPort.Port()
	cfg.Scheme = "http"
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create ScyllaDB client: %v", err)
	}
	t.Cleanup(client.Close)

	return host, client
}
