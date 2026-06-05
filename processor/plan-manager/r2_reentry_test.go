package planmanager

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestDetermineR2ReentryPoint_StoriesPhaseRoutesToArchitectureGenerated is
// the headline regression test for go-reviewer Pass-4 finding P4-C3.
//
// Pre-fix, story_rules.go emitted findings with `Phase: "stories"` but
// determineR2ReentryPoint's switch had no matching case, so the cascade
// fell through to the default branch — which nuked Requirements +
// Architecture + Scenarios and returned StatusApproved (rewind to
// requirements-gen). Sarah's defective Story output survived the regen
// because the plan re-traversed the requirements path that didn't fail.
//
// Post-fix, story-phase findings clear only Stories + Scenarios and
// return StatusArchitectureGenerated — Sarah's watcher claim point,
// triggering a re-prep of Stories from the (still-valid) architecture.
// TestDetermineR2ReentryPoint_PreviousArchitectureLifecycle pins the
// PreviousArchitectureJSON revision-carry-over contract: the architecture
// re-entry CAPTURES the prior architecture (so the architect revises rather
// than rewrites), while every other branch CLEARS any stale carry-over so a
// leftover from a failed prior architecture re-run can't leak into a
// plan/requirements re-entry whose regenerated requirements no longer match it.
func TestDetermineR2ReentryPoint_PreviousArchitectureLifecycle(t *testing.T) {
	arch := &workflow.ArchitectureDocument{Decisions: []workflow.ArchDecision{{Title: "use Go"}}}

	t.Run("architecture phase captures prior architecture", func(t *testing.T) {
		c := setupTestComponent(t)
		plan := &workflow.Plan{Slug: "demo", Architecture: arch}
		findings, _ := json.Marshal([]workflow.PlanReviewFinding{
			{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "architecture.component_missing_implementation_files"},
		})

		target := c.determineR2ReentryPoint(plan, findings)

		if target != workflow.StatusRequirementsGenerated {
			t.Fatalf("target = %s, want StatusRequirementsGenerated", target)
		}
		if plan.Architecture != nil {
			t.Error("plan.Architecture should be cleared for re-generation")
		}
		if plan.PreviousArchitectureJSON == "" {
			t.Error("PreviousArchitectureJSON should capture the prior architecture for the revision base")
		}
		if !strings.Contains(plan.PreviousArchitectureJSON, "use Go") {
			t.Errorf("PreviousArchitectureJSON missing prior content: %q", plan.PreviousArchitectureJSON)
		}
	})

	// Every non-architecture branch must clear stale carry-over (H1 leak guard).
	leakCases := []struct {
		name  string
		phase string
	}{
		{"plan phase clears stale carry-over", "plan"},
		{"requirements phase clears stale carry-over", "requirements"},
		{"scenarios phase clears stale carry-over", "scenarios"},
	}
	for _, tc := range leakCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := setupTestComponent(t)
			plan := &workflow.Plan{
				Slug:                     "demo",
				Architecture:             arch,
				PreviousArchitectureJSON: `{"stale":"from a failed prior architecture re-run"}`,
			}
			findings, _ := json.Marshal([]workflow.PlanReviewFinding{
				{Severity: "error", Status: "violation", Phase: tc.phase, SOPID: "x"},
			})

			c.determineR2ReentryPoint(plan, findings)

			if plan.PreviousArchitectureJSON != "" {
				t.Errorf("%s: stale PreviousArchitectureJSON survived (would leak into a forward-flow architect prompt): %q", tc.phase, plan.PreviousArchitectureJSON)
			}
		})
	}

	t.Run("unparseable findings clear stale carry-over", func(t *testing.T) {
		c := setupTestComponent(t)
		plan := &workflow.Plan{
			Slug:                     "demo",
			PreviousArchitectureJSON: `{"stale":"x"}`,
		}
		c.determineR2ReentryPoint(plan, json.RawMessage(`not json`))
		if plan.PreviousArchitectureJSON != "" {
			t.Errorf("stale PreviousArchitectureJSON survived the unparseable-findings fallback: %q", plan.PreviousArchitectureJSON)
		}
	})
}

