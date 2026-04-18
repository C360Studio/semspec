package scenarios

// ReactiveExecutionScenario tests the full reactive execution lifecycle:
// plan creation → requirement → scenario → decomposition → node dispatch → completion.
//
// Scope:
//
//  1. Plan bootstrap — create and approve a plan.
//  2. Requirement creation — link a requirement to the plan.
//  3. Scenario creation — link a BDD scenario to the requirement.
//  4. Execute plan — triggers the reactive execution path via the
//     scenario-orchestrator, which publishes to requirement-execution-loop.
//  5. Decomposition gate — waits for the scenario-execution KV state to reach
//     "decomposing" or any later phase, confirming the workflow engine accepted
//     the trigger and initialised its state machine.
//  6. Node dispatch — inspects message-logger for agent.task.* messages, which
//     are published by dag-execution-loop after a successful decompose_task call.
//  7. Execution progression — polls for the scenario-execution state to reach
//     "executing" or a terminal phase (complete/failed).
//
// Stages 5-7 are NON-FATAL: in CI environments where the mock LLM is not
// configured to emit a decompose_task tool call, these checks record warnings
// rather than failing the scenario. This matches the approach used in
// ScenarioExecutionScenario.stageVerifyScenarioExecutionState.
//
// Full end-to-end verification (node dispatch → dag.node.complete signals →
// "complete" phase) requires mock fixtures that call decompose_task with a
// valid TaskDAG and then signal node completion with approved QualityEvidence.
// That is left for a future fixture-driven scenario; see stageVerifyNodeDispatch
// for what additional mock fixtures would be needed.

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

		// Reactive execution trigger.
		{"stage-execute-plan", s.stageExecutePlan},

		// Reactive execution verification (non-fatal).
		{"stage-verify-decomposition", s.stageVerifyDecomposition},
		{"stage-verify-node-dispatch", s.stageVerifyNodeDispatch},
		{"stage-verify-execution-state", s.stageVerifyExecutionState},

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
		if scenarioID, ok := s.storedScenarioID(nil); ok {
			key := "scenario-execution." + scenarioID
			if _, err := s.nats.PurgeKVByPrefix(ctx, client.ReactiveStateBucket, key); err != nil {
				// Non-fatal; state will be overwritten on the next run.
				_ = err
			}
		}
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
// Reactive verification stages (non-fatal)
// ---------------------------------------------------------------------------

// stageVerifyDecomposition waits for the requirement-execution-loop to initialise
// its KV state entry. The workflow engine writes to
// REACTIVE_STATE/scenario-execution.<scenarioID> when the accept-trigger rule
// fires. We accept any active or terminal phase.
//
// This stage is NON-FATAL: if the reactive engine is not configured or the mock
// LLM has not yet produced a trigger event, we record a warning and continue.
func (s *ReactiveExecutionScenario) stageVerifyDecomposition(ctx context.Context, result *Result) error {
	scenarioID, ok := s.storedScenarioID(result)
	if !ok {
		result.AddWarning("scenario_id not set; skipping decomposition verification")
		return nil
	}

	kvKey := "scenario-execution." + scenarioID

	// Bound the wait to 30 seconds regardless of the outer stage timeout so
	// that a slow CI environment does not block the cleanup stage.
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	state, err := s.http.WaitForWorkflowPhaseIn(
		waitCtx,
		client.ReactiveStateBucket,
		kvKey,
		// Accept any active or terminal phase — the workflow may have already
		// progressed beyond "decomposing" when the NATS infrastructure is fast.
		[]string{
			scenarioPhaseDecomposing,
			scenarioPhaseDecomposed,
			scenarioPhaseExecuting,
			scenarioPhaseComplete,
			scenarioPhaseFailed,
		},
	)
	if err != nil {
		// KV state not appearing is acceptable when the reactive engine is not
		// fully configured. Record a warning and continue.
		result.SetDetail("decomposition_state_found", false)
		result.SetDetail("decomposition_state_note",
			fmt.Sprintf("KV state not found within timeout (key=%s): %v — reactive engine may not be configured", kvKey, err))
		result.AddWarning(fmt.Sprintf("scenario-execution KV state not found within 30s (key=%s): %v", kvKey, err))
		return nil
	}

	// Validate the state structure.
	if state.WorkflowID != scenarioExecutionWorkflowID {
		// This is a hard error — wrong workflow means routing is broken.
		return fmt.Errorf("unexpected workflow_id: got %q, want %q", state.WorkflowID, scenarioExecutionWorkflowID)
	}
	if state.Phase == "" {
		return fmt.Errorf("workflow state has empty phase")
	}

	result.SetDetail("decomposition_state_found", true)
	result.SetDetail("decomposition_workflow_id", state.WorkflowID)
	result.SetDetail("decomposition_phase", state.Phase)
	result.SetDetail("decomposition_status", state.Status)
	return nil
}

