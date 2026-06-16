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

func TestPrepareQAIsolation_ReturnsColdBuildCacheEnvAndCleanup(t *testing.T) {
	env, cleanup, summary, err := prepareQAIsolation("mavlink/hard", "run:1")
	if err != nil {
		t.Fatalf("prepareQAIsolation: %v", err)
	}
	t.Cleanup(cleanup)

	var gradleHome string
	var mavenRepo string
	for _, entry := range env {
		switch {
		case strings.HasPrefix(entry, "GRADLE_USER_HOME="):
			gradleHome = strings.TrimPrefix(entry, "GRADLE_USER_HOME=")
		case strings.HasPrefix(entry, "MAVEN_OPTS=-Dmaven.repo.local="):
			mavenRepo = strings.TrimPrefix(entry, "MAVEN_OPTS=-Dmaven.repo.local=")
		}
	}
	if gradleHome == "" {
		t.Fatalf("GRADLE_USER_HOME env missing: %v", env)
	}
	if mavenRepo == "" {
		t.Fatalf("MAVEN_OPTS maven.repo.local env missing: %v", env)
	}
	if !strings.Contains(summary, "QA cache isolation enabled") {
		t.Fatalf("summary = %q, want cache isolation evidence", summary)
	}

	root := filepath.Dir(gradleHome)
	if err := os.MkdirAll(gradleHome, 0o755); err != nil {
		t.Fatalf("mkdir gradle home: %v", err)
	}
	if err := os.MkdirAll(mavenRepo, 0o755); err != nil {
		t.Fatalf("mkdir maven repo: %v", err)
	}
	cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("cache root still exists after cleanup or unexpected stat error: %v", err)
	}
}

// TestRunSandboxQAIntegrationReportsSkipsAsEvidenceNotFailure asserts the
// 2026-06-16 redesign: a SKIP is no longer an automatic integration failure.
// When the build + every executed test pass, the run is Passed=true and the
// skipped tests are surfaced as SkippedTests evidence for qa-reviewer to reason
// about (legit sandbox limitation vs gaming). The XML below is the real shape
// Gradle's JUnit reporter emits for an assumeTrue-aborted test — a bare
// <skipped/> with NO reason (captured from the mavlink-hard run #6 fixture),
// which is exactly why the reason can't be parsed and the judgment is the
// reviewer's, not a regex's.
func TestRunSandboxQAIntegrationReportsSkipsAsEvidenceNotFailure(t *testing.T) {
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "build", "test-results", "test")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("mkdir results: %v", err)
	}
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="org.sensorhub.impl.sensor.mavsdk.MavsdkSmokeTest" tests="2" skipped="2" failures="0" errors="0">
  <testcase name="test_scenario_1_1_3()" classname="org.sensorhub.impl.sensor.mavsdk.MavsdkSmokeTest"><skipped/></testcase>
  <testcase name="test_scenario_1_1_4()" classname="org.sensorhub.impl.sensor.mavsdk.MavsdkSmokeTest"><skipped/></testcase>
</testsuite>`
	if err := os.WriteFile(filepath.Join(resultsDir, "TEST-MavsdkSmokeTest.xml"), []byte(xml), 0o644); err != nil {
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
		TestCommand: "true", // exit 0: build + executed tests "pass"
	}, "run-1", time.Now())

	if !got.Passed {
		t.Fatalf("Passed = false, want true — a skip is not a failure when the run exits 0")
	}
	if len(got.Failures) != 0 {
		t.Fatalf("Failures = %d, want 0 — skips must not be reported as failures", len(got.Failures))
	}
	if len(got.SkippedTests) != 2 {
		t.Fatalf("SkippedTests = %d, want 2", len(got.SkippedTests))
	}
	for _, s := range got.SkippedTests {
		if s.Suite != "org.sensorhub.impl.sensor.mavsdk.MavsdkSmokeTest" {
			t.Errorf("Suite = %q, want the JUnit classname", s.Suite)
		}
		if s.Name == "" {
			t.Errorf("skipped test missing name: %+v", s)
		}
	}
}

// TestParseSkippedJUnitCases covers the report shapes the parser must handle:
// a single <testsuite> root, a <testsuites> wrapper, and a NESTED <testsuite>
// (aggregate reports) — a skip in any of them must be found, never dropped.
func TestParseSkippedJUnitCases(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want int
	}{
		{
			name: "single testsuite root, bare skipped",
			xml:  `<testsuite><testcase classname="A" name="t1"><skipped/></testcase><testcase classname="A" name="t2"/></testsuite>`,
			want: 1,
		},
		{
			name: "testsuites wrapper",
			xml:  `<testsuites><testsuite><testcase classname="A" name="t1"><skipped/></testcase></testsuite><testsuite><testcase classname="B" name="t2"><skipped/></testcase></testsuite></testsuites>`,
			want: 2,
		},
		{
			name: "nested testsuite",
			xml:  `<testsuites><testsuite><testsuite><testcase classname="C" name="deep"><skipped/></testcase></testsuite></testsuite></testsuites>`,
			want: 1,
		},
		{
			name: "no skips",
			xml:  `<testsuite><testcase classname="A" name="t1"/></testsuite>`,
			want: 0,
		},
		{
			name: "malformed xml yields nothing",
			xml:  `<testsuite><testcase`,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSkippedJUnitCases([]byte(tc.xml), "report.xml", maxSkippedTestsReported)
			if len(got) != tc.want {
				t.Errorf("parseSkippedJUnitCases found %d skipped, want %d (%+v)", len(got), tc.want, got)
			}
		})
	}
}

// TestRunSandboxQAIntegrationFailsOnRealTestFailure asserts the deterministic
// red path is unchanged: a non-zero exit (real test/build failure) still fails
// closed regardless of any skip reasoning.
func TestRunSandboxQAIntegrationFailsOnRealTestFailure(t *testing.T) {
	dir := t.TempDir()
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
		TestCommand: "false", // exit 1
	}, "run-1", time.Now())

	if got.Passed {
		t.Fatalf("Passed = true, want false for a non-zero test command exit")
	}
	if len(got.Failures) != 1 {
		t.Fatalf("Failures = %d, want 1", len(got.Failures))
	}
}
