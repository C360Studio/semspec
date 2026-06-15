package structuralvalidator

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// writeChecklist writes a Checklist as JSON to <dir>/.semspec/checklist.json.
func writeChecklist(t *testing.T, dir string, cl workflow.Checklist) {
	t.Helper()
	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatalf("create .semspec dir: %v", err)
	}
	data, err := json.Marshal(cl)
	if err != nil {
		t.Fatalf("marshal checklist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(semspecDir, "checklist.json"), data, 0644); err != nil {
		t.Fatalf("write checklist.json: %v", err)
	}
}

// newTestExecutor returns an Executor pointing at dir with a 5 s default timeout.
func newTestExecutor(dir string) *Executor {
	return NewExecutor(dir, ".semspec/checklist.json", 5*time.Second)
}

// trueCmd returns a command that always exits 0.
func trueCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 0"
	}
	return "true"
}

// falseCmd returns a command that always exits non-zero.
func falseCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 1"
	}
	return "false"
}

// echoCmd returns a command that echoes its argument to stdout.
func echoCmd(msg string) string {
	if runtime.GOOS == "windows" {
		return "cmd /c echo " + msg
	}
	return "echo " + msg
}

// TestMissingChecklist verifies graceful degradation when checklist.json
// does not exist.
func TestMissingChecklist(t *testing.T) {
	dir := t.TempDir()
	exec := newTestExecutor(dir)

	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "test-slug",
		FilesModified: []string{"main.go"},
	})

	if err != nil {
		t.Fatalf("expected no error for missing checklist, got: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected Passed=true for missing checklist, got false")
	}
	if result.ChecksRun != 0 {
		t.Errorf("expected ChecksRun=0, got %d", result.ChecksRun)
	}
	if result.Warning == "" {
		t.Error("expected a non-empty Warning when checklist is missing")
	}
	if result.Slug != "test-slug" {
		t.Errorf("expected Slug=test-slug, got %q", result.Slug)
	}
}

// TestNoMatchingFiles verifies that when no file in FilesModified matches any
// check trigger pattern, zero checks run and the result passes.
func TestNoMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "go-build",
				Command:  falseCmd(), // would fail if run
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "no-match",
		FilesModified: []string{"README.md", "docs/index.html"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true when no checks run")
	}
	if result.ChecksRun != 0 {
		t.Errorf("expected ChecksRun=0, got %d", result.ChecksRun)
	}
}

// TestSingleMatchingCheck verifies that a matching check runs and its output
// is captured correctly.
func TestSingleMatchingCheck(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "echo-check",
				Command:  echoCmd("hello"),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "single-check",
		FilesModified: []string{"main.go"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected ChecksRun=1, got %d", result.ChecksRun)
	}
	if !result.Passed {
		t.Error("expected Passed=true for a passing check")
	}
	if len(result.CheckResults) != 1 {
		t.Fatalf("expected 1 CheckResult, got %d", len(result.CheckResults))
	}
	cr := result.CheckResults[0]
	if cr.Name != "echo-check" {
		t.Errorf("expected Name=echo-check, got %q", cr.Name)
	}
	if !cr.Passed {
		t.Error("expected individual check to pass")
	}
	if cr.ExitCode != 0 {
		t.Errorf("expected ExitCode=0, got %d", cr.ExitCode)
	}
	if cr.Duration == "" {
		t.Error("expected non-empty Duration")
	}
}

// TestFailedRequiredCheck verifies that a failing required check causes the
// overall result to be Passed=false.
func TestFailedRequiredCheck(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "fail-required",
				Command:  falseCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "fail-required",
		FilesModified: []string{"service.go"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false when a required check fails")
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected ChecksRun=1, got %d", result.ChecksRun)
	}
	if result.CheckResults[0].Passed {
		t.Error("expected individual check result to be Passed=false")
	}
}

// TestFailedOptionalCheck verifies that a failing optional check does not
// affect the aggregate Passed field but the failure is still recorded.
func TestFailedOptionalCheck(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "optional-fail",
				Command:  falseCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryLint,
				Required: false,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "optional-fail",
		FilesModified: []string{"handler.go"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true when only an optional check fails")
	}
	if len(result.CheckResults) != 1 {
		t.Fatalf("expected 1 CheckResult, got %d", len(result.CheckResults))
	}
	if result.CheckResults[0].Passed {
		t.Error("expected optional check itself to be recorded as failed")
	}
}

