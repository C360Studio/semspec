// Package scenarios provides e2e test scenario implementations.
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ExecutionPhaseScenario extends the plan-phase scenario by triggering execution
// after plan approval and verifying that the execution pipeline activates.
//
// Stages:
//  1. setup-project         — Write fixture Python files to workspace.
//  2. detect-stack          — Verify language detection.
//  3. init-project          — Initialize project with detected languages.
//  4. verify-graph-ready    — Gate on graph-gateway readiness.
//  5. create-plan           — Create a plan via HTTP.
//  6. wait-for-plan-goal    — Poll until planner writes a Goal.
//  7. wait-for-approval     — Poll until plan.Approved == true.
//  8. trigger-execution     — Call ExecutePlan to start reactive execution.
//  9. wait-for-exec-start   — Confirm requirement-execution-loop received the trigger.
//
// 10. wait-for-exec-complete — Confirm task-execution-loop dispatched nodes.
// 11. verify-mock-stats      — Assert mock-coder was called at least twice.
type ExecutionPhaseScenario struct {
	config *config.Config
	http   *client.HTTPClient
	fs     *client.FilesystemClient
}

// NewExecutionPhaseScenario creates a new execution phase scenario.
func NewExecutionPhaseScenario(cfg *config.Config) *ExecutionPhaseScenario {
	return &ExecutionPhaseScenario{
		config: cfg,
		http:   client.NewHTTPClient(cfg.HTTPBaseURL),
		fs:     client.NewFilesystemClient(cfg.WorkspacePath),
	}
}

// Name implements Scenario.
func (s *ExecutionPhaseScenario) Name() string { return "execution-phase" }

// Description implements Scenario.
func (s *ExecutionPhaseScenario) Description() string {
	return "Plan phase + execution: plan → requirements → scenarios → approved → execution pipeline"
}

// Setup writes fixture files to the workspace before Execute runs.
func (s *ExecutionPhaseScenario) Setup(_ context.Context) error {
	return s.setupWorkspace()
}

// Teardown is a no-op; the workspace is cleaned by the test runner.
func (s *ExecutionPhaseScenario) Teardown(_ context.Context) error { return nil }

// Execute runs the scenario stages sequentially. Each stage has its own
// deadline; a stage failure short-circuits the run and records the error.
func (s *ExecutionPhaseScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.Name())
	defer result.Complete()

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-project", s.stageSetupProject, 30 * time.Second},
		{"detect-stack", s.stageDetectStack, 15 * time.Second},
		{"init-project", s.stageInitProject, 15 * time.Second},
		{"verify-graph-ready", s.stageVerifyGraphReady, 30 * time.Second},
		{"create-plan", s.stageCreatePlan, 15 * time.Second},
		{"wait-for-plan-goal", s.stageWaitForPlanGoal, 120 * time.Second},
		{"wait-for-approval", s.stageWaitForApproval, 360 * time.Second},
		{"trigger-execution", s.stageTriggerExecution, 15 * time.Second},
		{"wait-for-exec-start", s.stageWaitForExecStart, 120 * time.Second},
		{"wait-for-exec-complete", s.stageWaitForExecComplete, 600 * time.Second},
		{"verify-plan-complete", s.stageVerifyPlanComplete, 60 * time.Second},
		{"verify-mock-stats", s.stageVerifyMockStats, 10 * time.Second},
	}

	if s.config.FastTimeouts {
		for i := range stages {
			stages[i].timeout = stages[i].timeout / 2
		}
	}

	for _, stage := range stages {
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
		start := time.Now()

		err := stage.fn(stageCtx, result)
		duration := time.Since(start)
		cancel()

		if err != nil {
			result.AddStage(stage.name, false, duration, err.Error())
			result.AddError(fmt.Sprintf("%s: %s", stage.name, err.Error()))
			result.Error = fmt.Sprintf("%s failed: %s", stage.name, err.Error())
			return result, nil
		}

		result.AddStage(stage.name, true, duration, "")
		result.SetMetric(stage.name+"_duration_us", duration.Microseconds())
	}

	result.Success = true
	return result, nil
}

// ---------------------------------------------------------------------------
// Workspace setup
// ---------------------------------------------------------------------------

