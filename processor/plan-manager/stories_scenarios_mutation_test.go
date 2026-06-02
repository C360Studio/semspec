package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// These tests close the coverage gap surfaced by the post-ADR-043 go-reviewer
// passes (Pass 2 H4 + Pass 4 H4): handleStoriesMutation and
// handleScenariosMutation had zero direct test coverage despite being the
// load-bearing mutation handlers for the entire per-Story chain. They pin
// current behavior so subsequent refactors (per-(Req,Story) wipe, scenario-ID
// format change) are pinned by failing tests, not silent regressions.

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// validStory returns a minimal Story that passes workflow.ValidateStory. No
// Status set — matches Sarah's emission shape, which the empty-Status branch
// in ValidateStory accepts without running readiness invariants.
func validStory(id, reqID, title string) workflow.Story {
	return workflow.Story{
		ID:            id,
		RequirementID: reqID,
		Title:         title,
	}
}

func TestHandleStoriesMutation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		// setup prepares the component + plan. Returns slug used by the test.
		setup func(t *testing.T) (*Component, string)
		req   storiesMutationRequest
		// expected outcomes
		wantSuccess     bool
		wantErrorSubstr string
		// post-condition assertions (only when wantSuccess)
		checkPlan func(t *testing.T, plan *workflow.Plan)
	}{
		{
			name: "happy path from preparing_stories",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "story-happy")
				plan.Status = workflow.StatusPreparingStories
				_ = c.plans.save(ctx, plan)
				return c, "story-happy"
			},
			req: storiesMutationRequest{
				Slug: "story-happy",
				Stories: []workflow.Story{
					validStory("story.story-happy.1.1", "req.story-happy.1", "Implement core"),
				},
				StoryCount: 1,
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusStoriesGenerated {
					t.Errorf("status = %s, want stories_generated", plan.EffectiveStatus())
				}
				if len(plan.Stories) != 1 {
					t.Errorf("Stories count = %d, want 1", len(plan.Stories))
				}
				if plan.Stories[0].ID != "story.story-happy.1.1" {
					t.Errorf("Stories[0].ID = %q, want story.story-happy.1.1", plan.Stories[0].ID)
				}
			},
		},
		{
			name: "empty slug rejected",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), ""
			},
			req: storiesMutationRequest{
				Slug: "",
				Stories: []workflow.Story{
					validStory("story.x.1.1", "req.x.1", "T"),
				},
			},
			wantSuccess:     false,
			wantErrorSubstr: "slug required",
		},
		{
			name: "plan not found",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), "nonexistent"
			},
			req: storiesMutationRequest{
				Slug: "nonexistent",
				Stories: []workflow.Story{
					validStory("story.x.1.1", "req.x.1", "T"),
				},
			},
			wantSuccess:     false,
			wantErrorSubstr: "plan not found",
		},
		{
			name: "validation failure propagated (missing requirement_id)",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "story-validate")
				plan.Status = workflow.StatusPreparingStories
				_ = c.plans.save(ctx, plan)
				return c, "story-validate"
			},
			req: storiesMutationRequest{
				Slug: "story-validate",
				Stories: []workflow.Story{
					{ID: "story.bad.1.1" /* RequirementID intentionally empty */, Title: "T"},
				},
			},
			wantSuccess:     false,
			wantErrorSubstr: "validate stories",
		},
		{
			name: "invalid transition from wrong status",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "story-wrong-status")
				plan.Status = workflow.StatusCreated // not preparing_stories
				_ = c.plans.save(ctx, plan)
				return c, "story-wrong-status"
			},
			req: storiesMutationRequest{
				Slug: "story-wrong-status",
				Stories: []workflow.Story{
					validStory("story.x.1.1", "req.x.1", "T"),
				},
			},
			wantSuccess:     false,
			wantErrorSubstr: "invalid transition",
		},
		{
			name: "scope auto-derived from Story.FilesOwned",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "story-scope")
				plan.Status = workflow.StatusPreparingStories
				plan.Scope = workflow.Scope{Include: []string{"existing.go"}}
				_ = c.plans.save(ctx, plan)
				return c, "story-scope"
			},
			req: storiesMutationRequest{
				Slug: "story-scope",
				Stories: []workflow.Story{
					{
						ID:            "story.story-scope.1.1",
						RequirementID: "req.story-scope.1",
						Title:         "T",
						FilesOwned:    []string{"src/new.go"},
					},
				},
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if len(plan.Scope.Create) != 1 || plan.Scope.Create[0] != "src/new.go" {
					t.Errorf("Scope.Create = %v, want [src/new.go]", plan.Scope.Create)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := tt.setup(t)

			data := marshalJSON(t, tt.req)
			resp := c.handleStoriesMutation(ctx, data)

			if resp.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v (error: %s)", resp.Success, tt.wantSuccess, resp.Error)
			}
			if !tt.wantSuccess && tt.wantErrorSubstr != "" {
				if !strings.Contains(resp.Error, tt.wantErrorSubstr) {
					t.Errorf("Error = %q, want substring %q", resp.Error, tt.wantErrorSubstr)
				}
			}
			if tt.wantSuccess && tt.checkPlan != nil {
				plan, ok := c.plans.get(tt.req.Slug)
				if !ok {
					t.Fatal("plan not found after mutation")
				}
				tt.checkPlan(t, plan)
			}
		})
	}
}

