package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestReviseArchitectureState_HappyPath pins the pure whole-phase state
// mutation an accepted architecture_revise PlanDecision performs from the
// implementing status: capture the prior architecture, wipe Architecture +
// Stories + Scenarios, route the diagnosis into ReviewFormattedFindings, and
// drive the back-transition to requirements_generated. Scoped decisions use
// reviseScopedArchitectureState via applyArchitectureRevise.
func TestReviseArchitectureState_HappyPath(t *testing.T) {
	plan := &workflow.Plan{
		Slug:   "mavlink-hard",
		Status: workflow.StatusImplementing,
		Architecture: &workflow.ArchitectureDocument{
			DataFlow: "sensor -> driver -> mavsdk",
		},
		Stories:   []workflow.Story{{ID: "story-1"}},
		Scenarios: []workflow.Scenario{{ID: "scenario-1"}},
	}
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.mavlink-hard.recovery.abcd1234",
		Kind:      workflow.PlanDecisionKindArchitectureRevise,
		Rationale: "Winston pinned the 3.x mavsdk API but the driver needs 2.x; every dev cycle re-hallucinates coords.",
	}

	transitioned, from := reviseArchitectureState(plan, proposal)

	if !transitioned {
		t.Fatalf("expected back-transition to be applied, got transitioned=false (from=%q)", from)
	}
	if from != workflow.StatusImplementing {
		t.Errorf("from status: got %q, want implementing", from)
	}
	if plan.Status != workflow.StatusRequirementsGenerated {
		t.Errorf("plan.Status: got %q, want requirements_generated", plan.Status)
	}
	if plan.Architecture != nil {
		t.Error("Architecture should be wiped")
	}
	if plan.Stories != nil {
		t.Error("Stories should be wiped")
	}
	if plan.Scenarios != nil {
		t.Error("Scenarios should be wiped")
	}
	if !strings.Contains(plan.PreviousArchitectureJSON, "mavsdk") {
		t.Errorf("PreviousArchitectureJSON should capture the prior architecture, got %q", plan.PreviousArchitectureJSON)
	}
	if plan.ReviewFormattedFindings != proposal.Rationale {
		t.Errorf("ReviewFormattedFindings: got %q, want the diagnosis", plan.ReviewFormattedFindings)
	}
}

// TestReviseArchitectureState_OutOfWindow verifies that a plan which has
// already moved past implementing (e.g. reached complete while the accept
// landed late) is left ENTIRELY untouched: no transition, AND no entity wipe.
// Wiping a terminal plan's architecture/stories/scenarios would corrupt it
// (go-reviewer M2). The wipe must be gated behind the transition check.
func TestReviseArchitectureState_OutOfWindow(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "mavlink-hard",
		Status:       workflow.StatusComplete,
		Architecture: &workflow.ArchitectureDocument{DataFlow: "x"},
		Stories:      []workflow.Story{{ID: "story-1"}},
		Scenarios:    []workflow.Scenario{{ID: "scenario-1"}},
	}
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.mavlink-hard.recovery.abcd1234",
		Kind:      workflow.PlanDecisionKindArchitectureRevise,
		Rationale: "diagnosis",
	}

	transitioned, from := reviseArchitectureState(plan, proposal)

	if transitioned {
		t.Errorf("expected transitioned=false from %q, got true", from)
	}
	if plan.Status != workflow.StatusComplete {
		t.Errorf("plan.Status should be unchanged, got %q", plan.Status)
	}
	if plan.Architecture == nil {
		t.Error("Architecture must NOT be wiped on an out-of-window accept (M2)")
	}
	if plan.Stories == nil || plan.Scenarios == nil {
		t.Error("Stories/Scenarios must NOT be wiped on an out-of-window accept (M2)")
	}
	if plan.ReviewFormattedFindings != "" {
		t.Error("ReviewFormattedFindings must NOT be set on an out-of-window accept (M2)")
	}
}