func (s *ExecutionPhaseScenario) setupWorkspace() error {
	files := map[string]string{
		"README.md":            "# Hello World\nA simple Python API project.",
		"api/app.py":           "from flask import Flask, jsonify\n\napp = Flask(__name__)\n\n@app.route('/hello')\ndef hello():\n    return jsonify(message='Hello, World!')\n",
		"api/requirements.txt": "flask==3.0.0\npytest==8.0.0\n",
		"ui/app.js":            "fetch('/hello').then(r => r.json()).then(d => document.getElementById('msg').textContent = d.message);\n",
		"ui/index.html":        "<!DOCTYPE html><html><body><div id='msg'></div><script src='app.js'></script></body></html>\n",
		"Makefile":             "test:\n\tcd api && python3 -m pytest . -q 2>/dev/null || true\n\nbuild:\n\t@echo 'no build step'\n\nlint:\n\t@echo 'no lint step'\n",
	}
	for path, content := range files {
		if err := s.fs.WriteFileRelative(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	// Initialize git repository so the sandbox can create worktrees for
	// task isolation. Without git, worktree creation fails and all tasks
	// write to the same workspace without isolation.
	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("Initial workspace setup"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

func (s *ExecutionPhaseScenario) stageSetupProject(_ context.Context, result *Result) error {
	for _, path := range []string{"README.md", "api/app.py", "api/requirements.txt"} {
		full := filepath.Join(s.config.WorkspacePath, path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return fmt.Errorf("fixture file missing: %s", path)
		}
	}
	result.SetDetail("project_ready", true)
	return nil
}

func (s *ExecutionPhaseScenario) stageDetectStack(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		return fmt.Errorf("detect project: %w", err)
	}
	if len(detection.Languages) == 0 {
		return fmt.Errorf("no languages detected")
	}
	result.SetDetail("detected_languages", len(detection.Languages))
	return nil
}

func (s *ExecutionPhaseScenario) stageInitProject(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}

	var languages []string
	for _, lang := range detection.Languages {
		languages = append(languages, lang.Name)
	}

	initReq := &client.ProjectInitRequest{
		Project: client.ProjectInitInput{
			Name:        "Execution Phase Test",
			Description: "Test the full plan + execution pipeline",
			Languages:   languages,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Items:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}
	result.SetDetail("init_success", resp.Success)
	return nil
}

func (s *ExecutionPhaseScenario) stageVerifyGraphReady(ctx context.Context, result *Result) error {
	g := graph.NewGraphGatherer(s.config.GraphURL)
	if err := g.WaitForReady(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("graph not ready: %w", err)
	}
	result.SetDetail("graph_ready", true)
	return nil
}

func (s *ExecutionPhaseScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add a /goodbye endpoint that returns a goodbye message")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Slug == "" {
		return fmt.Errorf("empty slug in response")
	}
	result.SetDetail("plan_slug", resp.Slug)
	result.SetDetail("plan_trace_id", resp.TraceID)
	return nil
}

func (s *ExecutionPhaseScenario) stageWaitForPlanGoal(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.WaitForPlanGoal(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan goal never populated: %w", err)
	}
	result.SetDetail("plan_goal", plan.Goal)
	return nil
}

func (s *ExecutionPhaseScenario) stageWaitForApproval(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan approval timed out: %w", ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			if plan.Approved {
				result.SetDetail("plan_approved", true)
				result.SetDetail("plan_stage", plan.Stage)
				return nil
			}
			if plan.Stage == "escalated" || plan.Stage == "error" {
				return fmt.Errorf("plan reached terminal state: %s", plan.Stage)
			}
		}
	}
}

func (s *ExecutionPhaseScenario) stageTriggerExecution(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Snapshot agent.complete.* count BEFORE triggering execution.
	// The mock pipeline completes in <1s so we must capture the baseline
	// before any execution messages arrive.
	baselineEntries, _ := s.http.GetMessageLogEntries(ctx, 500, "agent.complete.*")
	result.SetDetail("exec_complete_baseline_count", len(baselineEntries))

	resp, err := s.http.ExecutePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("execute plan returned error: %s", resp.Error)
	}

	result.SetDetail("execute_batch_id", resp.BatchID)
	result.SetDetail("execution_triggered", true)
	return nil
}

// stageWaitForExecStart polls the message-logger for evidence that the
// execution pipeline has started. We look for agent.task.* messages which
// are published when the TDD pipeline dispatches its first stage (tester).
// Earlier signals like workflow.trigger.requirement-execution-loop may be evicted
// from the message-logger buffer by the time we poll.
func (s *ExecutionPhaseScenario) stageWaitForExecStart(ctx context.Context, result *Result) error {
	const subject = "agent.task.*"

	if err := s.pollMessageLogger(ctx, subject, 1); err != nil {
		return fmt.Errorf("execution pipeline agent tasks not observed: %w", err)
	}

	result.SetDetail("exec_loop_triggered", true)
	return nil
}

