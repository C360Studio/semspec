package reactive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	workflow "github.com/c360studio/semspec/workflow"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
	"github.com/c360studio/semstreams/processor/reactive/testutil"
)

// ---------------------------------------------------------------------------
// Definition-level tests
// ---------------------------------------------------------------------------

func TestPhaseReviewWorkflow_Definition(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)

	if def.ID != "phase-review-loop" {
		t.Errorf("expected ID 'phase-review-loop', got %q", def.ID)
	}

	expectedRules := []struct {
		id         string
		actionType reactiveEngine.ActionType
	}{
		{"accept-trigger", reactiveEngine.ActionMutate},
		{"generate", reactiveEngine.ActionPublishAsync},
		{"review", reactiveEngine.ActionPublishAsync},
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

func TestPhaseReviewWorkflow_StateFactory(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)

	got := def.StateFactory()
	if got == nil {
		t.Fatal("StateFactory returned nil")
	}
	_, ok := got.(*PhaseReviewState)
	if !ok {
		t.Errorf("StateFactory returned wrong type: %T", got)
	}
}

// ---------------------------------------------------------------------------
// accept-trigger rule tests
// ---------------------------------------------------------------------------

func TestPhaseReviewWorkflow_AcceptTrigger(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &PhaseReviewState{}
	trigger := &workflow.TriggerPayload{
		Slug:          "my-phases",
		Title:         "My Phases",
		Description:   "A test phases generation",
		ProjectID:     "semspec.local.project.default",
		RequestID:     "req-456",
		TraceID:       "trace-def",
		LoopID:        "loop-uvw",
		Role:          "architect",
		Prompt:        "Break this plan into phases",
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

	if state.Slug != "my-phases" {
		t.Errorf("expected Slug 'my-phases', got %q", state.Slug)
	}
	if state.Title != "My Phases" {
		t.Errorf("expected Title 'My Phases', got %q", state.Title)
	}
	if state.ProjectID != "semspec.local.project.default" {
		t.Errorf("expected ProjectID, got %q", state.ProjectID)
	}
	if state.RequestID != "req-456" {
		t.Errorf("expected RequestID 'req-456', got %q", state.RequestID)
	}
	if state.TraceID != "trace-def" {
		t.Errorf("expected TraceID 'trace-def', got %q", state.TraceID)
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
	if state.WorkflowID != "phase-review-loop" {
		t.Errorf("expected WorkflowID 'phase-review-loop', got %q", state.WorkflowID)
	}
}

func TestPhaseReviewWorkflow_AcceptTrigger_SecondTriggerPreservesID(t *testing.T) {
	// A second trigger on an already-initialised state must not reset the ID.
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &PhaseReviewState{}
	state.ID = "phase-review.existing"
	state.WorkflowID = "phase-review-loop"

	trigger := &workflow.TriggerPayload{Slug: "existing", Title: "Existing"}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.ID != "phase-review.existing" {
		t.Errorf("ID should be preserved, got %q", state.ID)
	}
}

// ---------------------------------------------------------------------------
// generate rule tests
// ---------------------------------------------------------------------------

func TestPhaseReviewWorkflow_GenerateConditions(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generate")

	t.Run("matches generating phase with no pending task", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match reviewing phase", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		state.Phase = ReviewPhaseReviewing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match when pending task exists", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		state.PendingTaskID = "task-xyz"
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestPhaseReviewWorkflow_GeneratePayload(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generate")

	t.Run("first iteration uses direct prompt", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		state.Prompt = "Break this plan into implementation phases"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		req, ok := payload.(*PhaseGeneratorRequest)
		if !ok {
			t.Fatalf("expected *PhaseGeneratorRequest, got %T", payload)
		}
		if req.Slug != "gen-001" {
			t.Errorf("expected Slug 'gen-001', got %q", req.Slug)
		}
		if req.Prompt != "Break this plan into implementation phases" {
			t.Errorf("expected Prompt, got %q", req.Prompt)
		}
		if req.Revision {
			t.Error("expected Revision to be false on first iteration")
		}
	})

	t.Run("revision iteration includes reviewer feedback in prompt", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		state.Iteration = 1
		state.Summary = "Phases are too coarse-grained"
		state.FormattedFindings = "- Phase 2 needs to be split into smaller chunks"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		req, ok := payload.(*PhaseGeneratorRequest)
		if !ok {
			t.Fatalf("expected *PhaseGeneratorRequest, got %T", payload)
		}
		if !req.Revision {
			t.Error("expected Revision to be true on second iteration")
		}
		if !strings.Contains(req.Prompt, "REVISION REQUEST") {
			t.Errorf("expected revision prompt to contain 'REVISION REQUEST', got: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "Phases are too coarse-grained") {
			t.Errorf("expected revision prompt to contain summary, got: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "Phase 2 needs to be split") {
			t.Errorf("expected revision prompt to contain findings, got: %q", req.Prompt)
		}
	})
}

func TestPhaseReviewWorkflow_GeneratorResultMutation(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generate")

	t.Run("success transitions to reviewing", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		result := &PhaseGeneratorResult{
			RequestID:     "req-1",
			Slug:          "gen-001",
			Phases:        json.RawMessage(`[{"name":"phase-1"},{"name":"phase-2"}]`),
			PhaseCount:    2,
			LLMRequestIDs: []string{"llm-req-1"},
		}

		if err := rule.Action.MutateState(ctx, result); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseReviewing {
			t.Errorf("expected phase %q, got %q", ReviewPhaseReviewing, state.Phase)
		}
		if string(state.PhasesContent) != `[{"name":"phase-1"},{"name":"phase-2"}]` {
			t.Errorf("unexpected PhasesContent: %s", state.PhasesContent)
		}
		if state.PhaseCount != 2 {
			t.Errorf("expected PhaseCount 2, got %d", state.PhaseCount)
		}
		if len(state.LLMRequestIDs) == 0 || state.LLMRequestIDs[0] != "llm-req-1" {
			t.Errorf("expected LLMRequestIDs ['llm-req-1'], got %v", state.LLMRequestIDs)
		}
	})

	t.Run("wrong result type transitions to generator_failed", func(t *testing.T) {
		state := phaseGeneratingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		// Pass a mismatched result type.
		if err := rule.Action.MutateState(ctx, &ReviewResult{}); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseGeneratorFailed {
			t.Errorf("expected phase %q, got %q", ReviewPhaseGeneratorFailed, state.Phase)
		}
		if state.Error == "" {
			t.Error("expected Error to be set")
		}
	})
}

// ---------------------------------------------------------------------------
// review rule tests
// ---------------------------------------------------------------------------

func TestPhaseReviewWorkflow_ReviewConditions(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "review")

	t.Run("matches reviewing phase with no pending task", func(t *testing.T) {
		state := phaseReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match generating phase", func(t *testing.T) {
		state := phaseReviewingState("rev-001")
		state.Phase = ReviewPhaseGenerating
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match when pending task exists", func(t *testing.T) {
		state := phaseReviewingState("rev-001")
		state.PendingTaskID = "task-abc"
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestPhaseReviewWorkflow_ReviewerPayload(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "review")

	state := phaseReviewingState("rev-001")
	state.PhasesContent = json.RawMessage(`[{"name":"phase-1"}]`)
	state.ProjectID = "semspec.local.project.default"
	state.TraceID = "trace-789"
	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	req, ok := payload.(*PhaseReviewRequest)
	if !ok {
		t.Fatalf("expected *PhaseReviewRequest, got %T", payload)
	}
	if req.Slug != "rev-001" {
		t.Errorf("expected Slug 'rev-001', got %q", req.Slug)
	}
	if string(req.PlanContent) != `[{"name":"phase-1"}]` {
		t.Errorf("unexpected PlanContent: %s", req.PlanContent)
	}
	if req.ProjectID != "semspec.local.project.default" {
		t.Errorf("expected ProjectID, got %q", req.ProjectID)
	}
	if req.TraceID != "trace-789" {
		t.Errorf("expected TraceID 'trace-789', got %q", req.TraceID)
	}
}

func TestPhaseReviewWorkflow_ReviewerResultMutation(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "review")

	t.Run("approved verdict transitions to evaluated", func(t *testing.T) {
		state := phaseReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		result := &ReviewResult{
			RequestID:         "req-1",
			Slug:              "rev-001",
			Verdict:           "approved",
			Summary:           "Phases look good",
			Findings:          json.RawMessage(`[]`),
			FormattedFindings: "",
			LLMRequestIDs:     []string{"llm-rev-1"},
		}

		if err := rule.Action.MutateState(ctx, result); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseEvaluated {
			t.Errorf("expected phase %q, got %q", ReviewPhaseEvaluated, state.Phase)
		}
		if state.Verdict != "approved" {
			t.Errorf("expected Verdict 'approved', got %q", state.Verdict)
		}
		if state.Summary != "Phases look good" {
			t.Errorf("expected Summary 'Phases look good', got %q", state.Summary)
		}
	})

	t.Run("needs_changes verdict transitions to evaluated", func(t *testing.T) {
		state := phaseReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		result := &ReviewResult{
			Verdict:           "needs_changes",
			Summary:           "Phases are too broad",
			Findings:          json.RawMessage(`[{"issue":"phases too coarse"}]`),
			FormattedFindings: "- Phases are too broad",
		}

		if err := rule.Action.MutateState(ctx, result); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseEvaluated {
			t.Errorf("expected phase %q, got %q", ReviewPhaseEvaluated, state.Phase)
		}
		if state.Verdict != "needs_changes" {
			t.Errorf("expected Verdict 'needs_changes', got %q", state.Verdict)
		}
		if state.FormattedFindings != "- Phases are too broad" {
			t.Errorf("unexpected FormattedFindings: %q", state.FormattedFindings)
		}
	})

	t.Run("wrong result type transitions to reviewer_failed", func(t *testing.T) {
		state := phaseReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, &PhaseGeneratorResult{}); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseReviewerFailed {
			t.Errorf("expected phase %q, got %q", ReviewPhaseReviewerFailed, state.Phase)
		}
		if state.Error == "" {
			t.Error("expected Error to be set")
		}
	})
}

// ---------------------------------------------------------------------------
// handle-approved rule tests
// ---------------------------------------------------------------------------

func TestPhaseReviewWorkflow_HandleApproved(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-approved")

	t.Run("conditions pass for approved evaluated state", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "approved")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for non-approved verdict", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail for non-evaluated phase", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "approved")
		state.Phase = ReviewPhaseReviewing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct approved event payload", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "approved")
		state.Summary = "Phases are excellent"
		state.Findings = json.RawMessage(`[]`)
		state.ReviewerLLMRequestIDs = []string{"llm-rev-99"}
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		approved, ok := payload.(*PhasesApprovedPayload)
		if !ok {
			t.Fatalf("expected *PhasesApprovedPayload, got %T", payload)
		}
		if approved.Slug != "phases-001" {
			t.Errorf("expected Slug 'phases-001', got %q", approved.Slug)
		}
		if approved.Verdict != "approved" {
			t.Errorf("expected Verdict 'approved', got %q", approved.Verdict)
		}
		if approved.Summary != "Phases are excellent" {
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

func TestPhaseReviewWorkflow_HandleRevision(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-revision")

	t.Run("conditions pass for non-approved with iterations remaining", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		state.Iteration = 1
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for approved verdict", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "approved")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail at max iterations", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct revision event payload", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		state.Iteration = 1
		state.Findings = json.RawMessage(`[{"issue":"too broad"}]`)
		state.FormattedFindings = "- Phases too broad"
		state.ReviewerLLMRequestIDs = []string{"llm-rev-1"}
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		rev, ok := payload.(*PhasesRevisionPayload)
		if !ok {
			t.Fatalf("expected *PhasesRevisionPayload, got %T", payload)
		}
		if rev.Slug != "phases-001" {
			t.Errorf("expected Slug 'phases-001', got %q", rev.Slug)
		}
		if rev.Iteration != 1 {
			t.Errorf("expected Iteration 1, got %d", rev.Iteration)
		}
		if rev.Verdict != "needs_changes" {
			t.Errorf("expected Verdict 'needs_changes', got %q", rev.Verdict)
		}
		if rev.FormattedFindings != "- Phases too broad" {
			t.Errorf("expected FormattedFindings, got %q", rev.FormattedFindings)
		}
	})

	t.Run("mutation increments iteration and resets to generating", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
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

func TestPhaseReviewWorkflow_HandleEscalation(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-escalation")

	t.Run("conditions pass for non-approved at max iterations", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail when iteration is still under max", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		state.Iteration = 2
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct escalation event payload", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
		state.Iteration = 3
		state.FormattedFindings = "- Phases still too coarse"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		esc, ok := payload.(*PhaseEscalatePayload)
		if !ok {
			t.Fatalf("expected *PhaseEscalatePayload, got %T", payload)
		}
		if esc.Slug != "phases-001" {
			t.Errorf("expected Slug 'phases-001', got %q", esc.Slug)
		}
		if esc.Iteration != 3 {
			t.Errorf("expected Iteration 3, got %d", esc.Iteration)
		}
		if esc.LastVerdict != "needs_changes" {
			t.Errorf("expected LastVerdict 'needs_changes', got %q", esc.LastVerdict)
		}
		if !strings.Contains(esc.FormattedFindings, "Phases still too coarse") {
			t.Errorf("expected FormattedFindings to contain 'Phases still too coarse'")
		}
		if esc.Reason != "max phase review iterations exceeded" {
			t.Errorf("expected escalation reason, got %q", esc.Reason)
		}
	})

	t.Run("mutation marks execution as escalated", func(t *testing.T) {
		state := phaseEvaluatedState("phases-001", "needs_changes")
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

func TestPhaseReviewWorkflow_HandleError(t *testing.T) {
	def := BuildPhaseReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-error")

	t.Run("conditions pass for generator_failed phase", func(t *testing.T) {
		state := phaseFailedState("phases-001", ReviewPhaseGeneratorFailed, "generator crashed")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions pass for reviewer_failed phase", func(t *testing.T) {
		state := phaseFailedState("phases-001", ReviewPhaseReviewerFailed, "reviewer timed out")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for non-failure phase", func(t *testing.T) {
		state := phaseGeneratingState("phases-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct error event payload", func(t *testing.T) {
		state := phaseFailedState("phases-001", ReviewPhaseGeneratorFailed, "generator crashed")
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		errPayload, ok := payload.(*PhaseErrorPayload)
		if !ok {
			t.Fatalf("expected *PhaseErrorPayload, got %T", payload)
		}
		if errPayload.Slug != "phases-001" {
			t.Errorf("expected Slug 'phases-001', got %q", errPayload.Slug)
		}
		if errPayload.Error != "generator crashed" {
			t.Errorf("expected Error 'generator crashed', got %q", errPayload.Error)
		}
	})

	t.Run("mutation marks execution as failed", func(t *testing.T) {
		state := phaseFailedState("phases-001", ReviewPhaseGeneratorFailed, "timeout")
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

func TestPhaseReviewWorkflow_EventPayloadJSON(t *testing.T) {
	t.Run("PhasesApprovedPayload roundtrip", func(t *testing.T) {
		original := &PhasesApprovedPayload{
			PhasesApprovedEvent: workflow.PhasesApprovedEvent{
				Slug:          "my-phases",
				Verdict:       "approved",
				Summary:       "Good phases",
				LLMRequestIDs: []string{"req-1"},
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		// Wire format must match PhasesApprovedEvent (no wrapper).
		var event workflow.PhasesApprovedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal as PhasesApprovedEvent failed: %v", err)
		}
		if event.Slug != "my-phases" || event.Verdict != "approved" {
			t.Errorf("roundtrip mismatch: %+v", event)
		}

		var restored PhasesApprovedPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal back to PhasesApprovedPayload failed: %v", err)
		}
		if restored.Slug != "my-phases" {
			t.Errorf("expected Slug 'my-phases', got %q", restored.Slug)
		}
	})

	t.Run("PhasesRevisionPayload roundtrip", func(t *testing.T) {
		original := &PhasesRevisionPayload{
			PhasesRevisionNeededEvent: workflow.PhasesRevisionNeededEvent{
				Slug:              "my-phases",
				Iteration:         2,
				Verdict:           "needs_changes",
				FormattedFindings: "- Too broad",
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored PhasesRevisionPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Iteration != 2 {
			t.Errorf("expected Iteration 2, got %d", restored.Iteration)
		}
		if restored.FormattedFindings != "- Too broad" {
			t.Errorf("expected FormattedFindings '- Too broad', got %q", restored.FormattedFindings)
		}
	})

	t.Run("PhaseEscalatePayload roundtrip", func(t *testing.T) {
		original := &PhaseEscalatePayload{
			EscalationEvent: workflow.EscalationEvent{
				Slug:      "my-phases",
				Reason:    "max phase review iterations exceeded",
				Iteration: 3,
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored PhaseEscalatePayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Reason != "max phase review iterations exceeded" {
			t.Errorf("expected Reason, got %q", restored.Reason)
		}
		if restored.Iteration != 3 {
			t.Errorf("expected Iteration 3, got %d", restored.Iteration)
		}
	})

	t.Run("PhaseErrorPayload roundtrip", func(t *testing.T) {
		original := &PhaseErrorPayload{
			UserSignalErrorEvent: workflow.UserSignalErrorEvent{
				Slug:  "my-phases",
				Error: "phase generator timed out",
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored PhaseErrorPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Error != "phase generator timed out" {
			t.Errorf("expected Error 'phase generator timed out', got %q", restored.Error)
		}
	})
}

// ---------------------------------------------------------------------------
// Integration tests using TestEngine
// ---------------------------------------------------------------------------

func TestPhaseReviewWorkflow_HappyPath(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildPhaseReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "phase-review.happy-phases"

	// Step 1: Trigger → generating phase.
	initial := &PhaseReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "phase-review-loop",
			Phase:      ReviewPhaseGenerating,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:      "happy-phases",
		Title:     "Happy Path Phases",
		RequestID: "req-happy",
		ProjectID: "semspec.local.project.default",
	}
	if err := engine.TriggerKV(context.Background(), key, initial); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseGenerating)
	engine.AssertStatus(key, reactiveEngine.StatusRunning)

	// Step 2: Simulate phase generator callback — transition to reviewing.
	state := &PhaseReviewState{}
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	state.PhasesContent = json.RawMessage(`[{"name":"setup"},{"name":"implement"}]`)
	state.PhaseCount = 2
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
	state.Summary = "Phases are solid"
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

	// Verify BuildPayload produces a PhasesApprovedPayload.
	payload, err := approvedRule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	if _, ok := payload.(*PhasesApprovedPayload); !ok {
		t.Errorf("expected *PhasesApprovedPayload, got %T", payload)
	}
}

func TestPhaseReviewWorkflow_RevisionThenApproved(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildPhaseReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "phase-review.revision-phases"

	// Round 1: generating → reviewing → evaluated (needs_changes).
	state := &PhaseReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "phase-review-loop",
			Phase:      ReviewPhaseGenerating,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug: "revision-phases",
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
	state.Summary = "Phases need refinement"
	state.FormattedFindings = "- Phase boundaries unclear"
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

func TestPhaseReviewWorkflow_MaxIterationsEscalated(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildPhaseReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "phase-review.escalation-phases"

	// Simulate having reached max iterations still needing changes.
	state := &PhaseReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "phase-review-loop",
			Phase:      ReviewPhaseEvaluated,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  3, // at the limit
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:              "escalation-phases",
		Verdict:           "needs_changes",
		Summary:           "Still not granular enough",
		FormattedFindings: "- Multiple phase issues remain",
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
	esc, ok := payload.(*PhaseEscalatePayload)
	if !ok {
		t.Fatalf("expected *PhaseEscalatePayload, got %T", payload)
	}
	if esc.Slug != "escalation-phases" {
		t.Errorf("expected Slug 'escalation-phases', got %q", esc.Slug)
	}
	if esc.Iteration != 3 {
		t.Errorf("expected Iteration 3, got %d", esc.Iteration)
	}
}

// ---------------------------------------------------------------------------
// Test helpers specific to phase-review-loop
// ---------------------------------------------------------------------------

// phaseGeneratingState returns a PhaseReviewState in the generating phase.
func phaseGeneratingState(slug string) *PhaseReviewState {
	return &PhaseReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:        "phase-review." + slug,
			Phase:     ReviewPhaseGenerating,
			Status:    reactiveEngine.StatusRunning,
			Iteration: 0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Slug: slug,
	}
}

// phaseReviewingState returns a PhaseReviewState in the reviewing phase.
func phaseReviewingState(slug string) *PhaseReviewState {
	s := phaseGeneratingState(slug)
	s.Phase = ReviewPhaseReviewing
	s.PhasesContent = json.RawMessage(`[{"name":"phase-1"}]`)
	return s
}

// phaseEvaluatedState returns a PhaseReviewState in the evaluated phase with the given verdict.
func phaseEvaluatedState(slug, verdict string) *PhaseReviewState {
	s := phaseGeneratingState(slug)
	s.Phase = ReviewPhaseEvaluated
	s.Verdict = verdict
	s.Summary = "Phase review complete"
	return s
}

// phaseFailedState returns a PhaseReviewState in a failure phase.
func phaseFailedState(slug, phase, errMsg string) *PhaseReviewState {
	s := phaseGeneratingState(slug)
	s.Phase = phase
	s.Error = errMsg
	return s
}
