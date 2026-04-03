package operator

import (
	"bytes"
	"testing"

	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
)

func testIOStreams() genericclioptions.IOStreams {
	return genericclioptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}
}

func TestServeReportOptions_Validate(t *testing.T) {
	tests := []struct {
		name      string
		outputDir string
		archive   string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "both flags set",
			outputDir: "/some/dir",
			archive:   "/some/archive.tar.gz",
			wantErr:   true,
			errMsg:    "mutually exclusive",
		},
		{
			name:    "archive does not exist",
			archive: "/nonexistent/archive.tar.gz",
			wantErr: true,
			errMsg:  "cannot access archive",
		},
		{
			name:      "output-dir does not exist",
			outputDir: "/nonexistent/dir",
			wantErr:   true,
			errMsg:    "cannot access output directory",
		},
		{
			name:    "neither set (defaults to cwd)",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &ServeReportOptions{
				OutputDir:   tt.outputDir,
				FromArchive: tt.archive,
			}
			err := o.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" {
					if !containsSubstring(err.Error(), tt.errMsg) {
						t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestServeReportOptions_Complete_DefaultsCWD(t *testing.T) {
	o := &ServeReportOptions{}
	if err := o.Complete(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.dataDir == "" {
		t.Error("dataDir should be set to cwd")
	}
}

func TestServeReportOptions_Complete_OutputDir(t *testing.T) {
	o := &ServeReportOptions{OutputDir: "/tmp"}
	if err := o.Complete(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.dataDir != "/tmp" {
		t.Errorf("dataDir = %q, want '/tmp'", o.dataDir)
	}
}

func TestNewServeReportCmd_Exists(t *testing.T) {
	cmd := NewServeReportCmd(testIOStreams())
	if cmd.Use != "serve-report" {
		t.Errorf("Use = %q, want 'serve-report'", cmd.Use)
	}
	// Verify flags exist.
	if cmd.Flags().Lookup("output-dir") == nil {
		t.Error("missing --output-dir flag")
	}
	if cmd.Flags().Lookup("from-archive") == nil {
		t.Error("missing --from-archive flag")
	}
}

func TestNewDiagnoseCmd_HasServeReportSubcommand(t *testing.T) {
	cmd := NewDiagnoseCmd(testIOStreams())
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "serve-report" {
			found = true
			break
		}
	}
	if !found {
		t.Error("diagnose command missing serve-report subcommand")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
