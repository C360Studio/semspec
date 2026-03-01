package reactive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	workflow "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
	"github.com/c360studio/semstreams/processor/reactive/testutil"
)

const testStateBucket = "REACTIVE_STATE"

// ---------------------------------------------------------------------------
// Definition-level tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_Definition(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)

	if def.ID != "plan-review-loop" {
		t.Errorf("expected ID 'plan-review-loop', got %q", def.ID)
	}

	// Participant pattern produces 9 rules (vs old 7 callback-based rules).
	expectedRules := []struct {
		id         string
		actionType reactiveEngine.ActionType
	}{
		{"accept-trigger", reactiveEngine.ActionMutate},
		{"dispatch-generator", reactiveEngine.ActionPublish},
		{"generator-completed", reactiveEngine.ActionMutate},
		{"dispatch-reviewer", reactiveEngine.ActionPublish},
		{"reviewer-completed", reactiveEngine.ActionMutate},
		{"handle-approved", reactiveEngine.ActionComplete},
		{"handle-revision", reactiveEngine.ActionPublish},
		{"handle-escalation", reactiveEngine.ActionPublish},
		{"handle-error", reactiveEngine.ActionPublish},
	}

	if len(def.Rules) != len(expectedRules) {
		t.Fatalf("expected %d rules, got %d", len(expectedRules), len(def.Rules))
	}

	for i, want := range expectedRules {
		rule := def.Rules[i]
		if rule.ID != want.id {
			t.Errorf("rule[%d]: expected ID %q, got %q", i, want.id, rule.ID)
		}
		if rule.Action.Type != want.actionType {
			t.Errorf("rule[%d] %q: expected action type %v, got %v",
				i, want.id, want.actionType, rule.Action.Type)
		}
	}

	if def.StateBucket != testStateBucket {
		t.Errorf("expected state bucket %q, got %q", testStateBucket, def.StateBucket)
	}
	if def.MaxIterations != 3 {
		t.Errorf("expected MaxIterations 3, got %d", def.MaxIterations)
	}
}

func TestPlanReviewWorkflow_StateFactory(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)

	got := def.StateFactory()
	if got == nil {
		t.Fatal("StateFactory returned nil")
	}
	_, ok := got.(*PlanReviewState)
	if !ok {
		t.Errorf("StateFactory returned wrong type: %T", got)
	}
}

// ---------------------------------------------------------------------------
// accept-trigger rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_AcceptTrigger(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &PlanReviewState{}
	trigger := &workflow.TriggerPayload{
		Slug:          "my-plan",
		Title:         "My Plan",
		Description:   "A test plan",
		ProjectID:     workflow.ProjectEntityID("default"),
		RequestID:     "req-123",
		TraceID:       "trace-abc",
		LoopID:        "loop-xyz",
		Role:          "architect",
		Prompt:        "Design a system",
		ScopePatterns: []string{"backend/**"},
		Auto:          true,
	}

	ctx := &reactiveEngine.RuleContext{
		State:   state,
		Message: trigger,
	}

	// Condition should always be true.
	if len(rule.Conditions) == 0 {
		t.Fatal("accept-trigger has no conditions")
	}
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("condition %q should be true for accept-trigger", cond.Description)
		}
	}

	// Apply mutator.
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Slug != "my-plan" {
		t.Errorf("expected Slug 'my-plan', got %q", state.Slug)
	}
	if state.Title != "My Plan" {
		t.Errorf("expected Title 'My Plan', got %q", state.Title)
	}
	if state.ProjectID != workflow.ProjectEntityID("default") {
		t.Errorf("expected ProjectID, got %q", state.ProjectID)
	}
	if state.RequestID != "req-123" {
		t.Errorf("expected RequestID 'req-123', got %q", state.RequestID)
	}
	if state.TraceID != "trace-abc" {
		t.Errorf("expected TraceID 'trace-abc', got %q", state.TraceID)
	}
	if state.Role != "architect" {
		t.Errorf("expected Role 'architect', got %q", state.Role)
	}
	if !state.Auto {
		t.Error("expected Auto to be true")
	}
	if len(state.ScopePatterns) != 1 || state.ScopePatterns[0] != "backend/**" {
		t.Errorf("expected ScopePatterns ['backend/**'], got %v", state.ScopePatterns)
	}
	if state.Phase != ReviewPhaseGenerating {
		t.Errorf("expected phase %q, got %q", ReviewPhaseGenerating, state.Phase)
	}
	if state.ID == "" {
		t.Error("expected state ID to be populated")
	}
	if state.WorkflowID != "plan-review-loop" {
		t.Errorf("expected WorkflowID 'plan-review-loop', got %q", state.WorkflowID)
	}
}

