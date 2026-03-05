package reactive

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
	"github.com/c360studio/semstreams/processor/reactive/testutil"
)

// ---------------------------------------------------------------------------
// Definition-level tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_Definition(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	if def.ID != CoordinationLoopWorkflowID {
		t.Errorf("expected ID %q, got %q", CoordinationLoopWorkflowID, def.ID)
	}
	if def.ID != "coordination-loop" {
		t.Errorf("expected literal ID 'coordination-loop', got %q", def.ID)
	}

	expectedRules := []struct {
		id         string
		actionType reactiveEngine.ActionType
	}{
		{"accept-trigger", reactiveEngine.ActionMutate},
		{"dispatch-focus", reactiveEngine.ActionPublish},
		{"planner-result", reactiveEngine.ActionMutate},
		{"dispatch-synthesis", reactiveEngine.ActionPublish},
		{"coordination-complete", reactiveEngine.ActionComplete},
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

	// Coordination loop is single-pass — no iteration loop.
	if def.MaxIterations != 1 {
		t.Errorf("expected MaxIterations 1 (single-pass), got %d", def.MaxIterations)
	}
}

func TestCoordinationLoopWorkflow_StateFactory(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	got := def.StateFactory()
	if got == nil {
		t.Fatal("StateFactory returned nil")
	}
	_, ok := got.(*CoordinationState)
	if !ok {
		t.Errorf("StateFactory returned wrong type: %T", got)
	}
}

// ---------------------------------------------------------------------------
// accept-trigger rule tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_AcceptTrigger(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &CoordinationState{}
	trigger := &PlanCoordinatorRequest{
		Slug:        "coord-plan",
		Title:       "Coordination Plan",
		Description: "A test coordination",
		RequestID:   "req-coord-1",
		TraceID:     "trace-coord",
		LoopID:      "loop-coord",
		FocusAreas:  []string{"backend", "api"},
		MaxPlanners: 2,
		ProjectID:   workflow.ProjectEntityID("default"),
	}

	ctx := &reactiveEngine.RuleContext{
		State:   state,
		Message: trigger,
	}

	// accept-trigger uses Always() — all conditions must pass.
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("condition %q should be true for accept-trigger", cond.Description)
		}
	}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Slug != "coord-plan" {
		t.Errorf("expected Slug 'coord-plan', got %q", state.Slug)
	}
	if state.Title != "Coordination Plan" {
		t.Errorf("expected Title 'Coordination Plan', got %q", state.Title)
	}
	if state.Description != "A test coordination" {
		t.Errorf("expected Description 'A test coordination', got %q", state.Description)
	}
	if state.RequestID != "req-coord-1" {
		t.Errorf("expected RequestID 'req-coord-1', got %q", state.RequestID)
	}
	if state.TraceID != "trace-coord" {
		t.Errorf("expected TraceID 'trace-coord', got %q", state.TraceID)
	}
	if state.LoopID != "loop-coord" {
		t.Errorf("expected LoopID 'loop-coord', got %q", state.LoopID)
	}
	if state.MaxPlanners != 2 {
		t.Errorf("expected MaxPlanners 2, got %d", state.MaxPlanners)
	}
	if state.ProjectID != workflow.ProjectEntityID("default") {
		t.Errorf("expected ProjectID, got %q", state.ProjectID)
	}
	if len(state.FocusAreas) != 2 || state.FocusAreas[0] != "backend" {
		t.Errorf("expected FocusAreas ['backend','api'], got %v", state.FocusAreas)
	}
	if state.Phase != phases.CoordinationFocusing {
		t.Errorf("expected phase %q, got %q", phases.CoordinationFocusing, state.Phase)
	}
	if state.ID != "coordination.coord-plan" {
		t.Errorf("expected ID 'coordination.coord-plan', got %q", state.ID)
	}
	if state.WorkflowID != CoordinationLoopWorkflowID {
		t.Errorf("expected WorkflowID %q, got %q", CoordinationLoopWorkflowID, state.WorkflowID)
	}
	if state.Status != reactiveEngine.StatusRunning {
		t.Errorf("expected StatusRunning, got %v", state.Status)
	}
	if state.PlannerResults == nil {
		t.Error("expected PlannerResults map to be initialized")
	}
}