// TestTimeoutHandling verifies that a check that exceeds its timeout is
// recorded as failed with a non-zero exit code.
func TestTimeoutHandling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep command not available on Windows")
	}

	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "slow-check",
				Command:  "sleep 10",
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryTest,
				Required: true,
				Timeout:  "100ms",
			},
		},
	})

	exec := newTestExecutor(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := exec.Execute(ctx, &payloads.ValidationRequest{
		Slug:          "timeout-test",
		FilesModified: []string{"main.go"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false when a required check times out")
	}
	if len(result.CheckResults) != 1 {
		t.Fatalf("expected 1 CheckResult, got %d", len(result.CheckResults))
	}
	cr := result.CheckResults[0]
	if cr.Passed {
		t.Error("expected timed-out check to be recorded as failed")
	}
	if cr.ExitCode == 0 {
		t.Error("expected non-zero exit code for timed-out check")
	}
}

// TestWorkingDirectoryHandling verifies that checks run in the correct
// working directory when WorkingDir is set.
func TestWorkingDirectoryHandling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pwd command not available on Windows")
	}

	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:       "pwd-check",
				Command:    "pwd",
				Trigger:    []string{"*.go"},
				Category:   workflow.CheckCategoryCompile,
				Required:   true,
				Timeout:    "5s",
				WorkingDir: "sub",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "workdir-test",
		FilesModified: []string{"app.go"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected Passed=true, check stdout: %q", result.CheckResults[0].Stdout)
	}
}

// TestMultipleChecksWithMixedResults verifies that multiple checks run when
// all their patterns match, and that the aggregate result correctly reflects
// whether all required checks pass.
func TestMultipleChecksWithMixedResults(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "required-pass",
				Command:  trueCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
			{
				Name:     "optional-fail",
				Command:  falseCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryLint,
				Required: false,
				Timeout:  "5s",
			},
			{
				Name:     "required-fail",
				Command:  falseCmd(),
				Trigger:  []string{"*.ts"},
				Category: workflow.CheckCategoryTypecheck,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "mixed",
		FilesModified: []string{"main.go"}, // only *.go patterns match
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the two *.go checks run; required-fail targets *.ts and does not run.
	if result.ChecksRun != 2 {
		t.Errorf("expected ChecksRun=2, got %d", result.ChecksRun)
	}
	// required-pass passed, optional-fail failed but is not required → overall pass.
	if !result.Passed {
		t.Errorf("expected Passed=true: required check passed, only optional failed")
	}
}

// TestRunAllChecks_EmptyFilesModified verifies that when FilesModified is
// empty, all checks run regardless of trigger patterns (full scan mode).
func TestRunAllChecks_EmptyFilesModified(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "go-build",
				Command:  trueCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
			{
				Name:     "ts-check",
				Command:  trueCmd(),
				Trigger:  []string{"*.ts"},
				Category: workflow.CheckCategoryTypecheck,
				Required: true,
				Timeout:  "5s",
			},
			{
				Name:     "lint",
				Command:  trueCmd(),
				Trigger:  []string{"*.go", "*.ts"},
				Category: workflow.CheckCategoryLint,
				Required: false,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "run-all",
		FilesModified: []string{}, // empty → run all
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ChecksRun != 3 {
		t.Errorf("expected all 3 checks to run, got %d", result.ChecksRun)
	}
	if !result.Passed {
		t.Error("expected Passed=true when all checks pass")
	}
}

// TestRunAllChecks_NilFilesModified verifies nil FilesModified also triggers
// full scan mode.
func TestRunAllChecks_NilFilesModified(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "go-build",
				Command:  trueCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug: "nil-files",
		// FilesModified not set (nil)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected 1 check to run in full scan mode, got %d", result.ChecksRun)
	}
}

// TestRunAllChecks_WithFilesModified verifies that when FilesModified is
// populated, only checks with matching trigger patterns run.
func TestRunAllChecks_WithFilesModified(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "go-build",
				Command:  trueCmd(),
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
			{
				Name:     "ts-check",
				Command:  falseCmd(), // would fail if run
				Trigger:  []string{"*.ts"},
				Category: workflow.CheckCategoryTypecheck,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "selective",
		FilesModified: []string{"main.go"}, // only go check should run
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected 1 check to run (only go), got %d", result.ChecksRun)
	}
	if !result.Passed {
		t.Error("expected Passed=true — only the passing go check ran")
	}
}

func TestExecute_UsesGitStatusForChecklistTriggers(t *testing.T) {
	if _, err := osexec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "java-build",
				Command:  trueCmd(),
				Trigger:  []string{"*.java"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
		},
	})
	javaPath := filepath.Join(dir, "src", "main", "java", "Driver.java")
	if err := os.MkdirAll(filepath.Dir(javaPath), 0o755); err != nil {
		t.Fatalf("mkdir java dir: %v", err)
	}
	if err := os.WriteFile(javaPath, []byte("class Driver {}\n"), 0o644); err != nil {
		t.Fatalf("write java file: %v", err)
	}

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "git-status-triggers",
		FilesModified: []string{"README.md"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !hasCheckNamed(result.CheckResults, "java-build") {
		t.Fatalf("java-build check did not run from git status changes; results=%+v", result.CheckResults)
	}
}