func TestPlanReviewWorkflow_AcceptTrigger_SecondTriggerPreservesID(t *testing.T) {
	// A second trigger on an already-initialised state must not reset the ID.
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &PlanReviewState{}
	state.ID = "plan-review.existing"
	state.WorkflowID = "plan-review-loop"

	trigger := &workflow.TriggerPayload{Slug: "existing", Title: "Existing"}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.ID != "plan-review.existing" {
		t.Errorf("ID should be preserved, got %q", state.ID)
	}
}

// ---------------------------------------------------------------------------
// generate rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_GenerateConditions(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-generator")

	t.Run("matches generating phase", func(t *testing.T) {
		state := generatingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match reviewing phase", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Phase = ReviewPhaseReviewing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match planning (dispatched) phase", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Phase = phases.PlanPlanning
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestPlanReviewWorkflow_GeneratePayload(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-generator")

	t.Run("first iteration uses direct prompt", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Prompt = "Design auth service"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		req, ok := payload.(*PlannerRequest)
		if !ok {
			t.Fatalf("expected *PlannerRequest, got %T", payload)
		}
		if req.Slug != "gen-001" {
			t.Errorf("expected Slug 'gen-001', got %q", req.Slug)
		}
		if req.Prompt != "Design auth service" {
			t.Errorf("expected Prompt 'Design auth service', got %q", req.Prompt)
		}
		if req.Revision {
			t.Error("expected Revision to be false on first iteration")
		}
	})

	t.Run("revision iteration includes reviewer feedback in prompt", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Iteration = 1
		state.Summary = "Missing error handling"
		state.FormattedFindings = "- No error handling in service layer"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		req, ok := payload.(*PlannerRequest)
		if !ok {
			t.Fatalf("expected *PlannerRequest, got %T", payload)
		}
		if !req.Revision {
			t.Error("expected Revision to be true on second iteration")
		}
		if !strings.Contains(req.Prompt, "REVISION REQUEST") {
			t.Errorf("expected revision prompt to contain 'REVISION REQUEST', got: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "Missing error handling") {
			t.Errorf("expected revision prompt to contain summary, got: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "No error handling in service layer") {
			t.Errorf("expected revision prompt to contain findings, got: %q", req.Prompt)
		}
	})
}

func TestPlanReviewWorkflow_DispatchGeneratorMutation(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-generator")

	t.Run("sets phase to planning (dispatched)", func(t *testing.T) {
		state := generatingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != phases.PlanPlanning {
			t.Errorf("expected phase 'planning', got %q", state.Phase)
		}
	})
}

func TestPlanReviewWorkflow_GeneratorCompletedConditions(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generator-completed")

	t.Run("matches planned phase", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Phase = phases.PlanPlanned
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match generating phase", func(t *testing.T) {
		state := generatingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match planning (dispatched) phase", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Phase = phases.PlanPlanning
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestPlanReviewWorkflow_GeneratorCompletedMutation(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generator-completed")

	t.Run("transitions to reviewing phase", func(t *testing.T) {
		state := generatingState("gen-001")
		state.Phase = phases.PlanPlanned
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseReviewing {
			t.Errorf("expected phase %q, got %q", ReviewPhaseReviewing, state.Phase)
		}
	})
}

// ---------------------------------------------------------------------------
// review rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_ReviewConditions(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-reviewer")

	t.Run("matches reviewing phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match generating phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		state.Phase = ReviewPhaseGenerating
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match reviewing_dispatched phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		state.Phase = phases.PlanReviewingDispatched
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestPlanReviewWorkflow_ReviewerPayload(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-reviewer")

	state := reviewingState("rev-001")
	state.PlanContent = json.RawMessage(`{"title":"Auth Plan"}`)
	state.ProjectID = workflow.ProjectEntityID("default")
	state.TraceID = "trace-123"
	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	req, ok := payload.(*PlanReviewRequest)
	if !ok {
		t.Fatalf("expected *PlanReviewRequest, got %T", payload)
	}
	if req.Slug != "rev-001" {
		t.Errorf("expected Slug 'rev-001', got %q", req.Slug)
	}
	if string(req.PlanContent) != `{"title":"Auth Plan"}` {
		t.Errorf("unexpected PlanContent: %s", req.PlanContent)
	}
	if req.ProjectID != workflow.ProjectEntityID("default") {
		t.Errorf("expected ProjectID, got %q", req.ProjectID)
	}
	if req.TraceID != "trace-123" {
		t.Errorf("expected TraceID 'trace-123', got %q", req.TraceID)
	}
}

func TestPlanReviewWorkflow_DispatchReviewerMutation(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-reviewer")

	t.Run("sets phase to reviewing_dispatched", func(t *testing.T) {
		state := reviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != phases.PlanReviewingDispatched {
			t.Errorf("expected phase 'reviewing_dispatched', got %q", state.Phase)
		}
	})
}

func TestPlanReviewWorkflow_ReviewerCompletedConditions(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "reviewer-completed")

	t.Run("matches reviewed phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		state.Phase = phases.PlanReviewed
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match reviewing phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match reviewing_dispatched phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		state.Phase = phases.PlanReviewingDispatched
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestPlanReviewWorkflow_ReviewerCompletedMutation(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "reviewer-completed")

	t.Run("transitions to evaluated phase", func(t *testing.T) {
		state := reviewingState("rev-001")
		state.Phase = phases.PlanReviewed
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseEvaluated {
			t.Errorf("expected phase %q, got %q", ReviewPhaseEvaluated, state.Phase)
		}
	})
}

// ---------------------------------------------------------------------------
// handle-approved rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_HandleApproved(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-approved")

	t.Run("conditions pass for approved evaluated state", func(t *testing.T) {
		state := evaluatedState("plan-001", "approved")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for non-approved verdict", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail for non-evaluated phase", func(t *testing.T) {
		state := evaluatedState("plan-001", "approved")
		state.Phase = ReviewPhaseReviewing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct approved event payload", func(t *testing.T) {
		state := evaluatedState("plan-001", "approved")
		state.Summary = "Plan is excellent"
		state.ReviewerLLMRequestIDs = []string{"llm-rev-99"}
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		approved, ok := payload.(*PlanApprovedPayload)
		if !ok {
			t.Fatalf("expected *PlanApprovedPayload, got %T", payload)
		}
		if approved.Slug != "plan-001" {
			t.Errorf("expected Slug 'plan-001', got %q", approved.Slug)
		}
		if approved.Verdict != "approved" {
			t.Errorf("expected Verdict 'approved', got %q", approved.Verdict)
		}
		if approved.Summary != "Plan is excellent" {
			t.Errorf("expected Summary, got %q", approved.Summary)
		}
		if len(approved.LLMRequestIDs) == 0 || approved.LLMRequestIDs[0] != "llm-rev-99" {
			t.Errorf("expected LLMRequestIDs, got %v", approved.LLMRequestIDs)
		}
	})
}

// ---------------------------------------------------------------------------
// handle-revision rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_HandleRevision(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-revision")

	t.Run("conditions pass for non-approved with iterations remaining", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 1
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for approved verdict", func(t *testing.T) {
		state := evaluatedState("plan-001", "approved")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail at max iterations", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct revision event payload", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 1
		state.Findings = json.RawMessage(`[{"issue":"X"}]`)
		state.ReviewerLLMRequestIDs = []string{"llm-rev-1"}
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		rev, ok := payload.(*PlanRevisionPayload)
		if !ok {
			t.Fatalf("expected *PlanRevisionPayload, got %T", payload)
		}
		if rev.Slug != "plan-001" {
			t.Errorf("expected Slug 'plan-001', got %q", rev.Slug)
		}
		if rev.Iteration != 1 {
			t.Errorf("expected Iteration 1, got %d", rev.Iteration)
		}
		if rev.Verdict != "needs_changes" {
			t.Errorf("expected Verdict 'needs_changes', got %q", rev.Verdict)
		}
	})

	t.Run("mutation increments iteration and resets to generating", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 0
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseGenerating {
			t.Errorf("expected phase %q, got %q", ReviewPhaseGenerating, state.Phase)
		}
		if state.Iteration != 1 {
			t.Errorf("expected iteration 1, got %d", state.Iteration)
		}
		if state.Verdict != "" {
			t.Errorf("expected Verdict to be cleared, got %q", state.Verdict)
		}
	})
}

// ---------------------------------------------------------------------------
// handle-escalation rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_HandleEscalation(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-escalation")

	t.Run("conditions pass for non-approved at max iterations", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail when iteration is still under max", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 2
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct escalation event payload", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 3
		state.FormattedFindings = "- Critical finding"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		esc, ok := payload.(*PlanEscalatePayload)
		if !ok {
			t.Fatalf("expected *PlanEscalatePayload, got %T", payload)
		}
		if esc.Slug != "plan-001" {
			t.Errorf("expected Slug 'plan-001', got %q", esc.Slug)
		}
		if esc.Iteration != 3 {
			t.Errorf("expected Iteration 3, got %d", esc.Iteration)
		}
		if esc.LastVerdict != "needs_changes" {
			t.Errorf("expected LastVerdict 'needs_changes', got %q", esc.LastVerdict)
		}
		if !strings.Contains(esc.FormattedFindings, "Critical finding") {
			t.Errorf("expected FormattedFindings to contain 'Critical finding'")
		}
	})

	t.Run("mutation marks execution as escalated", func(t *testing.T) {
		state := evaluatedState("plan-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Status != reactiveEngine.StatusEscalated {
			t.Errorf("expected StatusEscalated, got %v", state.Status)
		}
		if state.Error == "" {
			t.Error("expected Error to be set on escalation")
		}
	})
}

