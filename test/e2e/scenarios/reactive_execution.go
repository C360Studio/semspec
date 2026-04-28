package scenarios

// ReactiveExecutionScenario tests the full execution lifecycle from explicit
// requirement/scenario CRUD through plan execution.
//
// Scope:
//
//  1. Plan bootstrap — create and approve a plan.
//  2. Requirement creation — link a requirement to the plan.
//  3. Scenario creation — link a BDD scenario to the requirement.
//  4. Execute plan — POST /plans/{slug}/execute.
//  5. Verify plan reaches a terminal status — polls PLAN_STATES via HTTP
//     until status ∈ {complete, reviewing_rollup, reviewing_qa}.
//
// This complements execution-phase (which lets the planner agent generate
// requirements/scenarios) by exercising the explicit CRUD path. Both
// scenarios converge on the same KV-watch completion gate.

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ReactiveExecutionScenario tests the full reactive execution lifecycle.
type ReactiveExecutionScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
}

// NewReactiveExecutionScenario creates a new reactive execution scenario.
func NewReactiveExecutionScenario(cfg *config.Config) *ReactiveExecutionScenario {
	return &ReactiveExecutionScenario{
		name:        "reactive-execution",
		description: "Tests full reactive execution lifecycle: plan → requirement → scenario → decomposition → node dispatch → completion",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ReactiveExecutionScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ReactiveExecutionScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ReactiveExecutionScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	return nil
}

// Execute runs the reactive execution scenario.
func (s *ReactiveExecutionScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		// Plan bootstrap.
		{"stage-create-plan", s.stageCreatePlan},
		{"stage-approve-plan", s.stageApprovePlan},

		// Requirement and scenario setup.
		{"stage-create-requirement", s.stageCreateRequirement},
		{"stage-create-scenario", s.stageCreateScenario},

		// Execution trigger + KV-watch completion check.
		{"stage-execute-plan", s.stageExecutePlan},
		{"stage-verify-plan-execution-complete", s.stageVerifyPlanExecutionComplete},

		// Cleanup.
		{"stage-cleanup", s.stageCleanup},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())

		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}

		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *ReactiveExecutionScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers — carry test-local state via Result.Details
// ---------------------------------------------------------------------------

func (s *ReactiveExecutionScenario) planSlug(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("plan_slug")
}

func (s *ReactiveExecutionScenario) storedRequirementID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("requirement_id")
}

func (s *ReactiveExecutionScenario) storedScenarioID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("scenario_id")
}

// ---------------------------------------------------------------------------
// Plan bootstrap stages
// ---------------------------------------------------------------------------

func (s *ReactiveExecutionScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "reactive execution test")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("create plan returned error: %s", resp.Error)
	}

	slug := resp.Slug
	if slug == "" && resp.Plan != nil {
		slug = resp.Plan.Slug
	}
	if slug == "" {
		return fmt.Errorf("create plan returned empty slug")
	}

	result.SetDetail("plan_slug", slug)

	if _, err := s.http.WaitForPlanCreated(ctx, slug); err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}

	return nil
}

func (s *ReactiveExecutionScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by stage-create-plan")
	}

	resp, err := s.http.PromotePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("promote returned error: %s", resp.Error)
	}

	result.SetDetail("plan_approved", true)
	return nil
}

// ---------------------------------------------------------------------------
// Requirement and scenario stages
// ---------------------------------------------------------------------------

func (s *ReactiveExecutionScenario) stageCreateRequirement(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by stage-create-plan")
	}

	req := &client.CreateRequirementRequest{
		Title:       "Health check endpoint returns status",
		Description: "The /health endpoint must return a 200 OK with a JSON status field",
	}

	requirement, err := s.http.CreateRequirement(ctx, slug, req)
	if err != nil {
		return fmt.Errorf("create requirement: %w", err)
	}
	if requirement.ID == "" {
		return fmt.Errorf("created requirement has empty ID")
	}
	if requirement.Title != req.Title {
		return fmt.Errorf("title mismatch: got %q, want %q", requirement.Title, req.Title)
	}
	if requirement.Status != "active" {
		return fmt.Errorf("expected status=active, got %q", requirement.Status)
	}

	result.SetDetail("requirement_id", requirement.ID)
	return nil
}

