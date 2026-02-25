package reactive

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock types for testing the shared builder
// ---------------------------------------------------------------------------

// mockReviewState is a minimal review state for testing BuildReviewLoopWorkflow.
type mockReviewState struct {
	reactiveEngine.ExecutionState
	Slug    string `json:"slug"`
	Verdict string `json:"verdict"`
	Output  string `json:"output,omitempty"`
}

func (s *mockReviewState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// mockTriggerMessage is used as the trigger message in tests.
type mockTriggerMessage struct {
	Slug string `json:"slug"`
}

// mockEventPayload implements message.Payload for test event assertions.
type mockEventPayload struct {
	Slug    string `json:"slug"`
	Verdict string `json:"verdict,omitempty"`
}

func (p *mockEventPayload) Schema() message.Type {
	return message.Type{Domain: "test", Category: "event", Version: "v1"}
}
func (p *mockEventPayload) Validate() error                 { return nil }
func (p *mockEventPayload) MarshalJSON() ([]byte, error)    { return json.Marshal(*p) }
func (p *mockEventPayload) UnmarshalJSON(data []byte) error { return json.Unmarshal(data, p) }

// Mock phase constants for tests.
const (
	testPhaseGenerating          = "generating"
	testPhaseGeneratorDispatched = "generator_dispatched"
	testPhaseGenerated           = "generated"
	testPhaseReviewing           = "reviewing"
	testPhaseReviewerDispatched  = "reviewer_dispatched"
	testPhaseReviewed            = "reviewed"
	testPhaseEvaluated           = "evaluated"
	testPhaseGeneratorFailed     = "generator_failed"
	testPhaseReviewerFailed      = "reviewer_failed"
)

// buildMockReviewLoopConfig creates a fully-populated ReviewLoopConfig for tests
// using the Participant pattern.
func buildMockReviewLoopConfig() ReviewLoopConfig {
	return ReviewLoopConfig{
		WorkflowID:    "test-review-loop",
		Description:   "Test OODA review loop",
		StateBucket:   "TEST_STATE",
		MaxIterations: 3,
		Timeout:       10 * time.Minute,
		StateFactory:  func() any { return &mockReviewState{} },

		TriggerStream:         "TEST_STREAM",
		TriggerSubject:        "test.trigger.review",
		TriggerMessageFactory: func() any { return &mockTriggerMessage{} },
		StateLookupKey: func(msg any) string {
			trigger := msg.(*mockTriggerMessage)
			return "test-review." + trigger.Slug
		},
		KVKeyPattern: "test-review.*",

		AcceptTrigger: func(ctx *reactiveEngine.RuleContext, _ any) error {
			state := ctx.State.(*mockReviewState)
			trigger := ctx.Message.(*mockTriggerMessage)
			state.Slug = trigger.Slug
			state.ID = "test-review." + trigger.Slug
			state.WorkflowID = "test-review-loop"
			state.Status = reactiveEngine.StatusRunning
			state.Phase = testPhaseGenerating
			return nil
		},
		VerdictAccessor: func(state any) string {
			if s, ok := state.(*mockReviewState); ok {
				return s.Verdict
			}
			return ""
		},

		// Generator - Participant pattern (fire-and-forget).
		GeneratorSubject: "test.async.generator",
		BuildGeneratorPayload: func(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
			state := ctx.State.(*mockReviewState)
			return &mockEventPayload{Slug: state.Slug}, nil
		},
		GeneratingPhase:          testPhaseGenerating,
		GeneratorDispatchedPhase: testPhaseGeneratorDispatched,
		GeneratorCompletedPhase:  testPhaseGenerated,

		// Reviewer - Participant pattern (fire-and-forget).
		ReviewerSubject: "test.async.reviewer",
		BuildReviewerPayload: func(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
			state := ctx.State.(*mockReviewState)
			return &mockEventPayload{Slug: state.Slug}, nil
		},
		ReviewingPhase:          testPhaseReviewing,
		ReviewerDispatchedPhase: testPhaseReviewerDispatched,
		ReviewerCompletedPhase:  testPhaseReviewed,
		EvaluatedPhase:          testPhaseEvaluated,

		// Failure phases.
		GeneratorFailedPhase: testPhaseGeneratorFailed,
		ReviewerFailedPhase:  testPhaseReviewerFailed,

		ApprovedEventSubject: "test.events.approved",
		BuildApprovedEvent: func(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
			state := ctx.State.(*mockReviewState)
			return &mockEventPayload{Slug: state.Slug, Verdict: "approved"}, nil
		},

		RevisionEventSubject: "test.events.revision_needed",
		BuildRevisionEvent: func(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
			state := ctx.State.(*mockReviewState)
			return &mockEventPayload{Slug: state.Slug, Verdict: state.Verdict}, nil
		},
		MutateOnRevision: func(ctx *reactiveEngine.RuleContext, _ any) error {
			state := ctx.State.(*mockReviewState)
			reactiveEngine.IncrementIteration(state)
			state.Verdict = ""
			state.Phase = testPhaseGenerating
			return nil
		},

		EscalateSubject: "test.signal.escalate",
		BuildEscalateEvent: func(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
			state := ctx.State.(*mockReviewState)
			return &mockEventPayload{Slug: state.Slug}, nil
		},
		MutateOnEscalation: func(ctx *reactiveEngine.RuleContext, _ any) error {
			reactiveEngine.EscalateExecution(ctx.State, "max iterations exceeded")
			return nil
		},

		ErrorSubject: "test.signal.error",
		BuildErrorEvent: func(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
			state := ctx.State.(*mockReviewState)
			return &mockEventPayload{Slug: state.Slug}, nil
		},
		MutateOnError: func(ctx *reactiveEngine.RuleContext, _ any) error {
			reactiveEngine.FailExecution(ctx.State, "component failed")
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// BuildReviewLoopWorkflow structure tests
// ---------------------------------------------------------------------------

func TestBuildReviewLoopWorkflow_ProducesCorrectRules(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	assert.Equal(t, "test-review-loop", def.ID)
	assert.Equal(t, "Test OODA review loop", def.Description)
	assert.Equal(t, "TEST_STATE", def.StateBucket)
	assert.Equal(t, 3, def.MaxIterations)
	assert.Equal(t, 10*time.Minute, def.Timeout)

	// Participant pattern produces 9 rules:
	// 1. accept-trigger
	// 2. dispatch-generator (fire-and-forget)
	// 3. generator-completed (react to component completion)
	// 4. dispatch-reviewer (fire-and-forget)
	// 5. reviewer-completed (react to component completion)
	// 6. handle-approved
	// 7. handle-revision
	// 8. handle-escalation
	// 9. handle-error
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

	require.Len(t, def.Rules, len(expectedRules))

	for i, want := range expectedRules {
		rule := def.Rules[i]
		assert.Equal(t, want.id, rule.ID, "rule[%d] ID", i)
		assert.Equal(t, want.actionType, rule.Action.Type, "rule[%d] %q action type", i, want.id)
	}
}

func TestBuildReviewLoopWorkflow_StateFactory(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	state := def.StateFactory()
	require.NotNil(t, state)

	_, ok := state.(*mockReviewState)
	assert.True(t, ok, "StateFactory should return *mockReviewState")
}

func TestBuildReviewLoopWorkflow_DispatchGeneratorRuleSubject(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	rule := findTestRule(t, def, "dispatch-generator")
	assert.Equal(t, "test.async.generator", rule.Action.PublishSubject)
}

func TestBuildReviewLoopWorkflow_DispatchReviewerRuleSubject(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	rule := findTestRule(t, def, "dispatch-reviewer")
	assert.Equal(t, "test.async.reviewer", rule.Action.PublishSubject)
}

func TestBuildReviewLoopWorkflow_ApprovedRuleSubject(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	rule := findTestRule(t, def, "handle-approved")
	assert.Equal(t, "test.events.approved", rule.Action.PublishSubject)
}

func TestBuildReviewLoopWorkflow_RevisionRuleSubject(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	rule := findTestRule(t, def, "handle-revision")
	assert.Equal(t, "test.events.revision_needed", rule.Action.PublishSubject)
}

func TestBuildReviewLoopWorkflow_EscalationRuleSubject(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	rule := findTestRule(t, def, "handle-escalation")
	assert.Equal(t, "test.signal.escalate", rule.Action.PublishSubject)
}

func TestBuildReviewLoopWorkflow_ErrorRuleSubject(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)

	rule := findTestRule(t, def, "handle-error")
	assert.Equal(t, "test.signal.error", rule.Action.PublishSubject)
}

// ---------------------------------------------------------------------------
// Condition tests
// ---------------------------------------------------------------------------

func TestVerdictIs_MatchesExpectedVerdict(t *testing.T) {
	accessor := func(state any) string {
		if s, ok := state.(*mockReviewState); ok {
			return s.Verdict
		}
		return ""
	}

	cond := verdictIs(accessor, "approved")

	t.Run("matches when verdict equals expected", func(t *testing.T) {
		state := &mockReviewState{Verdict: "approved"}
		ctx := &reactiveEngine.RuleContext{State: state}
		assert.True(t, cond(ctx))
	})

	t.Run("does not match different verdict", func(t *testing.T) {
		state := &mockReviewState{Verdict: "needs_changes"}
		ctx := &reactiveEngine.RuleContext{State: state}
		assert.False(t, cond(ctx))
	})

	t.Run("does not match empty verdict", func(t *testing.T) {
		state := &mockReviewState{}
		ctx := &reactiveEngine.RuleContext{State: state}
		assert.False(t, cond(ctx))
	})

	t.Run("handles nil state", func(t *testing.T) {
		ctx := &reactiveEngine.RuleContext{State: nil}
		assert.False(t, cond(ctx))
	})
}

func TestVerdictIsNot_ExcludesExpectedVerdict(t *testing.T) {
	accessor := func(state any) string {
		if s, ok := state.(*mockReviewState); ok {
			return s.Verdict
		}
		return ""
	}

	cond := verdictIsNot(accessor, "approved")

	t.Run("matches when verdict differs", func(t *testing.T) {
		state := &mockReviewState{Verdict: "needs_changes"}
		ctx := &reactiveEngine.RuleContext{State: state}
		assert.True(t, cond(ctx))
	})

	t.Run("does not match when verdict equals excluded", func(t *testing.T) {
		state := &mockReviewState{Verdict: "approved"}
		ctx := &reactiveEngine.RuleContext{State: state}
		assert.False(t, cond(ctx))
	})

	t.Run("matches empty verdict (empty is not approved)", func(t *testing.T) {
		state := &mockReviewState{}
		ctx := &reactiveEngine.RuleContext{State: state}
		assert.True(t, cond(ctx))
	})

	t.Run("handles nil state", func(t *testing.T) {
		ctx := &reactiveEngine.RuleContext{State: nil}
		assert.False(t, cond(ctx))
	})
}

// ---------------------------------------------------------------------------
// Functional behavior tests
// ---------------------------------------------------------------------------

func TestBuildReviewLoopWorkflow_AcceptTriggerMutator(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "accept-trigger")

	state := &mockReviewState{}
	trigger := &mockTriggerMessage{Slug: "test-slug"}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	// All conditions should pass (Always).
	for _, cond := range rule.Conditions {
		assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
	}

	// Apply mutator.
	err := rule.Action.MutateState(ctx, nil)
	require.NoError(t, err)

	assert.Equal(t, "test-slug", state.Slug)
	assert.Equal(t, "test-review.test-slug", state.ID)
	assert.Equal(t, testPhaseGenerating, state.Phase)
	assert.Equal(t, reactiveEngine.StatusRunning, state.Status)
}

func TestBuildReviewLoopWorkflow_DispatchGeneratorConditions(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "dispatch-generator")

	t.Run("matches generating phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:  testPhaseGenerating,
				Status: reactiveEngine.StatusRunning,
			},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("does not match dispatched phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:  testPhaseGeneratorDispatched,
				Status: reactiveEngine.StatusRunning,
			},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		anyFailed := false
		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				anyFailed = true
				break
			}
		}
		assert.True(t, anyFailed, "should fail when already dispatched")
	})
}

