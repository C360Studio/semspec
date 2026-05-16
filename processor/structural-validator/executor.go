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

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// CommandRunner executes a shell command and returns stdout, stderr, exit code.
// Implementations may run commands locally (os/exec) or remotely (sandbox).
type CommandRunner interface {
	Run(ctx context.Context, command string, workDir string, timeout time.Duration) (stdout, stderr string, exitCode int, err error)
}

// Executor runs checklist checks against a set of modified files.
type Executor struct {
	repoPath       string
	checklistPath  string
	defaultTimeout time.Duration
	sandboxClient  *sandbox.Client // nil = local execution
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
//
// When trigger.WorktreePath is set, checks execute against that directory
// instead of the configured repoPath. The checklist is always loaded from
// repoPath (project-level config), but commands run in the worktree.
//
// When trigger.TaskID is set and a sandbox client is configured, commands
// execute inside the sandbox container rather than locally. This ensures
// agent-generated code never runs outside the sandbox boundary.
func (e *Executor) Execute(ctx context.Context, trigger *payloads.ValidationRequest) (*payloads.ValidationResult, error) {
	checklist, err := e.loadChecklist()
	if err != nil {
		if os.IsNotExist(err) {
			return &payloads.ValidationResult{
				Slug:      trigger.Slug,
				Passed:    true,
				ChecksRun: 0,
				Warning:   "No checklist.json found. Structural validation skipped.",
			}, nil
		}
		return nil, fmt.Errorf("load checklist: %w", err)
	}

	// Determine the working directory for checks. Worktree path overrides
	// the default repoPath so validation runs against agent-modified files.
	workDir := e.repoPath
	if trigger.WorktreePath != "" {
		workDir = trigger.WorktreePath
	}

	// Select command runner: sandbox for agent-generated code, local for manual validation.
	runner, err := e.runnerForTrigger(trigger)
	if err != nil {
		return nil, err
	}

	// When FilesModified is empty, run all checks (full scan mode).
	// This is the default for workflow-triggered validation where the
	// developer agent doesn't report specific files modified.
	runAll := len(trigger.FilesModified) == 0

	var results []payloads.CheckResult
	for _, check := range checklist.Checks {
		if !runAll && !matchesAny(check.Trigger, trigger.FilesModified) {
			continue
		}

		result := e.runCheckIn(ctx, check, workDir, runner)
		results = append(results, result)
	}

	// Fallback: run go test on modified packages when the checklist does not
	// already include a go-test or go-test-modified check and Go files were
	// modified. Also check the checklist itself (not just triggered results)
	// to avoid duplicating a go-test check that exists but didn't match triggers.
	// Only fires in Go projects (go.mod exists) to avoid spurious failures.
	if !hasCheckNamed(results, "go-test") && !hasCheckNamed(results, "go-test-modified") &&
		!checklistHasName(checklist, "go-test") && !checklistHasName(checklist, "go-test-modified") {
		if hasGoFiles(trigger.FilesModified) && e.isGoProjectIn(workDir) {
			goTestResult := e.runGoTestOnModifiedIn(ctx, trigger.FilesModified, workDir, runner)
			results = append(results, goTestResult)
		}
	}

	// Always-on gate: tests-must-exist-for-changed-non-test-Go-files. Runs
	// regardless of checklist contents because `go test ./...` returns exit 0
	// when packages have no test files (Go quirk), so neither the project's
	// own go-test check nor the fallback above will catch a submission that
	// modifies code without adding any test. Caught take 21 (2026-05-08
	// openrouter @easy): llama-3.3-70b shipped main.go with no test file and
	// the code-reviewer approved because `go test ./...` reported "passes
	// all tests" — the auth/ package's pre-existing tests passed and the
	// reviewer didn't notice main.go's package showed `?  [no test files]`.
	if hasGoFiles(trigger.FilesModified) && e.isGoProjectIn(workDir) {
		results = append(results, e.runGoTestsExistOnModifiedIn(ctx, trigger.FilesModified, workDir, runner))
	}

	// Advisory anti-mock governance check — only when test files are present.
	if hasTestFiles(trigger.FilesModified) {
		antiMockResult := CheckAntiMock(workDir, trigger.FilesModified)
		results = append(results, antiMockResult)
	}

	// Advisory Testcontainers discipline check — fires whenever modified
	// files include test files in ANY supported language (not just Go).
	// Verifies that the architect's integration_targets are referenced by
	// the dev's tests (binding import + image coordinate). Pairs with
	// plan-reviewer criterion 7b — architect-side discipline + dev-side
	// enforcement together close the take-19/take-29 stub-JAR pattern.
	// Loads integration_targets from .semspec/plans/<slug>/plan.json on
	// disk; greenfield projects (no architecture) trivially pass.
	if len(filterTestFiles(trigger.FilesModified)) > 0 {
		targets := loadIntegrationTargets(e.repoPath, trigger.Slug)
		tcResult := CheckTestcontainersDiscipline(workDir, trigger.FilesModified, targets)
		results = append(results, tcResult)
	}

	// Deterministic stub-artifact detector — runs whenever .jar files
	// appear in filesModified, regardless of project language. Hard
	// reject (Required: true) on stubs because fabrication is a
	// ship-stopper that neither reviewer prose nor Testcontainers
	// discipline catches reliably. Take-19 deferred item (c).
	if hasJarFiles(trigger.FilesModified) {
		stubResult := CheckStubArtifacts(workDir, trigger.FilesModified)
		results = append(results, stubResult)
	}

	passed := allRequiredPassed(results)

	return &payloads.ValidationResult{
		Slug:         trigger.Slug,
		Passed:       passed,
		ChecksRun:    len(results),
		CheckResults: results,
	}, nil
}

// runnerForTrigger returns a sandbox runner when the sandbox is configured and
// the trigger includes a TaskID. When a TaskID is present but no sandbox is
// configured, it returns an error — agent-generated code must never execute
// outside the sandbox boundary. The local runner is only used for triggers
// without a TaskID (e.g., manual validation of the main workspace).
func (e *Executor) runnerForTrigger(trigger *payloads.ValidationRequest) (CommandRunner, error) {
	if trigger.TaskID != "" {
		if e.sandboxClient == nil {
			return nil, fmt.Errorf("sandbox_url not configured but TaskID %q present — "+
				"refusing to run agent-generated code outside sandbox", trigger.TaskID)
		}
		return &sandboxRunner{client: e.sandboxClient, taskID: trigger.TaskID}, nil
	}
	return &localRunner{}, nil
}

// ---------------------------------------------------------------------------
// CommandRunner implementations
// ---------------------------------------------------------------------------

// localRunner executes commands via os/exec on the local machine.
type localRunner struct{}

func (r *localRunner) Run(ctx context.Context, command, workDir string, timeout time.Duration) (string, string, int, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := splitCommand(command)
	if len(args) == 0 {
		return "", "empty command", -1, nil
	}

	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return stdout.String(), stderr.String(), exitCode, nil
}

// sandboxRunner delegates command execution to the sandbox container.
// Commands run inside the worktree identified by taskID.
type sandboxRunner struct {
	client *sandbox.Client
	taskID string
}

func (r *sandboxRunner) Run(ctx context.Context, command, workDir string, timeout time.Duration) (string, string, int, error) {
	// The sandbox Exec routes to the worktree by taskID and sets cwd to the
	// worktree root. For checks with a WorkingDir override (e.g., "api"),
	// the caller passes the absolute path (baseDir + check.WorkingDir).
	// We need to strip the worktree prefix to get the relative subdir, then
	// prepend a cd so the sandbox runs in that subdirectory.
	//
	// However, runCheckIn already computes workDir as filepath.Join(baseDir, check.WorkingDir).
	// For sandbox execution, we pass the command with a cd prefix when workDir
	// differs from what the sandbox would use as default (worktree root).
	// The sandbox always cds into the worktree root, so any additional WorkingDir
	// is relative to that.
	cmd := command
	if workDir != "" {
		// Wrap in shell to support cd + the original command.
		cmd = fmt.Sprintf("cd %s && %s", workDir, command)
	}

	result, err := r.client.Exec(ctx, r.taskID, cmd, int(timeout.Milliseconds()))
	if err != nil {
		return "", "", -1, err
	}
	return result.Stdout, result.Stderr, result.ExitCode, nil
}

// ---------------------------------------------------------------------------
// Check execution
// ---------------------------------------------------------------------------

// hasCheckNamed returns true if any result in the slice has the given name.
func hasCheckNamed(results []payloads.CheckResult, name string) bool {
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

// hasJarFiles returns true if any file in the list has a .jar extension
// (case-insensitive). Fast-path filter for the stub-artifact detector —
// skips zip-open work when there's no .jar in the modified set.
func hasJarFiles(files []string) bool {
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f), ".jar") {
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

// isGoProjectIn returns true if a go.mod file exists in dir.
func (e *Executor) isGoProjectIn(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
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

// runCheckIn executes a single check command against the given base directory.
func (e *Executor) runCheckIn(ctx context.Context, check workflow.Check, baseDir string, runner CommandRunner) payloads.CheckResult {
	timeout := e.defaultTimeout
	if check.Timeout != "" {
		if d, err := time.ParseDuration(check.Timeout); err == nil {
			timeout = d
		}
	}

	workDir := baseDir
	if check.WorkingDir != "" {
		workDir = filepath.Join(baseDir, check.WorkingDir)
	}

	start := time.Now()

	stdout, stderr, exitCode, err := runner.Run(ctx, check.Command, workDir, timeout)
	duration := time.Since(start)

	if err != nil {
		return payloads.CheckResult{
			Name:     check.Name,
			Passed:   false,
			Required: check.Required,
			Command:  check.Command,
			ExitCode: -1,
			Stderr:   fmt.Sprintf("runner error: %v", err),
			Duration: duration.String(),
		}
	}

	return payloads.CheckResult{
		Name:     check.Name,
		Passed:   exitCode == 0,
		Required: check.Required,
		Command:  check.Command,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
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
func allRequiredPassed(results []payloads.CheckResult) bool {
	for _, r := range results {
		if r.Required && !r.Passed {
			return false
		}
	}
	return true
}

// SummarizeFailures returns the names of required checks that failed and an
// excerpt of the first failure's stderr/stdout. Used by the log site so an
// operator tailing logs sees what failed without spelunking EXECUTION_STATES.
// Returns empty values when nothing failed — callers can pass the results
// straight to a structured logger and an all-pass run prints nothing extra.
//
// The excerpt prefers Stderr (where mvn/go put compile/test errors); falls
// back to Stdout when Stderr is empty (some tools log to stdout). Clipped
// to excerptMax runes with a trailing "…" marker so the log line stays
// bounded — full output remains in EXECUTION_STATES feedback for forensic
// review. Only required failures are summarized; advisory check failures
// don't affect Passed and shouldn't dominate the failure log.
func SummarizeFailures(results []payloads.CheckResult, excerptMax int) (failedNames []string, firstExcerpt string) {
	for _, r := range results {
		if !r.Required || r.Passed {
			continue
		}
		failedNames = append(failedNames, r.Name)
		if firstExcerpt == "" {
			src := strings.TrimSpace(r.Stderr)
			if src == "" {
				src = strings.TrimSpace(r.Stdout)
			}
			firstExcerpt = clipExcerpt(src, excerptMax)
		}
	}
	return failedNames, firstExcerpt
}

// clipExcerpt clips s to maxRunes runes, appending "…" if truncation
// occurred. Operates on runes (not bytes) so multi-byte characters in
// upstream tool output don't get split mid-glyph.
func clipExcerpt(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
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

// runGoTestsExistOnModifiedIn asserts that every package containing a modified
// non-test Go file has at least one `*_test.go` file. Closes the take-21 gap
// where `go test ./...` returns exit 0 when packages have NO test files (a Go
// quirk: "no test files" is not a failure mode), so the structural validator
// would happily approve a submission that ignored the plan's "include unit
// tests" requirement. The check is hardcoded into the validator (alongside
// the go-test fallback) rather than the per-project checklist because every
// Go agent submission should be held to "if you changed code, the package
// has test files" — there's no project-specific reason to opt out.
//
// Skips packages whose only modified file is itself a `_test.go` (editing a
// test, not new code). Pre-existing test files in the package count toward
// satisfaction — this gate enforces presence, not coverage of the new code.
// A coverage gate would be a separate, more complex check.
func (e *Executor) runGoTestsExistOnModifiedIn(ctx context.Context, filesModified []string, baseDir string, runner CommandRunner) payloads.CheckResult {
	pkgs := derivePackagesWithNonTestGoChanges(filesModified)
	if len(pkgs) == 0 {
		return payloads.CheckResult{
			Name:     "go-tests-exist-for-changes",
			Passed:   true,
			Required: true,
			Command:  "find <pkg> -maxdepth 1 -name '*_test.go' -type f (per package)",
			Stdout:   "no non-test Go files modified — nothing to verify",
			Duration: "0s",
		}
	}

	start := time.Now()
	var missing []string
	for _, pkg := range pkgs {
		// `find` with a quoted glob arg — splitCommand preserves the single-quoted
		// token. Returns exit 0 with empty stdout if no matches; non-empty stdout
		// when at least one test file exists.
		cmd := fmt.Sprintf("find %s -maxdepth 1 -name '*_test.go' -type f", pkg)
		stdout, _, exitCode, err := runner.Run(ctx, cmd, baseDir, 5*time.Second)
		if err != nil || exitCode != 0 {
			// Treat runtime errors as missing — better to false-positive than
			// silently let a no-tests submission through.
			missing = append(missing, pkg)
			continue
		}
		if strings.TrimSpace(stdout) == "" {
			missing = append(missing, pkg)
		}
	}
	duration := time.Since(start)

	if len(missing) > 0 {
		return payloads.CheckResult{
			Name:     "go-tests-exist-for-changes",
			Passed:   false,
			Required: true,
			Command:  "find <pkg> -maxdepth 1 -name '*_test.go' -type f (per package)",
			ExitCode: 1,
			Stderr: fmt.Sprintf("packages with non-test .go changes but no *_test.go file present: %s. "+
				"`go test` returns exit 0 when a package has no tests (a Go quirk), so the plan's "+
				"test requirement was silently dropped. Add a *_test.go file in each listed package "+
				"and resubmit.", strings.Join(missing, ", ")),
			Duration: duration.String(),
		}
	}

	return payloads.CheckResult{
		Name:     "go-tests-exist-for-changes",
		Passed:   true,
		Required: true,
		Command:  "find <pkg> -maxdepth 1 -name '*_test.go' -type f (per package)",
		Stdout:   fmt.Sprintf("all %d modified package(s) have at least one *_test.go file", len(pkgs)),
		Duration: duration.String(),
	}
}

// derivePackagesWithNonTestGoChanges returns the deduplicated list of Go
// package paths (relative to repoPath, in "./pkg/path" form) that contain
// at least one modified .go file that is NOT a *_test.go file. Mirrors
// DeriveGoTestPackages but excludes packages whose only Go changes are to
// test files — the tests-exist check has nothing to enforce when the only
// change IS a test file.
func derivePackagesWithNonTestGoChanges(filesModified []string) []string {
	seen := make(map[string]struct{})
	for _, f := range filesModified {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		dir := filepath.Dir(f)
		pkg := "./" + filepath.ToSlash(dir)
		if dir == "." {
			pkg = "./."
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

// runGoTestOnModifiedIn runs `go test` on the packages derived from the modified
// Go files against the given base directory.
func (e *Executor) runGoTestOnModifiedIn(ctx context.Context, filesModified []string, baseDir string, runner CommandRunner) payloads.CheckResult {
	pkgs := DeriveGoTestPackages(filesModified)
	if len(pkgs) == 0 {
		return payloads.CheckResult{
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

	start := time.Now()

	stdout, stderr, exitCode, err := runner.Run(ctx, cmd, baseDir, e.defaultTimeout)
	duration := time.Since(start)

	if err != nil {
		return payloads.CheckResult{
			Name:     "go-test-modified",
			Passed:   false,
			Required: true,
			Command:  cmd,
			ExitCode: -1,
			Stderr:   fmt.Sprintf("runner error: %v", err),
			Duration: duration.String(),
		}
	}

	return payloads.CheckResult{
		Name:     "go-test-modified",
		Passed:   exitCode == 0,
		Required: true,
		Command:  cmd,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
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
