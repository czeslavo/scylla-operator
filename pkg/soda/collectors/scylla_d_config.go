package collectors

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// ScyllaDConfigCollectorID is the unique identifier for the ScyllaDConfigCollector.
	ScyllaDConfigCollectorID engine.CollectorID = "ScyllaDConfigCollector"
)

// ScyllaDConfigResult holds the contents of all files in /etc/scylla.d/, keyed by filename.
type ScyllaDConfigResult struct {
	Files map[string]string `json:"files"` // Map of filename (e.g. "io.conf") to file contents
}

// GetScyllaDConfigResult is the typed accessor for ScyllaDConfigCollector results.
func GetScyllaDConfigResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*ScyllaDConfigResult, error) {
	result, ok := vitals.Get(ScyllaDConfigCollectorID, podKey)
	if !ok {
		return nil, fmt.Errorf("ScyllaDConfigCollector result not found for %v", podKey)
	}
	if result.Status != engine.CollectorPassed {
		return nil, fmt.Errorf("ScyllaDConfigCollector did not pass for %v: %s", podKey, result.Message)
	}
	typed, ok := result.Data.(*ScyllaDConfigResult)
	if !ok {
		return nil, fmt.Errorf("unexpected data type %T for ScyllaDConfigCollector", result.Data)
	}
	return typed, nil
}

// scyllaDConfigCollector collects each file under /etc/scylla.d/ as a separate artifact.
type scyllaDConfigCollector struct{}

var _ engine.Collector = (*scyllaDConfigCollector)(nil)

// NewScyllaDConfigCollector creates a new ScyllaDConfigCollector.
func NewScyllaDConfigCollector() engine.Collector {
	return &scyllaDConfigCollector{}
}

func (c *scyllaDConfigCollector) ID() engine.CollectorID          { return ScyllaDConfigCollectorID }
func (c *scyllaDConfigCollector) Name() string                    { return "Scylla drop-in config directory" }
func (c *scyllaDConfigCollector) Scope() engine.CollectorScope    { return engine.PerPod }
func (c *scyllaDConfigCollector) DependsOn() []engine.CollectorID { return nil }

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to list and read /etc/scylla.d/* files)
func (c *scyllaDConfigCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *scyllaDConfigCollector) Collect(ctx context.Context, params engine.CollectorParams) (*engine.CollectorResult, error) {
	if params.Pod == nil {
		return nil, fmt.Errorf("pod info not provided")
	}

	// List files in /etc/scylla.d/ (one path per line, no directories).
	lsOut, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName,
		[]string{"bash", "-c", "ls /etc/scylla.d/"})
	if err != nil {
		return nil, fmt.Errorf("listing /etc/scylla.d/: %w", err)
	}

	filenames := parseLines(lsOut)

	result := &ScyllaDConfigResult{
		Files: make(map[string]string, len(filenames)),
	}

	var artifacts []engine.Artifact

	for _, filename := range filenames {
		fullPath := "/etc/scylla.d/" + filename

		content, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName,
			[]string{"cat", fullPath})
		if err != nil {
			// Record that we tried but failed rather than aborting the whole collector.
			result.Files[filename] = fmt.Sprintf("(error reading file: %v)", err)
			continue
		}

		result.Files[filename] = content

		if params.ArtifactWriter != nil {
			// Store each file under its original basename, preserving the extension.
			artifactName := filepath.Base(filename)
			if relPath, err := params.ArtifactWriter.WriteArtifact(artifactName, []byte(content)); err == nil {
				artifacts = append(artifacts, engine.Artifact{
					RelativePath: relPath,
					Description:  fmt.Sprintf("Contents of %s", fullPath),
				})
			}
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("collected /etc/scylla.d/* (%d files)", len(filenames)),
		Artifacts: artifacts,
	}, nil
}

// parseLines splits output into non-empty trimmed lines.
func parseLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