func TestDetermineR2ReentryPoint_StoriesPhaseRoutesToArchitectureGenerated(t *testing.T) {
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		Slug:         "demo",
		Requirements: []workflow.Requirement{{ID: "req.demo.1", Title: "R"}},
		Architecture: &workflow.ArchitectureDocument{Decisions: []workflow.ArchDecision{{Title: "use Go"}}},
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "broken"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.demo.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
		},
	}
	findings, _ := json.Marshal([]workflow.PlanReviewFinding{
		{Severity: "error", Status: "violation", Phase: "stories", SOPID: "story.missing_files_owned"},
	})

	target := c.determineR2ReentryPoint(plan, findings)

	if target != workflow.StatusArchitectureGenerated {
		t.Errorf("target = %s, want %s (pre-fix P4-C3 would fall to default → StatusApproved, nuking Architecture too)", target, workflow.StatusArchitectureGenerated)
	}

	// Stories + Scenarios cleared (Sarah re-prep + Bob re-gen).
	if plan.Stories != nil {
		t.Errorf("plan.Stories = %v, want nil (Sarah re-prep required)", plan.Stories)
	}
	if plan.Scenarios != nil {
		t.Errorf("plan.Scenarios = %v, want nil (Bob re-gen required after Sarah re-preps)", plan.Scenarios)
	}

	// Requirements + Architecture preserved — they're upstream of Sarah and
	// passed review.
	if len(plan.Requirements) != 1 {
		t.Errorf("plan.Requirements wiped (len=%d), want preserved", len(plan.Requirements))
	}
	if plan.Architecture == nil {
		t.Errorf("plan.Architecture wiped, want preserved")
	}
}

// TestDetermineR2ReentryPoint_ScenariosCasePreservesStories pins the
// minimal-blast-radius semantic for scenarios-phase findings: when only
// Bob's output is at fault, Sarah's Stories survive. Pre-Train-D the
// scenarios case did NOT explicitly preserve Stories (the field hadn't
// existed yet at the time the function was written); the post-fix
// version makes that contract explicit.
func TestDetermineR2ReentryPoint_ScenariosCasePreservesStories(t *testing.T) {
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		Slug: "demo",
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "ok"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.demo.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
		},
	}
	findings, _ := json.Marshal([]workflow.PlanReviewFinding{
		{Severity: "error", Status: "violation", Phase: "scenarios", SOPID: "scenario.missing_tier_tag"},
	})

	target := c.determineR2ReentryPoint(plan, findings)

	if target != workflow.StatusArchitectureGenerated {
		t.Errorf("target = %s, want StatusArchitectureGenerated", target)
	}
	if len(plan.Stories) != 1 {
		t.Errorf("plan.Stories wiped, want preserved (only scenarios at fault)")
	}
	if plan.Scenarios != nil {
		t.Errorf("plan.Scenarios = %v, want nil (Bob re-gen)", plan.Scenarios)
	}
}

// TestDetermineR2ReentryPoint_UpstreamPhasesNukeStoriesToo pins that
// every cascade case that wipes upstream artifacts (plan / requirements /
// architecture) also wipes Stories — pre-fix the plan / requirements
// branches left Stories pinned to wiped Requirements, which would fail
// validation on Sarah's next dispatch.
func TestDetermineR2ReentryPoint_UpstreamPhasesNukeStoriesToo(t *testing.T) {
	c := setupTestComponent(t)
	cases := []struct {
		name  string
		phase string
		want  workflow.Status
	}{
		{"plan phase nukes Stories", "plan", workflow.StatusCreated},
		{"requirements phase nukes Stories", "requirements", workflow.StatusApproved},
		{"architecture phase nukes Stories", "architecture", workflow.StatusRequirementsGenerated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &workflow.Plan{
				Slug:         "demo",
				Requirements: []workflow.Requirement{{ID: "req.demo.1", Title: "R"}},
				Architecture: &workflow.ArchitectureDocument{Decisions: []workflow.ArchDecision{{Title: "use Go"}}},
				Stories:      []workflow.Story{{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "should be wiped"}},
				Scenarios:    []workflow.Scenario{{ID: "scen.demo.1", RequirementID: "req.demo.1"}},
			}
			findings, _ := json.Marshal([]workflow.PlanReviewFinding{
				{Severity: "error", Status: "violation", Phase: tc.phase, SOPID: "test.case"},
			})
			target := c.determineR2ReentryPoint(plan, findings)
			if target != tc.want {
				t.Errorf("target = %s, want %s", target, tc.want)
			}
			if plan.Stories != nil {
				t.Errorf("%s: plan.Stories = %v, want nil (upstream phase invalidates Stories)", tc.phase, plan.Stories)
			}
			if plan.Scenarios != nil {
				t.Errorf("%s: plan.Scenarios = %v, want nil", tc.phase, plan.Scenarios)
			}
		})
	}
}
