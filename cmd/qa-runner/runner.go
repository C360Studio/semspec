package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/google/uuid"
)

const (
	// actOutputCapBytes is the combined stdout+stderr cap for act invocations.
	// Larger than sandbox's 100 KiB because integration runs produce noisier output.
	actOutputCapBytes = 1 << 20 // 1 MiB

	// actLogExcerptBytes is the maximum number of bytes kept in QAFailure.LogExcerpt.
	actLogExcerptBytes = 4 * 1024

	// actDefaultTimeout is the fallback integration-run timeout when
	// QARequestedEvent.TimeoutSeconds is zero.
	actDefaultTimeout = 15 * time.Minute

	// defaultWorkflowPath is the workspace-relative path used when
	// QARequestedEvent.WorkflowPath is empty.
	defaultWorkflowPath = ".github/workflows/qa.yml"

	// actRunnerImage is the -P platform override passed to act.
	// Pinned to a stable catthehacker image that closely matches GitHub-hosted runners.
	actRunnerImage = "ubuntu-latest=catthehacker/ubuntu:act-latest"
)

// runQA invokes act against the project's qa workflow and returns a populated
// QACompletedEvent. It never panics — all errors are returned as QACompletedEvent
// fields (RunnerError for infra errors, Failures for test failures).
func (h *qaHandler) runQA(ctx context.Context, evt workflow.QARequestedEvent) *workflow.QACompletedEvent {
	runID := uuid.New().String()
	start := time.Now()

	workspaceHost, runnerErr := h.resolveWorkspace(evt)
	if runnerErr != "" {
		h.logger.Error("runQA: "+runnerErr, "slug", evt.Slug, "run_id", runID)
		return h.errorEvent(evt, runID, start, runnerErr)
	}

	workflowRelPath := evt.WorkflowPath
	if workflowRelPath == "" {
		workflowRelPath = defaultWorkflowPath
	}
	workflowFile := filepath.Join(workspaceHost, workflowRelPath)
	artifactServerPath := filepath.Join("/tmp", "artifacts-"+runID)
	artifactLogRelPath := filepath.Join(".semspec", "qa-artifacts", evt.Slug, runID, "act.log")

	h.logger.Info("Invoking act for integration QA",
		"slug", evt.Slug,
		"run_id", runID,
		"workspace_host", workspaceHost,
		"workflow_file", workflowFile,
		"mode", evt.Mode)

	timeout := h.resolveTimeout(evt)
	result := invokeAct(ctx, workspaceHost, workflowFile, artifactServerPath, timeout)
	if result.runnerErr != "" {
		h.logger.Error("act infra error", "slug", evt.Slug, "run_id", runID, "error", result.runnerErr)
		return h.errorEvent(evt, runID, start, result.runnerErr)
	}

	passed := result.exitCode == 0 && !result.timedOut
	durationMs := time.Since(start).Milliseconds()

	artifacts := h.archiveActLog(workspaceHost, evt.Slug, runID, artifactLogRelPath, result.output)
	artifacts = append(artifacts, h.collectActArtifacts(workspaceHost, evt.Slug, runID, artifactServerPath)...)

	failures := buildFailures(passed, result.output, result.exitCode, result.timedOut, timeout)
	if !passed {
		h.logger.Warn("Integration QA failed",
			"slug", evt.Slug, "run_id", runID,
			"exit_code", result.exitCode, "timed_out", result.timedOut, "duration_ms", durationMs)
	}

	return &workflow.QACompletedEvent{
		Slug:       evt.Slug,
		PlanID:     evt.PlanID,
		RunID:      runID,
		Level:      evt.Mode,
		Passed:     passed,
		Failures:   failures,
		Artifacts:  artifacts,
		DurationMs: durationMs,
		TraceID:    evt.TraceID,
	}
}

// resolveWorkspace returns the host-absolute workspace path from the event or
// the handler fallback. On success the error string is empty; on failure the
// workspace string is empty and the error string is the RunnerError value.
func (h *qaHandler) resolveWorkspace(evt workflow.QARequestedEvent) (string, string) {
	if evt.WorkspaceHostPath != "" {
		return evt.WorkspaceHostPath, ""
	}
	if h.projectHostPath != "" {
		return h.projectHostPath, ""
	}
	return "", "workspace_host_path not resolvable"
}

