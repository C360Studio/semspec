package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestSelectQAWorkDir(t *testing.T) {
	const repoPath = "/repo"

	// resolve mirrors Server.worktreeFor: returns the worktree path when it
	// exists, "" otherwise.
	resolvePresent := func(string) string { return "/repo/.semspec/worktrees/qa-auth" }
	resolveMissing := func(string) string { return "" }

	tests := []struct {
		name         string
		workspace    string
		resolve      func(string) string
		wantDir      string
		wantFellBack bool
	}{
		{
			name:         "no_workspace_uses_repo_root",
			workspace:    "",
			resolve:      resolveMissing, // must not even be consulted
			wantDir:      repoPath,
			wantFellBack: false,
		},
		{
			name:         "workspace_present_uses_worktree",
			workspace:    "qa-auth",
			resolve:      resolvePresent,
			wantDir:      "/repo/.semspec/worktrees/qa-auth",
			wantFellBack: false,
		},
		{
			name:         "workspace_missing_falls_back_and_flags",
			workspace:    "qa-auth",
			resolve:      resolveMissing,
			wantDir:      repoPath,
			wantFellBack: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, fellBack := selectQAWorkDir(repoPath, tt.workspace, tt.resolve)
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			if fellBack != tt.wantFellBack {
				t.Errorf("fellBack = %v, want %v", fellBack, tt.wantFellBack)
			}
		})
	}
}

func TestRunSandboxQAIntegrationFailsOnSkippedJUnitTests(t *testing.T) {
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "build", "test-results", "test")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("mkdir results: %v", err)
	}
	xml := `<testsuite tests="1" skipped="1"><testcase classname="DriverIT" name="sitl"><skipped/></testcase></testsuite>`
	if err := os.WriteFile(filepath.Join(resultsDir, "TEST-DriverIT.xml"), []byte(xml), 0o644); err != nil {
		t.Fatalf("write junit xml: %v", err)
	}

	h := &qaHandler{
		srv: &Server{
			repoPath:       dir,
			maxTimeout:     5 * time.Second,
			maxOutputBytes: 64 * 1024,
			worktreeRoot:   filepath.Join(dir, ".semspec", "worktrees"),
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	got := h.runSandboxQA(context.Background(), workflow.QARequestedEvent{
		Slug:        "mavlink-hard",
		PlanID:      "plan-1",
		Mode:        workflow.QALevelIntegration,
		TestCommand: "true",
	}, "run-1", time.Now())

	if got.Passed {
		t.Fatalf("Passed = true, want false for skipped integration tests")
	}
	if got.Level != workflow.QALevelIntegration {
		t.Fatalf("Level = %q, want integration", got.Level)
	}
	if len(got.Failures) != 1 {
		t.Fatalf("Failures = %d, want 1", len(got.Failures))
	}
	if !strings.Contains(got.Failures[0].Message, "skipped") {
		t.Errorf("failure message should mention skipped tests, got %q", got.Failures[0].Message)
	}
}