func TestHandleScenariosMutation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		setup           func(t *testing.T) (*Component, string)
		req             ScenariosMutationRequest
		wantSuccess     bool
		wantErrorSubstr string
		checkPlan       func(t *testing.T, plan *workflow.Plan)
	}{
		{
			name: "happy path: single requirement, single scenario batch",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-happy")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{{ID: "req.scen-happy.1", Title: "R1"}}
				_ = c.plans.save(ctx, plan)
				return c, "scen-happy"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-happy",
				RequirementID: "req.scen-happy.1",
				Scenarios: []workflow.Scenario{
					{ID: "scen.1", RequirementID: "req.scen-happy.1"},
				},
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusScenariosGenerated {
					t.Errorf("status = %s, want scenarios_generated (single req covered)", plan.EffectiveStatus())
				}
				if len(plan.Scenarios) != 1 {
					t.Errorf("Scenarios = %d, want 1", len(plan.Scenarios))
				}
			},
		},
		{
			name: "convergence holds when one of two requirements still uncovered",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-partial")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{
					{ID: "req.scen-partial.1", Title: "R1"},
					{ID: "req.scen-partial.2", Title: "R2"},
				}
				_ = c.plans.save(ctx, plan)
				return c, "scen-partial"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-partial",
				RequirementID: "req.scen-partial.1",
				Scenarios: []workflow.Scenario{
					{ID: "scen.1", RequirementID: "req.scen-partial.1"},
				},
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusGeneratingScenarios {
					t.Errorf("status = %s, want generating_scenarios (req.2 still uncovered)", plan.EffectiveStatus())
				}
				if len(plan.Scenarios) != 1 {
					t.Errorf("Scenarios = %d, want 1", len(plan.Scenarios))
				}
			},
		},
		{
			name: "wipe-by-RequirementID: second batch on same req replaces prior",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-wipe")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{{ID: "req.scen-wipe.1", Title: "R1"}}
				plan.Scenarios = []workflow.Scenario{
					{ID: "scen.old1", RequirementID: "req.scen-wipe.1"},
					{ID: "scen.old2", RequirementID: "req.scen-wipe.1"},
				}
				_ = c.plans.save(ctx, plan)
				return c, "scen-wipe"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-wipe",
				RequirementID: "req.scen-wipe.1",
				Scenarios: []workflow.Scenario{
					{ID: "scen.new1", RequirementID: "req.scen-wipe.1"},
				},
			},
			wantSuccess: true,
			// Pins Pass-2 C2 current behavior: prior scenarios for this RequirementID
			// are wiped. Pinning this lets us catch the change of shape when we move
			// to per-(Req,Story) wipe-replace in a follow-up PR.
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if len(plan.Scenarios) != 1 {
					t.Errorf("Scenarios = %d, want 1 (old batch wiped, new batch kept)", len(plan.Scenarios))
				}
				if plan.Scenarios[0].ID != "scen.new1" {
					t.Errorf("Scenarios[0].ID = %q, want scen.new1", plan.Scenarios[0].ID)
				}
			},
		},
		{
			name: "merge keeps other requirements' scenarios",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-merge")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{
					{ID: "req.scen-merge.1", Title: "R1"},
					{ID: "req.scen-merge.2", Title: "R2"},
				}
				plan.Scenarios = []workflow.Scenario{
					{ID: "scen.req2", RequirementID: "req.scen-merge.2"},
				}
				_ = c.plans.save(ctx, plan)
				return c, "scen-merge"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-merge",
				RequirementID: "req.scen-merge.1",
				Scenarios: []workflow.Scenario{
					{ID: "scen.req1", RequirementID: "req.scen-merge.1"},
				},
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if len(plan.Scenarios) != 2 {
					t.Errorf("Scenarios = %d, want 2 (req-2 scenario preserved + req-1 added)", len(plan.Scenarios))
				}
				if plan.EffectiveStatus() != workflow.StatusScenariosGenerated {
					t.Errorf("status = %s, want scenarios_generated (both reqs now covered)", plan.EffectiveStatus())
				}
			},
		},
		{
			name: "empty slug rejected",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), ""
			},
			req: ScenariosMutationRequest{
				Slug:          "",
				RequirementID: "req.x.1",
				Scenarios:     []workflow.Scenario{{ID: "s1", RequirementID: "req.x.1"}},
			},
			wantSuccess:     false,
			wantErrorSubstr: "slug and requirement_id required",
		},
		{
			name: "empty requirement_id rejected",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), ""
			},
			req: ScenariosMutationRequest{
				Slug:          "anything",
				RequirementID: "",
				Scenarios:     []workflow.Scenario{{ID: "s1"}},
			},
			wantSuccess:     false,
			wantErrorSubstr: "slug and requirement_id required",
		},
		{
			name: "plan not found",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), "nonexistent"
			},
			req: ScenariosMutationRequest{
				Slug:          "nonexistent",
				RequirementID: "req.x.1",
				Scenarios:     []workflow.Scenario{{ID: "s1", RequirementID: "req.x.1"}},
			},
			wantSuccess:     false,
			wantErrorSubstr: "plan not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := tt.setup(t)

			data := marshalJSON(t, tt.req)
			resp := c.handleScenariosMutation(ctx, data)

			if resp.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v (error: %s)", resp.Success, tt.wantSuccess, resp.Error)
			}
			if !tt.wantSuccess && tt.wantErrorSubstr != "" {
				if !strings.Contains(resp.Error, tt.wantErrorSubstr) {
					t.Errorf("Error = %q, want substring %q", resp.Error, tt.wantErrorSubstr)
				}
			}
			if tt.wantSuccess && tt.checkPlan != nil {
				plan, ok := c.plans.get(tt.req.Slug)
				if !ok {
					t.Fatal("plan not found after mutation")
				}
				tt.checkPlan(t, plan)
			}
		})
	}
}
