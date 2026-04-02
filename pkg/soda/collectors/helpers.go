package collectors

import (
	"context"
	"fmt"
	"path"

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

// collectAndWriteManifests marshals each item to YAML and writes it as an artifact.
// Marshal and write errors are non-fatal (silently skipped). Returns the collected artifacts.
func collectAndWriteManifests[T any](
	writer engine.ArtifactWriter,
	items []T,
	filenameFn func(*T) string,
	descriptionFn func(*T) string,
) []engine.Artifact {
	if writer == nil {
		return nil
	}
	var artifacts []engine.Artifact
	for i := range items {
		item := &items[i]
		marshalAndWriteYAML(writer, filenameFn(item), descriptionFn(item), item, &artifacts)
	}
	return artifacts
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

// collectContainerLogs fetches current and previous logs for each container and writes
// them as artifacts. Log fetch and artifact write errors are non-fatal.
// pathPrefix is prepended to filenames (e.g. "ns/podName" for operator logs, "" for per-node).
func collectContainerLogs(
	ctx context.Context,
	fetcher engine.PodLogFetcher,
	writer engine.ArtifactWriter,
	namespace, podName string,
	containerNames []string,
	pathPrefix string,
) []engine.Artifact {
	var artifacts []engine.Artifact
	for _, containerName := range containerNames {
		currentFile := containerName + ".current.log"
		previousFile := containerName + ".previous.log"
		if pathPrefix != "" {
			currentFile = path.Join(pathPrefix, currentFile)
			previousFile = path.Join(pathPrefix, previousFile)
		}

		// Current logs (non-fatal on error).
		fetchAndWriteContainerLogs(ctx, fetcher, namespace, podName, containerName, false, //nolint:errcheck
			currentFile,
			fmt.Sprintf("Current logs for container %s in pod %s/%s", containerName, namespace, podName),
			writer, &artifacts)

		// Previous logs (best-effort: skip if no previous run or any error).
		fetchAndWriteContainerLogs(ctx, fetcher, namespace, podName, containerName, true, //nolint:errcheck
			previousFile,
			fmt.Sprintf("Previous logs for container %s in pod %s/%s", containerName, namespace, podName),
			writer, &artifacts)
	}
	return artifacts
}