// ---------------------------------------------------------------------------
// handle-error rule tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_HandleError(t *testing.T) {
	def := BuildPlanReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-error")

	t.Run("conditions pass for generator_failed phase", func(t *testing.T) {
		state := failedState("plan-001", ReviewPhaseGeneratorFailed, "planner crashed")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions pass for reviewer_failed phase", func(t *testing.T) {
		state := failedState("plan-001", ReviewPhaseReviewerFailed, "reviewer timed out")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for non-failure phase", func(t *testing.T) {
		state := generatingState("plan-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct error event payload", func(t *testing.T) {
		state := failedState("plan-001", ReviewPhaseGeneratorFailed, "planner crashed")
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		errPayload, ok := payload.(*PlanErrorPayload)
		if !ok {
			t.Fatalf("expected *PlanErrorPayload, got %T", payload)
		}
		if errPayload.Slug != "plan-001" {
			t.Errorf("expected Slug 'plan-001', got %q", errPayload.Slug)
		}
		if errPayload.Error != "planner crashed" {
			t.Errorf("expected Error 'planner crashed', got %q", errPayload.Error)
		}
	})

	t.Run("mutation marks execution as failed", func(t *testing.T) {
		state := failedState("plan-001", ReviewPhaseGeneratorFailed, "timeout")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Status != reactiveEngine.StatusFailed {
			t.Errorf("expected StatusFailed, got %v", state.Status)
		}
	})
}

// ---------------------------------------------------------------------------
// Event payload JSON roundtrip tests
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_EventPayloadJSON(t *testing.T) {
	t.Run("PlanApprovedPayload roundtrip", func(t *testing.T) {
		original := &PlanApprovedPayload{
			PlanApprovedEvent: workflow.PlanApprovedEvent{
				Slug:          "my-plan",
				Verdict:       "approved",
				Summary:       "Good plan",
				LLMRequestIDs: []string{"req-1"},
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		// Wire format must match PlanApprovedEvent (no wrapper).
		var event workflow.PlanApprovedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal as PlanApprovedEvent failed: %v", err)
		}
		if event.Slug != "my-plan" || event.Verdict != "approved" {
			t.Errorf("roundtrip mismatch: %+v", event)
		}

		var restored PlanApprovedPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal back to PlanApprovedPayload failed: %v", err)
		}
		if restored.Slug != "my-plan" {
			t.Errorf("expected Slug 'my-plan', got %q", restored.Slug)
		}
	})

	t.Run("PlanRevisionPayload roundtrip", func(t *testing.T) {
		original := &PlanRevisionPayload{
			PlanRevisionNeededEvent: workflow.PlanRevisionNeededEvent{
				Slug:      "my-plan",
				Iteration: 2,
				Verdict:   "needs_changes",
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored PlanRevisionPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Iteration != 2 {
			t.Errorf("expected Iteration 2, got %d", restored.Iteration)
		}
	})

	t.Run("PlanEscalatePayload roundtrip", func(t *testing.T) {
		original := &PlanEscalatePayload{
			EscalationEvent: workflow.EscalationEvent{
				Slug:      "my-plan",
				Reason:    "max iterations exceeded",
				Iteration: 3,
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored PlanEscalatePayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Reason != "max iterations exceeded" {
			t.Errorf("expected Reason 'max iterations exceeded', got %q", restored.Reason)
		}
	})
}

// ---------------------------------------------------------------------------
// Integration tests using TestEngine
// ---------------------------------------------------------------------------

func TestPlanReviewWorkflow_HappyPath(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildPlanReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "plan-review.happy-plan"

	// Step 1: Trigger → generating phase.
	initial := &PlanReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "plan-review-loop",
			Phase:      ReviewPhaseGenerating,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:      "happy-plan",
		Title:     "Happy Path Plan",
		RequestID: "req-happy",
		ProjectID: workflow.ProjectEntityID("default"),
	}
	if err := engine.TriggerKV(context.Background(), key, initial); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseGenerating)
	engine.AssertStatus(key, reactiveEngine.StatusRunning)

	// Step 2: Simulate planner callback — transition to reviewing.
	state := &PlanReviewState{}
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	state.PlanContent = json.RawMessage(`{"steps":["design","implement"]}`)
	state.LLMRequestIDs = []string{"llm-gen-1"}
	state.Phase = ReviewPhaseReviewing
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (reviewing) failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseReviewing)

	// Step 3: Simulate reviewer callback — verdict approved.
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	state.Verdict = "approved"
	state.Summary = "Plan is solid"
	state.ReviewerLLMRequestIDs = []string{"llm-rev-1"}
	state.Phase = ReviewPhaseEvaluated
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (evaluated) failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseEvaluated)

	// Verify the handle-approved rule would fire: check conditions manually.
	approvedRule := findRule(t, def, "handle-approved")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx := &reactiveEngine.RuleContext{State: state}
	for _, cond := range approvedRule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("handle-approved condition %q should pass after approval", cond.Description)
		}
	}

	// Verify BuildPayload produces a PlanApprovedPayload.
	payload, err := approvedRule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	if _, ok := payload.(*PlanApprovedPayload); !ok {
		t.Errorf("expected *PlanApprovedPayload, got %T", payload)
	}
}

