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

// validStory returns a minimal Story that passes workflow.ValidateStory.
// Status is left empty — matches Sarah's omitempty emission shape. Post-
// Train-D, the readiness invariants apply on empty Status, so the helper
// supplies one source file and one task. Callers that need to test the
// invariant failures should build a Story inline with the missing field.
func validStory(id, reqID, title string) workflow.Story {
	return workflow.Story{
		ID:             id,
		RequirementIDs: []string{reqID},
		ComponentName:  "placeholder-component",
		Title:          title,
		FilesOwned:     []string{"src/" + id + ".go"},
		Tasks: []workflow.Task{
			{ID: "task." + id + ".1", StoryID: id, Description: "implement"},
		},
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
						ID:             "story.story-scope.1.1",
						RequirementIDs: []string{"req.story-scope.1"},
						ComponentName:  "placeholder-component",
						Title:          "T",
						FilesOwned:     []string{"src/new.go"},
						Tasks: []workflow.Task{
							{ID: "task.story.story-scope.1.1.1", StoryID: "story.story-scope.1.1", Description: "implement"},
						},
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
			name: "legacy fallback (StoryID empty): second batch on same req replaces prior",
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
				// StoryID left empty → legacy per-Requirement dispatch fallback.
				Scenarios: []workflow.Scenario{
					{ID: "scen.new1", RequirementID: "req.scen-wipe.1"},
				},
			},
			wantSuccess: true,
			// Pre-Sarah plans and mock fixtures dispatch without StoryID; the
			// handler wipes ALL prior scenarios under the RequirementID to
			// match pre-ADR-043 behavior. Pinned so subsequent producer
			// migrations that drop the legacy mode flag this as a regression.
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
			name: "per-Story merge: two Stories under same Req each persist their scenarios",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-per-story")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{{ID: "req.scen-per-story.1", Title: "R1"}}
				// Story A already landed its scenarios.
				plan.Scenarios = []workflow.Scenario{
					{ID: "scen.A.1", RequirementID: "req.scen-per-story.1", StoryID: "story.A"},
				}
				_ = c.plans.save(ctx, plan)
				return c, "scen-per-story"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-per-story",
				RequirementID: "req.scen-per-story.1",
				StoryID:       "story.B",
				Scenarios: []workflow.Scenario{
					{ID: "scen.B.1", RequirementID: "req.scen-per-story.1", StoryID: "story.B"},
				},
			},
			wantSuccess: true,
			// Closes Pass-2 C2: Story B's mutation must not wipe Story A's
			// scenarios on the same Requirement.
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if len(plan.Scenarios) != 2 {
					t.Fatalf("Scenarios = %d, want 2 (Story A preserved + Story B added)", len(plan.Scenarios))
				}
				ids := map[string]bool{}
				for _, s := range plan.Scenarios {
					ids[s.ID] = true
				}
				if !ids["scen.A.1"] || !ids["scen.B.1"] {
					t.Errorf("Scenarios IDs = %v, want both scen.A.1 and scen.B.1", ids)
				}
			},
		},
		{
			name: "per-Story re-dispatch (same StoryID) wipes only that Story's prior scenarios",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-redispatch")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{{ID: "req.scen-redispatch.1", Title: "R1"}}
				// Both Stories already landed; Story B is going to re-dispatch.
				plan.Scenarios = []workflow.Scenario{
					{ID: "scen.A.1", RequirementID: "req.scen-redispatch.1", StoryID: "story.A"},
					{ID: "scen.B.old1", RequirementID: "req.scen-redispatch.1", StoryID: "story.B"},
					{ID: "scen.B.old2", RequirementID: "req.scen-redispatch.1", StoryID: "story.B"},
				}
				_ = c.plans.save(ctx, plan)
				return c, "scen-redispatch"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-redispatch",
				RequirementID: "req.scen-redispatch.1",
				StoryID:       "story.B",
				Scenarios: []workflow.Scenario{
					{ID: "scen.B.new", RequirementID: "req.scen-redispatch.1", StoryID: "story.B"},
				},
			},
			wantSuccess: true,
			// Pinned: Story B's old scenarios are wiped (same StoryID),
			// Story A's scenario survives (different StoryID).
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if len(plan.Scenarios) != 2 {
					t.Fatalf("Scenarios = %d, want 2 (Story A preserved + Story B's new batch)", len(plan.Scenarios))
				}
				ids := map[string]bool{}
				for _, s := range plan.Scenarios {
					ids[s.ID] = true
				}
				if !ids["scen.A.1"] {
					t.Errorf("Story A's scen.A.1 should survive sibling-Story re-dispatch")
				}
				if !ids["scen.B.new"] {
					t.Errorf("Story B's new batch (scen.B.new) should be present")
				}
				if ids["scen.B.old1"] || ids["scen.B.old2"] {
					t.Errorf("Story B's old scenarios should be wiped on same-StoryID re-dispatch")
				}
			},
		},
		{
			name: "per-Story dispatch with StoryID set does NOT wipe legacy (StoryID empty) scenarios on same Req",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "scen-mixed")
				plan.Status = workflow.StatusGeneratingScenarios
				plan.Requirements = []workflow.Requirement{{ID: "req.scen-mixed.1", Title: "R1"}}
				// A legacy scenario (no StoryID) is already attached to this Req.
				plan.Scenarios = []workflow.Scenario{
					{ID: "scen.legacy", RequirementID: "req.scen-mixed.1"},
				}
				_ = c.plans.save(ctx, plan)
				return c, "scen-mixed"
			},
			req: ScenariosMutationRequest{
				Slug:          "scen-mixed",
				RequirementID: "req.scen-mixed.1",
				StoryID:       "story.X",
				Scenarios: []workflow.Scenario{
					{ID: "scen.X.1", RequirementID: "req.scen-mixed.1", StoryID: "story.X"},
				},
			},
			wantSuccess: true,
			// Per-Story mutation must not wipe sibling scenarios with empty
			// StoryID — they belong to "no Story / legacy" and would only be
			// touched by another empty-StoryID dispatch. Documents the
			// mixed-state shape; production plans should converge to one mode
			// per Req but the merge must be safe during the transition.
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if len(plan.Scenarios) != 2 {
					t.Errorf("Scenarios = %d, want 2 (legacy preserved + new Story.X added)", len(plan.Scenarios))
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