func TestExecute_FailsIncompleteGradleWrapper(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{Version: "1"})
	if err := os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write gradlew: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gradle", "wrapper"), 0o755); err != nil {
		t.Fatalf("mkdir wrapper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gradle", "wrapper", "gradle-wrapper.properties"), []byte("distributionUrl=https://services.gradle.org/distributions/gradle.zip\n"), 0o644); err != nil {
		t.Fatalf("write wrapper properties: %v", err)
	}

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "gradle-wrapper",
		FilesModified: []string{"build.gradle"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Passed {
		t.Fatalf("Passed = true, want false for incomplete Gradle wrapper")
	}
	found := false
	for _, check := range result.CheckResults {
		if check.Name == "gradle-wrapper-completeness" {
			found = true
			if check.Passed {
				t.Fatalf("gradle-wrapper-completeness passed unexpectedly: %+v", check)
			}
			if !strings.Contains(check.Stderr, "gradle-wrapper.jar") {
				t.Fatalf("failure should name missing jar, got %q", check.Stderr)
			}
		}
	}
	if !found {
		t.Fatalf("gradle-wrapper-completeness check did not run; results=%+v", result.CheckResults)
	}
}

// TestRunAllChecks_EmptyTrigger verifies that a check with no Trigger
// patterns always runs, even when FilesModified is non-empty. Empty trigger
// reads as "always run" — the alternative ("never matches") makes the
// checklist authoring footgun caught in PR #8 (hello-world-py pip-install
// silently skipped). Required-by-design test for the executor contract.
func TestRunAllChecks_EmptyTrigger(t *testing.T) {
	dir := t.TempDir()
	writeChecklist(t, dir, workflow.Checklist{
		Version: "1",
		Checks: []workflow.Check{
			{
				Name:     "always",
				Command:  trueCmd(),
				Trigger:  nil, // empty trigger → always run
				Category: workflow.CheckCategoryLint,
				Required: true,
				Timeout:  "5s",
			},
			{
				Name:     "go-only",
				Command:  falseCmd(), // would fail if run
				Trigger:  []string{"*.go"},
				Category: workflow.CheckCategoryCompile,
				Required: true,
				Timeout:  "5s",
			},
		},
	})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "empty-trigger",
		FilesModified: []string{"README.md"}, // matches no patterns
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected 1 check to run (the always-run one), got %d", result.ChecksRun)
	}
	if !result.Passed {
		t.Error("expected Passed=true — only the passing always-run check ran")
	}
	if len(result.CheckResults) > 0 && result.CheckResults[0].Name != "always" {
		t.Errorf("expected the 'always' check to run, got %q", result.CheckResults[0].Name)
	}
}

// TestSplitCommand exercises the command tokeniser directly.
func TestSplitCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"go build ./...", []string{"go", "build", "./..."}},
		{"echo hello", []string{"echo", "hello"}},
		{"sh -c 'go test ./...'", []string{"sh", "-c", "go test ./..."}},
		{`sh -c "go vet ./..."`, []string{"sh", "-c", "go vet ./..."}},
		{"true", []string{"true"}},
		{"", nil},
	}

	for _, tc := range tests {
		got := splitCommand(tc.input)
		if len(got) != len(tc.expected) {
			t.Errorf("splitCommand(%q): got %v, want %v", tc.input, got, tc.expected)
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("splitCommand(%q)[%d]: got %q, want %q", tc.input, i, got[i], tc.expected[i])
			}
		}
	}
}

