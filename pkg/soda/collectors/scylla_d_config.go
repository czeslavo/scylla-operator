package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// ScyllaDConfigCollectorID is the unique identifier for the ScyllaDConfigCollector.
	ScyllaDConfigCollectorID engine.CollectorID = "ScyllaDConfigCollector"
)

// ScyllaDConfigResult holds the concatenated contents of all files in /etc/scylla.d/.
type ScyllaDConfigResult struct {
	RawContents string `json:"raw_contents"` // Concatenated output of all /etc/scylla.d/* files
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

// ReadScyllaDConfigOutput reads the raw scylla-d-contents.txt artifact.
func ReadScyllaDConfigOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(ScyllaDConfigCollectorID, podKey, "scylla-d-contents.txt")
}

// scyllaDConfigCollector collects the contents of all files under /etc/scylla.d/.
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
//   - core/v1: pods/exec — create (to read /etc/scylla.d/* files)
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

	stdout, _, err := params.PodExecutor.Execute(ctx, params.Pod.Namespace, params.Pod.Name, scyllaContainerName,
		[]string{"bash", "-c", `for f in /etc/scylla.d/*; do echo "=== $f ==="; cat "$f" 2>/dev/null || echo "(file not readable)"; done`})
	if err != nil {
		return nil, fmt.Errorf("reading /etc/scylla.d/*: %w", err)
	}

	raw := strings.TrimSpace(stdout)

	result := &ScyllaDConfigResult{
		RawContents: raw,
	}

	// Count the number of files found (each starts with "=== /etc/scylla.d/...").
	fileCount := strings.Count(raw, "=== /etc/scylla.d/")

	var artifacts []engine.Artifact
	if params.ArtifactWriter != nil {
		if relPath, err := params.ArtifactWriter.WriteArtifact("scylla-d-contents.txt", []byte(raw)); err == nil {
			artifacts = append(artifacts, engine.Artifact{RelativePath: relPath, Description: "Contents of all files in /etc/scylla.d/"})
		}
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   fmt.Sprintf("collected /etc/scylla.d/* (%d files)", fileCount),
		Artifacts: artifacts,
	}, nil
}