func (s *ReactiveExecutionScenario) stageCreateScenario(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by stage-create-plan")
	}
	requirementID, ok := s.storedRequirementID(result)
	if !ok {
		return fmt.Errorf("requirement_id not set by stage-create-requirement")
	}

	req := &client.CreateScenarioRequest{
		RequirementID: requirementID,
		Given:         "the service is running and listening on port 8080",
		When:          "the client sends a GET request to /health",
		Then: []string{
			"the response status code is 200 OK",
			"the response body contains a JSON object with a status field set to \"ok\"",
		},
	}

	scenario, err := s.http.CreateScenario(ctx, slug, req)
	if err != nil {
		return fmt.Errorf("create scenario: %w", err)
	}
	if scenario.ID == "" {
		return fmt.Errorf("created scenario has empty ID")
	}
	if scenario.RequirementID != requirementID {
		return fmt.Errorf("requirement_id mismatch: got %q, want %q", scenario.RequirementID, requirementID)
	}
	if scenario.Status != "pending" {
		return fmt.Errorf("expected status=pending, got %q", scenario.Status)
	}

	result.SetDetail("scenario_id", scenario.ID)
	return nil
}

// ---------------------------------------------------------------------------
// Execution trigger stage
// ---------------------------------------------------------------------------

// stageExecutePlan calls ExecutePlan, which advances the plan status to
// ready_for_execution (in reactive mode) and triggers the scenario-orchestrator
// to publish to workflow.trigger.requirement-execution-loop for each pending scenario.
func (s *ReactiveExecutionScenario) stageExecutePlan(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by stage-create-plan")
	}

	// The KV-driven pipeline may auto-advance the plan to implementing before
	// we call execute. Check current status first.
	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	switch plan.Status {
	case "implementing":
		result.SetDetail("execute_plan_triggered", true)
		result.AddWarning("plan already implementing (auto-triggered via KV pipeline)")
		return nil
	case "ready_for_execution":
		// Expected state — proceed with execute call.
	default:
		// Without LLM, plan may not reach ready_for_execution. Skip gracefully.
		result.SetDetail("execute_plan_triggered", false)
		result.AddWarning(fmt.Sprintf("plan status=%s, not ready_for_execution — skipping execute (no LLM)", plan.Status))
		return nil
	}

	resp, err := s.http.ExecutePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("execute plan returned error: %s", resp.Error)
	}

	result.SetDetail("execute_plan_batch_id", resp.BatchID)
	result.SetDetail("execute_plan_triggered", true)
	return nil
}

// ---------------------------------------------------------------------------
// Execution verification (KV-watch idiomatic)
// ---------------------------------------------------------------------------

// stageVerifyPlanExecutionComplete polls PLAN_STATES via the HTTP API until the
// plan reaches a terminal status. Components self-trigger off PLAN_STATES so
// the plan's status field is the authoritative completion signal. (Predecessor
// stages stageVerifyDecomposition + stageVerifyNodeDispatch + stageVerifyExecutionState
// polled REACTIVE_STATE / agent.task.*, both of which died with the rules-engine
// removal.)
func (s *ReactiveExecutionScenario) stageVerifyPlanExecutionComplete(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set in result")
	}

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

// ---------------------------------------------------------------------------
// Cleanup stage
// ---------------------------------------------------------------------------

func (s *ReactiveExecutionScenario) stageCleanup(_ context.Context, result *Result) error {
	// NATS close is handled by Teardown; nil out to prevent double-close.
	s.nats = nil

	result.SetDetail("cleanup_done", true)
	return nil
}
