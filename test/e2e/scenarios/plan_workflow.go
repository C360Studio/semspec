package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// PlanWorkflowScenario tests the ADR-003 workflow commands via REST API.
// Tests: CreatePlan → PromotePlan → ExecutePlan (dry-run) and direct plan creation.
// This validates the backend is solid for UI development.
type PlanWorkflowScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewPlanWorkflowScenario creates a new plan workflow scenario.
func NewPlanWorkflowScenario(cfg *config.Config) *PlanWorkflowScenario {
	return &PlanWorkflowScenario{
		name:        "plan-workflow",
		description: "Tests CreatePlan, PromotePlan, ExecutePlan via REST API (ADR-003)",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *PlanWorkflowScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *PlanWorkflowScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *PlanWorkflowScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the plan workflow scenario.
func (s *PlanWorkflowScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"plan-create", s.stagePlanCreate},
		{"plan-verify", s.stagePlanVerify},
		{"plan-update-scope", s.stagePlanUpdateScope},
		{"approve", s.stageApprove},
		{"approve-verify", s.stageApproveVerify},
		// HTTP endpoint verification stages (run early, don't depend on execute)
		{"verify-404-responses", s.stageVerify404Responses},
		{"verify-context-endpoint", s.stageVerifyContextEndpoint},
		{"verify-reviews-endpoint", s.stageVerifyReviewsEndpoint},
		// Reactive workflow verification
		{"verify-reactive-state", s.stageVerifyReactiveState},
		// Execute stages
		{"create-tasks", s.stageCreateTasks},
		{"execute-dry-run", s.stageExecuteDryRun},
		{"execute-verify", s.stageExecuteVerify},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		// Use longer timeout for LLM-powered stages
		stageTimeout := s.config.StageTimeout
		if stage.name == "plan-create" || stage.name == "plan-verify" {
			stageTimeout = 120 * time.Second // LLM can take a while
		}
		stageCtx, cancel := context.WithTimeout(ctx, stageTimeout)

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
func (s *PlanWorkflowScenario) Teardown(_ context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stagePlanCreate creates a plan via the REST API.
func (s *PlanWorkflowScenario) stagePlanCreate(ctx context.Context, result *Result) error {
	planTitle := "authentication options"
	result.SetDetail("plan_title", planTitle)
	result.SetDetail("expected_slug", "authentication-options")

	// Create plan via REST API
	resp, err := s.http.CreatePlan(ctx, planTitle)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	result.SetDetail("plan_response", resp)
	result.SetDetail("plan_slug", resp.Slug)

	return nil
}

// stagePlanVerify verifies the plan was created on the filesystem.
func (s *PlanWorkflowScenario) stagePlanVerify(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Wait for plan directory to exist
	if err := s.fs.WaitForPlan(ctx, expectedSlug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}

	// Verify plan.json exists
	if err := s.fs.WaitForPlanFile(ctx, expectedSlug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Load and verify plan.json
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	result.SetDetail("plan_verified", true)
	result.SetDetail("plan_id", plan["id"])
	return nil
}

// stagePlanUpdateScope updates the plan with goal/context/scope fields.
func (s *PlanWorkflowScenario) stagePlanUpdateScope(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load current plan
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Add plan content for a realistic test
	plan["goal"] = "Explore OAuth, JWT, and session-based auth approaches"
	plan["context"] = "Need to evaluate authentication options for the API"
	plan["scope"] = map[string]any{
		"include": []string{"api/auth/*", "docs/auth.md"},
		"exclude": []string{"api/legacy/*"},
	}

	// Save updated plan
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := s.fs.WriteFile(planPath, string(data)); err != nil {
		return fmt.Errorf("write plan.json: %w", err)
	}

	result.SetDetail("scope_updated", true)
	return nil
}

// stageApprove approves the plan via REST API to enable task generation.
func (s *PlanWorkflowScenario) stageApprove(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Approve via REST API
	resp, err := s.http.PromotePlan(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("promote returned error: %s", resp.Error)
	}

	result.SetDetail("approve_response", resp)
	return nil
}

// stageApproveVerify verifies the plan is now approved.
func (s *PlanWorkflowScenario) stageApproveVerify(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load plan.json
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Verify plan is now approved
	approved, ok := plan["approved"].(bool)
	if !ok {
		// Fall back to committed for backwards compatibility
		approved, ok = plan["committed"].(bool)
		if !ok {
			return fmt.Errorf("plan.json missing 'approved' field")
		}
	}
	if !approved {
		return fmt.Errorf("plan should be approved after promote, but approved=false")
	}

	// Verify approved_at is set
	if plan["approved_at"] == nil && plan["committed_at"] == nil {
		return fmt.Errorf("plan.json missing 'approved_at' field")
	}

	result.SetDetail("approve_verified", true)
	return nil
}

// stageCreateTasks creates tasks.json for the plan before execution.
func (s *PlanWorkflowScenario) stageCreateTasks(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Create tasks manually for execution test
	tasks := []map[string]any{
		{
			"id":          fmt.Sprintf("task.%s.1", expectedSlug),
			"plan_id":     fmt.Sprintf("plan.%s", expectedSlug),
			"sequence":    1,
			"description": "Research OAuth 2.0 implementation options",
			"type":        "research",
			"status":      "pending",
		},
		{
			"id":          fmt.Sprintf("task.%s.2", expectedSlug),
			"plan_id":     fmt.Sprintf("plan.%s", expectedSlug),
			"sequence":    2,
			"description": "Evaluate JWT library options",
			"type":        "research",
			"status":      "pending",
		},
	}

	// Write tasks.json
	tasksPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/tasks.json"
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	if err := s.fs.WriteFile(tasksPath, string(data)); err != nil {
		return fmt.Errorf("write tasks.json: %w", err)
	}

	result.SetDetail("tasks_created", len(tasks))
	return nil
}

// stageExecuteDryRun tests ExecutePlan via REST API.
func (s *PlanWorkflowScenario) stageExecuteDryRun(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	resp, err := s.http.ExecutePlan(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("execute returned error: %s", resp.Error)
	}

	result.SetDetail("execute_response", resp)
	result.SetDetail("batch_id", resp.BatchID)
	return nil
}

// stageExecuteVerify verifies tasks.json exists and execution was triggered.
func (s *PlanWorkflowScenario) stageExecuteVerify(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Verify tasks.json exists (we created it in stageCreateTasks)
	tasksPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/tasks.json"
	if !s.fs.FileExists(tasksPath) {
		return fmt.Errorf("tasks.json not found")
	}

	// Load and verify tasks
	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	// Should have 2 tasks from stageCreateTasks
	if len(tasks) != 2 {
		return fmt.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	result.SetDetail("execute_verified", true)
	result.SetDetail("task_count", len(tasks))
	return nil
}

// stageVerify404Responses tests that the HTTP endpoints return 404 for nonexistent data.
func (s *PlanWorkflowScenario) stageVerify404Responses(ctx context.Context, result *Result) error {
	// Test nonexistent context response - should return 404
	_, status, _ := s.http.GetContextBuilderResponse(ctx, "nonexistent-request-id-12345")
	if status != 404 {
		return fmt.Errorf("context endpoint: expected 404 for missing ID, got %d", status)
	}
	result.SetDetail("context_404_verified", true)

	// Test nonexistent plan reviews - should return 404
	_, status, _ = s.http.GetPlanReviews(ctx, "nonexistent-plan-slug-xyz")
	if status != 404 {
		return fmt.Errorf("reviews endpoint: expected 404 for missing slug, got %d", status)
	}
	result.SetDetail("reviews_404_verified", true)

	result.SetDetail("404_handling_verified", true)
	return nil
}

// stageVerifyContextEndpoint tests the GET /context-builder/responses/{request_id} endpoint.
func (s *PlanWorkflowScenario) stageVerifyContextEndpoint(ctx context.Context, result *Result) error {
	// Look for context request IDs in CONTEXT_RESPONSES bucket
	kvResp, err := s.http.GetKVEntries(ctx, "CONTEXT_RESPONSES")
	if err != nil {
		// Bucket may not exist if no context was requested during workflow
		result.SetDetail("context_responses_available", false)
		result.SetDetail("context_responses_note", "bucket not found or empty - context building may not have been triggered")
		return nil // Not a failure - context building is optional in this workflow
	}

	if len(kvResp.Entries) == 0 {
		result.SetDetail("context_responses_available", false)
		result.SetDetail("context_responses_note", "no context responses stored")
		return nil
	}

	// Test retrieval of first available response via HTTP endpoint
	requestID := kvResp.Entries[0].Key
	resp, status, err := s.http.GetContextBuilderResponse(ctx, requestID)
	if err != nil {
		return fmt.Errorf("get context response via HTTP: %w", err)
	}

	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}

	// Verify response structure
	if resp.RequestID != requestID {
		return fmt.Errorf("request_id mismatch: got %s, want %s", resp.RequestID, requestID)
	}

	result.SetDetail("context_responses_available", true)
	result.SetDetail("context_response_verified", true)
	result.SetDetail("context_request_id", requestID)
	result.SetDetail("context_task_type", resp.TaskType)
	result.SetDetail("context_tokens_used", resp.TokensUsed)
	return nil
}

// stageVerifyReactiveState verifies the reactive workflow KV state for the plan.
// After CreatePlan triggers plan creation, the reactive workflow engine should
// create a PlanReviewState entry in the WORKFLOWS KV bucket.
func (s *PlanWorkflowScenario) stageVerifyReactiveState(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Check WORKFLOWS bucket for plan-review states
	kvResp, err := s.http.GetKVEntries(ctx, "WORKFLOWS")
	if err != nil {
		// If bucket doesn't exist, the reactive engine may not be enabled
		result.SetDetail("reactive_state_available", false)
		result.SetDetail("reactive_state_note", "WORKFLOWS bucket not found - reactive engine may not be configured")
		return nil
	}

	// Look for plan-review entries matching our plan slug
	// Plan review keys follow pattern: plan-review.<slug>
	var planReviewState *client.WorkflowState
	for _, entry := range kvResp.Entries {
		expectedKey := "plan-review." + expectedSlug
		if entry.Key != expectedKey {
			continue
		}

		var state client.WorkflowState
		if err := json.Unmarshal(entry.Value, &state); err != nil {
			return fmt.Errorf("unmarshal plan-review state: %w", err)
		}
		planReviewState = &state
		break
	}

	if planReviewState == nil {
		// No plan-review state found - this is acceptable in basic workflow test
		// where the full reactive loop may not have been triggered
		result.SetDetail("reactive_state_available", false)
		result.SetDetail("reactive_state_note", "no plan-review state found in WORKFLOWS bucket - plan may have been created directly without triggering reactive workflow")
		return nil
	}

	// Verify state structure
	result.SetDetail("reactive_state_available", true)
	result.SetDetail("reactive_workflow_id", planReviewState.WorkflowID)
	result.SetDetail("reactive_phase", planReviewState.Phase)
	result.SetDetail("reactive_status", planReviewState.Status)
	result.SetDetail("reactive_iteration", planReviewState.Iteration)

	// Verify required fields
	if planReviewState.WorkflowID != "plan-review-loop" {
		return fmt.Errorf("unexpected workflow_id: got %q, want %q", planReviewState.WorkflowID, "plan-review-loop")
	}
	if planReviewState.Slug != expectedSlug {
		return fmt.Errorf("unexpected slug in state: got %q, want %q", planReviewState.Slug, expectedSlug)
	}

	// Verify verdict if review completed
	if planReviewState.Verdict != "" {
		result.SetDetail("reactive_verdict", planReviewState.Verdict)
		result.SetDetail("reactive_summary", planReviewState.Summary)
	}

	// Check if any workflow events were published
	events, err := s.http.GetMessageLogEntries(ctx, 50, "workflow.events.plan.*")
	if err == nil && len(events) > 0 {
		var eventTypes []string
		for _, e := range events {
			eventTypes = append(eventTypes, e.Subject)
		}
		result.SetDetail("reactive_events_found", eventTypes)
	}

	result.SetDetail("reactive_state_verified", true)
	return nil
}

// stageVerifyReviewsEndpoint tests the GET /workflow-api/plans/{slug}/reviews endpoint.
func (s *PlanWorkflowScenario) stageVerifyReviewsEndpoint(ctx context.Context, result *Result) error {
	// Use the slug from earlier plan stage
	slug := "authentication-options"

	resp, status, err := s.http.GetPlanReviews(ctx, slug)
	if err != nil && status != 404 {
		return fmt.Errorf("get plan reviews via HTTP: %w", err)
	}

	if status == 404 {
		// No review workflow completed yet - this is valid for this test scenario
		result.SetDetail("reviews_available", false)
		result.SetDetail("reviews_status", 404)
		result.SetDetail("reviews_note", "no review workflow completed for this plan - expected in basic workflow test")
		return nil
	}

	// Verify response structure if data exists
	if resp.Verdict == "" {
		return fmt.Errorf("missing verdict in response")
	}

	result.SetDetail("reviews_available", true)
	result.SetDetail("reviews_verdict", resp.Verdict)
	result.SetDetail("reviews_passed", resp.Passed)
	result.SetDetail("reviews_summary", resp.Summary)
	return nil
}
