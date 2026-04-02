package analyzers

import (
	"strings"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func TestOSSupportAnalyzer_Metadata(t *testing.T) {
	a := NewOSSupportAnalyzer()
	if a.ID() != OSSupportAnalyzerID {
		t.Errorf("ID = %q, want %q", a.ID(), OSSupportAnalyzerID)
	}
	if a.Name() == "" {
		t.Error("Name() is empty")
	}
	deps := a.DependsOn()
	if len(deps) != 1 || deps[0] != collectors.OSInfoCollectorID {
		t.Errorf("DependsOn = %v, want [%s]", deps, collectors.OSInfoCollectorID)
	}
}

func TestOSSupportAnalyzer_SupportedUbuntu(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "Ubuntu",
			OSVersion: "22.04",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
	if !strings.Contains(result.Message, "Ubuntu") {
		t.Errorf("message = %q, want to contain 'Ubuntu'", result.Message)
	}
}

func TestOSSupportAnalyzer_SupportedRHEL(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "Red Hat Enterprise Linux",
			OSVersion: "9.7",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}

func TestOSSupportAnalyzer_SupportedDebian(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "Debian GNU/Linux",
			OSVersion: "12",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}

func TestOSSupportAnalyzer_SupportedRockyLinux(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "Rocky Linux",
			OSVersion: "9.3",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}

func TestOSSupportAnalyzer_SupportedAmazonLinux(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "Amazon Linux",
			OSVersion: "2023",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}

func TestOSSupportAnalyzer_UnknownOS(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "Alpine Linux",
			OSVersion: "3.19",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "Alpine Linux") {
		t.Errorf("message = %q, want to contain 'Alpine Linux'", result.Message)
	}
}

func TestOSSupportAnalyzer_EmptyOSName(t *testing.T) {
	vitals := engine.NewVitals()
	podKey := engine.ScopeKey{Namespace: "ns", Name: "pod-0"}
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.OSInfoResult{
			OSName:    "",
			OSVersion: "",
		},
	})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "unknown OS") {
		t.Errorf("message = %q, want to contain 'unknown OS'", result.Message)
	}
}

func TestOSSupportAnalyzer_MultiplePods_AllSupported(t *testing.T) {
	vitals := engine.NewVitals()
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.OSInfoResult{
				OSName:    "Ubuntu",
				OSVersion: "22.04",
			},
		})
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.OSInfoResult{
				OSName:    "Ubuntu",
				OSVersion: "22.04",
			},
		})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
}

func TestOSSupportAnalyzer_MultiplePods_MixedSupport(t *testing.T) {
	vitals := engine.NewVitals()
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.OSInfoResult{
				OSName:    "Ubuntu",
				OSVersion: "22.04",
			},
		})
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-1"}, &engine.CollectorResult{
			Status: engine.CollectorPassed,
			Data: &collectors.OSInfoResult{
				OSName:    "Alpine Linux",
				OSVersion: "3.19",
			},
		})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "Alpine Linux") {
		t.Errorf("message = %q, want to contain 'Alpine Linux'", result.Message)
	}
}

func TestOSSupportAnalyzer_NoPods(t *testing.T) {
	vitals := engine.NewVitals()

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
	if !strings.Contains(result.Message, "No OS information") {
		t.Errorf("message = %q, want to contain 'No OS information'", result.Message)
	}
}

func TestOSSupportAnalyzer_SkipsFailedCollector(t *testing.T) {
	vitals := engine.NewVitals()
	vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode,
		engine.ScopeKey{Namespace: "ns", Name: "pod-0"}, &engine.CollectorResult{
			Status:  engine.CollectorFailed,
			Message: "exec failed",
		})

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerWarning {
		t.Errorf("status = %v, want WARNING", result.Status)
	}
}

func TestIsOSSupported(t *testing.T) {
	tests := []struct {
		osName string
		want   bool
	}{
		{"Ubuntu", true},
		{"ubuntu", true},
		{"UBUNTU", true},
		{"Red Hat Enterprise Linux", true},
		{"Debian GNU/Linux", true},
		{"CentOS Linux", true},
		{"CentOS Stream", true},
		{"Amazon Linux", true},
		{"Rocky Linux", true},
		{"Alpine Linux", false},
		{"Arch Linux", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.osName, func(t *testing.T) {
			got := isOSSupported(tt.osName)
			if got != tt.want {
				t.Errorf("isOSSupported(%q) = %v, want %v", tt.osName, got, tt.want)
			}
		})
	}
}

func TestOSSupportAnalyzer_DeduplicatesOS(t *testing.T) {
	vitals := engine.NewVitals()
	// Three pods all running Ubuntu 22.04.
	for _, name := range []string{"pod-0", "pod-1", "pod-2"} {
		vitals.Store(collectors.OSInfoCollectorID, engine.PerScyllaNode,
			engine.ScopeKey{Namespace: "ns", Name: name}, &engine.CollectorResult{
				Status: engine.CollectorPassed,
				Data: &collectors.OSInfoResult{
					OSName:    "Ubuntu",
					OSVersion: "22.04",
				},
			})
	}

	a := NewOSSupportAnalyzer()
	result := a.AnalyzePerScyllaCluster(engine.PerScyllaClusterAnalyzerParams{Vitals: vitals})

	if result.Status != engine.AnalyzerPassed {
		t.Errorf("status = %v, want PASSED", result.Status)
	}
	// Should not duplicate "Ubuntu 22.04" in the message.
	count := strings.Count(result.Message, "Ubuntu 22.04")
	if count != 1 {
		t.Errorf("message = %q, want exactly one 'Ubuntu 22.04'", result.Message)
	}
}
