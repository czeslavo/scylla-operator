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
	return engine.GetResult[ScyllaDConfigResult](vitals, ScyllaDConfigCollectorID, podKey)
}

// scyllaDConfigCollector collects each file under /etc/scylla.d/ as a separate artifact.
type scyllaDConfigCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*scyllaDConfigCollector)(nil)

// NewScyllaDConfigCollector creates a new ScyllaDConfigCollector.
func NewScyllaDConfigCollector() engine.PerScyllaNodeCollector {
	return &scyllaDConfigCollector{CollectorBase: engine.NewCollectorBase(ScyllaDConfigCollectorID, "Scylla drop-in config directory", engine.PerScyllaNode, nil)}
}

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

func (c *scyllaDConfigCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	// List files in /etc/scylla.d/ (one path per line, no directories).
	lsOut, err := ExecInScyllaPod(ctx, params,
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

		content, err := ExecInScyllaPod(ctx, params, []string{"cat", fullPath})
		if err != nil {
			// Record that we tried but failed rather than aborting the whole collector.
			result.Files[filename] = fmt.Sprintf("(error reading file: %v)", err)
			continue
		}

		result.Files[filename] = content

		// Store each file under its original basename, preserving the extension.
		artifactName := filepath.Base(filename)
		writeArtifact(params.ArtifactWriter, artifactName, []byte(content), fmt.Sprintf("Contents of %s", fullPath), &artifacts)
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