// TestMatchesAny exercises the pattern-matching helper.
func TestMatchesAny(t *testing.T) {
	tests := []struct {
		patterns []string
		files    []string
		want     bool
	}{
		{[]string{"*.go"}, []string{"main.go"}, true},
		{[]string{"*.go"}, []string{"processor/foo/bar.go"}, true}, // base name match
		{[]string{"*.go"}, []string{"README.md"}, false},
		{[]string{"*.ts", "*.svelte"}, []string{"App.svelte"}, true},
		{[]string{"*.ts", "*.svelte"}, []string{"main.go"}, false},
		{[]string{}, []string{"main.go"}, false},
		{[]string{"*.go"}, []string{}, false},
	}

	for _, tc := range tests {
		got := matchesAny(tc.patterns, tc.files)
		if got != tc.want {
			t.Errorf("matchesAny(%v, %v) = %v, want %v", tc.patterns, tc.files, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// DeriveGoTestPackages tests
// ---------------------------------------------------------------------------

func TestDeriveGoTestPackages_MixedFileTypes(t *testing.T) {
	pkgs := DeriveGoTestPackages([]string{
		"processor/foo/handler.go",
		"processor/foo/handler_test.go",
		"processor/bar/types.go",
		"ui/src/app.svelte",
		"README.md",
	})

	// Should derive two packages: ./processor/foo and ./processor/bar
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d: %v", len(pkgs), pkgs)
	}
	pkgSet := map[string]bool{}
	for _, p := range pkgs {
		pkgSet[p] = true
	}
	if !pkgSet["./processor/foo"] {
		t.Error("expected ./processor/foo in packages")
	}
	if !pkgSet["./processor/bar"] {
		t.Error("expected ./processor/bar in packages")
	}
}

func TestDeriveGoTestPackages_NoGoFiles(t *testing.T) {
	pkgs := DeriveGoTestPackages([]string{
		"ui/src/app.svelte",
		"README.md",
		"package.json",
	})
	if pkgs != nil {
		t.Errorf("expected nil for no Go files, got %v", pkgs)
	}
}

func TestDeriveGoTestPackages_DuplicatePackages(t *testing.T) {
	pkgs := DeriveGoTestPackages([]string{
		"workflow/reactive/task_execution.go",
		"workflow/reactive/task_execution_test.go",
		"workflow/reactive/dag_execution.go",
	})

	// All three files are in the same package
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 deduplicated package, got %d: %v", len(pkgs), pkgs)
	}
	if pkgs[0] != "./workflow/reactive" {
		t.Errorf("expected ./workflow/reactive, got %s", pkgs[0])
	}
}

func TestDeriveGoTestPackages_RootPackage(t *testing.T) {
	pkgs := DeriveGoTestPackages([]string{"main.go"})
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d: %v", len(pkgs), pkgs)
	}
	if pkgs[0] != "." {
		t.Errorf("expected '.' for root package, got %s", pkgs[0])
	}
}

func TestDeriveGoTestPackages_EmptyList(t *testing.T) {
	pkgs := DeriveGoTestPackages([]string{})
	if pkgs != nil {
		t.Errorf("expected nil for empty list, got %v", pkgs)
	}
}

// TestDerivePackagesWithNonTestGoChanges_FiltersTestFiles guards the helper:
// when the only modified Go files in a package are *_test.go, the package
// must NOT appear in the result (the tests-exist gate has nothing to enforce
// when the developer is editing tests).
func TestDerivePackagesWithNonTestGoChanges_FiltersTestFiles(t *testing.T) {
	pkgs := derivePackagesWithNonTestGoChanges([]string{
		"main.go",                    // → ./.
		"internal/auth/auth.go",      // → ./internal/auth
		"internal/auth/auth_test.go", // skipped (test-only would fall back, but pkg is included via auth.go)
		"internal/util/util_test.go", // skipped — pkg has only test changes
		"README.md",                  // skipped — non-Go
	})
	want := map[string]bool{"./.": true, "./internal/auth": true}
	if len(pkgs) != len(want) {
		t.Fatalf("expected %d packages, got %d: %v", len(want), len(pkgs), pkgs)
	}
	for _, p := range pkgs {
		if !want[p] {
			t.Errorf("unexpected package %q in result", p)
		}
	}
}

// TestRunGoTestsExist_FailsWhenNoTestsForChangedFile pins the take-21 fix:
// when the developer modifies main.go but doesn't ship main_test.go, the
// gate must REJECT, even when `go test ./...` would happily return exit 0
// (Go quirk: "no test files" = exit 0). The check is hardcoded into Run()
// so it cannot be opted out of via checklist contents.
func TestRunGoTestsExist_FailsWhenNoTestsForChangedFile(t *testing.T) {
	dir := t.TempDir()
	// Mark this as a Go project so the gate fires.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// main.go exists; no main_test.go alongside.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	// Empty checklist so only the always-on gate runs (plus the go-test fallback,
	// which would pass — that's the bug shape we're guarding against).
	writeChecklist(t, dir, workflow.Checklist{Version: "1", Checks: nil})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "missing-tests",
		FilesModified: []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gateResult *payloads.CheckResult
	for i := range result.CheckResults {
		if result.CheckResults[i].Name == "go-tests-exist-for-changes" {
			gateResult = &result.CheckResults[i]
		}
	}
	if gateResult == nil {
		t.Fatalf("go-tests-exist-for-changes check did not run; got checks: %v", result.CheckResults)
	}
	if gateResult.Passed {
		t.Errorf("expected gate FAIL (no main_test.go for main.go); got Passed=true. Stderr: %s", gateResult.Stderr)
	}
	if result.Passed {
		t.Errorf("expected overall result Passed=false when required gate fails; got true")
	}
}

// TestRunGoTestsExist_PassesWhenTestFileExists confirms the gate doesn't
// false-positive: when a package has a pre-existing *_test.go, modifying
// the impl file is fine even if no new test file ships in this submission.
// (The gate enforces presence, not coverage — a coverage check is a
// separate, more complex concern.)
func TestRunGoTestsExist_PassesWhenTestFileExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write main_test.go: %v", err)
	}
	writeChecklist(t, dir, workflow.Checklist{Version: "1", Checks: nil})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "tests-present",
		FilesModified: []string{"main.go"}, // only impl file modified; pre-existing test counts
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range result.CheckResults {
		if r.Name == "go-tests-exist-for-changes" && !r.Passed {
			t.Errorf("expected gate PASS (main_test.go exists); got Passed=false. Stderr: %s", r.Stderr)
		}
	}
}