func TestBuildReviewLoopWorkflow_GeneratorCompletedConditions(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "generator-completed")

	t.Run("matches generated phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:  testPhaseGenerated,
				Status: reactiveEngine.StatusRunning,
			},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("does not match generating phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:  testPhaseGenerating,
				Status: reactiveEngine.StatusRunning,
			},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		anyFailed := false
		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				anyFailed = true
				break
			}
		}
		assert.True(t, anyFailed, "should fail when still generating")
	})
}

func TestBuildReviewLoopWorkflow_HandleApprovedConditions(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-approved")

	t.Run("matches evaluated phase with approved verdict", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{Phase: testPhaseEvaluated},
			Verdict:        "approved",
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("fails for non-approved verdict", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{Phase: testPhaseEvaluated},
			Verdict:        "needs_changes",
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		anyFailed := false
		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				anyFailed = true
				break
			}
		}
		assert.True(t, anyFailed, "should fail for non-approved verdict")
	})
}

func TestBuildReviewLoopWorkflow_HandleRevisionConditions(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-revision")

	t.Run("matches when not approved and under max iterations", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:     testPhaseEvaluated,
				Iteration: 1,
			},
			Verdict: "needs_changes",
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("fails at max iterations", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:     testPhaseEvaluated,
				Iteration: 3,
			},
			Verdict: "needs_changes",
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		anyFailed := false
		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				anyFailed = true
				break
			}
		}
		assert.True(t, anyFailed, "should fail at max iterations")
	})
}

