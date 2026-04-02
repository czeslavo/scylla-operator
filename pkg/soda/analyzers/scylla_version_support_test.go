package analyzers

import (
	"strings"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func TestScyllaVersionSupportAnalyzer_Metadata(t *testing.T) {
	a := NewScyllaVersionSupportAnalyzer()
	if a.ID() != ScyllaVersionSupportAnalyzerID {
		t.Errorf("ID = %q, want %q", a.ID(), ScyllaVersionSupportAnalyzerID)
	}
	if a.Name() == "" {
		t.Error("Name() is empty")
	}
	deps := a.DependsOn()
	if len(deps) != 1 || deps[0] != collectors.ScyllaVersionCollectorID {
		t.Errorf("DependsOn = %v, want [%s]", deps, collectors.ScyllaVersionCollectorID)
	}
}

func TestScyllaVersionSupportAnalyzer_SupportedOSS(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   &collectors.ScyllaVersionResult{Version: "6.2.2"},
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
	if !strings.Contains(result.Message, "6.2.2") {
		t.Errorf("message = %q, want to contain '6.2.2'", result.Message)
	}
}

func TestScyllaVersionSupportAnalyzer_SupportedEnterprise(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   &collectors.ScyllaVersionResult{Version: "2025.1.0"},
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}

func TestScyllaVersionSupportAnalyzer_WarningOSS(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   &collectors.ScyllaVersionResult{Version: "5.4.3"},
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "5.4.3") {
		t.Errorf("message = %q, want to contain '5.4.3'", result.Message)
	}
}

func TestScyllaVersionSupportAnalyzer_WarningEnterprise(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   &collectors.ScyllaVersionResult{Version: "2024.2.1"},
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
}

func TestScyllaVersionSupportAnalyzer_UnsupportedOSS(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   &collectors.ScyllaVersionResult{Version: "4.6.3"},
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerFailed {
		t.Errorf("status = %v, want FAILED", result.Status)
	}
	if !strings.Contains(result.Message, "Unsupported") {
		t.Errorf("message = %q, want to contain 'Unsupported'", result.Message)
	}
}

func TestScyllaVersionSupportAnalyzer_MultiplePods_MixedVersions(t *testing.T) {
	vitals := engine.NewVitals()
	// One supported, one unsupported.
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data:   &collectors.ScyllaVersionResult{Version: "6.2.2"},
		})
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data:   &collectors.ScyllaVersionResult{Version: "4.3.0"},
		})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	// Unsupported takes priority.
	if result.Status != engine.AnalyzerFailed {
		t.Errorf("status = %v, want FAILED", result.Status)
	}
}

func TestScyllaVersionSupportAnalyzer_NoPods(t *testing.T) {
	vitals := engine.NewVitals()

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "No Scylla version") {
		t.Errorf("message = %q, want to contain 'No Scylla version'", result.Message)
	}
}

func TestScyllaVersionSupportAnalyzer_SkipsFailedCollector(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status:  engine.CollectorFailed,
		Message: "exec failed",
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	// No versions available → warning.
	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
}

func TestScyllaVersionSupportAnalyzer_EmptyVersion(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data:   &collectors.ScyllaVersionResult{Version: ""},
	})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
}

func TestScyllaVersionSupportAnalyzer_DeduplicatesVersions(t *testing.T) {
	vitals := engine.NewVitals()
	// Two pods with same version.
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data:   &collectors.ScyllaVersionResult{Version: "6.2.2"},
		})
	vitals.Store(collectors.ScyllaVersionCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data:   &collectors.ScyllaVersionResult{Version: "6.2.2"},
		})

	a := NewScyllaVersionSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
	// The message should not duplicate the version.
	count := strings.Count(result.Message, "6.2.2")
	if count != 1 {
		t.Errorf("message = %q, want exactly one '6.2.2'", result.Message)
	}
}

func TestCheckVersionSupport(t *testing.T) {
	tests := []struct {
		version string
		want    versionSupportLevel
	}{
		// OSS supported
		{"6.0.0", versionSupported},
		{"6.2.2", versionSupported},
		{"7.0.0", versionSupported},
		// OSS warning
		{"5.4.0", versionWarning},
		{"5.4.9", versionWarning},
		// OSS unsupported
		{"5.3.0", versionUnsupported},
		{"4.6.3", versionUnsupported},
		{"3.0.0", versionUnsupported},
		// Enterprise supported
		{"2025.1.0", versionSupported},
		{"2026.1.0", versionSupported},
		// Enterprise warning
		{"2024.1.0", versionWarning},
		{"2024.2.1", versionWarning},
		// Unknown format
		{"abc", versionWarning},
		{"", versionWarning},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := checkVersionSupport(tt.version)
			if got != tt.want {
				t.Errorf("checkVersionSupport(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestParseMajorMinor(t *testing.T) {
	tests := []struct {
		version   string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{"6.2.2", 6, 2, true},
		{"5.4.0", 5, 4, true},
		{"2025.1.0", 2025, 1, true},
		{"abc", 0, 0, false},
		{"6", 0, 0, false},
		{"", 0, 0, false},
		{"a.b", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			major, minor, ok := parseMajorMinor(tt.version)
			if ok != tt.wantOK {
				t.Errorf("parseMajorMinor(%q) ok = %v, want %v", tt.version, ok, tt.wantOK)
			}
			if ok && (major != tt.wantMajor || minor != tt.wantMinor) {
				t.Errorf("parseMajorMinor(%q) = (%d, %d), want (%d, %d)", tt.version, major, minor, tt.wantMajor, tt.wantMinor)
			}
		})
	}
}
