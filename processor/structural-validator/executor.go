package structuralvalidator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/reactive"
)

// Executor runs checklist checks against a set of modified files.
type Executor struct {
	repoPath       string
	checklistPath  string
	defaultTimeout time.Duration
}

// NewExecutor creates an Executor rooted at repoPath.
// checklistPath is relative to repoPath; defaultTimeout is used when a
// check does not declare its own Timeout.
func NewExecutor(repoPath, checklistPath string, defaultTimeout time.Duration) *Executor {
	return &Executor{
		repoPath:       repoPath,
		checklistPath:  checklistPath,
		defaultTimeout: defaultTimeout,
	}
}

// Execute runs all triggered checks for the given trigger and returns the result.
// If the checklist file is missing, it returns a passing result with a warning
// rather than an error, to allow graceful degradation in pipelines that have
// not yet been initialised.
func (e *Executor) Execute(ctx context.Context, trigger *reactive.ValidationRequest) (*ValidationResult, error) {
	checklist, err := e.loadChecklist()
	if err != nil {
		if os.IsNotExist(err) {
			return &ValidationResult{
				Slug:      trigger.Slug,
				Passed:    true,
				ChecksRun: 0,
				Warning:   "No checklist.json found. Structural validation skipped.",
			}, nil
		}
		return nil, fmt.Errorf("load checklist: %w", err)
	}

	// When FilesModified is empty, run all checks (full scan mode).
	// This is the default for workflow-triggered validation where the
	// developer agent doesn't report specific files modified.
	runAll := len(trigger.FilesModified) == 0

	var results []CheckResult
	for _, check := range checklist.Checks {
		if !runAll && !matchesAny(check.Trigger, trigger.FilesModified) {
			continue
		}

		result := e.runCheck(ctx, check)
		results = append(results, result)
	}

	// Fallback: run go test on modified packages when the checklist does not
	// already include a go-test or go-test-modified check and Go files were
	// modified. Also check the checklist itself (not just triggered results)
	// to avoid duplicating a go-test check that exists but didn't match triggers.
	// Only fires in Go projects (go.mod exists) to avoid spurious failures.
	if !hasCheckNamed(results, "go-test") && !hasCheckNamed(results, "go-test-modified") &&
		!checklistHasName(checklist, "go-test") && !checklistHasName(checklist, "go-test-modified") {
		if hasGoFiles(trigger.FilesModified) && e.isGoProject() {
			goTestResult := e.runGoTestOnModified(ctx, trigger.FilesModified)
			results = append(results, goTestResult)
		}
	}

	// Advisory anti-mock governance check — only when test files are present.
	if hasTestFiles(trigger.FilesModified) {
		antiMockResult := CheckAntiMock(e.repoPath, trigger.FilesModified)
		results = append(results, antiMockResult)
	}

	passed := allRequiredPassed(results)

	return &ValidationResult{
		Slug:         trigger.Slug,
		Passed:       passed,
		ChecksRun:    len(results),
		CheckResults: results,
	}, nil
}

// hasCheckNamed returns true if any result in the slice has the given name.
func hasCheckNamed(results []CheckResult, name string) bool {
	for _, r := range results {
		if r.Name == name {
			return true
		}
	}
	return false
}

// hasGoFiles returns true if any file in the list ends with ".go".
func hasGoFiles(files []string) bool {
	for _, f := range files {
		if strings.HasSuffix(f, ".go") {
			return true
		}
	}
	return false
}

// hasTestFiles returns true if any file in the list ends with "_test.go".
func hasTestFiles(files []string) bool {
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			return true
		}
	}
	return false
}

// checklistHasName returns true if the checklist contains a check with the given name.
func checklistHasName(cl *workflow.Checklist, name string) bool {
	for _, c := range cl.Checks {
		if c.Name == name {
			return true
		}
	}
	return false
}

// isGoProject returns true if a go.mod file exists in repoPath.
func (e *Executor) isGoProject() bool {
	_, err := os.Stat(filepath.Join(e.repoPath, "go.mod"))
	return err == nil
}

