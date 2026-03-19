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

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// PlanPhaseScenario tests the full plan phase pipeline:
// create plan → planner → requirements → scenarios → review → approved.
//
// The plan-coordinator handles the entire pipeline internally.
// This scenario triggers via HTTP and polls until completion.
type PlanPhaseScenario struct {
	config *config.Config
	http   *client.HTTPClient
	fs     *client.FilesystemClient
}

// NewPlanPhaseScenario creates a new plan phase scenario.
func NewPlanPhaseScenario(cfg *config.Config) *PlanPhaseScenario {
	return &PlanPhaseScenario{
		config: cfg,
		http:   client.NewHTTPClient(cfg.HTTPBaseURL),
		fs:     client.NewFilesystemClient(cfg.WorkspacePath),
	}
}

func (s *PlanPhaseScenario) Name() string { return "plan-phase" }
func (s *PlanPhaseScenario) Description() string {
	return "Full plan phase: plan → requirements → scenarios → review → approved"
}
func (s *PlanPhaseScenario) Setup(ctx context.Context) error    { return s.setupWorkspace() }
func (s *PlanPhaseScenario) Teardown(ctx context.Context) error { return nil }

// Execute runs the scenario stages sequentially.
func (s *PlanPhaseScenario) Execute(ctx context.Context) (*Result, error) {
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
		{"wait-for-approval", s.stageWaitForApproval, 180 * time.Second},
		{"verify-requirements", s.stageVerifyRequirements, 15 * time.Second},
		{"verify-scenarios", s.stageVerifyScenarios, 15 * time.Second},
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
// Stages
// ---------------------------------------------------------------------------

func (s *PlanPhaseScenario) setupWorkspace() error {
	// Write a minimal Python project for the planner to work with.
	files := map[string]string{
		"README.md":            "# Hello World\nA simple Python API project.",
		"api/app.py":           "from flask import Flask, jsonify\n\napp = Flask(__name__)\n\n@app.route('/hello')\ndef hello():\n    return jsonify(message='Hello, World!')\n",
		"api/requirements.txt": "flask==3.0.0\npytest==8.0.0\n",
		"ui/app.js":            "fetch('/hello').then(r => r.json()).then(d => document.getElementById('msg').textContent = d.message);\n",
		"ui/index.html":        "<!DOCTYPE html><html><body><div id='msg'></div><script src='app.js'></script></body></html>\n",
	}
	for path, content := range files {
		if err := s.fs.WriteFileRelative(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func (s *PlanPhaseScenario) stageSetupProject(ctx context.Context, result *Result) error {
	// Verify workspace has the fixture files.
	for _, path := range []string{"README.md", "api/app.py", "api/requirements.txt"} {
		full := filepath.Join(s.config.WorkspacePath, path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return fmt.Errorf("fixture file missing: %s", path)
		}
	}
	result.SetDetail("project_ready", true)
	return nil
}

func (s *PlanPhaseScenario) stageDetectStack(ctx context.Context, result *Result) error {
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

func (s *PlanPhaseScenario) stageInitProject(ctx context.Context, result *Result) error {
	detectionRaw, _ := result.GetDetail("detected_languages")
	_ = detectionRaw // Detection already verified

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
			Name:        "Plan Phase Test",
			Description: "Test the plan phase pipeline",
			Languages:   languages,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Rules:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}
	result.SetDetail("init_success", resp.Success)
	return nil
}

func (s *PlanPhaseScenario) stageVerifyGraphReady(ctx context.Context, result *Result) error {
	g := gatherers.NewGraphGatherer(s.config.GraphURL)
	if err := g.WaitForReady(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("graph not ready: %w", err)
	}
	result.SetDetail("graph_ready", true)
	return nil
}

func (s *PlanPhaseScenario) stageCreatePlan(ctx context.Context, result *Result) error {
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

func (s *PlanPhaseScenario) stageWaitForPlanGoal(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.WaitForPlanGoal(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan goal never populated: %w", err)
	}

	result.SetDetail("plan_goal", plan.Goal)
	return nil
}

func (s *PlanPhaseScenario) stageWaitForApproval(ctx context.Context, result *Result) error {
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
			// Check for escalation/failure
			if plan.Stage == "escalated" || plan.Stage == "error" {
				return fmt.Errorf("plan reached terminal state: %s", plan.Stage)
			}
		}
	}
}

func (s *PlanPhaseScenario) stageVerifyRequirements(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	requirements, err := s.http.ListRequirements(ctx, slug)
	if err != nil {
		return fmt.Errorf("list requirements: %w", err)
	}
	if len(requirements) == 0 {
		return fmt.Errorf("no requirements generated")
	}

	result.SetDetail("requirement_count", len(requirements))
	return nil
}

func (s *PlanPhaseScenario) stageVerifyScenarios(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// ListScenarios takes a requirementID filter — pass empty for all.
	scenarios, err := s.http.ListScenarios(ctx, slug, "")
	if err != nil {
		return fmt.Errorf("list scenarios: %w", err)
	}
	if len(scenarios) == 0 {
		return fmt.Errorf("no scenarios generated")
	}

	result.SetDetail("scenario_count", len(scenarios))
	return nil
}

func (s *PlanPhaseScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.config.MockLLMURL == "" {
		return nil // Skip if not using mock LLM
	}

	// GET /stats from mock LLM server.
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

	var stats map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("parse mock stats: %w", err)
	}

	result.SetDetail("mock_stats", stats)

	// Verify expected models were called.
	expectedModels := []string{"mock-planner", "mock-reviewer"}
	for _, model := range expectedModels {
		if count, ok := stats[model]; !ok || count == 0 {
			return fmt.Errorf("expected mock model %q to be called, got %d calls", model, count)
		}
	}

	var summary []string
	for model, count := range stats {
		summary = append(summary, fmt.Sprintf("%s=%d", model, count))
	}
	result.SetDetail("mock_call_summary", strings.Join(summary, ", "))

	return nil
}
