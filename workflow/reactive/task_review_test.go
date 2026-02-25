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

func TestTaskReviewWorkflow_Definition(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)

	if def.ID != "task-review-loop" {
		t.Errorf("expected ID 'task-review-loop', got %q", def.ID)
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

func TestTaskReviewWorkflow_StateFactory(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)

	got := def.StateFactory()
	if got == nil {
		t.Fatal("StateFactory returned nil")
	}
	_, ok := got.(*TaskReviewState)
	if !ok {
		t.Errorf("StateFactory returned wrong type: %T", got)
	}
}

// ---------------------------------------------------------------------------
// accept-trigger rule tests
// ---------------------------------------------------------------------------

func TestTaskReviewWorkflow_AcceptTrigger(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &TaskReviewState{}
	trigger := &workflow.TriggerPayload{
		Slug:          "my-tasks",
		Title:         "My Tasks",
		Description:   "A test task list",
		ProjectID:     "semspec.local.project.default",
		RequestID:     "req-456",
		TraceID:       "trace-def",
		LoopID:        "loop-uvw",
		Role:          "developer",
		Prompt:        "Break down the implementation",
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

	if state.Slug != "my-tasks" {
		t.Errorf("expected Slug 'my-tasks', got %q", state.Slug)
	}
	if state.Title != "My Tasks" {
		t.Errorf("expected Title 'My Tasks', got %q", state.Title)
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
	if state.Role != "developer" {
		t.Errorf("expected Role 'developer', got %q", state.Role)
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
	if state.WorkflowID != "task-review-loop" {
		t.Errorf("expected WorkflowID 'task-review-loop', got %q", state.WorkflowID)
	}
}

func TestTaskReviewWorkflow_AcceptTrigger_SecondTriggerPreservesID(t *testing.T) {
	// A second trigger on an already-initialised state must not reset the ID.
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &TaskReviewState{}
	state.ID = "task-review.existing"
	state.WorkflowID = "task-review-loop"

	trigger := &workflow.TriggerPayload{Slug: "existing", Title: "Existing"}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.ID != "task-review.existing" {
		t.Errorf("ID should be preserved, got %q", state.ID)
	}
}

// ---------------------------------------------------------------------------
// generate rule tests
// ---------------------------------------------------------------------------

func TestTaskReviewWorkflow_GenerateConditions(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generate")

	t.Run("matches generating phase with no pending task", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match reviewing phase", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		state.Phase = ReviewPhaseReviewing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match when pending task exists", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		state.PendingTaskID = "task-xyz"
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestTaskReviewWorkflow_GeneratePayload(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generate")

	t.Run("first iteration uses direct prompt", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		state.Prompt = "Implement auth service tasks"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		req, ok := payload.(*TaskGeneratorRequest)
		if !ok {
			t.Fatalf("expected *TaskGeneratorRequest, got %T", payload)
		}
		if req.Slug != "gen-001" {
			t.Errorf("expected Slug 'gen-001', got %q", req.Slug)
		}
		if req.Prompt != "Implement auth service tasks" {
			t.Errorf("expected Prompt 'Implement auth service tasks', got %q", req.Prompt)
		}
		if req.Revision {
			t.Error("expected Revision to be false on first iteration")
		}
	})

	t.Run("revision iteration includes reviewer feedback in prompt", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		state.Iteration = 1
		state.Summary = "Missing error handling tasks"
		state.FormattedFindings = "- No error handling task in service layer"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		req, ok := payload.(*TaskGeneratorRequest)
		if !ok {
			t.Fatalf("expected *TaskGeneratorRequest, got %T", payload)
		}
		if !req.Revision {
			t.Error("expected Revision to be true on second iteration")
		}
		if !strings.Contains(req.Prompt, "REVISION REQUEST") {
			t.Errorf("expected revision prompt to contain 'REVISION REQUEST', got: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "Missing error handling tasks") {
			t.Errorf("expected revision prompt to contain summary, got: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "No error handling task in service layer") {
			t.Errorf("expected revision prompt to contain findings, got: %q", req.Prompt)
		}
	})
}

func TestTaskReviewWorkflow_GeneratorResultMutation(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "generate")

	t.Run("success transitions to reviewing", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		result := &TaskGeneratorResult{
			RequestID:     "req-1",
			Slug:          "gen-001",
			Tasks:         json.RawMessage(`[{"id":"task-1","title":"Implement login"}]`),
			TaskCount:     1,
			LLMRequestIDs: []string{"llm-req-1"},
		}

		if err := rule.Action.MutateState(ctx, result); err != nil {
			t.Fatalf("MutateState failed: %v", err)
		}
		if state.Phase != ReviewPhaseReviewing {
			t.Errorf("expected phase %q, got %q", ReviewPhaseReviewing, state.Phase)
		}
		if string(state.TasksContent) != `[{"id":"task-1","title":"Implement login"}]` {
			t.Errorf("unexpected TasksContent: %s", state.TasksContent)
		}
		if state.TaskCount != 1 {
			t.Errorf("expected TaskCount 1, got %d", state.TaskCount)
		}
		if len(state.LLMRequestIDs) == 0 || state.LLMRequestIDs[0] != "llm-req-1" {
			t.Errorf("expected LLMRequestIDs ['llm-req-1'], got %v", state.LLMRequestIDs)
		}
	})

	t.Run("wrong result type transitions to generator_failed", func(t *testing.T) {
		state := taskGeneratingState("gen-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		// Pass a mismatched result type.
		if err := rule.Action.MutateState(ctx, &TaskReviewResult{}); err != nil {
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

func TestTaskReviewWorkflow_ReviewConditions(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "review")

	t.Run("matches reviewing phase with no pending task", func(t *testing.T) {
		state := taskReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match generating phase", func(t *testing.T) {
		state := taskReviewingState("rev-001")
		state.Phase = ReviewPhaseGenerating
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match when pending task exists", func(t *testing.T) {
		state := taskReviewingState("rev-001")
		state.PendingTaskID = "task-abc"
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestTaskReviewWorkflow_ReviewerPayload(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "review")

	state := taskReviewingState("rev-001")
	state.TasksContent = json.RawMessage(`[{"id":"task-1"}]`)
	state.ProjectID = "semspec.local.project.default"
	state.TraceID = "trace-123"
	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	req, ok := payload.(*TaskReviewRequest)
	if !ok {
		t.Fatalf("expected *TaskReviewRequest, got %T", payload)
	}
	if req.Slug != "rev-001" {
		t.Errorf("expected Slug 'rev-001', got %q", req.Slug)
	}
	if len(req.Tasks) != 1 || req.Tasks[0].ID != "task-1" {
		t.Errorf("unexpected Tasks: %+v", req.Tasks)
	}
	if req.ProjectID != "semspec.local.project.default" {
		t.Errorf("expected ProjectID, got %q", req.ProjectID)
	}
	if req.TraceID != "trace-123" {
		t.Errorf("expected TraceID 'trace-123', got %q", req.TraceID)
	}
}

func TestTaskReviewWorkflow_ReviewerResultMutation(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "review")

	t.Run("approved verdict transitions to evaluated", func(t *testing.T) {
		state := taskReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		result := &TaskReviewResult{
			RequestID:         "req-1",
			Slug:              "rev-001",
			Verdict:           "approved",
			Summary:           "Tasks look good",
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
		if state.Summary != "Tasks look good" {
			t.Errorf("expected Summary 'Tasks look good', got %q", state.Summary)
		}
	})

	t.Run("needs_changes verdict transitions to evaluated", func(t *testing.T) {
		state := taskReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		result := &TaskReviewResult{
			Verdict:           "needs_changes",
			Summary:           "Tasks missing error handling",
			Findings:          json.RawMessage(`[{"issue":"no error handling task"}]`),
			FormattedFindings: "- Missing error handling task",
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
		if state.FormattedFindings != "- Missing error handling task" {
			t.Errorf("unexpected FormattedFindings: %q", state.FormattedFindings)
		}
	})

	t.Run("wrong result type transitions to reviewer_failed", func(t *testing.T) {
		state := taskReviewingState("rev-001")
		ctx := &reactiveEngine.RuleContext{State: state}

		if err := rule.Action.MutateState(ctx, &TaskGeneratorResult{}); err != nil {
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

func TestTaskReviewWorkflow_HandleApproved(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-approved")

	t.Run("conditions pass for approved evaluated state", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "approved")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for non-approved verdict", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail for non-evaluated phase", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "approved")
		state.Phase = ReviewPhaseReviewing
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct approved event payload", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "approved")
		state.Summary = "Tasks are excellent"
		state.TaskCount = 5
		state.ReviewerLLMRequestIDs = []string{"llm-rev-99"}
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		approved, ok := payload.(*TasksApprovedPayload)
		if !ok {
			t.Fatalf("expected *TasksApprovedPayload, got %T", payload)
		}
		if approved.Slug != "tasks-001" {
			t.Errorf("expected Slug 'tasks-001', got %q", approved.Slug)
		}
		if approved.Verdict != "approved" {
			t.Errorf("expected Verdict 'approved', got %q", approved.Verdict)
		}
		if approved.Summary != "Tasks are excellent" {
			t.Errorf("expected Summary, got %q", approved.Summary)
		}
		if approved.TaskCount != 5 {
			t.Errorf("expected TaskCount 5, got %d", approved.TaskCount)
		}
		if len(approved.LLMRequestIDs) == 0 || approved.LLMRequestIDs[0] != "llm-rev-99" {
			t.Errorf("expected LLMRequestIDs, got %v", approved.LLMRequestIDs)
		}
	})
}

// ---------------------------------------------------------------------------
// handle-revision rule tests
// ---------------------------------------------------------------------------

func TestTaskReviewWorkflow_HandleRevision(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-revision")

	t.Run("conditions pass for non-approved with iterations remaining", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		state.Iteration = 1
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for approved verdict", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "approved")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("conditions fail at max iterations", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct revision event payload", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		state.Iteration = 1
		state.Findings = json.RawMessage(`[{"issue":"X"}]`)
		state.FormattedFindings = "- Missing task X"
		state.ReviewerLLMRequestIDs = []string{"llm-rev-1"}
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		rev, ok := payload.(*TasksRevisionPayload)
		if !ok {
			t.Fatalf("expected *TasksRevisionPayload, got %T", payload)
		}
		if rev.Slug != "tasks-001" {
			t.Errorf("expected Slug 'tasks-001', got %q", rev.Slug)
		}
		if rev.Iteration != 1 {
			t.Errorf("expected Iteration 1, got %d", rev.Iteration)
		}
		if rev.Verdict != "needs_changes" {
			t.Errorf("expected Verdict 'needs_changes', got %q", rev.Verdict)
		}
		if rev.FormattedFindings != "- Missing task X" {
			t.Errorf("unexpected FormattedFindings: %q", rev.FormattedFindings)
		}
	})

	t.Run("mutation increments iteration and resets to generating", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
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

func TestTaskReviewWorkflow_HandleEscalation(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-escalation")

	t.Run("conditions pass for non-approved at max iterations", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		state.Iteration = 3
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail when iteration is still under max", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		state.Iteration = 2
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct escalation event payload", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
		state.Iteration = 3
		state.FormattedFindings = "- Critical task finding"
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		esc, ok := payload.(*TaskEscalatePayload)
		if !ok {
			t.Fatalf("expected *TaskEscalatePayload, got %T", payload)
		}
		if esc.Slug != "tasks-001" {
			t.Errorf("expected Slug 'tasks-001', got %q", esc.Slug)
		}
		if esc.Iteration != 3 {
			t.Errorf("expected Iteration 3, got %d", esc.Iteration)
		}
		if esc.LastVerdict != "needs_changes" {
			t.Errorf("expected LastVerdict 'needs_changes', got %q", esc.LastVerdict)
		}
		if !strings.Contains(esc.FormattedFindings, "Critical task finding") {
			t.Errorf("expected FormattedFindings to contain 'Critical task finding'")
		}
	})

	t.Run("mutation marks execution as escalated", func(t *testing.T) {
		state := taskEvaluatedState("tasks-001", "needs_changes")
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

func TestTaskReviewWorkflow_HandleError(t *testing.T) {
	def := BuildTaskReviewWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-error")

	t.Run("conditions pass for generator_failed phase", func(t *testing.T) {
		state := taskFailedState("tasks-001", ReviewPhaseGeneratorFailed, "generator crashed")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions pass for reviewer_failed phase", func(t *testing.T) {
		state := taskFailedState("tasks-001", ReviewPhaseReviewerFailed, "reviewer timed out")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("conditions fail for non-failure phase", func(t *testing.T) {
		state := taskGeneratingState("tasks-001")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("builds correct error event payload", func(t *testing.T) {
		state := taskFailedState("tasks-001", ReviewPhaseGeneratorFailed, "generator crashed")
		ctx := &reactiveEngine.RuleContext{State: state}

		payload, err := rule.Action.BuildPayload(ctx)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}
		errPayload, ok := payload.(*TaskErrorPayload)
		if !ok {
			t.Fatalf("expected *TaskErrorPayload, got %T", payload)
		}
		if errPayload.Slug != "tasks-001" {
			t.Errorf("expected Slug 'tasks-001', got %q", errPayload.Slug)
		}
		if errPayload.Error != "generator crashed" {
			t.Errorf("expected Error 'generator crashed', got %q", errPayload.Error)
		}
	})

	t.Run("mutation marks execution as failed", func(t *testing.T) {
		state := taskFailedState("tasks-001", ReviewPhaseGeneratorFailed, "timeout")
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

func TestTaskReviewWorkflow_EventPayloadJSON(t *testing.T) {
	t.Run("TasksApprovedPayload roundtrip", func(t *testing.T) {
		original := &TasksApprovedPayload{
			TasksApprovedEvent: workflow.TasksApprovedEvent{
				Slug:          "my-tasks",
				Verdict:       "approved",
				Summary:       "Good tasks",
				TaskCount:     4,
				LLMRequestIDs: []string{"req-1"},
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		// Wire format must match TasksApprovedEvent (no wrapper).
		var event workflow.TasksApprovedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal as TasksApprovedEvent failed: %v", err)
		}
		if event.Slug != "my-tasks" || event.Verdict != "approved" {
			t.Errorf("roundtrip mismatch: %+v", event)
		}
		if event.TaskCount != 4 {
			t.Errorf("expected TaskCount 4, got %d", event.TaskCount)
		}

		var restored TasksApprovedPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal back to TasksApprovedPayload failed: %v", err)
		}
		if restored.Slug != "my-tasks" {
			t.Errorf("expected Slug 'my-tasks', got %q", restored.Slug)
		}
	})

	t.Run("TasksRevisionPayload roundtrip", func(t *testing.T) {
		original := &TasksRevisionPayload{
			TasksRevisionNeededEvent: workflow.TasksRevisionNeededEvent{
				Slug:              "my-tasks",
				Iteration:         2,
				Verdict:           "needs_changes",
				FormattedFindings: "- Missing task Y",
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored TasksRevisionPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Iteration != 2 {
			t.Errorf("expected Iteration 2, got %d", restored.Iteration)
		}
		if restored.FormattedFindings != "- Missing task Y" {
			t.Errorf("unexpected FormattedFindings: %q", restored.FormattedFindings)
		}
	})

	t.Run("TaskEscalatePayload roundtrip", func(t *testing.T) {
		original := &TaskEscalatePayload{
			EscalationEvent: workflow.EscalationEvent{
				Slug:      "my-tasks",
				Reason:    "max task review iterations exceeded",
				Iteration: 3,
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored TaskEscalatePayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Reason != "max task review iterations exceeded" {
			t.Errorf("expected Reason 'max task review iterations exceeded', got %q", restored.Reason)
		}
	})

	t.Run("TaskErrorPayload roundtrip", func(t *testing.T) {
		original := &TaskErrorPayload{
			UserSignalErrorEvent: workflow.UserSignalErrorEvent{
				Slug:  "my-tasks",
				Error: "generator crashed",
			},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var restored TaskErrorPayload
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if restored.Error != "generator crashed" {
			t.Errorf("expected Error 'generator crashed', got %q", restored.Error)
		}
	})
}

// ---------------------------------------------------------------------------
// Integration tests using TestEngine
// ---------------------------------------------------------------------------

func TestTaskReviewWorkflow_HappyPath(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildTaskReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "task-review.happy-tasks"

	// Step 1: Start in generating phase.
	initial := &TaskReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "task-review-loop",
			Phase:      ReviewPhaseGenerating,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:      "happy-tasks",
		Title:     "Happy Path Tasks",
		RequestID: "req-happy",
		ProjectID: "semspec.local.project.default",
	}
	if err := engine.TriggerKV(context.Background(), key, initial); err != nil {
		t.Fatalf("TriggerKV failed: %v", err)
	}
	engine.AssertPhase(key, ReviewPhaseGenerating)
	engine.AssertStatus(key, reactiveEngine.StatusRunning)

	// Step 2: Simulate generator callback — transition to reviewing.
	state := &TaskReviewState{}
	if err := engine.GetStateAs(key, state); err != nil {
		t.Fatalf("GetStateAs failed: %v", err)
	}
	state.TasksContent = json.RawMessage(`[{"id":"task-1"},{"id":"task-2"}]`)
	state.TaskCount = 2
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
	state.Summary = "Tasks are solid"
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

	// Verify BuildPayload produces a TasksApprovedPayload.
	payload, err := approvedRule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}
	if _, ok := payload.(*TasksApprovedPayload); !ok {
		t.Errorf("expected *TasksApprovedPayload, got %T", payload)
	}
}

func TestTaskReviewWorkflow_RevisionThenApproved(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildTaskReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "task-review.revision-tasks"

	// Round 1: generating → reviewing → evaluated (needs_changes).
	state := &TaskReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "task-review-loop",
			Phase:      ReviewPhaseGenerating,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug: "revision-tasks",
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
	state.Summary = "Tasks need more detail"
	state.FormattedFindings = "- Missing acceptance criteria"
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

func TestTaskReviewWorkflow_MaxIterationsEscalated(t *testing.T) {
	engine := testutil.NewTestEngine(t)
	def := BuildTaskReviewWorkflow(testStateBucket)

	if err := engine.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	const key = "task-review.escalation-tasks"

	// Simulate having reached max iterations still needing changes.
	state := &TaskReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         key,
			WorkflowID: "task-review-loop",
			Phase:      ReviewPhaseEvaluated,
			Status:     reactiveEngine.StatusRunning,
			Iteration:  3, // at the limit
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		Slug:              "escalation-tasks",
		Verdict:           "needs_changes",
		Summary:           "Tasks still not good enough",
		FormattedFindings: "- Multiple issues remain in task definitions",
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
	esc, ok := payload.(*TaskEscalatePayload)
	if !ok {
		t.Fatalf("expected *TaskEscalatePayload, got %T", payload)
	}
	if esc.Slug != "escalation-tasks" {
		t.Errorf("expected Slug 'escalation-tasks', got %q", esc.Slug)
	}
	if esc.Iteration != 3 {
		t.Errorf("expected Iteration 3, got %d", esc.Iteration)
	}
}

// ---------------------------------------------------------------------------
// Task review test helpers
// ---------------------------------------------------------------------------

// taskGeneratingState returns a TaskReviewState in the generating phase.
func taskGeneratingState(slug string) *TaskReviewState {
	return &TaskReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:        "task-review." + slug,
			Phase:     ReviewPhaseGenerating,
			Status:    reactiveEngine.StatusRunning,
			Iteration: 0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Slug: slug,
	}
}

// taskReviewingState returns a TaskReviewState in the reviewing phase.
func taskReviewingState(slug string) *TaskReviewState {
	s := taskGeneratingState(slug)
	s.Phase = ReviewPhaseReviewing
	s.TasksContent = json.RawMessage(`[{"id":"task-1"}]`)
	s.TaskCount = 1
	return s
}

// taskEvaluatedState returns a TaskReviewState in the evaluated phase with the given verdict.
func taskEvaluatedState(slug, verdict string) *TaskReviewState {
	s := taskGeneratingState(slug)
	s.Phase = ReviewPhaseEvaluated
	s.Verdict = verdict
	s.Summary = "Review complete"
	return s
}

// taskFailedState returns a TaskReviewState in a failure phase.
func taskFailedState(slug, phase, errMsg string) *TaskReviewState {
	s := taskGeneratingState(slug)
	s.Phase = phase
	s.Error = errMsg
	return s
}