// resolveTimeout honors an explicit event override when set; otherwise uses
// the handler default. Callers that set TimeoutSeconds know their workload
// better than the generic default — don't silently cap them.
func (h *qaHandler) resolveTimeout(evt workflow.QARequestedEvent) time.Duration {
	if evt.TimeoutSeconds > 0 {
		return time.Duration(evt.TimeoutSeconds) * time.Second
	}
	if h.defaultTimeout > 0 {
		return h.defaultTimeout
	}
	return actDefaultTimeout
}

// errorEvent constructs a terminal QACompletedEvent for an infra error that
// occurred before act could be invoked.
func (h *qaHandler) errorEvent(
	evt workflow.QARequestedEvent,
	runID string,
	start time.Time,
	runnerErr string,
) *workflow.QACompletedEvent {
	return &workflow.QACompletedEvent{
		Slug:        evt.Slug,
		PlanID:      evt.PlanID,
		RunID:       runID,
		Level:       evt.Mode,
		Passed:      false,
		DurationMs:  time.Since(start).Milliseconds(),
		RunnerError: runnerErr,
		TraceID:     evt.TraceID,
	}
}

// actResult carries the outcome of an act invocation.
type actResult struct {
	output    string
	exitCode  int
	timedOut  bool
	runnerErr string // non-empty only for infra errors (e.g. act not in PATH)
}

// invokeAct runs act push against workflowFile inside workspaceHost, capturing
// combined stdout+stderr up to actOutputCapBytes.
//
// The act command:
//
//	act push
//	  -W <workflowFile>            pick the specific workflow file
//	  -P ubuntu-latest=<image>     pin runner image to catthehacker
//	  --bind                       bind-mount workspace (faster for big trees)
//	  --artifact-server-path <dir> collect uploaded artifacts
//	  -v                           verbose — job/step progress visible in log
//	  --rm                         clean up containers after run
func invokeAct(
	ctx context.Context,
	workspaceHost, workflowFile, artifactServerPath string,
	timeout time.Duration,
) actResult {
	if _, err := exec.LookPath("act"); err != nil {
		return actResult{runnerErr: "act binary not found in PATH"}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "act", "push",
		"-W", workflowFile,
		"-P", actRunnerImage,
		"--bind",
		"--artifact-server-path", artifactServerPath,
		"-v",
		"--rm",
	)
	cmd.Dir = workspaceHost

	var combined actCappedWriter
	combined.limit = actOutputCapBytes
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	runErr := cmd.Run()
	timedOut := runCtx.Err() == context.DeadlineExceeded

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if !timedOut {
			exitCode = 1
		}
	}

	return actResult{
		output:   combined.String(),
		exitCode: exitCode,
		timedOut: timedOut,
	}
}

// buildFailures constructs the QAFailure slice for a failed run. Returns nil
// on a passing run. v1 emits a single failure entry — per-job parsing is
// deferred to a future phase.
func buildFailures(
	passed bool,
	output string,
	exitCode int,
	timedOut bool,
	timeout time.Duration,
) []workflow.QAFailure {
	if passed {
		return nil
	}
	excerpt := output
	if len(excerpt) > actLogExcerptBytes {
		excerpt = excerpt[len(excerpt)-actLogExcerptBytes:]
	}
	msg := fmt.Sprintf("act exited with code %d", exitCode)
	if timedOut {
		msg = fmt.Sprintf("act timed out after %s", timeout)
	}
	return []workflow.QAFailure{
		{
			JobName:    "act",
			Message:    msg,
			LogExcerpt: excerpt,
		},
	}
}

// archiveActLog writes the full act output to disk and returns a QAArtifactRef
// slice on success. On write failure the error is logged; an empty slice is
// returned so the caller still publishes QACompletedEvent without artifact refs.
func (h *qaHandler) archiveActLog(
	workspaceHost, slug, runID, relPath, content string,
) []workflow.QAArtifactRef {
	absPath := filepath.Join(workspaceHost, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		h.logger.Warn("Failed to create qa-artifact directory — act log will not be archived",
			"slug", slug, "run_id", runID, "path", absPath, "error", err)
		return nil
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		h.logger.Warn("Failed to write act log — artifact will not be archived",
			"slug", slug, "run_id", runID, "path", absPath, "error", err)
		return nil
	}
	return []workflow.QAArtifactRef{
		{
			Path:    relPath, // workspace-relative — consumers resolve against their own root
			Type:    "log",
			Purpose: "act output",
		},
	}
}