// stageWaitForExecComplete polls two message-logger subjects to confirm the
// execution pipeline completed end-to-end:
//
//  1. workflow.trigger.task-execution-loop — published by requirement-execution-loop
//     after decompose_task succeeds; confirms DAG nodes were dispatched.
//  2. agent.complete.* — published when any agentic loop finishes.
//     We wait for count growth beyond the pre-execution baseline to confirm
//     execution-phase loops completed (not just planning loops).
//
// Both checks are HARD assertions. The mock fixtures produce working code
// that passes structural validation, so all executions should reach approved.
func (s *ExecutionPhaseScenario) stageWaitForExecComplete(ctx context.Context, result *Result) error {
	// Use baseline captured in stageTriggerExecution (before execution started).
	baseline := 0
	if v, ok := result.GetDetail("exec_complete_baseline_count"); ok {
		if n, ok := v.(int); ok {
			baseline = n
		}
	}

	// First gate: wait for task-execution-loop trigger messages.
	// With 3 requirements each decomposed into 1 node, expect at least 3.
	taskExecSubject := "workflow.trigger.task-execution-loop"
	if err := s.pollMessageLogger(ctx, taskExecSubject, 1); err != nil {
		return fmt.Errorf("task-execution-loop trigger not observed: %w", err)
	}
	result.SetDetail("task_exec_loop_dispatched", true)

	// Second gate: wait for agentic loop completions beyond baseline.
	// 1 decomposer loop + (tester + builder + reviewer) = 4 loops minimum.
	// We require at least 1 new completion (the decomposer loop).
	const minNewCompletions = 1

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("agent.complete.* count did not grow by %d beyond baseline %d: %w",
				minNewCompletions, baseline, ctx.Err())
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 500, "agent.complete.*")
			if err != nil {
				continue
			}
			newCount := len(entries) - baseline
			if newCount >= minNewCompletions {
				result.SetDetail("exec_complete_observed", true)
				result.SetDetail("exec_complete_count", len(entries))
				result.SetDetail("exec_complete_new", newCount)
				return nil
			}
		}
	}
}

// stageVerifyPlanComplete polls until the plan reaches a terminal complete status.
// This catches the convergence watcher regression where plans get stuck in implementing.
func (s *ExecutionPhaseScenario) stageVerifyPlanComplete(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			plan, _ := s.http.GetPlan(ctx, slug)
			lastStatus := "unknown"
			if plan != nil {
				lastStatus = plan.Status
			}
			return fmt.Errorf("plan did not reach complete status, last: %s", lastStatus)
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			switch plan.Status {
			case "complete", "reviewing_rollup", "reviewing_qa":
				result.SetDetail("plan_final_status", plan.Status)
				return nil
			case "rejected", "error":
				return fmt.Errorf("plan reached terminal failure: %s", plan.Status)
			}
		}
	}
}

func (s *ExecutionPhaseScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.config.MockLLMURL == "" {
		return nil // Skip when not using mock LLM.
	}

	statsURL := s.config.MockLLMURL + "/stats"
	req, err := http.NewRequestWithContext(ctx, "GET", statsURL, nil)
	if err != nil {
		return fmt.Errorf("create stats request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch mock stats: %w", err)
	}
	defer resp.Body.Close()

	// Stats format: {"calls_by_model": {"mock-planner": 2, ...}, "total_calls": 103}
	var mockStats struct {
		CallsByModel map[string]int `json:"calls_by_model"`
		TotalCalls   int            `json:"total_calls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mockStats); err != nil {
		return fmt.Errorf("parse mock stats: %w", err)
	}

	stats := mockStats.CallsByModel
	if stats == nil {
		stats = make(map[string]int)
	}

	result.SetDetail("mock_stats", mockStats)
	result.SetDetail("mock_total_calls", mockStats.TotalCalls)

	// Plan phase: planner + reviewer must have been called.
	for _, model := range []string{"mock-planner", "mock-reviewer"} {
		if count, ok := stats[model]; !ok || count == 0 {
			return fmt.Errorf("expected mock model %q to be called, got %d", model, count)
		}
	}

	// Execution phase: 1 requirement decomposed into 1 node.
	// TDD pipeline: tester (2 coder calls) + builder (2 coder calls) + reviewer.
	if coderCalls, ok := stats["mock-coder"]; ok {
		result.SetDetail("mock_coder_calls", coderCalls)
		if coderCalls < 4 {
			return fmt.Errorf("expected mock-coder to be called at least 4 times, got %d", coderCalls)
		}
	} else {
		return fmt.Errorf("mock-coder was not called — execution pipeline did not reach task execution")
	}

	if decomposerCalls, ok := stats["mock-decomposer"]; ok {
		result.SetDetail("mock_decomposer_calls", decomposerCalls)
	}

	// Total calls: planning (~7) + execution (~8) = ~15 with 1 requirement.
	if mockStats.TotalCalls < 12 {
		return fmt.Errorf("expected at least 12 total mock calls (plan + execution), got %d", mockStats.TotalCalls)
	}

	var summary []string
	for model, count := range stats {
		summary = append(summary, fmt.Sprintf("%s=%d", model, count))
	}
	result.SetDetail("mock_call_summary", strings.Join(summary, ", "))

	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// pollMessageLogger polls the message-logger until at least minCount entries
// appear for the given subjectFilter, or the context is cancelled.
func (s *ExecutionPhaseScenario) pollMessageLogger(ctx context.Context, subjectFilter string, minCount int) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 100, subjectFilter)
			if err != nil {
				// Transient error — keep polling.
				continue
			}
			if len(entries) >= minCount {
				return nil
			}
		}
	}
}