// TestReviseArchitectureState_NoPriorArchitecture confirms PreviousArchitectureJSON
// stays empty (no stale leftover) when the plan has no architecture to capture,
// and the transition + wipe still proceed.
func TestReviseArchitectureState_NoPriorArchitecture(t *testing.T) {
	plan := &workflow.Plan{
		Slug:                     "mavlink-hard",
		Status:                   workflow.StatusImplementing,
		PreviousArchitectureJSON: "stale-leftover",
	}
	proposal := &workflow.PlanDecision{Kind: workflow.PlanDecisionKindArchitectureRevise}

	transitioned, _ := reviseArchitectureState(plan, proposal)

	if !transitioned {
		t.Error("expected transition to requirements_generated")
	}
	if plan.PreviousArchitectureJSON != "" {
		t.Errorf("PreviousArchitectureJSON should be cleared when no architecture exists, got %q", plan.PreviousArchitectureJSON)
	}
}

func TestReviseArchitectureState_FromRejectedForPostQARecovery(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "mavlink-hard",
		Status:       workflow.StatusRejected,
		Architecture: &workflow.ArchitectureDocument{DataFlow: "prior design"},
		Stories:      []workflow.Story{{ID: "story-1"}},
		Scenarios:    []workflow.Scenario{{ID: "scenario-1"}},
	}
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.mavlink-hard.recovery.post-qa",
		Kind:      workflow.PlanDecisionKindArchitectureRevise,
		Rationale: "QA found the architecture dependency contract was wrong.",
	}

	transitioned, from := reviseArchitectureState(plan, proposal)

	if !transitioned {
		t.Fatalf("expected rejected plan to transition for post-QA architecture recovery, from=%q", from)
	}
	if from != workflow.StatusRejected {
		t.Fatalf("from = %s, want rejected", from)
	}
	if plan.Status != workflow.StatusRequirementsGenerated {
		t.Fatalf("Status = %s, want requirements_generated", plan.Status)
	}
	if plan.Architecture != nil || plan.Stories != nil || plan.Scenarios != nil {
		t.Fatalf("architecture/stories/scenarios were not wiped: arch=%v stories=%v scenarios=%v", plan.Architecture, plan.Stories, plan.Scenarios)
	}
}

