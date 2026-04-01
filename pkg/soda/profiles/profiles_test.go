package profiles

import (
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func TestAllProfiles_ContainsFullProfile(t *testing.T) {
	profiles := AllProfiles()

	full, ok := profiles[FullProfileName]
	if !ok {
		t.Fatalf("missing %q profile", FullProfileName)
	}

	if full.Name != FullProfileName {
		t.Errorf("Name = %q, want %q", full.Name, FullProfileName)
	}
	if full.Description == "" {
		t.Error("Description is empty")
	}
}

func TestAllProfiles_ProfileCount(t *testing.T) {
	profiles := AllProfiles()
	if len(profiles) != 3 {
		t.Errorf("profile count = %d, want 3 (health, logs, full)", len(profiles))
	}
}

// TestAllProfiles_AllProfilesPresent verifies the three named profiles all exist.
func TestAllProfiles_AllProfilesPresent(t *testing.T) {
	profiles := AllProfiles()

	for _, name := range []string{FullProfileName, HealthProfileName, LogsProfileName} {
		p, ok := profiles[name]
		if !ok {
			t.Errorf("missing profile %q", name)
			continue
		}
		if p.Name != name {
			t.Errorf("profile %q: Name = %q, want %q", name, p.Name, name)
		}
		if p.Description == "" {
			t.Errorf("profile %q: Description is empty", name)
		}
	}
}

// TestFullProfile_IncludesSubProfiles verifies that the full profile composes
// health and logs via Includes rather than repeating their collectors/analyzers.
func TestFullProfile_IncludesSubProfiles(t *testing.T) {
	profiles := AllProfiles()
	full := profiles[FullProfileName]

	includesSet := make(map[string]bool, len(full.Includes))
	for _, name := range full.Includes {
		includesSet[name] = true
	}

	for _, sub := range []string{HealthProfileName, LogsProfileName} {
		if !includesSet[sub] {
			t.Errorf("full profile Includes does not contain %q", sub)
		}
	}
}

// TestHealthProfile_ContainsAllAnalyzers verifies the health profile owns all
// current analyzers (which full inherits via Includes).
func TestHealthProfile_ContainsAllAnalyzers(t *testing.T) {
	profiles := AllProfiles()
	health := profiles[HealthProfileName]

	expectedAnalyzers := []engine.AnalyzerID{
		analyzers.ScyllaVersionSupportAnalyzerID,
		analyzers.SchemaAgreementAnalyzerID,
		analyzers.OSSupportAnalyzerID,
		analyzers.GossipHealthAnalyzerID,
		analyzers.TopologyHealthAnalyzerID,
	}

	if len(health.Analyzers) != len(expectedAnalyzers) {
		t.Fatalf("health profile analyzer count = %d, want %d", len(health.Analyzers), len(expectedAnalyzers))
	}

	analyzerSet := make(map[engine.AnalyzerID]bool, len(health.Analyzers))
	for _, id := range health.Analyzers {
		analyzerSet[id] = true
	}
	for _, expected := range expectedAnalyzers {
		if !analyzerSet[expected] {
			t.Errorf("missing analyzer %q in health profile", expected)
		}
	}
}

// TestHealthProfile_AnalyzersMatchRegistry ensures every analyzer ID in the
// health profile is registered in the global analyzer registry.
func TestHealthProfile_AnalyzersMatchRegistry(t *testing.T) {
	profiles := AllProfiles()
	health := profiles[HealthProfileName]
	allAnalyzers := analyzers.AllAnalyzersMap()

	for _, id := range health.Analyzers {
		if _, ok := allAnalyzers[id]; !ok {
			t.Errorf("health profile references analyzer %q not in registry", id)
		}
	}
}

// TestLogsProfile_NoAnalyzers verifies the logs profile intentionally has no analyzers.
func TestLogsProfile_NoAnalyzers(t *testing.T) {
	profiles := AllProfiles()
	logs := profiles[LogsProfileName]

	if len(logs.Analyzers) != 0 {
		t.Errorf("logs profile should have no analyzers, got %d: %v", len(logs.Analyzers), logs.Analyzers)
	}
}

// TestLogsProfile_ContainsLogCollectors verifies the logs profile includes all
// three log collector IDs.
func TestLogsProfile_ContainsLogCollectors(t *testing.T) {
	profiles := AllProfiles()
	logs := profiles[LogsProfileName]

	expectedCollectors := []engine.CollectorID{
		collectors.ScyllaNodeLogsCollectorID,
		collectors.OperatorPodLogsCollectorID,
		collectors.ScyllaClusterJobLogsCollectorID,
	}

	collectorSet := make(map[engine.CollectorID]bool, len(logs.Collectors))
	for _, id := range logs.Collectors {
		collectorSet[id] = true
	}
	for _, expected := range expectedCollectors {
		if !collectorSet[expected] {
			t.Errorf("missing collector %q in logs profile", expected)
		}
	}
}