func TestPlanReviewWorkflow_RevisionThenApproved(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildPlanReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "plan-review.revision-plan"

	// Round 1: generating → reviewing → evaluated (needs_changes).
	state := &PlanReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "plan-review-loop",
			Phase:      ReviewPhaseGenerating,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug: "revision-plan",
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}

	// Reviewer says needs_changes.
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	state.Phase = ReviewPhaseEvaluated
	state.Verdict = "needs_changes"
	state.Summary = "Needs work"
	state.FormattedFindings = "- Missing tests"
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (evaluated) failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseEvaluated)

	// Apply handle-revision mutator manually to simulate the rule firing.
	revisionRule := findRule(t, def, "handle-revision")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx := &reactiveEngine.RuleContext{State: state}
	if err := revisionRule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("revision MutateState failed: %v", err)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (revision) failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseGenerating)
	engine.AssertIteration(key, 1)

	// Round 2: generating → reviewing → evaluated (approved).
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	state.Phase = ReviewPhaseEvaluated
	state.Verdict = "approved"
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (approved) failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseEvaluated)

	// Verify handle-approved now fires.
	approvedRule := findRule(t, def, "handle-approved")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx = &reactiveEngine.RuleContext{State: state}
	assertAllConditionsPass(t, approvedRule, ctx)
}

