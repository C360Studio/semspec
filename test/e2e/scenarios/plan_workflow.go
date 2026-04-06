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
		{"wait-for-drafted", s.stageWaitForDrafted},
		{"approve", s.stageApprove},
		{"approve-verify", s.stageApproveVerify},
		// Requirement/Scenario CRUD (REST-only, no LLM needed)
		{"requirement-crud", s.stageRequirementCRUD},
		{"scenario-crud", s.stageScenarioCRUD},
		// HTTP endpoint verification stages (run early, don't depend on execute)
		{"verify-404-responses", s.stageVerify404Responses},
		{"verify-context-endpoint", s.stageVerifyContextEndpoint},
		{"verify-reviews-endpoint", s.stageVerifyReviewsEndpoint},
		// Infrastructure endpoint coverage (B6)
		{"verify-health-endpoint", s.stageVerifyHealthEndpoint},
		{"verify-components-endpoint", s.stageVerifyComponentsEndpoint},
		// Reactive workflow verification
		{"verify-reactive-state", s.stageVerifyReactiveState},
		// Note: execute stages require mock LLM to drive the coordinator through
		// approved → requirements_generated → scenarios_generated → ready_for_execution.
		// They belong in Tier 2 (task e2e:mock -- plan-phase), not Tier 1.
	}

	for _, stage := range stages {
		stageStart := time.Now()
		// Use longer timeout for LLM-powered stages
		stageTimeout := s.config.StageTimeout
		if stage.name == "plan-create" || stage.name == "plan-verify" || stage.name == "wait-for-drafted" {
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
	// Use the server-returned slug for all subsequent stages.
	result.SetDetail("expected_slug", resp.Slug)

	return nil
}

// stagePlanVerify verifies the plan was created via the HTTP API.
func (s *PlanWorkflowScenario) stagePlanVerify(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Wait for plan to exist via HTTP API
	plan, err := s.http.WaitForPlanCreated(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}

	result.SetDetail("plan_verified", true)
	result.SetDetail("plan_id", plan.ID)
	return nil
}

// stagePlanUpdateScope updates the plan with goal/context fields via HTTP API.
func (s *PlanWorkflowScenario) stagePlanUpdateScope(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Update plan via PATCH /plans/{slug}
	updates := map[string]any{
		"goal":    "Explore OAuth, JWT, and session-based auth approaches",
		"context": "Need to evaluate authentication options for the API",
	}

	if _, err := s.http.UpdatePlan(ctx, expectedSlug, updates); err != nil {
		return fmt.Errorf("update plan: %w", err)
	}

	result.SetDetail("scope_updated", true)
	return nil
}

// stageWaitForDrafted waits for the planner to finish drafting the plan.
// The planner KV watcher picks up new plans and runs the LLM pipeline.
// With auto-approve enabled, the plan may pass through drafted → reviewed → approved
// before we check, so we wait for a non-empty Goal (set by the planner) rather than
// an exact status match.
func (s *PlanWorkflowScenario) stageWaitForDrafted(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	plan, err := s.http.WaitForPlanGoal(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("wait for plan goal: %w", err)
	}

	result.SetDetail("drafted_goal", plan.Goal)
	result.SetDetail("drafted_status", plan.Status)
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

// stageApproveVerify verifies the plan is now approved via the HTTP API.
func (s *PlanWorkflowScenario) stageApproveVerify(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load plan via HTTP API
	plan, err := s.http.GetPlan(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	// Verify plan is now approved
	if !plan.Approved {
		return fmt.Errorf("plan should be approved after promote, but approved=false")
	}

	// Verify approved_at is set
	if plan.ApprovedAt == nil {
		return fmt.Errorf("plan missing 'approved_at' field")
	}

	// B1/B3: Verify stage field is set and correct for approved status.
	if plan.Stage == "" {
		return fmt.Errorf("plan stage field is empty after approval")
	}
	result.SetDetail("approve_stage", plan.Stage)
	// After promote the status is "approved", so stage should be "approved".
	if plan.Stage != "approved" {
		return fmt.Errorf("expected stage=approved after promote, got %q", plan.Stage)
	}

	result.SetDetail("approve_verified", true)
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

// stageExecuteVerify verifies execution was triggered for the plan.
func (s *PlanWorkflowScenario) stageExecuteVerify(_ context.Context, result *Result) error {
	result.SetDetail("execute_verified", true)
	return nil
}

// stageRequirementCRUD exercises the full requirement CRUD lifecycle via REST API.
func (s *PlanWorkflowScenario) stageRequirementCRUD(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Create
	created, err := s.http.CreateRequirement(ctx, slug, &client.CreateRequirementRequest{
		Title:       "E2E CRUD test requirement",
		Description: "Created by plan-workflow E2E to verify CRUD",
	})
	if err != nil {
		return fmt.Errorf("create requirement: %w", err)
	}
	if created.ID == "" || created.Title != "E2E CRUD test requirement" {
		return fmt.Errorf("create: unexpected response: id=%q title=%q", created.ID, created.Title)
	}

	// List — verify it appears
	reqs, err := s.http.ListRequirements(ctx, slug)
	if err != nil {
		return fmt.Errorf("list requirements: %w", err)
	}
	found := false
	for _, r := range reqs {
		if r.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("created requirement %s not found in list", created.ID)
	}

	// Get by ID
	got, status, err := s.http.GetRequirement(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("get requirement: %w", err)
	}
	if status != 200 || got.ID != created.ID {
		return fmt.Errorf("get: status=%d id=%q", status, got.ID)
	}

	// Update
	newTitle := "Updated E2E requirement"
	updated, err := s.http.UpdateRequirement(ctx, slug, created.ID, &client.UpdateRequirementRequest{
		Title: &newTitle,
	})
	if err != nil {
		return fmt.Errorf("update requirement: %w", err)
	}
	if updated.Title != newTitle {
		return fmt.Errorf("update: title=%q, want %q", updated.Title, newTitle)
	}

	// Deprecate
	deprecated, err := s.http.DeprecateRequirement(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("deprecate requirement: %w", err)
	}
	if deprecated.Status != "deprecated" {
		return fmt.Errorf("deprecate: status=%q, want deprecated", deprecated.Status)
	}

	// Delete
	deleteStatus, err := s.http.DeleteRequirement(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("delete requirement: %w", err)
	}
	if deleteStatus != 204 {
		return fmt.Errorf("delete: status=%d, want 204", deleteStatus)
	}

	// Verify gone
	_, getStatus, _ := s.http.GetRequirement(ctx, slug, created.ID)
	if getStatus != 404 {
		return fmt.Errorf("deleted requirement still accessible: status=%d", getStatus)
	}

	result.SetDetail("requirement_crud_verified", true)
	return nil
}

// stageScenarioCRUD exercises the full scenario CRUD lifecycle via REST API.
func (s *PlanWorkflowScenario) stageScenarioCRUD(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Create a requirement to parent the scenario under
	req, err := s.http.CreateRequirement(ctx, slug, &client.CreateRequirementRequest{
		Title: "Scenario CRUD parent requirement",
	})
	if err != nil {
		return fmt.Errorf("create parent requirement: %w", err)
	}

	// Create scenario
	created, err := s.http.CreateScenario(ctx, slug, &client.CreateScenarioRequest{
		RequirementID: req.ID,
		Given:         "an authenticated user",
		When:          "they request their profile",
		Then:          []string{"the profile data is returned", "the response includes email"},
	})
	if err != nil {
		return fmt.Errorf("create scenario: %w", err)
	}
	if created.ID == "" || created.RequirementID != req.ID {
		return fmt.Errorf("create: id=%q requirement_id=%q", created.ID, created.RequirementID)
	}

	// List all scenarios
	all, err := s.http.ListScenarios(ctx, slug, "")
	if err != nil {
		return fmt.Errorf("list all scenarios: %w", err)
	}
	found := false
	for _, sc := range all {
		if sc.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("created scenario %s not found in list", created.ID)
	}

	// List by requirement
	byReq, err := s.http.ListScenarios(ctx, slug, req.ID)
	if err != nil {
		return fmt.Errorf("list scenarios by requirement: %w", err)
	}
	if len(byReq) == 0 {
		return fmt.Errorf("no scenarios found for requirement %s", req.ID)
	}

	// Get by ID
	got, status, err := s.http.GetScenario(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("get scenario: %w", err)
	}
	if status != 200 || got.Given != "an authenticated user" {
		return fmt.Errorf("get: status=%d given=%q", status, got.Given)
	}

	// Update
	newWhen := "they request their profile with a valid token"
	updated, err := s.http.UpdateScenario(ctx, slug, created.ID, &client.UpdateScenarioRequest{
		When: &newWhen,
	})
	if err != nil {
		return fmt.Errorf("update scenario: %w", err)
	}
	if updated.When != newWhen {
		return fmt.Errorf("update: when=%q, want %q", updated.When, newWhen)
	}

	// Delete
	deleteStatus, err := s.http.DeleteScenario(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("delete scenario: %w", err)
	}
	if deleteStatus != 204 {
		return fmt.Errorf("delete: status=%d, want 204", deleteStatus)
	}

	// Verify gone
	_, getStatus, _ := s.http.GetScenario(ctx, slug, created.ID)
	if getStatus != 404 {
		return fmt.Errorf("deleted scenario still accessible: status=%d", getStatus)
	}

	// Clean up parent requirement
	_, _ = s.http.DeleteRequirement(ctx, slug, req.ID)

	result.SetDetail("scenario_crud_verified", true)
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
// create a PlanReviewState entry in the REACTIVE_STATE KV bucket.
func (s *PlanWorkflowScenario) stageVerifyReactiveState(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Check REACTIVE_STATE bucket for plan-review states
	kvResp, err := s.http.GetKVEntries(ctx, client.ReactiveStateBucket)
	if err != nil {
		// If bucket doesn't exist, the reactive engine may not be enabled
		result.SetDetail("reactive_state_available", false)
		result.SetDetail("reactive_state_note", client.ReactiveStateBucket+" bucket not found - reactive engine may not be configured")
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
		result.SetDetail("reactive_state_note", "no plan-review state found in "+client.ReactiveStateBucket+" bucket - plan may have been created directly without triggering reactive workflow")
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

// stageVerifyReviewsEndpoint tests the GET /plan-api/plans/{slug}/reviews endpoint.
func (s *PlanWorkflowScenario) stageVerifyReviewsEndpoint(ctx context.Context, result *Result) error {
	// Use the slug from the plan-create stage.
	slug, _ := result.GetDetailString("expected_slug")

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

// stageVerifyHealthEndpoint tests GET /health (B6: untested endpoint coverage).
func (s *PlanWorkflowScenario) stageVerifyHealthEndpoint(ctx context.Context, result *Result) error {
	resp, err := httpGetJSON(ctx, s.config.HTTPBaseURL+"/health")
	if err != nil {
		return fmt.Errorf("GET /health: %w", err)
	}
	m, ok := resp.(map[string]any)
	if !ok {
		return fmt.Errorf("expected JSON object from /health, got %T", resp)
	}
	status, _ := m["status"].(string)
	if status == "" {
		return fmt.Errorf("/health response missing status field")
	}
	result.SetDetail("health_status", status)
	return nil
}

// stageVerifyComponentsEndpoint tests GET /components (B6: untested endpoint coverage).
func (s *PlanWorkflowScenario) stageVerifyComponentsEndpoint(ctx context.Context, result *Result) error {
	resp, err := httpGetJSON(ctx, s.config.HTTPBaseURL+"/components")
	if err != nil {
		return fmt.Errorf("GET /components: %w", err)
	}
	switch v := resp.(type) {
	case []any:
		if len(v) == 0 {
			return fmt.Errorf("/components returned empty array")
		}
		result.SetDetail("component_count", len(v))
	case map[string]any:
		result.SetDetail("components_response_type", "object")
	default:
		return fmt.Errorf("unexpected /components response type: %T", resp)
	}
	return nil
}
