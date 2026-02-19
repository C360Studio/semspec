package structuralvalidator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
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

	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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
	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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
	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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
	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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
	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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

	result, err := exec.Execute(ctx, &ValidationTrigger{
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
	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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
	result, err := exec.Execute(context.Background(), &ValidationTrigger{
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
