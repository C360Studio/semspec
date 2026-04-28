package scenarios

// ScenarioExecutionScenario tests the Requirement/Scenario CRUD APIs.
//
// Scope:
//
//  1. Requirement CRUD — create, get, list, update, deprecate, delete
//  2. Scenario CRUD   — create, get, list (with filter), update, delete
//  3. 404 responses for non-existent resources
//
// The reactive-workflow trigger stages were deleted when the rules engine
// was removed (KV watchers replaced workflow.trigger.* subjects). Real
// execution coverage now lives in hello-world-code-execution and
// execution-phase, which exercise the KV self-trigger path end-to-end.

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ScenarioExecutionScenario tests requirement/scenario CRUD.
type ScenarioExecutionScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
}

// NewScenarioExecutionScenario creates a new scenario execution scenario.
func NewScenarioExecutionScenario(cfg *config.Config) *ScenarioExecutionScenario {
	return &ScenarioExecutionScenario{
		name:        "scenario-execution",
		description: "Tests Requirement/Scenario CRUD and scenario-execution + DAG reactive workflow trigger",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ScenarioExecutionScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ScenarioExecutionScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ScenarioExecutionScenario) Setup(ctx context.Context) error {
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

// Execute runs the scenario execution scenario.
func (s *ScenarioExecutionScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		// Plan bootstrap — requirement/scenario CRUD depends on an approved plan.
		// Wait for the planner to draft before approving to avoid racing the KV watcher.
		{"create-plan", s.stageCreatePlan},
		{"wait-for-drafted", s.stageWaitForDrafted},
		{"approve-plan", s.stageApprovePlan},

		// Requirement CRUD
		{"requirement-create", s.stageRequirementCreate},
		{"requirement-get", s.stageRequirementGet},
		{"requirement-list", s.stageRequirementList},
		{"requirement-update", s.stageRequirementUpdate},
		{"requirement-404", s.stageRequirement404},

		// Scenario CRUD
		{"scenario-create", s.stageScenarioCreate},
		{"scenario-create-second", s.stageScenarioCreateSecond},
		{"scenario-get", s.stageScenarioGet},
		{"scenario-list-all", s.stageScenarioListAll},
		{"scenario-list-filtered", s.stageScenarioListFiltered},
		{"scenario-update", s.stageScenarioUpdate},
		{"scenario-404", s.stageScenario404},

		// Cleanup: delete scenario and deprecate requirement
		{"scenario-delete", s.stageScenarioDelete},
		{"requirement-deprecate", s.stageRequirementDeprecate},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageTimeout := s.config.StageTimeout
		if stage.name == "wait-for-drafted" {
			stageTimeout = 120 * time.Second
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
func (s *ScenarioExecutionScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers — carry test-local state via Result.Details
// ---------------------------------------------------------------------------

func (s *ScenarioExecutionScenario) planSlug(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("plan_slug")
}

func (s *ScenarioExecutionScenario) storedRequirementID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("requirement_id")
}

func (s *ScenarioExecutionScenario) storedScenarioID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("scenario_id")
}

func (s *ScenarioExecutionScenario) storedScenarioIDSecond(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("scenario_id_second")
}

// ---------------------------------------------------------------------------
// Plan bootstrap stages
// ---------------------------------------------------------------------------

func (s *ScenarioExecutionScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "scenario execution feature")
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
	result.SetDetail("expected_slug", "scenario-execution-feature")

	if _, err := s.http.WaitForPlanCreated(ctx, slug); err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}

	return nil
}

func (s *ScenarioExecutionScenario) stageWaitForDrafted(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by create-plan stage")
	}

	// This is a CRUD test — we don't need the planner to generate a goal.
	// Wait briefly for drafted status; if no LLM is available the plan stays
	// in "created" which is fine for CRUD verification.
	shortCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.http.WaitForPlanGoal(shortCtx, slug); err != nil {
		result.AddWarning("planner did not generate goal (no LLM) — continuing with CRUD tests")
	}
	return nil
}