func TestApplyArchitectureRevise_ScopedResetUsesAffectedRequirementClosure(t *testing.T) {
	c := setupTestComponent(t)
	// Scoped architecture_revise resets scope=requirements → the typed family
	// reset. plan-manager names the closure reqIDs; execution-manager enumerates
	// each requirement's key families (#294). Capture the reqIDs plan-manager
	// issued resets for.
	var reset []string
	c.reqFamilyResetSender = func(_ context.Context, _, reqID string) (int, error) {
		reset = append(reset, reqID)
		return 1, nil
	}

	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Requirements: []workflow.Requirement{
			{ID: "bootstrap", Title: "Project bootstrap"},
			{ID: "unrelated", Title: "Independent feature"},
			{ID: "contract", Title: "External dependency API contract", DependsOn: []string{"bootstrap"}},
			{ID: "consumer", Title: "Consumer integration", DependsOn: []string{"contract"}},
		},
		Architecture: &workflow.ArchitectureDocument{DataFlow: "prior design"},
		Stories: []workflow.Story{
			{ID: "story.contract", RequirementIDs: []string{"contract"}},
			{ID: "story.consumer", RequirementIDs: []string{"consumer"}},
			{ID: "story.unrelated", RequirementIDs: []string{"unrelated"}},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.contract", RequirementID: "contract", StoryID: "story.contract"},
			{ID: "scen.consumer", RequirementID: "consumer", StoryID: "story.consumer"},
			{ID: "scen.unrelated", RequirementID: "unrelated", StoryID: "story.unrelated"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.recovery.contract",
		Kind:           workflow.PlanDecisionKindArchitectureRevise,
		Rationale:      "Add a verified external dependency and API contract instead of hand-rolled protocol behavior.",
		AffectedReqIDs: []string{"contract"},
	}

	if err := c.applyArchitectureRevise(context.Background(), plan, proposal); err != nil {
		t.Fatalf("applyArchitectureRevise: %v", err)
	}

	// Typed resets issued for the dirty closure (contract + its dependent
	// consumer), NOT the independent bootstrap/unrelated requirements.
	assertResetKeys(t, reset, []string{"contract", "consumer"})
	assertNoResetKeys(t, reset, []string{"bootstrap", "unrelated"})
	if plan.Status != workflow.StatusRequirementsGenerated {
		t.Fatalf("plan.Status = %s, want requirements_generated", plan.Status)
	}
	if plan.Architecture != nil {
		t.Fatal("Architecture should be cleared so Winston revises it")
	}
	if plan.PendingArchitectureRevision == nil {
		t.Fatal("PendingArchitectureRevision should record the scoped dirty closure")
	}
	assertStringSet(t, plan.PendingArchitectureRevision.RequirementIDs, []string{"contract", "consumer"})
	assertStringSet(t, plan.PendingArchitectureRevision.StoryIDs, []string{"story.contract", "story.consumer"})
	if len(plan.Stories) != 3 {
		t.Fatalf("Stories len = %d, want 3 preserved until scoped merge", len(plan.Stories))
	}
	if len(plan.Scenarios) != 3 {
		t.Fatalf("Scenarios len = %d, want 3 preserved until scoped story merge", len(plan.Scenarios))
	}
	if got := storyRecoveryHint(plan.Stories, "story.unrelated"); got != "" {
		t.Fatalf("unrelated story RecoveryHint = %q, want empty", got)
	}
}

func TestApplyArchitectureRevise_ScopedResetExpandsMNStoryCoverage(t *testing.T) {
	c := setupTestComponent(t)
	// Scoped architecture_revise resets scope=requirements → the typed family
	// reset; capture the reqIDs plan-manager named (#294).
	var reset []string
	c.reqFamilyResetSender = func(_ context.Context, _, reqID string) (int, error) {
		reset = append(reset, reqID)
		return 1, nil
	}

	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Requirements: []workflow.Requirement{
			{ID: "telemetry"},
			{ID: "control"},
			{ID: "async"},
			{ID: "unrelated"},
		},
		Architecture: &workflow.ArchitectureDocument{DataFlow: "prior design"},
		Stories: []workflow.Story{
			{ID: "story.mapper", RequirementIDs: []string{"telemetry", "control", "async"}},
			{ID: "story.unrelated", RequirementIDs: []string{"unrelated"}},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.recovery.mapper",
		Kind:           workflow.PlanDecisionKindArchitectureRevise,
		AffectedReqIDs: []string{"telemetry"},
		Rationale:      "Revise the shared mapper architecture.",
	}

	if err := c.applyArchitectureRevise(context.Background(), plan, proposal); err != nil {
		t.Fatalf("applyArchitectureRevise: %v", err)
	}

	// M:N expansion: AffectedReqIDs=[telemetry] pulls its co-covered siblings
	// control+async (shared story.mapper), NOT unrelated.
	assertResetKeys(t, reset, []string{"telemetry", "control", "async"})
	assertNoResetKeys(t, reset, []string{"unrelated"})
	assertStringSet(t, plan.PendingArchitectureRevision.RequirementIDs, []string{"async", "control", "telemetry"})
	assertStringSet(t, plan.PendingArchitectureRevision.StoryIDs, []string{"story.mapper"})
}

func TestPlanDecisionResetScope_UnscopedRequiresExplicitEvidence(t *testing.T) {
	scope, reqIDs, err := planDecisionResetScope(&workflow.Plan{
		Requirements: []workflow.Requirement{{ID: "contract"}},
	}, &workflow.PlanDecision{Kind: workflow.PlanDecisionKindArchitectureRevise})

	if err == nil {
		t.Fatal("planDecisionResetScope returned nil error; want explicit evidence required for unscoped reset")
	}
	if scope != "" {
		t.Fatalf("scope = %q, want empty on rejected unscoped reset", scope)
	}
	if reqIDs != nil {
		t.Fatalf("reqIDs = %v, want nil", reqIDs)
	}
}

func TestPlanDecisionResetScope_UnscopedRequiresWholePhaseEvidence(t *testing.T) {
	scope, reqIDs, err := planDecisionResetScope(&workflow.Plan{
		Requirements: []workflow.Requirement{{ID: "contract"}},
	}, &workflow.PlanDecision{
		Kind: workflow.PlanDecisionKindArchitectureRevise,
		ContractImpact: &workflow.ContractImpact{
			Kind:        workflow.ContractImpactChange,
			Summary:     "The accepted decision invalidates the architecture phase.",
			AffectedIDs: []string{"contract.phase:architecture"},
		},
	})

	if err != nil {
		t.Fatalf("planDecisionResetScope returned error: %v", err)
	}
	if scope != "all" {
		t.Fatalf("scope = %q, want all", scope)
	}
	if reqIDs != nil {
		t.Fatalf("reqIDs = %v, want nil", reqIDs)
	}
}

func TestApplyArchitectureRevise_UnscopedWithoutPhaseEvidenceLeavesPlanUntouched(t *testing.T) {
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		Slug:         "demo",
		Status:       workflow.StatusImplementing,
		Architecture: &workflow.ArchitectureDocument{DataFlow: "prior"},
		Stories:      []workflow.Story{{ID: "story.unrelated"}},
		Scenarios:    []workflow.Scenario{{ID: "scenario.unrelated"}},
	}
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.demo.recovery.unscoped",
		Kind:      workflow.PlanDecisionKindArchitectureRevise,
		Rationale: "Architecture needs another look, but no target was named.",
		ContractImpact: &workflow.ContractImpact{
			Kind:    workflow.ContractImpactRefine,
			Summary: "No whole-phase contract change.",
		},
	}

	if err := c.applyArchitectureRevise(context.Background(), plan, proposal); err == nil {
		t.Fatal("applyArchitectureRevise returned nil; want unscoped reset rejected")
	}
	if plan.Status != workflow.StatusImplementing {
		t.Fatalf("Status = %s, want implementing", plan.Status)
	}
	if plan.Architecture == nil || plan.Architecture.DataFlow != "prior" {
		t.Fatalf("Architecture = %+v, want original preserved", plan.Architecture)
	}
	if len(plan.Stories) != 1 || plan.Stories[0].ID != "story.unrelated" {
		t.Fatalf("Stories = %+v, want original preserved", plan.Stories)
	}
	if len(plan.Scenarios) != 1 || plan.Scenarios[0].ID != "scenario.unrelated" {
		t.Fatalf("Scenarios = %+v, want original preserved", plan.Scenarios)
	}
}