func TestCoordinationLoopWorkflow_AcceptTrigger_SecondTriggerPreservesID(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &CoordinationState{}
	state.ID = "coordination.existing"
	state.WorkflowID = CoordinationLoopWorkflowID

	trigger := &PlanCoordinatorRequest{
		Slug:      "existing",
		Title:     "Existing",
		RequestID: "req-existing",
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	// ID must not be reset on a re-trigger.
	if state.ID != "coordination.existing" {
		t.Errorf("ID should be preserved on re-trigger, got %q", state.ID)
	}
}

func TestCoordinationLoopWorkflow_AcceptTrigger_WrongMessageType(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &CoordinationState{}
	ctx := &reactiveEngine.RuleContext{
		State:   state,
		Message: &PlannerRequest{Slug: "wrong-type"},
	}

	if err := rule.Action.MutateState(ctx, nil); err == nil {
		t.Error("expected error for wrong message type, got nil")
	}
}

func TestCoordinationLoopWorkflow_AcceptTrigger_WrongStateType(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	// Wrong state type should produce an error.
	ctx := &reactiveEngine.RuleContext{
		State:   &PlanReviewState{},
		Message: &PlanCoordinatorRequest{Slug: "test", RequestID: "req"},
	}

	if err := rule.Action.MutateState(ctx, nil); err == nil {
		t.Error("expected error for wrong state type, got nil")
	}
}

// ---------------------------------------------------------------------------
// dispatch-focus rule tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_DispatchFocusConditions(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-focus")

	t.Run("matches focusing phase", func(t *testing.T) {
		state := coordinationFocusingState("focus-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match focus_dispatched phase", func(t *testing.T) {
		state := coordinationFocusingState("focus-001")
		state.Phase = phases.CoordinationFocusDispatched
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match synthesizing phase", func(t *testing.T) {
		state := coordinationFocusingState("focus-001")
		state.Phase = phases.CoordinationSynthesizing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match planners_failed phase", func(t *testing.T) {
		state := coordinationFocusingState("focus-001")
		state.Phase = phases.CoordinationPlannersFailed
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestCoordinationLoopWorkflow_DispatchFocusPayload(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-focus")

	state := coordinationFocusingState("focus-001")
	state.Title = "Focus Title"
	state.Description = "Focus Desc"
	state.FocusAreas = []string{"domain-a", "domain-b"}
	state.MaxPlanners = 2
	state.ProjectID = workflow.ProjectEntityID("default")
	state.TraceID = "trace-focus"
	state.LoopID = "loop-focus"
	state.RequestID = "req-focus"
	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	req, ok := payload.(*PlanCoordinatorRequest)
	if !ok {
		t.Fatalf("expected *PlanCoordinatorRequest, got %T", payload)
	}
	if req.ExecutionID != state.ID {
		t.Errorf("expected ExecutionID %q, got %q", state.ID, req.ExecutionID)
	}
	if req.Slug != "focus-001" {
		t.Errorf("expected Slug 'focus-001', got %q", req.Slug)
	}
	if req.Title != "Focus Title" {
		t.Errorf("expected Title 'Focus Title', got %q", req.Title)
	}
	if req.Description != "Focus Desc" {
		t.Errorf("expected Description 'Focus Desc', got %q", req.Description)
	}
	if req.MaxPlanners != 2 {
		t.Errorf("expected MaxPlanners 2, got %d", req.MaxPlanners)
	}
	if req.TraceID != "trace-focus" {
		t.Errorf("expected TraceID 'trace-focus', got %q", req.TraceID)
	}
	if req.LoopID != "loop-focus" {
		t.Errorf("expected LoopID 'loop-focus', got %q", req.LoopID)
	}
}

func TestCoordinationLoopWorkflow_DispatchFocusMutation(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-focus")

	state := coordinationFocusingState("focus-001")
	ctx := &reactiveEngine.RuleContext{State: state}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}
	if state.Phase != phases.CoordinationFocusDispatched {
		t.Errorf("expected phase %q, got %q", phases.CoordinationFocusDispatched, state.Phase)
	}
}

// ---------------------------------------------------------------------------
// planner-result rule tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_PlannerResultConditions(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	t.Run("always condition passes for any state", func(t *testing.T) {
		state := coordinationPlannersDispatchedState("planners-001", 2)
		ctx := &reactiveEngine.RuleContext{
			State:   state,
			Message: &CoordinationPlannerResult{ExecutionID: state.ID, PlannerID: "p1", Slug: "planners-001", Status: "completed"},
		}
		assertAllConditionsPass(t, rule, ctx)
	})
}

func TestCoordinationLoopWorkflow_PlannerResultMutation_SingleResult(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	// Two planners expected; only one result arrives — should stay in planners_dispatched.
	state := coordinationPlannersDispatchedState("merge-001", 2)
	result := &CoordinationPlannerResult{
		ExecutionID:  state.ID,
		PlannerID:    "planner-a",
		FocusArea:    "backend",
		Status:       "completed",
		Result:       json.RawMessage(`{"plan":"backend plan"}`),
		LLMRequestID: "llm-a",
	}

	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	// One result recorded, but not all done — phase should not advance.
	if len(state.PlannerResults) != 1 {
		t.Errorf("expected 1 planner result, got %d", len(state.PlannerResults))
	}
	if state.PlannerResults["planner-a"] == nil {
		t.Error("expected planner-a result to be stored")
	}
	if state.Phase != phases.CoordinationPlannersDispatched {
		t.Errorf("expected phase to stay %q, got %q", phases.CoordinationPlannersDispatched, state.Phase)
	}
	// LLM request ID should be accumulated.
	if len(state.LLMRequestIDs) != 1 || state.LLMRequestIDs[0] != "llm-a" {
		t.Errorf("expected LLMRequestIDs ['llm-a'], got %v", state.LLMRequestIDs)
	}
}

func TestCoordinationLoopWorkflow_PlannerResultMutation_AllCompleted_TransitionsToSynthesizing(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	// Two planners; one result already stored.
	state := coordinationPlannersDispatchedState("all-done-001", 2)
	state.PlannerResults["planner-a"] = &PlannerOutcome{
		PlannerID: "planner-a",
		FocusArea: "backend",
		Status:    "completed",
	}
	state.LLMRequestIDs = []string{"llm-a"}

	// Second and final result arrives.
	result := &CoordinationPlannerResult{
		ExecutionID:  state.ID,
		PlannerID:    "planner-b",
		FocusArea:    "api",
		Status:       "completed",
		Result:       json.RawMessage(`{"plan":"api plan"}`),
		LLMRequestID: "llm-b",
	}

	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if len(state.PlannerResults) != 2 {
		t.Errorf("expected 2 planner results, got %d", len(state.PlannerResults))
	}
	if state.Phase != phases.CoordinationSynthesizing {
		t.Errorf("expected phase %q after all planners done, got %q", phases.CoordinationSynthesizing, state.Phase)
	}
	if len(state.LLMRequestIDs) != 2 {
		t.Errorf("expected 2 LLM request IDs accumulated, got %v", state.LLMRequestIDs)
	}
	if state.Error != "" {
		t.Errorf("expected no error when at least one planner succeeded, got %q", state.Error)
	}
}

func TestCoordinationLoopWorkflow_PlannerResultMutation_AllFailed_TransitionsToPlannersFailed(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	state := coordinationPlannersDispatchedState("all-failed-001", 2)
	state.PlannerResults["planner-a"] = &PlannerOutcome{
		PlannerID: "planner-a",
		Status:    "failed",
		Error:     "timeout",
	}

	// Final result — also a failure.
	result := &CoordinationPlannerResult{
		ExecutionID: state.ID,
		PlannerID:   "planner-b",
		FocusArea:   "api",
		Status:      "failed",
		Error:       "LLM error",
	}

	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Phase != phases.CoordinationPlannersFailed {
		t.Errorf("expected phase %q when all planners fail, got %q", phases.CoordinationPlannersFailed, state.Phase)
	}
	if state.Error == "" {
		t.Error("expected Error to be set when all planners fail")
	}
}

func TestCoordinationLoopWorkflow_PlannerResultMutation_MixedResults_TransitionsToSynthesizing(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	// Mixed: one failed, one succeeded — should proceed to synthesizing.
	state := coordinationPlannersDispatchedState("mixed-001", 2)
	state.PlannerResults["planner-a"] = &PlannerOutcome{
		PlannerID: "planner-a",
		Status:    "failed",
		Error:     "LLM crashed",
	}

	result := &CoordinationPlannerResult{
		ExecutionID: state.ID,
		PlannerID:   "planner-b",
		FocusArea:   "api",
		Status:      "completed",
		Result:      json.RawMessage(`{"plan":"partial plan"}`),
	}

	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	// At least one succeeded — should synthesize.
	if state.Phase != phases.CoordinationSynthesizing {
		t.Errorf("expected phase %q (partial success is sufficient), got %q", phases.CoordinationSynthesizing, state.Phase)
	}
}

func TestCoordinationLoopWorkflow_PlannerResultMutation_NoPlannerCount_DoesNotTransition(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	// PlannerCount=0 means allPlannersDone() always returns false.
	state := coordinationPlannersDispatchedState("zero-count-001", 0)

	result := &CoordinationPlannerResult{
		ExecutionID: state.ID,
		PlannerID:   "planner-a",
		Status:      "completed",
	}

	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	// PlannerCount=0 means we never consider all planners done.
	if state.Phase == phases.CoordinationSynthesizing {
		t.Error("should not transition to synthesizing when PlannerCount is 0")
	}
}

func TestCoordinationLoopWorkflow_PlannerResultMutation_WrongMessageType(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "planner-result")

	state := coordinationPlannersDispatchedState("wrong-type", 1)
	ctx := &reactiveEngine.RuleContext{
		State:   state,
		Message: &PlannerRequest{Slug: "wrong"},
	}

	if err := rule.Action.MutateState(ctx, nil); err == nil {
		t.Error("expected error for wrong message type, got nil")
	}
}