func TestPlanReviewWorkflow_MaxIterationsEscalated(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildPlanReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "plan-review.escalation-plan"

	// Simulate having reached max iterations still needing changes.
	state := &PlanReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "plan-review-loop",
			Phase:      ReviewPhaseEvaluated,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  3, // at the limit
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:              "escalation-plan",
		Verdict:           "needs_changes",
		Summary:           "Still not good enough",
		FormattedFindings: "- Multiple issues remain",
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseEvaluated)

	// handle-revision should NOT fire (iteration >= 3).
	revisionRule := findRule(t, def, "handle-revision")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx := &reactiveEngine.RuleContext{State: state}
	allPass := true
	for _, cond := range revisionRule.Conditions {
		if !cond.Evaluate(ctx) {
			allPass = false
			break
		}
	}
	if allPass {
		t.Error("handle-revision should NOT fire when iteration >= max")
	}

	// handle-escalation SHOULD fire.
	escalationRule := findRule(t, def, "handle-escalation")
	assertAllConditionsPass(t, escalationRule, ctx)

	// Apply escalation mutator.
	if err := escalationRule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("escalation MutateState failed: %v", err)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (escalated) failed: %v", err)
	}
	engine.AssertStatus(key, reactiveEngine.StatusEscalated)

	// Verify escalation payload.
	payload, err := escalationRule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	esc, ok := payload.(*PlanEscalatePayload)
	if !ok {
		t.Fatalf("expected *PlanEscalatePayload, got %T", payload)
	}
	if esc.Slug != "escalation-plan" {
		t.Errorf("expected Slug 'escalation-plan', got %q", esc.Slug)
	}
	if esc.Iteration != 3 {
		t.Errorf("expected Iteration 3, got %d", esc.Iteration)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// findRule returns the rule with the given ID, failing the test if not found.
func findRule(t *testing.T, def *reactiveEngine.Definition, id string) *reactiveEngine.RuleDef {
	t.Helper()
	for i := range def.Rules {
		if def.Rules[i].ID == id {
			return &def.Rules[i]
		}
	}
	t.Fatalf("rule %q not found in workflow %q", id, def.ID)
	return nil
}

// assertAllConditionsPass checks that every condition on the rule evaluates to true.
func assertAllConditionsPass(t *testing.T, rule *reactiveEngine.RuleDef, ctx *reactiveEngine.RuleContext) {
	t.Helper()
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("rule %q: condition %q should pass but failed", rule.ID, cond.Description)
		}
	}
}