func TestHandleStoriesMutation_ScopedArchitectureRevisionMergesDirtyStoriesOnly(t *testing.T) {
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, "demo")
	plan.Status = workflow.StatusPreparingStories
	plan.Requirements = []workflow.Requirement{
		{ID: "bootstrap"},
		{ID: "unrelated"},
		{ID: "contract", DependsOn: []string{"bootstrap"}},
		{ID: "consumer", DependsOn: []string{"contract"}},
	}
	plan.PendingArchitectureRevision = &workflow.ArchitectureRevisionScope{
		ProposalID:     "plan-decision.demo.recovery.contract",
		RequirementIDs: []string{"contract", "consumer"},
		StoryIDs:       []string{"story.contract.old", "story.consumer.old"},
	}
	plan.Stories = []workflow.Story{
		testStory("story.bootstrap.old", []string{"bootstrap"}, "src/bootstrap.go"),
		testStory("story.unrelated.old", []string{"unrelated"}, "src/unrelated.go"),
		testStory("story.contract.old", []string{"contract"}, "src/contract_old.go"),
		testStory("story.consumer.old", []string{"consumer"}, "src/consumer_old.go"),
	}
	plan.Scenarios = []workflow.Scenario{
		{ID: "scen.bootstrap", RequirementID: "bootstrap", StoryID: "story.bootstrap.old"},
		{ID: "scen.unrelated", RequirementID: "unrelated", StoryID: "story.unrelated.old"},
		{ID: "scen.contract", RequirementID: "contract", StoryID: "story.contract.old"},
		{ID: "scen.consumer", RequirementID: "consumer", StoryID: "story.consumer.old"},
	}
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	emitted := []workflow.Story{
		testStory("story.bootstrap.new", []string{"bootstrap"}, "src/bootstrap_new.go"),
		testStory("story.unrelated.new", []string{"unrelated"}, "src/unrelated_new.go"),
		testStory("story.contract.new", []string{"contract"}, "src/contract_new.go"),
		testStory("story.consumer.new", []string{"consumer"}, "src/consumer_new.go"),
	}
	data, err := json.Marshal(storiesMutationRequest{Slug: "demo", Stories: emitted})
	if err != nil {
		t.Fatalf("marshal stories mutation: %v", err)
	}

	resp := c.handleStoriesMutation(context.Background(), data)
	if !resp.Success {
		t.Fatalf("handleStoriesMutation failed: %s", resp.Error)
	}

	loaded, ok := c.plans.get("demo")
	if !ok {
		t.Fatal("plan not found after stories mutation")
	}
	if loaded.Status != workflow.StatusStoriesGenerated {
		t.Fatalf("Status = %s, want stories_generated", loaded.Status)
	}
	if loaded.PendingArchitectureRevision == nil {
		t.Fatal("PendingArchitectureRevision should remain until scoped scenarios converge")
	}
	assertStoryPresent(t, loaded.Stories, "story.bootstrap.old")
	assertStoryPresent(t, loaded.Stories, "story.unrelated.old")
	assertStoryPresent(t, loaded.Stories, "story.contract.new")
	assertStoryPresent(t, loaded.Stories, "story.consumer.new")
	assertStoryAbsent(t, loaded.Stories, "story.bootstrap.new")
	assertStoryAbsent(t, loaded.Stories, "story.unrelated.new")
	assertStoryAbsent(t, loaded.Stories, "story.contract.old")
	assertStoryAbsent(t, loaded.Stories, "story.consumer.old")
	assertScenarioIDs(t, loaded.Scenarios, []string{"scen.bootstrap", "scen.unrelated"})
}

