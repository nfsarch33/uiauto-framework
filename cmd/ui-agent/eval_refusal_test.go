package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalCmdRefusesFusionWithoutOmniParser: a suite naming the fusion
// executor with an empty --omniparser must refuse with a usage error
// BEFORE the run. Deleting the guard in eval.go is the mutant this
// kills: without it the run executes and exits 0, not 1 with the
// message.
func TestEvalCmdRefusesFusionWithoutOmniParser(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	suiteBody := "name: fusion-misconfig\ndefaults: {repeats: 1, timeout_s: 30}\nscenarios:\n  - id: s1\n    executor: browser-use-fusion\n    task: t\n    url: http://fixtures.test/\n"
	if err := os.WriteFile(suite, []byte(suiteBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := evalCmd()
	cmd.SetArgs([]string{"--suite", suite})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("err = nil, want the refusal")
	}
	if !strings.Contains(err.Error(), "--omniparser is empty") {
		t.Fatalf("err = %v, want the usage message naming --omniparser", err)
	}
}