// assertSomeConditionFails checks that at least one condition on the rule evaluates to false.
func assertSomeConditionFails(t *testing.T, rule *reactiveEngine.RuleDef, ctx *reactiveEngine.RuleContext) {
	t.Helper()
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			return // at least one failed — correct
		}
	}
	t.Errorf("rule %q: expected at least one condition to fail, but all passed", rule.ID)
}

// generatingState returns a PlanReviewState in the generating phase.
func generatingState(slug string) *PlanReviewState {
	return &PlanReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:        "plan-review." + slug,
			Phase:     ReviewPhaseGenerating,
			Status:    reactiveEngine.StatusRunning,
			Iteration: 0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Slug: slug,
	}
}

// reviewingState returns a PlanReviewState in the reviewing phase.
func reviewingState(slug string) *PlanReviewState {
	s := generatingState(slug)
	s.Phase = ReviewPhaseReviewing
	s.PlanContent = json.RawMessage(`{"title":"Draft"}`)
	return s
}

// evaluatedState returns a PlanReviewState in the evaluated phase with the given verdict.
func evaluatedState(slug, verdict string) *PlanReviewState {
	s := generatingState(slug)
	s.Phase = ReviewPhaseEvaluated
	s.Verdict = verdict
	s.Summary = "Review complete"
	return s
}

// failedState returns a PlanReviewState in a failure phase.
func failedState(slug, phase, errMsg string) *PlanReviewState {
	s := generatingState(slug)
	s.Phase = phase
	s.Error = errMsg
	return s
}