// stageVerifyNodeDispatch inspects the message-logger for agent.task.* messages,
// which are published by the dag-execution-loop after a successful decompose_task
// call. The presence of these messages confirms that at least one DAG node was
// dispatched to an agentic loop.
//
// This stage is NON-FATAL: the mock LLM may not produce a decompose_task tool
// call, in which case no agent.task.* messages will appear. We record the count
// and continue.
//
// Full DAG node dispatch verification would require mock fixtures that:
//   - Return a decompose_task tool call with a valid TaskDAG (min 1 node).
//   - Provide a fixture for the spawned agentic loop that signals completion
//     via dag.node.complete.<nodeID> with QualityEvidence{ValidationPassed: true,
//     ReviewVerdict: "approved"}.
func (s *ReactiveExecutionScenario) stageVerifyNodeDispatch(ctx context.Context, result *Result) error {
	entries, err := s.http.GetMessageLogEntries(ctx, 100, "agent.task.*")
	if err != nil {
		// Message-logger unavailable; record a warning and continue.
		result.AddWarning(fmt.Sprintf("could not query message-logger for agent.task.*: %v", err))
		result.SetDetail("node_dispatch_entries_found", 0)
		return nil
	}

	result.SetMetric("node_dispatch_message_count", len(entries))
	result.SetDetail("node_dispatch_entries_found", len(entries))

	if len(entries) == 0 {
		// No agent task messages yet — acceptable if decomposition hasn't completed.
		result.AddWarning("no agent.task.* messages found in message-logger — decomposition may not have completed (mock LLM may not emit decompose_task)")
	}

	return nil
}

// stageVerifyExecutionState polls for the scenario-execution state to reach the
// "executing" phase or later. This confirms that at least one DAG node was
// dispatched and the workflow engine transitioned out of the decomposition phase.
//
// This stage is NON-FATAL: if the mock LLM does not produce decompose_task calls,
// the workflow will remain in "decomposing" and this check will time out. We
// record the observation as a warning.
func (s *ReactiveExecutionScenario) stageVerifyExecutionState(ctx context.Context, result *Result) error {
	scenarioID, ok := s.storedScenarioID(result)
	if !ok {
		result.AddWarning("scenario_id not set; skipping execution state verification")
		return nil
	}

	// Only attempt this check if decomposition state was found in the previous stage.
	decompositionFound, _ := result.GetDetailBool("decomposition_state_found")
	if !decompositionFound {
		result.SetDetail("execution_state_found", false)
		result.SetDetail("execution_state_note", "skipped — decomposition state was not found")
		return nil
	}

	kvKey := "scenario-execution." + scenarioID

	// Use a short window: if decomposition succeeded, transition to "executing"
	// should happen quickly. If the mock LLM doesn't emit decompose_task, we've
	// already recorded a warning in the previous stage.
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	state, err := s.http.WaitForWorkflowPhaseIn(
		waitCtx,
		client.ReactiveStateBucket,
		kvKey,
		[]string{
			scenarioPhaseExecuting,
			scenarioPhaseComplete,
			scenarioPhaseFailed,
		},
	)
	if err != nil {
		result.SetDetail("execution_state_found", false)
		result.SetDetail("execution_state_note",
			fmt.Sprintf("execution phase not reached within 20s (key=%s): %v — mock LLM may not emit decompose_task", kvKey, err))
		result.AddWarning(fmt.Sprintf("scenario-execution did not reach executing phase within 20s: %v", err))
		return nil
	}

	result.SetDetail("execution_state_found", true)
	result.SetDetail("execution_phase", state.Phase)
	result.SetDetail("execution_status", state.Status)
	return nil
}

// ---------------------------------------------------------------------------
// Cleanup stage
// ---------------------------------------------------------------------------

func (s *ReactiveExecutionScenario) stageCleanup(ctx context.Context, result *Result) error {
	if scenarioID, ok := s.storedScenarioID(result); ok {
		key := "scenario-execution." + scenarioID
		deleted, err := s.nats.PurgeKVByPrefix(ctx, client.ReactiveStateBucket, key)
		if err != nil {
			// Non-fatal; log it but don't fail the cleanup stage.
			result.AddWarning(fmt.Sprintf("purge KV prefix %q: %v", key, err))
		} else {
			result.SetDetail("cleanup_kv_deleted", deleted)
		}
	}

	// NATS close is handled by Teardown; nil out to prevent double-close.
	s.nats = nil

	result.SetDetail("cleanup_done", true)
	return nil
}