// loadChecklist reads and parses the checklist file.
func (e *Executor) loadChecklist() (*workflow.Checklist, error) {
	path := filepath.Join(e.repoPath, e.checklistPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cl workflow.Checklist
	if err := json.Unmarshal(data, &cl); err != nil {
		return nil, fmt.Errorf("parse checklist JSON: %w", err)
	}
	return &cl, nil
}

// runCheck executes a single check command and captures its output.
func (e *Executor) runCheck(ctx context.Context, check workflow.Check) CheckResult {
	timeout := e.defaultTimeout
	if check.Timeout != "" {
		if d, err := time.ParseDuration(check.Timeout); err == nil {
			timeout = d
		}
	}

	workDir := e.repoPath
	if check.WorkingDir != "" {
		workDir = filepath.Join(e.repoPath, check.WorkingDir)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Split command into argv — support simple shell-style tokenisation without
	// invoking a shell, which avoids shell-injection while handling quoted args.
	args := splitCommand(check.Command)
	if len(args) == 0 {
		return CheckResult{
			Name:     check.Name,
			Passed:   false,
			Required: check.Required,
			Command:  check.Command,
			ExitCode: -1,
			Stderr:   "empty command",
			Duration: time.Since(start).String(),
		}
	}

	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Context deadline exceeded or other OS-level error.
			exitCode = -1
		}
	}

	passed := exitCode == 0

	return CheckResult{
		Name:     check.Name,
		Passed:   passed,
		Required: check.Required,
		Command:  check.Command,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration.String(),
	}
}

// matchesAny returns true if any file in files matches any pattern in patterns.
// Uses filepath.Match for standard glob semantics consistent with the rest of
// the Go standard library.
func matchesAny(patterns []string, files []string) bool {
	for _, pattern := range patterns {
		for _, file := range files {
			// Try both the raw file path and its base name so patterns like
			// "*.go" match files reported as "processor/foo/bar.go".
			if matched, _ := filepath.Match(pattern, file); matched {
				return true
			}
			if matched, _ := filepath.Match(pattern, filepath.Base(file)); matched {
				return true
			}
		}
	}
	return false
}

// allRequiredPassed returns true when every check marked required has passed.
// Optional failing checks do not affect the aggregate result.
func allRequiredPassed(results []CheckResult) bool {
	for _, r := range results {
		if r.Required && !r.Passed {
			return false
		}
	}
	return true
}

// DeriveGoTestPackages returns the deduplicated list of Go package paths
// (relative to repoPath, in "./pkg/path" form) that should be tested given a
// list of modified files. Only files ending in ".go" are considered. Files
// outside the module (i.e. with no directory component) map to ".".
// Returns nil when no Go files are present in filesModified.
func DeriveGoTestPackages(filesModified []string) []string {
	seen := make(map[string]struct{})
	for _, f := range filesModified {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		dir := filepath.Dir(f)
		// filepath.Dir on a bare filename returns ".".
		pkg := "./" + filepath.ToSlash(dir)
		if dir == "." {
			pkg = "."
		}
		seen[pkg] = struct{}{}
	}

	if len(seen) == 0 {
		return nil
	}

	pkgs := make([]string, 0, len(seen))
	for p := range seen {
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// runGoTestOnModified runs `go test` on the packages derived from the modified
// Go files. If there are no Go files in filesModified, it returns a passing
// result with a note. The check is advisory when the checklist already
// provides a go-test check.
func (e *Executor) runGoTestOnModified(ctx context.Context, filesModified []string) CheckResult {
	pkgs := DeriveGoTestPackages(filesModified)
	if len(pkgs) == 0 {
		return CheckResult{
			Name:     "go-test-modified",
			Passed:   true,
			Required: true,
			Command:  "go test (skipped)",
			Stdout:   "no Go files modified",
			Duration: "0s",
		}
	}

	args := append([]string{"test"}, pkgs...)
	cmd := "go " + strings.Join(args, " ")

	cmdCtx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	start := time.Now()

	goCmd := exec.CommandContext(cmdCtx, "go", args...)
	goCmd.Dir = e.repoPath

	var stdout, stderr strings.Builder
	goCmd.Stdout = &stdout
	goCmd.Stderr = &stderr

	runErr := goCmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return CheckResult{
		Name:     "go-test-modified",
		Passed:   exitCode == 0,
		Required: true,
		Command:  cmd,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration.String(),
	}
}

// splitCommand performs minimal whitespace-based tokenisation of a command
// string, preserving single- and double-quoted tokens.
// It is intentionally simple: it does not support escape sequences or nested
// quoting.  For complex commands the caller should wrap the command in a shell
// invocation (e.g. "sh -c '...'").
func splitCommand(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for _, r := range cmd {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == ' ' && !inSingle && !inDouble:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