// collectActArtifacts copies files from the act artifact server staging dir
// (tmpDir) into the workspace artifact directory and returns QAArtifactRef
// entries. The tmpDir is removed after copying. Errors are logged and skipped
// so the caller always gets a complete (possibly empty) artifact list.
func (h *qaHandler) collectActArtifacts(
	workspaceHost, slug, runID, tmpDir string,
) []workflow.QAArtifactRef {
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		// act produced no uploaded artifacts — normal for runs without upload-artifact steps.
		return nil
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			h.logger.Warn("Failed to clean up act artifact staging dir",
				"slug", slug, "run_id", runID, "tmp_dir", tmpDir, "error", err)
		}
	}()

	destBase := filepath.Join(workspaceHost, ".semspec", "qa-artifacts", slug, runID)
	if err := os.MkdirAll(destBase, 0o755); err != nil {
		h.logger.Warn("Failed to create artifact destination directory",
			"slug", slug, "run_id", runID, "dest", destBase, "error", err)
		return nil
	}

	var refs []workflow.QAArtifactRef

	walkErr := filepath.WalkDir(tmpDir, func(src string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(tmpDir, src)
		if err != nil {
			return err
		}

		dest := filepath.Join(destBase, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			h.logger.Warn("Failed to create artifact sub-directory",
				"slug", slug, "run_id", runID, "path", dest, "error", err)
			return nil // skip this file, continue walk
		}

		if err := copyFile(src, dest); err != nil {
			h.logger.Warn("Failed to copy act artifact",
				"slug", slug, "run_id", runID, "src", src, "dest", dest, "error", err)
			return nil // skip, continue
		}

		wsRelPath := filepath.Join(".semspec", "qa-artifacts", slug, runID, rel)
		refs = append(refs, workflow.QAArtifactRef{
			Path:    wsRelPath,
			Type:    inferArtifactType(filepath.Ext(rel)),
			Purpose: "act artifact",
		})
		return nil
	})
	if walkErr != nil {
		h.logger.Warn("Error walking act artifact staging dir",
			"slug", slug, "run_id", runID, "tmp_dir", tmpDir, "error", walkErr)
	}

	return refs
}

// copyFile copies src to dst, creating dst with 0644 permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy content: %w", err)
	}
	return nil
}

// inferArtifactType maps a file extension to a QAArtifactRef type value.
// Unknown extensions return "log" as a safe default.
func inferArtifactType(ext string) string {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "screenshot"
	case ".zip", ".tar", ".gz", ".tgz":
		return "trace"
	case ".out", ".cov":
		return "coverage"
	default:
		return "log"
	}
}

// actCappedWriter accumulates written bytes up to limit, silently discarding
// the rest. A truncation notice is appended when the cap is hit.
// It is safe for concurrent writes from act's combined stdout+stderr stream.
type actCappedWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	limit  int
	capped bool
}

func (w *actCappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.capped {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.capLocked()
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = io.Copy(&w.buf, bytes.NewReader(p[:remaining]))
		w.capLocked()
	} else {
		_, _ = w.buf.Write(p)
	}
	return len(p), nil
}

// capLocked appends the truncation marker. Must be called with w.mu held.
func (w *actCappedWriter) capLocked() {
	if !w.capped {
		w.capped = true
		_, _ = w.buf.WriteString("\n[output truncated]")
	}
}

// String returns the buffered content with trailing newlines trimmed.
func (w *actCappedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimRight(w.buf.String(), "\n")
}

// logActVersion logs the installed act version at startup, so the qa-runner
// log carries a clear record of the toolchain version in use.
func logActVersion(logger *slog.Logger) {
	out, err := exec.Command("act", "--version").Output()
	if err != nil {
		logger.Warn("Could not determine act version", "error", err)
		return
	}
	logger.Info("act version", "version", strings.TrimSpace(string(out)))
}