// ---------------------------------------------------------------------------
// dispatch-synthesis rule tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_DispatchSynthesisConditions(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-synthesis")

	t.Run("matches synthesizing phase", func(t *testing.T) {
		state := coordinationSynthesizingState("synth-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match synthesis_dispatched phase", func(t *testing.T) {
		state := coordinationSynthesizingState("synth-001")
		state.Phase = phases.CoordinationSynthesisDispatched
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match focusing phase", func(t *testing.T) {
		state := coordinationSynthesizingState("synth-001")
		state.Phase = phases.CoordinationFocusing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match synthesized phase", func(t *testing.T) {
		state := coordinationSynthesizingState("synth-001")
		state.Phase = phases.CoordinationSynthesized
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestCoordinationLoopWorkflow_DispatchSynthesisPayload(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-synthesis")

	state := coordinationSynthesizingState("synth-001")
	state.Title = "Synthesis Title"
	state.ProjectID = workflow.ProjectEntityID("default")
	state.TraceID = "trace-synth"
	state.LoopID = "loop-synth"
	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	req, ok := payload.(*CoordinationSynthesisRequest)
	if !ok {
		t.Fatalf("expected *CoordinationSynthesisRequest, got %T", payload)
	}
	if req.ExecutionID != state.ID {
		t.Errorf("expected ExecutionID %q, got %q", state.ID, req.ExecutionID)
	}
	if req.Slug != "synth-001" {
		t.Errorf("expected Slug 'synth-001', got %q", req.Slug)
	}
	if req.Title != "Synthesis Title" {
		t.Errorf("expected Title 'Synthesis Title', got %q", req.Title)
	}
	if req.TraceID != "trace-synth" {
		t.Errorf("expected TraceID 'trace-synth', got %q", req.TraceID)
	}
	if req.LoopID != "loop-synth" {
		t.Errorf("expected LoopID 'loop-synth', got %q", req.LoopID)
	}
}

func TestCoordinationLoopWorkflow_DispatchSynthesisMutation(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-synthesis")

	state := coordinationSynthesizingState("synth-001")
	ctx := &reactiveEngine.RuleContext{State: state}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}
	if state.Phase != phases.CoordinationSynthesisDispatched {
		t.Errorf("expected phase %q, got %q", phases.CoordinationSynthesisDispatched, state.Phase)
	}
}

// ---------------------------------------------------------------------------
// coordination-complete rule tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_CoordinationCompleteConditions(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "coordination-complete")

	t.Run("conditions pass for synthesized phase", func(t *testing.T) {
		state := coordinationSynthesizedState("complete-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for synthesis_dispatched phase", func(t *testing.T) {
		state := coordinationSynthesizedState("complete-001")
		state.Phase = phases.CoordinationSynthesisDispatched
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail for synthesizing phase", func(t *testing.T) {
		state := coordinationSynthesizedState("complete-001")
		state.Phase = phases.CoordinationSynthesizing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail when already completed", func(t *testing.T) {
		state := coordinationSynthesizedState("complete-001")
		state.Status = reactiveEngine.StatusCompleted
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestCoordinationLoopWorkflow_CoordinationCompletePayload(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "coordination-complete")

	state := coordinationSynthesizedState("complete-001")
	state.RequestID = "req-complete"
	state.TraceID = "trace-complete"
	state.PlannerCount = 2
	state.LLMRequestIDs = []string{"llm-1", "llm-2"}
	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	completed, ok := payload.(*CoordinationCompletedPayload)
	if !ok {
		t.Fatalf("expected *CoordinationCompletedPayload, got %T", payload)
	}
	if completed.Slug != "complete-001" {
		t.Errorf("expected Slug 'complete-001', got %q", completed.Slug)
	}
	if completed.RequestID != "req-complete" {
		t.Errorf("expected RequestID 'req-complete', got %q", completed.RequestID)
	}
	if completed.TraceID != "trace-complete" {
		t.Errorf("expected TraceID 'trace-complete', got %q", completed.TraceID)
	}
	if completed.PlannerCount != 2 {
		t.Errorf("expected PlannerCount 2, got %d", completed.PlannerCount)
	}
	if len(completed.LLMRequestIDs) != 2 {
		t.Errorf("expected 2 LLMRequestIDs, got %v", completed.LLMRequestIDs)
	}
}

// ---------------------------------------------------------------------------
// handle-error rule tests
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_HandleErrorConditions(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-error")

	t.Run("conditions pass for focus_failed phase", func(t *testing.T) {
		state := coordinationFailedState("err-001", phases.CoordinationFocusFailed, "focus timed out")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions pass for planners_failed phase", func(t *testing.T) {
		state := coordinationFailedState("err-001", phases.CoordinationPlannersFailed, "all planners failed")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions pass for synthesis_failed phase", func(t *testing.T) {
		state := coordinationFailedState("err-001", phases.CoordinationSynthesisFailed, "synthesis crashed")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for focusing phase (not a failure phase)", func(t *testing.T) {
		state := coordinationFocusingState("err-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail for synthesizing phase (not a failure phase)", func(t *testing.T) {
		state := coordinationSynthesizingState("err-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail when already completed", func(t *testing.T) {
		state := coordinationFailedState("err-001", phases.CoordinationFocusFailed, "failed")
		state.Status = reactiveEngine.StatusFailed
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestCoordinationLoopWorkflow_HandleErrorPayload(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-error")

	t.Run("builds error payload with state error message", func(t *testing.T) {
		state := coordinationFailedState("err-slug", phases.CoordinationFocusFailed, "focus determination timed out")
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		errPayload, ok := payload.(*CoordinationErrorPayload)
		if !ok {
			t.Fatalf("expected *CoordinationErrorPayload, got %T", payload)
		}
		if errPayload.Slug != "err-slug" {
			t.Errorf("expected Slug 'err-slug', got %q", errPayload.Slug)
		}
		if errPayload.Error != "focus determination timed out" {
			t.Errorf("expected Error 'focus determination timed out', got %q", errPayload.Error)
		}
	})

	t.Run("falls back to phase name when error is empty", func(t *testing.T) {
		state := coordinationFailedState("err-slug", phases.CoordinationSynthesisFailed, "")
		state.Error = "" // Explicitly clear error.
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		errPayload, ok := payload.(*CoordinationErrorPayload)
		if !ok {
			t.Fatalf("expected *CoordinationErrorPayload, got %T", payload)
		}
		if errPayload.Error == "" {
			t.Error("expected Error to be filled with fallback message")
		}
	})
}

func TestCoordinationLoopWorkflow_HandleErrorMutation(t *testing.T) {
	def := BuildCoordinationLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-error")

	t.Run("marks execution as failed", func(t *testing.T) {
		state := coordinationFailedState("err-001", phases.CoordinationFocusFailed, "timeout")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Status != reactiveEngine.StatusFailed {
			t.Errorf("expected StatusFailed, got %v", state.Status)
		}
	})

	t.Run("marks planners_failed as failed", func(t *testing.T) {
		state := coordinationFailedState("err-001", phases.CoordinationPlannersFailed, "all planners failed")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, nil); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Status != reactiveEngine.StatusFailed {
			t.Errorf("expected StatusFailed, got %v", state.Status)
		}
	})

	t.Run("wrong state type returns error", func(t *testing.T) {
		ctx := &reactiveEngine.RuleContext{State: &PlanReviewState{}}
		if err := rule.Action.MutateState(ctx, nil); err == nil {
			t.Error("expected error for wrong state type, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// allPlannersDone helper tests
// ---------------------------------------------------------------------------

func TestAllPlannersDone(t *testing.T) {
	tests := []struct {
		name          string
		plannerCount  int
		resultsStored int
		want          bool
	}{
		{"zero count never done", 0, 0, false},
		{"zero count with results", 0, 5, false},
		{"count not yet met", 3, 2, false},
		{"exactly met", 2, 2, true},
		{"exceeded (defensive)", 2, 3, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := &CoordinationState{}
			state.PlannerCount = tc.plannerCount
			state.PlannerResults = make(map[string]*PlannerOutcome)
			for i := range tc.resultsStored {
				state.PlannerResults[fmt.Sprintf("p%d", i)] = &PlannerOutcome{PlannerID: fmt.Sprintf("p%d", i)}
			}
			got := allPlannersDone(state)
			if got != tc.want {
				t.Errorf("allPlannersDone = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// JSON roundtrip tests for coordination payload types
// ---------------------------------------------------------------------------

func TestCoordinationLoopWorkflow_PayloadJSONRoundtrips(t *testing.T) {
	t.Run("CoordinationPlannerMessage roundtrip", func(t *testing.T) {
		original := &CoordinationPlannerMessage{
			ExecutionID:      "coord.exec-1",
			PlannerID:        "planner-1",
			Slug:             "my-plan",
			Title:            "My Plan",
			FocusArea:        "backend",
			FocusDescription: "Design backend services",
			Hints:            []string{"use gRPC", "hexagonal arch"},
			TraceID:          "trace-xyz",
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored CoordinationPlannerMessage
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.ExecutionID != original.ExecutionID {
			t.Errorf("ExecutionID mismatch: got %q, want %q", restored.ExecutionID, original.ExecutionID)
		}
		if restored.PlannerID != original.PlannerID {
			t.Errorf("PlannerID mismatch: got %q, want %q", restored.PlannerID, original.PlannerID)
		}
		if len(restored.Hints) != 2 {
			t.Errorf("Hints mismatch: got %v", restored.Hints)
		}
	})

	t.Run("CoordinationPlannerResult roundtrip", func(t *testing.T) {
		original := &CoordinationPlannerResult{
			ExecutionID:  "coord.exec-1",
			PlannerID:    "planner-2",
			Slug:         "my-plan",
			FocusArea:    "api",
			Status:       "completed",
			Result:       json.RawMessage(`{"tasks":[{"id":"t1"}]}`),
			LLMRequestID: "llm-req-42",
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored CoordinationPlannerResult
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Status != "completed" {
			t.Errorf("Status mismatch: got %q", restored.Status)
		}
		if restored.LLMRequestID != "llm-req-42" {
			t.Errorf("LLMRequestID mismatch: got %q", restored.LLMRequestID)
		}
	})

	t.Run("CoordinationSynthesisRequest roundtrip", func(t *testing.T) {
		original := &CoordinationSynthesisRequest{
			ExecutionID: "coord.exec-1",
			Slug:        "my-plan",
			Title:       "My Plan",
			ProjectID:   workflow.ProjectEntityID("default"),
			TraceID:     "trace-99",
			LoopID:      "loop-99",
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored CoordinationSynthesisRequest
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.ExecutionID != original.ExecutionID {
			t.Errorf("ExecutionID mismatch: got %q", restored.ExecutionID)
		}
		if restored.Slug != original.Slug {
			t.Errorf("Slug mismatch: got %q", restored.Slug)
		}
	})

	t.Run("CoordinationCompletedPayload roundtrip", func(t *testing.T) {
		original := &CoordinationCompletedPayload{
			Slug:          "my-plan",
			RequestID:     "req-complete",
			TraceID:       "trace-done",
			PlannerCount:  3,
			LLMRequestIDs: []string{"llm-1", "llm-2", "llm-3"},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored CoordinationCompletedPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.PlannerCount != 3 {
			t.Errorf("PlannerCount mismatch: got %d", restored.PlannerCount)
		}
		if len(restored.LLMRequestIDs) != 3 {
			t.Errorf("LLMRequestIDs mismatch: got %v", restored.LLMRequestIDs)
		}
	})

	t.Run("CoordinationErrorPayload marshals as UserSignalErrorEvent", func(t *testing.T) {
		original := &CoordinationErrorPayload{
			UserSignalErrorEvent: workflow.UserSignalErrorEvent{
				Slug:  "my-plan",
				Error: "coordination failed",
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		// Wire format must match UserSignalErrorEvent (no wrapper struct).
		var event workflow.UserSignalErrorEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal as UserSignalErrorEvent failed: %v", err)
		}
		if event.Slug != "my-plan" || event.Error != "coordination failed" {
			t.Errorf("roundtrip mismatch: %+v", event)
		}

		var restored CoordinationErrorPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal back to CoordinationErrorPayload failed: %v", err)
		}
		if restored.Slug != "my-plan" {
			t.Errorf("expected Slug 'my-plan', got %q", restored.Slug)
		}
	})
}

// ---------------------------------------------------------------------------
// Payload schema and validate tests
// ---------------------------------------------------------------------------

func TestCoordinationPayload_SchemaAndValidate(t *testing.T) {
	t.Run("CoordinationPlannerMessage schema", func(t *testing.T) {
		m := &CoordinationPlannerMessage{ExecutionID: "e1", PlannerID: "p1"}
		if err := m.Validate(); err != nil {
			t.Errorf("Validate failed for valid message: %v", err)
		}
		schema := m.Schema()
		if schema.Domain != "workflow" || schema.Category != "coordination-planner-message" {
			t.Errorf("unexpected schema: %+v", schema)
		}
	})

	t.Run("CoordinationPlannerMessage validate missing execution_id", func(t *testing.T) {
		m := &CoordinationPlannerMessage{PlannerID: "p1"} // no ExecutionID
		if err := m.Validate(); err == nil {
			t.Error("expected validation error for missing execution_id")
		}
	})

	t.Run("CoordinationPlannerMessage validate missing planner_id", func(t *testing.T) {
		m := &CoordinationPlannerMessage{ExecutionID: "e1"} // no PlannerID
		if err := m.Validate(); err == nil {
			t.Error("expected validation error for missing planner_id")
		}
	})

	t.Run("CoordinationPlannerResult schema", func(t *testing.T) {
		r := &CoordinationPlannerResult{ExecutionID: "e1", PlannerID: "p1"}
		if err := r.Validate(); err != nil {
			t.Errorf("Validate failed: %v", err)
		}
		schema := r.Schema()
		if schema.Category != "coordination-planner-result" {
			t.Errorf("unexpected schema category: %q", schema.Category)
		}
	})

	t.Run("CoordinationPlannerResult validate missing execution_id", func(t *testing.T) {
		r := &CoordinationPlannerResult{PlannerID: "p1"}
		if err := r.Validate(); err == nil {
			t.Error("expected validation error")
		}
	})

	t.Run("CoordinationSynthesisRequest schema", func(t *testing.T) {
		r := &CoordinationSynthesisRequest{ExecutionID: "e1", Slug: "s1"}
		if err := r.Validate(); err != nil {
			t.Errorf("Validate failed: %v", err)
		}
		schema := r.Schema()
		if schema.Category != "coordination-synthesis-request" {
			t.Errorf("unexpected schema category: %q", schema.Category)
		}
	})

	t.Run("CoordinationSynthesisRequest validate missing slug", func(t *testing.T) {
		r := &CoordinationSynthesisRequest{ExecutionID: "e1"}
		if err := r.Validate(); err == nil {
			t.Error("expected validation error for missing slug")
		}
	})

	t.Run("CoordinationCompletedPayload validate", func(t *testing.T) {
		p := &CoordinationCompletedPayload{Slug: "s1"}
		if err := p.Validate(); err != nil {
			t.Errorf("Validate failed: %v", err)
		}
	})

	t.Run("CoordinationCompletedPayload validate missing slug", func(t *testing.T) {
		p := &CoordinationCompletedPayload{}
		if err := p.Validate(); err == nil {
			t.Error("expected validation error for missing slug")
		}
	})

	t.Run("CoordinationErrorPayload validate", func(t *testing.T) {
		p := &CoordinationErrorPayload{UserSignalErrorEvent: workflow.UserSignalErrorEvent{Slug: "s1", Error: "boom"}}
		if err := p.Validate(); err != nil {
			t.Errorf("Validate failed: %v", err)
		}
	})

	t.Run("CoordinationErrorPayload validate missing slug", func(t *testing.T) {
		p := &CoordinationErrorPayload{}
		if err := p.Validate(); err == nil {
			t.Error("expected validation error for missing slug")
		}
	})
}

// ---------------------------------------------------------------------------
// Integration tests using TestEngine
// ---------------------------------------------------------------------------

// applyPlannerResult simulates a planner result arriving and applies it via MutateState.
func applyPlannerResult(t *testing.T, engine *testutil.TestEngine, key string, rule *reactiveEngine.RuleDef, plannerID, focusArea, llmReqID string) {
	t.Helper()
	state := &CoordinationState{}
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	result := &CoordinationPlannerResult{
		ExecutionID:  state.ID,
		PlannerID:    plannerID,
		Slug:         "happy-coord",
		FocusArea:    focusArea,
		Status:       "completed",
		LLMRequestID: llmReqID,
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("%s MutateState failed: %v", plannerID, err)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (%s result) failed: %v", plannerID, err)
	}
}

func TestCoordinationLoopWorkflow_HappyPath_FocusingToSynthesized(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "coordination.happy-coord"

	// Step 1: Write state in focusing phase — dispatch-focus rule should fire.
	initial := newCoordinationRunningState(key, "happy-coord", phases.CoordinationFocusing, 2)
	if err := engine.TriggerKV(context.Background(), key, initial); err != nil {
		t.Fatalf("TriggerKV (focusing) failed: %v", err)
	}
	engine.AssertPhase(key, phases.CoordinationFocusing)
	engine.AssertStatus(key, reactiveEngine.StatusRunning)

	// Verify dispatch-focus rule conditions pass.
	dispatchFocusRule := findRule(t, def, "dispatch-focus")
	state := &CoordinationState{}
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	assertAllConditionsPass(t, dispatchFocusRule, &reactiveEngine.RuleContext{State: state})

	// Step 2: Simulate focus handler completing — planners dispatched.
	state.Phase = phases.CoordinationPlannersDispatched
	state.PlannerCount = 2
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (planners_dispatched) failed: %v", err)
	}
	engine.AssertPhase(key, phases.CoordinationPlannersDispatched)

	// Step 3-4: Two planner results arrive — second triggers synthesizing.
	plannerResultRule := findRule(t, def, "planner-result")
	applyPlannerResult(t, engine, key, plannerResultRule, "planner-a", "backend", "llm-a")
	engine.AssertPhase(key, phases.CoordinationPlannersDispatched) // still waiting
	applyPlannerResult(t, engine, key, plannerResultRule, "planner-b", "api", "llm-b")
	engine.AssertPhase(key, phases.CoordinationSynthesizing) // all done

	// Step 5: Dispatch-synthesis rule fires.
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	dispatchSynthRule := findRule(t, def, "dispatch-synthesis")
	assertAllConditionsPass(t, dispatchSynthRule, &reactiveEngine.RuleContext{State: state})

	// Step 6: Synthesis completes.
	state.Phase = phases.CoordinationSynthesized
	state.SynthesizedPlan = json.RawMessage(`{"title":"Unified Plan"}`)
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (synthesized) failed: %v", err)
	}

	// Step 7: coordination-complete rule fires.
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	completeRule := findRule(t, def, "coordination-complete")
	assertAllConditionsPass(t, completeRule, &reactiveEngine.RuleContext{State: state})

	payload, err := completeRule.Action.BuildPayload(&reactiveEngine.RuleContext{State: state})
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	completed, ok := payload.(*CoordinationCompletedPayload)
	if !ok {
		t.Fatalf("expected *CoordinationCompletedPayload, got %T", payload)
	}
	if completed.Slug != "happy-coord" {
		t.Errorf("expected Slug 'happy-coord', got %q", completed.Slug)
	}
}

func TestCoordinationLoopWorkflow_FocusFailed_TriggersError(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "coordination.focus-failed"

	// Focus handler sets phase to focus_failed.
	state := newCoordinationRunningState(key, "focus-failed", phases.CoordinationFocusFailed, 0)
	state.Error = "focus determination timed out"
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (focus_failed) failed: %v", err)
	}
	engine.AssertPhase(key, phases.CoordinationFocusFailed)

	// handle-error rule should fire.
	handleErrorRule := findRule(t, def, "handle-error")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx := &reactiveEngine.RuleContext{State: state}
	assertAllConditionsPass(t, handleErrorRule, ctx)

	// Error payload must carry the error message.
	payload, err := handleErrorRule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	errPayload, ok := payload.(*CoordinationErrorPayload)
	if !ok {
		t.Fatalf("expected *CoordinationErrorPayload, got %T", payload)
	}
	if errPayload.Error != "focus determination timed out" {
		t.Errorf("expected Error 'focus determination timed out', got %q", errPayload.Error)
	}

	// Apply mutator — should mark as failed.
	if err := handleErrorRule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("error MutateState failed: %v", err)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (failed) failed: %v", err)
	}
	engine.AssertStatus(key, reactiveEngine.StatusFailed)
}

func TestCoordinationLoopWorkflow_AllPlannersFailed_TriggersError(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "coordination.planners-failed"

	// Both planners fail via the planner-result mutator.
	state := newCoordinationRunningState(key, "planners-failed", phases.CoordinationPlannersDispatched, 2)

	plannerResultRule := findRule(t, def, "planner-result")

	// First failure.
	resultA := &CoordinationPlannerResult{
		ExecutionID: state.ID, PlannerID: "p1", Slug: "planners-failed", Status: "failed", Error: "LLM error",
	}
	ctxA := &reactiveEngine.RuleContext{State: state, Message: resultA}
	if err := plannerResultRule.Action.MutateState(ctxA, nil); err != nil {
		t.Fatalf("planner-a MutateState failed: %v", err)
	}

	// Second failure — all planners done, all failed.
	resultB := &CoordinationPlannerResult{
		ExecutionID: state.ID, PlannerID: "p2", Slug: "planners-failed", Status: "failed", Error: "timeout",
	}
	ctxB := &reactiveEngine.RuleContext{State: state, Message: resultB}
	if err := plannerResultRule.Action.MutateState(ctxB, nil); err != nil {
		t.Fatalf("planner-b MutateState failed: %v", err)
	}

	if state.Phase != phases.CoordinationPlannersFailed {
		t.Fatalf("expected planners_failed phase after all planners fail, got %q", state.Phase)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (planners_failed) failed: %v", err)
	}
	engine.AssertPhase(key, phases.CoordinationPlannersFailed)

	// handle-error must fire.
	handleErrorRule := findRule(t, def, "handle-error")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx := &reactiveEngine.RuleContext{State: state}
	assertAllConditionsPass(t, handleErrorRule, ctx)

	// Apply the error mutator and confirm terminal status.
	if err := handleErrorRule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("error MutateState failed: %v", err)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (failed) failed: %v", err)
	}
	engine.AssertStatus(key, reactiveEngine.StatusFailed)
}

func TestCoordinationLoopWorkflow_SynthesisFailed_TriggersError(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "coordination.synthesis-failed"

	// Synthesis handler fails after planners succeed.
	state := newCoordinationRunningState(key, "synthesis-failed", phases.CoordinationSynthesisFailed, 2)
	state.Error = "synthesis LLM timed out"
	// Pretend planners had results.
	state.PlannerResults = map[string]*PlannerOutcome{
		"p1": {PlannerID: "p1", Status: "completed"},
		"p2": {PlannerID: "p2", Status: "completed"},
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}
	engine.AssertPhase(key, phases.CoordinationSynthesisFailed)

	// handle-error rule fires.
	handleErrorRule := findRule(t, def, "handle-error")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	ctx := &reactiveEngine.RuleContext{State: state}
	assertAllConditionsPass(t, handleErrorRule, ctx)

	payload, err := handleErrorRule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	errPayload, ok := payload.(*CoordinationErrorPayload)
	if !ok {
		t.Fatalf("expected *CoordinationErrorPayload, got %T", payload)
	}
	if errPayload.Error != "synthesis LLM timed out" {
		t.Errorf("expected synthesis error message, got %q", errPayload.Error)
	}
}

func TestCoordinationLoopWorkflow_SinglePlanner_HappyPath(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildCoordinationLoopWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "coordination.single-planner"

	// Single-planner coordination.
	state := newCoordinationRunningState(key, "single-planner", phases.CoordinationPlannersDispatched, 1)
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}

	// One result arrives — immediately transitions to synthesizing.
	plannerResultRule := findRule(t, def, "planner-result")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	result := &CoordinationPlannerResult{
		ExecutionID:  state.ID,
		PlannerID:    "only-planner",
		Slug:         "single-planner",
		Status:       "completed",
		Result:       json.RawMessage(`{"tasks":[]}`),
		LLMRequestID: "llm-solo",
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: result}
	if err := plannerResultRule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Phase != phases.CoordinationSynthesizing {
		t.Errorf("expected synthesizing after single planner completes, got %q", state.Phase)
	}
	if err := engine.TriggerKV(context.Background(), key, state); err != nil {
		t.Fatalf("TriggerKV (synthesizing) failed: %v", err)
	}
	engine.AssertPhase(key, phases.CoordinationSynthesizing)

	// dispatch-synthesis rule should fire.
	dispatchSynthRule := findRule(t, def, "dispatch-synthesis")
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	synthCtx := &reactiveEngine.RuleContext{State: state}
	assertAllConditionsPass(t, dispatchSynthRule, synthCtx)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// coordinationFocusingState returns a CoordinationState in the focusing phase.
func coordinationFocusingState(slug string) *CoordinationState {
	return &CoordinationState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:        "coordination." + slug,
			Phase:     phases.CoordinationFocusing,
			Status:    reactiveEngine.StatusRunning,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Slug:           slug,
		PlannerResults: make(map[string]*PlannerOutcome),
	}
}

// coordinationPlannersDispatchedState returns a CoordinationState waiting for planner results.
func coordinationPlannersDispatchedState(slug string, plannerCount int) *CoordinationState {
	s := coordinationFocusingState(slug)
	s.Phase = phases.CoordinationPlannersDispatched
	s.PlannerCount = plannerCount
	return s
}

// coordinationSynthesizingState returns a CoordinationState in the synthesizing phase.
func coordinationSynthesizingState(slug string) *CoordinationState {
	s := coordinationFocusingState(slug)
	s.Phase = phases.CoordinationSynthesizing
	s.PlannerCount = 2
	s.PlannerResults = map[string]*PlannerOutcome{
		"p1": {PlannerID: "p1", Status: "completed"},
		"p2": {PlannerID: "p2", Status: "completed"},
	}
	return s
}

// coordinationSynthesizedState returns a CoordinationState in the synthesized phase.
func coordinationSynthesizedState(slug string) *CoordinationState {
	s := coordinationSynthesizingState(slug)
	s.Phase = phases.CoordinationSynthesized
	s.SynthesizedPlan = json.RawMessage(`{"title":"Synthesized Plan"}`)
	return s
}

// coordinationFailedState returns a CoordinationState in a failure phase.
func coordinationFailedState(slug, phase, errMsg string) *CoordinationState {
	s := coordinationFocusingState(slug)
	s.Phase = phase
	s.Error = errMsg
	return s
}

// newCoordinationRunningState builds a fully initialised CoordinationState
// suitable for writing to the TestEngine KV store.
func newCoordinationRunningState(key, slug, phase string, plannerCount int) *CoordinationState {
	return &CoordinationState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: CoordinationLoopWorkflowID,
			Phase:      phase,
			Status:     reactiveEngine.StatusRunning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:           slug,
		PlannerCount:   plannerCount,
		PlannerResults: make(map[string]*PlannerOutcome),
	}
}
