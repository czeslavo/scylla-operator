package collectors

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	// OSInfoCollectorID is the unique identifier for the OSInfoCollector.
	OSInfoCollectorID engine.CollectorID = "OSInfoCollector"

	scyllaContainerName = "scylla"
)

// OSInfoResult holds the parsed OS information from a pod.
type OSInfoResult struct {
	Architecture  string            `json:"architecture"`    // e.g. "x86_64"
	KernelVersion string            `json:"kernel_version"`  // e.g. "5.15.0-1041-gke"
	OSName        string            `json:"os_name"`         // e.g. "Red Hat Enterprise Linux"
	OSVersion     string            `json:"os_version"`      // e.g. "9.7"
	OSReleaseFull map[string]string `json:"os_release_full"` // Full parsed /etc/os-release
}

// GetOSInfoResult is the typed accessor for OSInfoCollector results.
func GetOSInfoResult(vitals *engine.Vitals, podKey engine.ScopeKey) (*OSInfoResult, error) {
	return engine.GetResult[OSInfoResult](vitals, OSInfoCollectorID, podKey)
}

// ReadUnameOutput reads the raw uname.log artifact.
func ReadUnameOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(OSInfoCollectorID, podKey, "uname.log")
}

// ReadOSReleaseOutput reads the raw os-release.log artifact.
func ReadOSReleaseOutput(reader engine.ArtifactReader, podKey engine.ScopeKey) ([]byte, error) {
	return reader.ReadArtifact(OSInfoCollectorID, podKey, "os-release.log")
}

// osInfoCollector collects OS information from Scylla pods.
type osInfoCollector struct {
	engine.CollectorBase
}

var _ engine.PerScyllaNodeCollector = (*osInfoCollector)(nil)

// NewOSInfoCollector creates a new OSInfoCollector.
func NewOSInfoCollector() engine.PerScyllaNodeCollector {
	return &osInfoCollector{CollectorBase: engine.NewCollectorBase(OSInfoCollectorID, "OS information", "Reads /etc/os-release inside each Scylla pod to identify the OS distribution and version.", engine.PerScyllaNode, nil)}
}

// RBAC implements engine.RBACProvider.
// Required permissions:
//   - core/v1: pods/exec — create (to run uname --all and cat /etc/os-release)
func (c *osInfoCollector) RBAC() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec"},
			Verbs:     []string{"create"},
		},
	}
}

func (c *osInfoCollector) CollectPerScyllaNode(ctx context.Context, params engine.PerScyllaNodeCollectorParams) (*engine.CollectorResult, error) {
	// Execute uname --all.
	unameOut, err := ExecInScyllaPod(ctx, params, []string{"uname", "--all"})
	if err != nil {
		return nil, fmt.Errorf("executing uname: %w", err)
	}

	// Execute cat /etc/os-release.
	osReleaseOut, err := ExecInScyllaPod(ctx, params, []string{"cat", "/etc/os-release"})
	if err != nil {
		return nil, fmt.Errorf("reading /etc/os-release: %w", err)
	}

	// Parse results.
	result := parseOSInfo(strings.TrimSpace(unameOut), osReleaseOut)

	// Write artifacts.
	var artifacts []engine.Artifact
	writeArtifact(params.ArtifactWriter, "uname.log", []byte(unameOut), "Raw uname --all output", &artifacts)
	writeArtifact(params.ArtifactWriter, "os-release.log", []byte(osReleaseOut), "Raw /etc/os-release content", &artifacts)

	message := fmt.Sprintf("%s %s %s", result.OSName, result.OSVersion, result.Architecture)
	if result.OSName == "" {
		message = fmt.Sprintf("kernel %s %s", result.KernelVersion, result.Architecture)
	}

	return &engine.CollectorResult{
		Status:    engine.CollectorPassed,
		Data:      result,
		Message:   message,
		Artifacts: artifacts,
	}, nil
}

// parseOSInfo extracts OS information from uname and os-release output.
func parseOSInfo(unameLine string, osReleaseContent string) *OSInfoResult {
	result := &OSInfoResult{
		OSReleaseFull: make(map[string]string),
	}

	// Parse uname output: "Linux hostname 5.15.0 #1 SMP x86_64 GNU/Linux"
	// Fields: sysname nodename release version machine processor hardware-platform os
	parts := strings.Fields(unameLine)
	if len(parts) >= 3 {
		result.KernelVersion = parts[2]
	}
	// Machine architecture is typically the second-to-last or a later field.
	// In "uname --all" output, machine is field index 4 (0-indexed) for
	// standard Linux: "Linux host 5.15.0 #1_SMP x86_64 x86_64 x86_64 GNU/Linux"
	// But it varies. Look for a known architecture pattern.
	for _, part := range parts {
		if isArchitecture(part) {
			result.Architecture = part
			break
		}
	}

	// Parse /etc/os-release.
	scanner := bufio.NewScanner(strings.NewReader(osReleaseContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, "\"")
		result.OSReleaseFull[key] = value

		switch key {
		case "NAME":
			result.OSName = value
		case "VERSION_ID":
			result.OSVersion = value
		}
	}

	return result
}

// isArchitecture checks if a string looks like a CPU architecture identifier.
func isArchitecture(s string) bool {
	switch s {
	case "x86_64", "amd64", "aarch64", "arm64", "s390x", "ppc64le", "i686", "i386":
		return true
	}
	return false
}