func TestBuildReviewLoopWorkflow_HandleEscalationConditions(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-escalation")

	t.Run("matches when not approved and at max iterations", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:     testPhaseEvaluated,
				Iteration: 3,
			},
			Verdict: "needs_changes",
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("fails when under max iterations", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:     testPhaseEvaluated,
				Iteration: 2,
			},
			Verdict: "needs_changes",
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		anyFailed := false
		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				anyFailed = true
				break
			}
		}
		assert.True(t, anyFailed, "should fail when under max iterations")
	})
}

func TestBuildReviewLoopWorkflow_HandleErrorConditions(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-error")

	t.Run("matches generator_failed phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{Phase: testPhaseGeneratorFailed},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("matches reviewer_failed phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{Phase: testPhaseReviewerFailed},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		for _, cond := range rule.Conditions {
			assert.True(t, cond.Evaluate(ctx), "condition %q should pass", cond.Description)
		}
	})

	t.Run("does not match generating phase", func(t *testing.T) {
		state := &mockReviewState{
			ExecutionState: reactiveEngine.ExecutionState{Phase: testPhaseGenerating},
		}
		ctx := &reactiveEngine.RuleContext{State: state}
		anyFailed := false
		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				anyFailed = true
				break
			}
		}
		assert.True(t, anyFailed, "should not match generating phase")
	})
}