func TestHandleScenariosMutation_ScopedArchitectureRevisionPreservesUnrelatedScenarios(t *testing.T) {
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, "demo")
	plan.Status = workflow.StatusGeneratingScenarios
	plan.Requirements = []workflow.Requirement{
		{ID: "bootstrap"},
		{ID: "unrelated"},
		{ID: "contract", DependsOn: []string{"bootstrap"}},
		{ID: "consumer", DependsOn: []string{"contract"}},
	}
	plan.PendingArchitectureRevision = &workflow.ArchitectureRevisionScope{
		ProposalID:     "plan-decision.demo.recovery.contract",
		RequirementIDs: []string{"contract", "consumer"},
		StoryIDs:       []string{"story.contract.new", "story.consumer.new"},
	}
	plan.Stories = []workflow.Story{
		testStory("story.bootstrap.old", []string{"bootstrap"}, "src/bootstrap.go"),
		testStory("story.unrelated.old", []string{"unrelated"}, "src/unrelated.go"),
		testStory("story.contract.new", []string{"contract"}, "src/contract_new.go"),
		testStory("story.consumer.new", []string{"consumer"}, "src/consumer_new.go"),
	}
	plan.Scenarios = []workflow.Scenario{
		{ID: "scen.bootstrap", RequirementID: "bootstrap", StoryID: "story.bootstrap.old"},
		{ID: "scen.unrelated", RequirementID: "unrelated", StoryID: "story.unrelated.old"},
	}
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	contractData, err := json.Marshal(ScenariosMutationRequest{
		Slug:          "demo",
		RequirementID: "contract",
		StoryID:       "story.contract.new",
		Scenarios: []workflow.Scenario{{
			ID:            "scen.contract.new",
			RequirementID: "contract",
			StoryID:       "story.contract.new",
		}},
	})
	if err != nil {
		t.Fatalf("marshal contract scenarios mutation: %v", err)
	}

	resp := c.handleScenariosMutation(context.Background(), contractData)
	if !resp.Success {
		t.Fatalf("handleScenariosMutation contract failed: %s", resp.Error)
	}

	loaded, ok := c.plans.get("demo")
	if !ok {
		t.Fatal("plan not found after contract scenarios mutation")
	}
	if loaded.Status != workflow.StatusGeneratingScenarios {
		t.Fatalf("Status = %s, want generating_scenarios before dirty scope converges", loaded.Status)
	}
	if loaded.PendingArchitectureRevision == nil {
		t.Fatal("PendingArchitectureRevision should remain until all dirty scenarios converge")
	}
	assertScenarioIDs(t, loaded.Scenarios, []string{
		"scen.bootstrap",
		"scen.unrelated",
		"scen.contract.new",
	})

	consumerData, err := json.Marshal(ScenariosMutationRequest{
		Slug:          "demo",
		RequirementID: "consumer",
		StoryID:       "story.consumer.new",
		Scenarios: []workflow.Scenario{{
			ID:            "scen.consumer.new",
			RequirementID: "consumer",
			StoryID:       "story.consumer.new",
		}},
	})
	if err != nil {
		t.Fatalf("marshal consumer scenarios mutation: %v", err)
	}

	resp = c.handleScenariosMutation(context.Background(), consumerData)
	if !resp.Success {
		t.Fatalf("handleScenariosMutation consumer failed: %s", resp.Error)
	}

	loaded, ok = c.plans.get("demo")
	if !ok {
		t.Fatal("plan not found after consumer scenarios mutation")
	}
	if loaded.Status != workflow.StatusScenariosGenerated {
		t.Fatalf("Status = %s, want scenarios_generated after dirty scope converges", loaded.Status)
	}
	if loaded.PendingArchitectureRevision != nil {
		t.Fatalf("PendingArchitectureRevision = %+v, want cleared after scoped scenarios converge", loaded.PendingArchitectureRevision)
	}
	assertScenarioIDs(t, loaded.Scenarios, []string{
		"scen.bootstrap",
		"scen.unrelated",
		"scen.contract.new",
		"scen.consumer.new",
	})
}

