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

// fetchMockStats fetches the mock-llm call counts from its /stats endpoint.
func fetchMockStats(ctx context.Context, mockURL string) (map[string]int, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", mockURL+"/stats", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create stats request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch mock stats: %w", err)
	}
	defer resp.Body.Close()
	var ms struct {
		CallsByModel map[string]int `json:"calls_by_model"`
		TotalCalls   int            `json:"total_calls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, 0, fmt.Errorf("parse mock stats: %w", err)
	}
	if ms.CallsByModel == nil {
		ms.CallsByModel = map[string]int{}
	}
	return ms.CallsByModel, ms.TotalCalls, nil
}

// ParallelAssemblyScenario is the offline regression for the 2026-06-14
// mavlink-hard assembly wedge and the ADR-049 move-3 fix.
//
// It drives a multi-requirement plan whose two components own DISJOINT,
// narrow territories (src/a and src/b), but whose developer creates the shared
// canonical entry file (src/core/driver.py) that NEITHER component declares —
// the exact out-of-territory shape that, on 2026-06-14, every parallel story
// independently created and that only collided at the terminal assembly merge
// (after ~$50 of tokens).
//
// ADR-049 move 3 must catch that out-of-territory creation at the DEV-REVIEW
// NODE: the file-ownership containment gate classifies src/core/driver.py as a
// NewUnownedOutOfTerritory ownership gap, the execution-manager fast-fails it
// straight to recovery (skipping the TDD budget), and the plan never reaches a
// clean complete. This scenario asserts exactly that — the planning-gap
// escalation reason appears in EXECUTION_STATES and the plan does NOT complete.
//
// This is the multi-requirement parallel-assembly coverage that the mock ladder
// lacked (every other executing mock scenario is single-story), and it is the
// free pre-paid regression gate for the wedge that ADR-049 closes.
type ParallelAssemblyScenario struct {
	config *config.Config
	http   *client.HTTPClient
	fs     *client.FilesystemClient
}

// NewParallelAssemblyScenario creates a new parallel-assembly scenario.
func NewParallelAssemblyScenario(cfg *config.Config) *ParallelAssemblyScenario {
	return &ParallelAssemblyScenario{
		config: cfg,
		http:   client.NewHTTPClient(cfg.HTTPBaseURL),
		fs:     client.NewFilesystemClient(cfg.WorkspacePath),
	}
}

// Name implements Scenario.
func (s *ParallelAssemblyScenario) Name() string { return "exec-ownership-gate" }

// Description implements Scenario.
func (s *ParallelAssemblyScenario) Description() string {
	return "ADR-049 move 3: an out-of-territory shared file is caught at the dev-review node, not deferred to assembly"
}

// Setup writes fixture files to the workspace before Execute runs.
func (s *ParallelAssemblyScenario) Setup(_ context.Context) error { return s.setupWorkspace() }

// Teardown is a no-op; the workspace is cleaned by the test runner.
func (s *ParallelAssemblyScenario) Teardown(_ context.Context) error { return nil }

// Execute runs the scenario stages sequentially.
func (s *ParallelAssemblyScenario) Execute(ctx context.Context) (*Result, error) {
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
		{"wait-for-ownership-gap", s.stageWaitForOwnershipGap, 300 * time.Second},
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

func (s *ParallelAssemblyScenario) setupWorkspace() error {
	files := map[string]string{
		"README.md":            "# Parallel Assembly\nA Python service with feature packages.",
		"api/app.py":           "def main():\n    return 'ok'\n",
		"api/requirements.txt": "pytest==8.0.0\n",
		"Makefile":             "test:\n\t@python3 -m pytest -q 2>/dev/null || true\n\nbuild:\n\t@echo 'no build step'\n\nlint:\n\t@echo 'no lint step'\n",
	}
	for path, content := range files {
		if err := s.fs.WriteFileRelative(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
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
// Stages (the plan-prep stages mirror execution-phase)
// ---------------------------------------------------------------------------

func (s *ParallelAssemblyScenario) stageSetupProject(_ context.Context, result *Result) error {
	for _, path := range []string{"README.md", "api/app.py", "api/requirements.txt"} {
		full := filepath.Join(s.config.WorkspacePath, path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return fmt.Errorf("fixture file missing: %s", path)
		}
	}
	result.SetDetail("project_ready", true)
	return nil
}

func (s *ParallelAssemblyScenario) stageDetectStack(ctx context.Context, result *Result) error {
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

func (s *ParallelAssemblyScenario) stageInitProject(ctx context.Context, result *Result) error {
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
			Name:        "Parallel Assembly Test",
			Description: "Test ADR-049 move-3 ownership gap at the dev node",
			Languages:   languages,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{Version: "1.0.0", Items: []any{}},
	}
	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}
	result.SetDetail("init_success", resp.Success)
	return nil
}

func (s *ParallelAssemblyScenario) stageVerifyGraphReady(ctx context.Context, result *Result) error {
	g := graph.NewGraphGatherer(s.config.GraphURL)
	if err := g.WaitForReady(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("graph not ready: %w", err)
	}
	result.SetDetail("graph_ready", true)
	return nil
}

func (s *ParallelAssemblyScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add feature A and feature B, each registered through the shared core driver")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Slug == "" {
		return fmt.Errorf("empty slug in response")
	}
	result.SetDetail("plan_slug", resp.Slug)
	return nil
}

func (s *ParallelAssemblyScenario) stageWaitForPlanGoal(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	plan, err := s.http.WaitForPlanGoal(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan goal never populated: %w", err)
	}
	result.SetDetail("plan_goal", plan.Goal)
	return nil
}

func (s *ParallelAssemblyScenario) stageWaitForApproval(ctx context.Context, result *Result) error {
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
			// The plan MUST pass R2 review to reach execution — a blocking
			// finding here would mean the repaired/new rules wrongly rejected a
			// conformant architecture (disjoint files, scope-aligned).
			if plan.Stage == "escalated" || plan.Stage == "error" || plan.Status == "rejected" {
				return fmt.Errorf("plan reached terminal state before approval: stage=%s status=%s", plan.Stage, plan.Status)
			}
		}
	}
}

func (s *ParallelAssemblyScenario) stageTriggerExecution(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	if plan, err := s.http.GetPlan(ctx, slug); err == nil && executionAlreadyStarted(plan.Status, plan.Stage) {
		result.SetDetail("execution_already_started", true)
		result.SetDetail("execution_stage", plan.Stage)
		return nil
	}

	resp, err := s.http.ExecutePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("execute plan returned error: %s", resp.Error)
	}
	result.SetDetail("execution_triggered", true)
	return nil
}

// stageWaitForOwnershipGap is the assertion of ADR-049 move 3. The developer
// creates src/core/driver.py — outside the story's declared territory (src/a or
// src/b) — so the dev-review ownership gate must hard-fail it as a planning gap
// and fast-fail to recovery. We poll EXECUTION_STATES for the planning-gap
// escalation reason and require that the plan does NOT reach a clean complete.
// A plan that reaches "complete" means the out-of-territory write sailed through
// (the regression this scenario guards against).
func (s *ParallelAssemblyScenario) stageWaitForOwnershipGap(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Substrings that uniquely identify the ADR-049 move-3 escalation reason
	// emitted by execution-manager (handleValidationFailedLocked) — see
	// processor/execution-manager/component.go and ownership_check.go.
	markers := []string{"planning gap (ADR-049 ownership)", "ADR-049 ownership", "declared file scope"}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ownership/planning gap never observed in EXECUTION_STATES: %w", ctx.Err())
		case <-ticker.C:
			// A clean completion is a regression: move 3 should have blocked it.
			plan, err := s.http.GetPlan(ctx, slug)
			if err == nil && plan.Status == "complete" {
				return fmt.Errorf("plan reached 'complete' — the out-of-territory write was NOT caught at the dev node (ADR-049 move-3 regression)")
			}

			kvResp, err := s.http.GetKVEntries(ctx, "EXECUTION_STATES")
			if err != nil {
				continue
			}
			for _, entry := range kvResp.Entries {
				raw := string(entry.Value)
				for _, m := range markers {
					if strings.Contains(raw, m) {
						result.SetDetail("ownership_gap_key", entry.Key)
						result.SetDetail("ownership_gap_marker", m)
						if plan != nil {
							result.SetDetail("plan_status_at_gap", plan.Status)
						}
						return nil
					}
				}
			}
		}
	}
}

func (s *ParallelAssemblyScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.config.MockLLMURL == "" {
		return nil
	}
	stats, total, err := fetchMockStats(ctx, s.config.MockLLMURL)
	if err != nil {
		return err
	}
	result.SetDetail("mock_total_calls", total)

	// The dev loop must have run for the gate to fire — mock-coder must have
	// been called (it wrote the out-of-territory file).
	if c := stats["mock-coder"]; c == 0 {
		return fmt.Errorf("mock-coder was never called — execution did not reach the dev node")
	}
	for _, model := range []string{"mock-planner", "mock-reviewer"} {
		if stats[model] == 0 {
			return fmt.Errorf("expected mock model %q to be called", model)
		}
	}
	var summary []string
	for model, count := range stats {
		summary = append(summary, fmt.Sprintf("%s=%d", model, count))
	}
	result.SetDetail("mock_call_summary", strings.Join(summary, ", "))
	return nil
}