func (s *ScenarioExecutionScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by create-plan stage")
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
// Requirement CRUD stages
// ---------------------------------------------------------------------------

func (s *ScenarioExecutionScenario) stageRequirementCreate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	req := &client.CreateRequirementRequest{
		Title:       "User can authenticate with a token",
		Description: "The system must validate bearer tokens for all protected endpoints",
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
	if requirement.PlanID == "" {
		return fmt.Errorf("requirement missing plan_id")
	}

	result.SetDetail("requirement_id", requirement.ID)
	result.SetDetail("requirement_plan_id", requirement.PlanID)
	return nil
}

func (s *ScenarioExecutionScenario) stageRequirementGet(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	requirement, status, err := s.http.GetRequirement(ctx, slug, requirementID)
	if err != nil {
		return fmt.Errorf("get requirement: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if requirement.ID != requirementID {
		return fmt.Errorf("ID mismatch: got %q, want %q", requirement.ID, requirementID)
	}

	result.SetDetail("requirement_get_verified", true)
	return nil
}

func (s *ScenarioExecutionScenario) stageRequirementList(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	requirements, err := s.http.ListRequirements(ctx, slug)
	if err != nil {
		return fmt.Errorf("list requirements: %w", err)
	}
	if len(requirements) == 0 {
		return fmt.Errorf("expected at least 1 requirement, got 0")
	}

	// Verify our requirement is in the list.
	found := false
	for _, r := range requirements {
		if r.ID == requirementID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("created requirement %q not found in list", requirementID)
	}

	result.SetDetail("requirement_list_count", len(requirements))
	return nil
}

func (s *ScenarioExecutionScenario) stageRequirementUpdate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	updatedDesc := "Validates bearer tokens using JWT RS256 signing"
	req := &client.UpdateRequirementRequest{
		Description: &updatedDesc,
	}

	requirement, err := s.http.UpdateRequirement(ctx, slug, requirementID, req)
	if err != nil {
		return fmt.Errorf("update requirement: %w", err)
	}
	if requirement.Description != updatedDesc {
		return fmt.Errorf("description not updated: got %q, want %q", requirement.Description, updatedDesc)
	}

	result.SetDetail("requirement_updated", true)
	return nil
}

func (s *ScenarioExecutionScenario) stageRequirement404(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	// Non-existent requirement should return 404.
	_, status, _ := s.http.GetRequirement(ctx, slug, "requirement.nonexistent.99999")
	if status != 404 {
		return fmt.Errorf("expected 404 for non-existent requirement, got %d", status)
	}

	// Non-existent plan should also return a non-200 (500 or 404).
	_, status, _ = s.http.GetRequirement(ctx, "nonexistent-plan-slug", "requirement.foo.1")
	if status == 200 {
		return fmt.Errorf("expected non-200 for non-existent plan, got 200")
	}

	result.SetDetail("requirement_404_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Scenario CRUD stages
// ---------------------------------------------------------------------------

func (s *ScenarioExecutionScenario) stageScenarioCreate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	req := &client.CreateScenarioRequest{
		RequirementID: requirementID,
		Given:         "a valid bearer token is present in the Authorization header",
		When:          "the client calls a protected endpoint",
		Then: []string{
			"the request is authenticated successfully",
			"the response status is 200 OK",
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
	if len(scenario.Then) != 2 {
		return fmt.Errorf("expected 2 then clauses, got %d", len(scenario.Then))
	}

	result.SetDetail("scenario_id", scenario.ID)
	return nil
}

func (s *ScenarioExecutionScenario) stageScenarioCreateSecond(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	// Create a second scenario for the same requirement, to test list filtering.
	req := &client.CreateScenarioRequest{
		RequirementID: requirementID,
		Given:         "an expired bearer token is present in the Authorization header",
		When:          "the client calls a protected endpoint",
		Then:          []string{"the request is rejected with 401 Unauthorized"},
	}

	scenario, err := s.http.CreateScenario(ctx, slug, req)
	if err != nil {
		return fmt.Errorf("create second scenario: %w", err)
	}
	if scenario.ID == "" {
		return fmt.Errorf("created second scenario has empty ID")
	}

	result.SetDetail("scenario_id_second", scenario.ID)
	return nil
}

func (s *ScenarioExecutionScenario) stageScenarioGet(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	scenarioID, _ := s.storedScenarioID(result)

	scenario, status, err := s.http.GetScenario(ctx, slug, scenarioID)
	if err != nil {
		return fmt.Errorf("get scenario: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if scenario.ID != scenarioID {
		return fmt.Errorf("ID mismatch: got %q, want %q", scenario.ID, scenarioID)
	}
	if scenario.Given == "" {
		return fmt.Errorf("scenario missing Given clause")
	}
	if scenario.When == "" {
		return fmt.Errorf("scenario missing When clause")
	}
	if len(scenario.Then) == 0 {
		return fmt.Errorf("scenario has empty Then clauses")
	}

	result.SetDetail("scenario_get_verified", true)
	return nil
}

func (s *ScenarioExecutionScenario) stageScenarioListAll(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	scenarioID, _ := s.storedScenarioID(result)
	secondID, _ := s.storedScenarioIDSecond(result)

	// List without filter — should return both scenarios.
	scenarios, err := s.http.ListScenarios(ctx, slug, "")
	if err != nil {
		return fmt.Errorf("list scenarios: %w", err)
	}
	if len(scenarios) < 2 {
		return fmt.Errorf("expected at least 2 scenarios, got %d", len(scenarios))
	}

	idSet := make(map[string]bool)
	for _, sc := range scenarios {
		idSet[sc.ID] = true
	}
	if !idSet[scenarioID] {
		return fmt.Errorf("first scenario %q not found in list", scenarioID)
	}
	if !idSet[secondID] {
		return fmt.Errorf("second scenario %q not found in list", secondID)
	}

	result.SetDetail("scenario_list_count", len(scenarios))
	return nil
}

func (s *ScenarioExecutionScenario) stageScenarioListFiltered(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	// Filter by requirement_id — should still return both (both belong to same requirement).
	scenarios, err := s.http.ListScenarios(ctx, slug, requirementID)
	if err != nil {
		return fmt.Errorf("list scenarios filtered: %w", err)
	}
	if len(scenarios) < 2 {
		return fmt.Errorf("expected at least 2 scenarios for requirement %q, got %d", requirementID, len(scenarios))
	}
	for _, sc := range scenarios {
		if sc.RequirementID != requirementID {
			return fmt.Errorf("scenario %q has wrong requirement_id %q, want %q", sc.ID, sc.RequirementID, requirementID)
		}
	}

	// Filter by a non-existent requirement_id — should return empty list (not 404).
	empty, err := s.http.ListScenarios(ctx, slug, "requirement.nonexistent.0")
	if err != nil {
		return fmt.Errorf("list scenarios filtered (empty): %w", err)
	}
	if len(empty) != 0 {
		return fmt.Errorf("expected 0 scenarios for non-existent requirement, got %d", len(empty))
	}

	result.SetDetail("scenario_list_filtered_count", len(scenarios))
	result.SetDetail("scenario_list_filtered_verified", true)
	return nil
}

func (s *ScenarioExecutionScenario) stageScenarioUpdate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	scenarioID, _ := s.storedScenarioID(result)

	updatedWhen := "the client calls a protected endpoint with the token"
	req := &client.UpdateScenarioRequest{
		When: &updatedWhen,
	}

	scenario, err := s.http.UpdateScenario(ctx, slug, scenarioID, req)
	if err != nil {
		return fmt.Errorf("update scenario: %w", err)
	}
	if scenario.When != updatedWhen {
		return fmt.Errorf("When not updated: got %q, want %q", scenario.When, updatedWhen)
	}

	result.SetDetail("scenario_updated", true)
	return nil
}

func (s *ScenarioExecutionScenario) stageScenario404(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	_, status, _ := s.http.GetScenario(ctx, slug, "scenario.nonexistent.99999")
	if status != 404 {
		return fmt.Errorf("expected 404 for non-existent scenario, got %d", status)
	}

	result.SetDetail("scenario_404_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Cleanup stages
// ---------------------------------------------------------------------------

func (s *ScenarioExecutionScenario) stageScenarioDelete(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	secondID, _ := s.storedScenarioIDSecond(result)

	// Delete the second scenario (keep the first for the trigger stage reference).
	status, err := s.http.DeleteScenario(ctx, slug, secondID)
	if err != nil {
		return fmt.Errorf("delete scenario: %w", err)
	}
	if status != 204 {
		return fmt.Errorf("expected HTTP 204 on delete, got %d", status)
	}

	// Verify it is gone.
	_, getStatus, _ := s.http.GetScenario(ctx, slug, secondID)
	if getStatus != 404 {
		return fmt.Errorf("expected 404 after delete, got %d", getStatus)
	}

	result.SetDetail("scenario_delete_verified", true)
	return nil
}

func (s *ScenarioExecutionScenario) stageRequirementDeprecate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	requirementID, _ := s.storedRequirementID(result)

	requirement, err := s.http.DeprecateRequirement(ctx, slug, requirementID)
	if err != nil {
		return fmt.Errorf("deprecate requirement: %w", err)
	}
	if requirement.Status != "deprecated" {
		return fmt.Errorf("expected status=deprecated, got %q", requirement.Status)
	}

	result.SetDetail("requirement_deprecated", true)
	return nil
}