func TestBuildReviewLoopWorkflow_RevisionMutatorsWork(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-revision")

	state := &mockReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			Phase:     testPhaseEvaluated,
			Iteration: 0,
		},
		Verdict: "needs_changes",
	}
	ctx := &reactiveEngine.RuleContext{State: state}

	err := rule.Action.MutateState(ctx, nil)
	require.NoError(t, err)

	assert.Equal(t, testPhaseGenerating, state.Phase)
	assert.Equal(t, 1, state.Iteration)
	assert.Empty(t, state.Verdict)
}

func TestBuildReviewLoopWorkflow_EscalationMutatorsWork(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-escalation")

	state := &mockReviewState{
		ExecutionState: reactiveEngine.ExecutionState{
			Phase:     testPhaseEvaluated,
			Iteration: 3,
		},
		Verdict: "needs_changes",
	}
	ctx := &reactiveEngine.RuleContext{State: state}

	err := rule.Action.MutateState(ctx, nil)
	require.NoError(t, err)

	assert.Equal(t, reactiveEngine.StatusEscalated, state.Status)
}

func TestBuildReviewLoopWorkflow_ErrorMutatorsWork(t *testing.T) {
	cfg := buildMockReviewLoopConfig()
	def := BuildReviewLoopWorkflow(cfg)
	rule := findTestRule(t, def, "handle-error")

	state := &mockReviewState{
		ExecutionState: reactiveEngine.ExecutionState{Phase: testPhaseGeneratorFailed},
	}
	ctx := &reactiveEngine.RuleContext{State: state}

	err := rule.Action.MutateState(ctx, nil)
	require.NoError(t, err)

	assert.Equal(t, reactiveEngine.StatusFailed, state.Status)
}

// ---------------------------------------------------------------------------
// Verify plan-review-loop uses shared builder correctly
// ---------------------------------------------------------------------------

func TestBuildPlanReviewWorkflow_UsesSharedBuilder(t *testing.T) {
	// BuildPlanReviewWorkflow should produce the same 9-rule structure as
	// BuildReviewLoopWorkflow. This is a structural regression test.
	def := BuildPlanReviewWorkflow("REACTIVE_STATE")

	// Participant pattern produces 9 rules.
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

	require.Len(t, def.Rules, len(expectedRules))
	for i, want := range expectedRules {
		assert.Equal(t, want.id, def.Rules[i].ID, "rule[%d]", i)
		assert.Equal(t, want.actionType, def.Rules[i].Action.Type, "rule[%d] %q", i, want.id)
	}
}

// ---------------------------------------------------------------------------
// Test setPhase helper
// ---------------------------------------------------------------------------

func TestSetPhase_UpdatesExecutionStatePhase(t *testing.T) {
	mutator := setPhase("new_phase")

	state := &mockReviewState{
		ExecutionState: reactiveEngine.ExecutionState{Phase: "old_phase"},
	}
	ctx := &reactiveEngine.RuleContext{State: state}

	err := mutator(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, "new_phase", state.Phase)
}

func TestSetPhase_ReturnsErrorForNilState(t *testing.T) {
	mutator := setPhase("new_phase")
	ctx := &reactiveEngine.RuleContext{State: nil}

	err := mutator(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state is nil")
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func findTestRule(t *testing.T, def *reactiveEngine.Definition, id string) *reactiveEngine.RuleDef {
	t.Helper()
	for i := range def.Rules {
		if def.Rules[i].ID == id {
			return &def.Rules[i]
		}
	}
	t.Fatalf("rule %q not found in workflow %q", id, def.ID)
	return nil
}

// Ensure mockReviewState satisfies StateAccessor at compile time.
var _ = func() {
	var s mockReviewState
	_ = s.GetExecutionState()
}
