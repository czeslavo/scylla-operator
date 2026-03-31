package profiles

import (
	"testing"

	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
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

func TestFullProfile_ContainsAllAnalyzers(t *testing.T) {
	profiles := AllProfiles()
	full := profiles[FullProfileName]

	expectedAnalyzers := []engine.AnalyzerID{
		analyzers.ScyllaVersionSupportAnalyzerID,
		analyzers.SchemaAgreementAnalyzerID,
		analyzers.OSSupportAnalyzerID,
	}

	if len(full.Analyzers) != len(expectedAnalyzers) {
		t.Fatalf("analyzer count = %d, want %d", len(full.Analyzers), len(expectedAnalyzers))
	}

	analyzerSet := make(map[engine.AnalyzerID]bool)
	for _, id := range full.Analyzers {
		analyzerSet[id] = true
	}

	for _, expected := range expectedAnalyzers {
		if !analyzerSet[expected] {
			t.Errorf("missing analyzer %q in full profile", expected)
		}
	}
}

func TestFullProfile_AnalyzersMatchRegistry(t *testing.T) {
	profiles := AllProfiles()
	full := profiles[FullProfileName]
	allAnalyzers := analyzers.AllAnalyzersMap()

	for _, id := range full.Analyzers {
		if _, ok := allAnalyzers[id]; !ok {
			t.Errorf("profile references analyzer %q not in registry", id)
		}
	}
}

func TestAllProfiles_ProfileCount(t *testing.T) {
	profiles := AllProfiles()
	if len(profiles) != 1 {
		t.Errorf("profile count = %d, want 1 (full only for PoC)", len(profiles))
	}
}
