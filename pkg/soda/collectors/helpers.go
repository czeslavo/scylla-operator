package collectors

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	"sigs.k8s.io/yaml"
)

// ExecInScyllaPod runs a command inside the scylla container of the node
// described by params.ScyllaNode and returns its stdout.
func ExecInScyllaPod(ctx context.Context, params engine.PerScyllaNodeCollectorParams, command []string) (string, error) {
	stdout, _, err := params.PodExecutor.Execute(
		ctx,
		params.ScyllaNode.Namespace,
		params.ScyllaNode.Name,
		scyllaContainerName,
		command,
	)
	if err != nil {
		return "", fmt.Errorf("executing %v in pod %s/%s: %w",
			command, params.ScyllaNode.Namespace, params.ScyllaNode.Name, err)
	}
	return stdout, nil
}

// writeArtifact writes content to the artifact writer (if non-nil), appending
// to artifacts on success and silently ignoring write errors (non-fatal).
func writeArtifact(w engine.ArtifactWriter, filename string, content []byte, description string, artifacts *[]engine.Artifact) {
	if w == nil {
		return
	}
	relPath, err := w.WriteArtifact(filename, content)
	if err != nil {
		return
	}
	*artifacts = append(*artifacts, engine.Artifact{RelativePath: relPath, Description: description})
}

// marshalAndWriteYAML serializes v to YAML and writes it as an artifact.
// Errors from marshaling or writing are silently ignored (non-fatal).
func marshalAndWriteYAML(w engine.ArtifactWriter, filename string, description string, v any, artifacts *[]engine.Artifact) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return
	}
	writeArtifact(w, filename, data, description, artifacts)
}

// fetchAndWriteContainerLogs retrieves container logs via PodLogFetcher and
// writes them as an artifact. Returns the raw bytes on success.
func fetchAndWriteContainerLogs(
	ctx context.Context,
	fetcher engine.PodLogFetcher,
	namespace, podName, containerName string,
	previous bool,
	filename string,
	description string,
	artifactWriter engine.ArtifactWriter,
	artifacts *[]engine.Artifact,
) ([]byte, error) {
	logs, err := fetcher.GetPodLogs(ctx, namespace, podName, containerName, previous)
	if err != nil {
		return nil, err
	}
	writeArtifact(artifactWriter, filename, logs, description, artifacts)
	return logs, nil
}