func TestApplyArchitectureRevise_PreservesUnrelatedCompletedExecutions(t *testing.T) {
	c := setupTestComponent(t)
	// Scoped architecture_revise resets scope=requirements → the typed family
	// reset; capture the reqIDs plan-manager named. The independent
	// completed-unrelated requirement is outside the contract→consumer closure,
	// so plan-manager never issues a reset for it (#294).
	var reset []string
	c.reqFamilyResetSender = func(_ context.Context, _, reqID string) (int, error) {
		reset = append(reset, reqID)
		return 1, nil
	}

	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Requirements: []workflow.Requirement{
			{ID: "contract"},
			{ID: "consumer", DependsOn: []string{"contract"}},
			{ID: "completed-unrelated"},
		},
		Architecture: &workflow.ArchitectureDocument{DataFlow: "prior design"},
	}
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.recovery.arch",
		Kind:           workflow.PlanDecisionKindArchitectureRevise,
		AffectedReqIDs: []string{"contract"},
		Rationale:      "Repair the architecture contract for the dependency branch.",
	}

	if err := c.applyArchitectureRevise(context.Background(), plan, proposal); err != nil {
		t.Fatalf("applyArchitectureRevise: %v", err)
	}

	assertResetKeys(t, reset, []string{"contract", "consumer"})
	assertNoResetKeys(t, reset, []string{"completed-unrelated"})
}

func testStory(id string, reqIDs []string, file string) workflow.Story {
	return workflow.Story{
		ID:             id,
		ComponentName:  id + "-component",
		RequirementIDs: reqIDs,
		Title:          id,
		FilesOwned:     []string{file},
		Tasks: []workflow.Task{{
			ID:          "task." + id,
			StoryID:     id,
			Description: "verify " + id,
		}},
	}
}

func storyRecoveryHint(stories []workflow.Story, id string) string {
	for _, story := range stories {
		if story.ID == id {
			return story.RecoveryHint
		}
	}
	return ""
}

func assertStringSet(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want set %v", got, want)
	}
	seen := make(map[string]struct{}, len(got))
	for _, id := range got {
		seen[id] = struct{}{}
	}
	for _, id := range want {
		if _, ok := seen[id]; !ok {
			t.Fatalf("got %v, missing %q", got, id)
		}
	}
}

func assertStoryPresent(t *testing.T, stories []workflow.Story, id string) {
	t.Helper()
	for _, story := range stories {
		if story.ID == id {
			return
		}
	}
	t.Fatalf("stories = %+v, missing %q", stories, id)
}

func assertStoryAbsent(t *testing.T, stories []workflow.Story, id string) {
	t.Helper()
	for _, story := range stories {
		if story.ID == id {
			t.Fatalf("stories = %+v, should not include %q", stories, id)
		}
	}
}

func assertScenarioIDs(t *testing.T, scenarios []workflow.Scenario, want []string) {
	t.Helper()
	got := make([]string, 0, len(scenarios))
	for _, scenario := range scenarios {
		got = append(got, scenario.ID)
	}
	assertStringSet(t, got, want)
}

func assertResetKeys(t *testing.T, got []string, want []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(got))
	for _, key := range got {
		seen[key] = struct{}{}
	}
	for _, key := range want {
		if _, ok := seen[key]; !ok {
			t.Fatalf("reset keys = %v, missing %q", got, key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("reset keys = %v, want exactly %v", got, want)
	}
}

func assertNoResetKeys(t *testing.T, got []string, forbidden []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(got))
	for _, key := range got {
		seen[key] = struct{}{}
	}
	for _, key := range forbidden {
		if _, ok := seen[key]; ok {
			t.Fatalf("reset keys = %v, should not include %q", got, key)
		}
	}
}
