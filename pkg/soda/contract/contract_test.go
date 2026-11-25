package contract

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestScriptAndParseOutput runs the generated script with the local sh, against files the test creates.
func TestScriptAndParseOutput(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A fake interpreter that reports version 3.7.1 and has no module "absent".
	py := filepath.Join(dir, "py'thon")
	if err := os.WriteFile(py, []byte("#!/bin/sh\ncase \"$2\" in *version_info*) echo 3.7.1;; 'import absent') exit 1;; esac\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := &Contract{
		Interpreter: py,
		Requirements: []Requirement{
			{ID: "tool", Level: LevelRequired, Check: Check{Type: "executable", Path: tool}},
			{ID: "no-tool", Level: LevelRecommended, Check: Check{Type: "executable", Path: filepath.Join(dir, "missing")}},
			{ID: "dir", Level: LevelRequired, Check: Check{Type: "writableDirectory", Path: dir}},
			{ID: "cmds", Level: LevelOptional, Check: Check{Type: "command", Names: []string{"sh", "soda-no-such-command"}}},
			{ID: "pyver", Level: LevelRequired, Check: Check{Type: "pythonVersion", Min: "3.8"}},
			{ID: "pymod", Level: LevelRequired, Check: Check{Type: "pythonModule", Module: "absent"}},
		},
	}

	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(c.Script())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}

	got := map[string]bool{}
	details := map[string]string{}
	for _, r := range c.ParseOutput(string(out)) {
		got[r.ID] = r.Satisfied
		details[r.ID] = r.Detail
	}
	want := map[string]bool{"tool": true, "no-tool": false, "dir": true, "cmds": false, "pyver": false, "pymod": false}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("unexpected results (-want +got):\n%s\noutput:\n%s", diff, out)
	}
	if details["cmds"] != "not found: soda-no-such-command" {
		t.Errorf("unexpected detail for cmds: %q", details["cmds"])
	}
	if details["pyver"] != "3.7.1 is older than 3.8" {
		t.Errorf("unexpected detail for pyver: %q", details["pyver"])
	}
}

func TestParseOutputReportsMissingLines(t *testing.T) {
	c := &Contract{Requirements: []Requirement{{ID: "a", Level: LevelRequired}}}
	res := c.ParseOutput("noise\n")
	if len(res) != 1 || res[0].Satisfied || res[0].Detail != "no result from the check script" {
		t.Errorf("unexpected result: %+v", res)
	}
}