// TestRunGoTestsExist_SkipsWhenOnlyTestFileModified ensures the gate doesn't
// fire when the developer is editing test files only — that's a legitimate
// non-implementation change.
func TestRunGoTestsExist_SkipsWhenOnlyTestFileModified(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	writeChecklist(t, dir, workflow.Checklist{Version: "1", Checks: nil})

	exec := newTestExecutor(dir)
	result, err := exec.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "test-only-edit",
		FilesModified: []string{"main_test.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range result.CheckResults {
		if r.Name == "go-tests-exist-for-changes" {
			if !r.Passed {
				t.Errorf("expected PASS for test-only edit; got Passed=false. Stderr: %s", r.Stderr)
			}
			if !strings.Contains(r.Stdout, "no non-test Go files modified") {
				t.Errorf("expected stdout to indicate skip-with-success; got %q", r.Stdout)
			}
		}
	}
}

// TestSummarizeFailures_ReportsFailedRequiredCheckNames pins the P0 fix
// (gemini @hard 2026-05-10): the structural-validator log was emitting
// `passed=false checks_run=1 warning=""` with no detail on which check
// failed or why. Operators tailing logs had zero diagnostic without
// spelunking EXECUTION_STATES. SummarizeFailures must surface the names
// of failing required checks so the log site can include them.
func TestSummarizeFailures_ReportsFailedRequiredCheckNames(t *testing.T) {
	results := []payloads.CheckResult{
		{Name: "lint", Required: true, Passed: true},
		{Name: "mvn-test", Required: true, Passed: false, Stderr: "BUILD FAILURE: cannot resolve org.foo:bar"},
		{Name: "anti-mock", Required: false, Passed: false, Stdout: "advisory: mock-heavy"},
		{Name: "go-tests-exist", Required: true, Passed: false, Stderr: "missing _test.go in pkg/foo"},
	}

	failed, excerpt := SummarizeFailures(results, 200)

	if len(failed) != 2 {
		t.Fatalf("expected 2 failed required checks, got %d: %v", len(failed), failed)
	}
	if failed[0] != "mvn-test" || failed[1] != "go-tests-exist" {
		t.Errorf("expected [mvn-test go-tests-exist], got %v", failed)
	}
	// First-failure excerpt should come from mvn-test (first failed required),
	// not from the advisory anti-mock check that ran in between.
	if !strings.Contains(excerpt, "BUILD FAILURE") {
		t.Errorf("expected excerpt from first failed required check (mvn-test), got %q", excerpt)
	}
	if strings.Contains(excerpt, "advisory") {
		t.Errorf("excerpt must not include advisory-check output; got %q", excerpt)
	}
}

// TestSummarizeFailures_ReturnsEmptyWhenAllPass confirms the helper stays
// silent on the happy path so an all-pass run doesn't add noise to the
// structured log.
func TestSummarizeFailures_ReturnsEmptyWhenAllPass(t *testing.T) {
	results := []payloads.CheckResult{
		{Name: "lint", Required: true, Passed: true},
		{Name: "mvn-test", Required: true, Passed: true},
	}
	failed, excerpt := SummarizeFailures(results, 200)
	if len(failed) != 0 {
		t.Errorf("expected no failed checks on all-pass, got %v", failed)
	}
	if excerpt != "" {
		t.Errorf("expected empty excerpt on all-pass, got %q", excerpt)
	}
}

// TestSummarizeFailures_FallsBackToStdoutWhenStderrEmpty handles tools
// (e.g., some test runners) that report failures on stdout. Without
// stdout fallback the operator would see the check name but no excerpt
// — half the value of the fix.
func TestSummarizeFailures_FallsBackToStdoutWhenStderrEmpty(t *testing.T) {
	results := []payloads.CheckResult{
		{Name: "go-test", Required: true, Passed: false, Stdout: "FAIL TestFoo (0.01s)", Stderr: ""},
	}
	failed, excerpt := SummarizeFailures(results, 200)
	if len(failed) != 1 || failed[0] != "go-test" {
		t.Fatalf("expected [go-test], got %v", failed)
	}
	if !strings.Contains(excerpt, "FAIL TestFoo") {
		t.Errorf("expected stdout fallback excerpt, got %q", excerpt)
	}
}

// TestSummarizeFailures_ClipsLongOutput keeps log lines bounded. mvn
// build output can run thousands of lines; the log line must stay
// readable while full output remains in EXECUTION_STATES feedback.
func TestSummarizeFailures_ClipsLongOutput(t *testing.T) {
	long := strings.Repeat("A", 1000)
	results := []payloads.CheckResult{
		{Name: "mvn-test", Required: true, Passed: false, Stderr: long},
	}
	_, excerpt := SummarizeFailures(results, 200)
	if !strings.HasSuffix(excerpt, "…") {
		t.Errorf("expected truncation marker, got %q", excerpt)
	}
	// 200 runes + "…" rune = 201 runes total
	if got := len([]rune(excerpt)); got != 201 {
		t.Errorf("expected 201 runes after clip+marker, got %d", got)
	}
}

// TestSummarizeFailures_AdvisoryFailuresIgnored confirms only required
// failures count. Advisory checks (anti-mock, hints) failing must not
// dominate the log when the run actually passed.
func TestSummarizeFailures_AdvisoryFailuresIgnored(t *testing.T) {
	results := []payloads.CheckResult{
		{Name: "lint", Required: true, Passed: true},
		{Name: "anti-mock", Required: false, Passed: false, Stdout: "lots of mocks"},
	}
	failed, excerpt := SummarizeFailures(results, 200)
	if len(failed) != 0 {
		t.Errorf("advisory failures must not be reported as failed_checks; got %v", failed)
	}
	if excerpt != "" {
		t.Errorf("advisory failures must not produce an excerpt; got %q", excerpt)
	}
}
