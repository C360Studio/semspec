package reactive

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	workflow "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// Minimal in-memory KV mock for cascade executor tests
// ---------------------------------------------------------------------------

// testKVEntry implements jetstream.KeyValueEntry minimally.
type testKVEntry struct {
	key      string
	value    []byte
	revision uint64
}

func (e *testKVEntry) Bucket() string                  { return "test" }
func (e *testKVEntry) Key() string                     { return e.key }
func (e *testKVEntry) Value() []byte                   { return e.value }
func (e *testKVEntry) Revision() uint64                { return e.revision }
func (e *testKVEntry) Delta() uint64                   { return 0 }
func (e *testKVEntry) Created() time.Time              { return time.Time{} }
func (e *testKVEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

// simpleKV is a minimal in-memory KV store satisfying cascadeStateStore.
type simpleKV struct {
	mu       sync.Mutex
	data     map[string]*testKVEntry
	revision uint64
}

func newSimpleKV() *simpleKV {
	return &simpleKV{data: make(map[string]*testKVEntry)}
}

func (kv *simpleKV) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	entry, ok := kv.data[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return entry, nil
}

func (kv *simpleKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.revision++
	kv.data[key] = &testKVEntry{key: key, value: value, revision: kv.revision}
	return kv.revision, nil
}

func (kv *simpleKV) Update(_ context.Context, key string, value []byte, revision uint64) (uint64, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	existing, ok := kv.data[key]
	if !ok || existing.revision != revision {
		return 0, jetstream.ErrKeyExists // revision conflict
	}
	kv.revision++
	kv.data[key] = &testKVEntry{key: key, value: value, revision: kv.revision}
	return kv.revision, nil
}

// ---------------------------------------------------------------------------
// Definition-level tests
// ---------------------------------------------------------------------------

func TestChangeProposalWorkflow_Definition(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)

	if def.ID != ChangeProposalLoopWorkflowID {
		t.Errorf("expected ID %q, got %q", ChangeProposalLoopWorkflowID, def.ID)
	}

	expectedRules := []struct {
		id         string
		actionType reactiveEngine.ActionType
	}{
		{"accept-trigger", reactiveEngine.ActionMutate},
		{"dispatch-review", reactiveEngine.ActionPublish}, // PublishWithMutation
		{"review-completed", reactiveEngine.ActionMutate},
		{"handle-accepted", reactiveEngine.ActionPublish}, // PublishWithMutation → cascading
		{"cascade-completed", reactiveEngine.ActionComplete},
		{"handle-rejected", reactiveEngine.ActionComplete},
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

	if def.MaxIterations != 1 {
		t.Errorf("expected MaxIterations 1, got %d", def.MaxIterations)
	}
}

func TestChangeProposalWorkflow_StateFactory(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)

	got := def.StateFactory()
	if got == nil {
		t.Fatal("StateFactory returned nil")
	}
	_, ok := got.(*ChangeProposalState)
	if !ok {
		t.Errorf("StateFactory returned wrong type: %T", got)
	}
}

// ---------------------------------------------------------------------------
// accept-trigger mutator tests
// ---------------------------------------------------------------------------

func TestChangeProposalWorkflow_AcceptTrigger(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	state := &ChangeProposalState{}
	trigger := &workflow.TriggerPayload{
		Slug:       "my-plan",
		ProposalID: "change-proposal.my-plan.1",
		ProjectID:  "semspec.local.project.default",
		TraceID:    "trace-abc",
	}

	ctx := &reactiveEngine.RuleContext{
		State:   state,
		Message: trigger,
	}

	// accept-trigger uses Always() — all conditions should evaluate true.
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("condition %q should be true for accept-trigger", cond.Description)
		}
	}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Slug != "my-plan" {
		t.Errorf("expected Slug 'my-plan', got %q", state.Slug)
	}
	if state.ProposalID != "change-proposal.my-plan.1" {
		t.Errorf("expected ProposalID 'change-proposal.my-plan.1', got %q", state.ProposalID)
	}
	if state.ProjectID != "semspec.local.project.default" {
		t.Errorf("expected ProjectID 'semspec.local.project.default', got %q", state.ProjectID)
	}
	if state.TraceID != "trace-abc" {
		t.Errorf("expected TraceID 'trace-abc', got %q", state.TraceID)
	}
	if state.Phase != phases.ChangeProposalReviewing {
		t.Errorf("expected phase %q, got %q", phases.ChangeProposalReviewing, state.Phase)
	}
	if state.ID != "change-proposal.my-plan.change-proposal.my-plan.1" {
		t.Errorf("expected ID 'change-proposal.my-plan.change-proposal.my-plan.1', got %q", state.ID)
	}
	if state.WorkflowID != ChangeProposalLoopWorkflowID {
		t.Errorf("expected WorkflowID %q, got %q", ChangeProposalLoopWorkflowID, state.WorkflowID)
	}
}

func TestChangeProposalWorkflow_AcceptTrigger_MissingProposalID(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	ctx := &reactiveEngine.RuleContext{
		State: &ChangeProposalState{},
		Message: &workflow.TriggerPayload{
			Slug: "my-plan",
			// ProposalID intentionally omitted
		},
	}

	if err := rule.Action.MutateState(ctx, nil); err == nil {
		t.Fatal("expected error for missing proposal_id, got nil")
	}
}

func TestChangeProposalWorkflow_AcceptTrigger_MissingSlug(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	ctx := &reactiveEngine.RuleContext{
		State: &ChangeProposalState{},
		Message: &workflow.TriggerPayload{
			ProposalID: "change-proposal.x.1",
			// Slug intentionally omitted
		},
	}

	if err := rule.Action.MutateState(ctx, nil); err == nil {
		t.Fatal("expected error for missing slug, got nil")
	}
}

// ---------------------------------------------------------------------------
// dispatch-review payload builder tests
// ---------------------------------------------------------------------------

func TestChangeProposalWorkflow_ReviewPayload(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-review")

	state := &ChangeProposalState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:    "change-proposal.my-plan.prop-1",
			Phase: phases.ChangeProposalReviewing,
		},
		Slug:       "my-plan",
		ProposalID: "prop-1",
		ProjectID:  "plan-id-abc",
		TraceID:    "trace-xyz",
	}

	ctx := &reactiveEngine.RuleContext{State: state}

	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	req, ok := payload.(*ChangeProposalReviewRequest)
	if !ok {
		t.Fatalf("expected *ChangeProposalReviewRequest, got %T", payload)
	}

	if req.ExecutionID != "change-proposal.my-plan.prop-1" {
		t.Errorf("expected ExecutionID %q, got %q", "change-proposal.my-plan.prop-1", req.ExecutionID)
	}
	if req.ProposalID != "prop-1" {
		t.Errorf("expected ProposalID 'prop-1', got %q", req.ProposalID)
	}
	if req.Slug != "my-plan" {
		t.Errorf("expected Slug 'my-plan', got %q", req.Slug)
	}
}

// ---------------------------------------------------------------------------
// handle-accepted / handle-rejected condition tests
// ---------------------------------------------------------------------------

func TestChangeProposalWorkflow_HandleAccepted_Conditions(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-accepted")

	t.Run("all conditions satisfied", func(t *testing.T) {
		state := &ChangeProposalState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:  phases.ChangeProposalEvaluated,
				Status: reactiveEngine.StatusRunning,
			},
			Verdict: "accepted",
		}
		ctx := &reactiveEngine.RuleContext{State: state}

		for _, cond := range rule.Conditions {
			if !cond.Evaluate(ctx) {
				t.Errorf("condition %q should be true", cond.Description)
			}
		}
	})

	t.Run("wrong verdict", func(t *testing.T) {
		state := &ChangeProposalState{
			ExecutionState: reactiveEngine.ExecutionState{
				Phase:  phases.ChangeProposalEvaluated,
				Status: reactiveEngine.StatusRunning,
			},
			Verdict: "rejected",
		}
		ctx := &reactiveEngine.RuleContext{State: state}

		verdictCond := rule.Conditions[1] // "verdict is accepted"
		if verdictCond.Evaluate(ctx) {
			t.Error("verdict condition should be false when verdict is 'rejected'")
		}
	})
}

func TestChangeProposalWorkflow_HandleRejected_Conditions(t *testing.T) {
	def := BuildChangeProposalLoopWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-rejected")

	state := &ChangeProposalState{
		ExecutionState: reactiveEngine.ExecutionState{
			Phase:  phases.ChangeProposalEvaluated,
			Status: reactiveEngine.StatusRunning,
		},
		Verdict: "rejected",
	}
	ctx := &reactiveEngine.RuleContext{State: state}

	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("condition %q should be true for handle-rejected", cond.Description)
		}
	}
}

// ---------------------------------------------------------------------------
// CascadeExecutor integration tests
// ---------------------------------------------------------------------------

func TestCascadeExecutor_Execute_MarksTasksDirty(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := workflow.NewManager(tmpDir)

	proposal := buildCascadeTestData(t, ctx, m)

	const slug = "cascade-test"
	stateKey := "change-proposal.cascade-test.change-proposal.cascade-test.1"
	kv := buildCascadeKVState(t, ctx, stateKey, proposal.ID, slug)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	executor := NewCascadeExecutor(m, kv, logger)

	req := &ChangeProposalCascadeRequest{
		ExecutionID: stateKey,
		ProposalID:  proposal.ID,
		Slug:        slug,
	}
	if err := executor.Execute(ctx, req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertCascadeTaskStatuses(t, ctx, m, slug)
	assertCascadeProposalArchived(t, ctx, m, slug)
	assertCascadeKVFinalState(t, ctx, kv, stateKey, 2)
}

// buildCascadeTestData creates the plan, requirements, scenarios, tasks, and change proposal
// needed for cascade executor integration tests. It returns the saved ChangeProposal.
func buildCascadeTestData(t *testing.T, ctx context.Context, m *workflow.Manager) workflow.ChangeProposal {
	t.Helper()

	plan, err := m.CreatePlan(ctx, "cascade-test", "Cascade Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now()
	req1 := workflow.Requirement{
		ID:          "requirement.cascade-test.1",
		PlanID:      plan.ID,
		Title:       "Auth requirement",
		Description: "Users must authenticate",
		Status:      workflow.RequirementStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	req2 := workflow.Requirement{
		ID:          "requirement.cascade-test.2",
		PlanID:      plan.ID,
		Title:       "Unaffected requirement",
		Description: "Some other requirement",
		Status:      workflow.RequirementStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := m.SaveRequirements(ctx, []workflow.Requirement{req1, req2}, "cascade-test"); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	// sc1 belongs to req1 (affected), sc2 belongs to req2 (unaffected).
	sc1 := workflow.Scenario{
		ID:            "scenario.cascade-test.1.1",
		RequirementID: req1.ID,
		Given:         "user not logged in",
		When:          "visiting protected page",
		Then:          []string{"redirected to login"},
		Status:        workflow.ScenarioStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	sc2 := workflow.Scenario{
		ID:            "scenario.cascade-test.2.1",
		RequirementID: req2.ID,
		Given:         "something else",
		When:          "another action",
		Then:          []string{"another outcome"},
		Status:        workflow.ScenarioStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := m.SaveScenarios(ctx, []workflow.Scenario{sc1, sc2}, "cascade-test"); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	// task1 covers sc1 (affected), task2 covers sc2 (not affected), task3 covers both (affected).
	tasks := []workflow.Task{
		{
			ID: "task.cascade-test.1", PlanID: plan.ID, Sequence: 1,
			Description: "Implement login page", Status: workflow.TaskStatusApproved,
			ScenarioIDs: []string{sc1.ID}, CreatedAt: now,
		},
		{
			ID: "task.cascade-test.2", PlanID: plan.ID, Sequence: 2,
			Description: "Implement unrelated feature", Status: workflow.TaskStatusApproved,
			ScenarioIDs: []string{sc2.ID}, CreatedAt: now,
		},
		{
			ID: "task.cascade-test.3", PlanID: plan.ID, Sequence: 3,
			Description: "Integration task", Status: workflow.TaskStatusApproved,
			ScenarioIDs: []string{sc1.ID, sc2.ID}, CreatedAt: now,
		},
	}
	if err := m.SaveTasks(ctx, tasks, "cascade-test"); err != nil {
		t.Fatalf("SaveTasks: %v", err)
	}

	proposal := workflow.ChangeProposal{
		ID:             "change-proposal.cascade-test.1",
		PlanID:         plan.ID,
		Title:          "Expand auth scope",
		Status:         workflow.ChangeProposalStatusAccepted,
		ProposedBy:     "user",
		AffectedReqIDs: []string{req1.ID},
		CreatedAt:      now,
	}
	if err := m.SaveChangeProposals(ctx, []workflow.ChangeProposal{proposal}, "cascade-test"); err != nil {
		t.Fatalf("SaveChangeProposals: %v", err)
	}
	return proposal
}

// buildCascadeKVState creates an in-memory KV and writes the initial ChangeProposalState.
func buildCascadeKVState(t *testing.T, ctx context.Context, stateKey, proposalID, slug string) *simpleKV {
	t.Helper()
	kv := newSimpleKV()
	initialState := ChangeProposalState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:     stateKey,
			Phase:  phases.ChangeProposalCascading,
			Status: reactiveEngine.StatusRunning,
		},
		Slug:       slug,
		ProposalID: proposalID,
	}
	stateData, err := json.Marshal(initialState)
	if err != nil {
		t.Fatalf("marshal initial state: %v", err)
	}
	if _, err := kv.Put(ctx, stateKey, stateData); err != nil {
		t.Fatalf("put initial state: %v", err)
	}
	return kv
}

// assertCascadeTaskStatuses verifies that task1 and task3 are dirty and task2 is unchanged.
func assertCascadeTaskStatuses(t *testing.T, ctx context.Context, m *workflow.Manager, slug string) {
	t.Helper()
	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	taskMap := make(map[string]workflow.Task, len(tasks))
	for _, tk := range tasks {
		taskMap[tk.ID] = tk
	}
	if taskMap["task.cascade-test.1"].Status != workflow.TaskStatusDirty {
		t.Errorf("task1: expected status %q, got %q", workflow.TaskStatusDirty, taskMap["task.cascade-test.1"].Status)
	}
	if taskMap["task.cascade-test.2"].Status != workflow.TaskStatusApproved {
		t.Errorf("task2: expected status %q (unchanged), got %q", workflow.TaskStatusApproved, taskMap["task.cascade-test.2"].Status)
	}
	if taskMap["task.cascade-test.3"].Status != workflow.TaskStatusDirty {
		t.Errorf("task3: expected status %q, got %q", workflow.TaskStatusDirty, taskMap["task.cascade-test.3"].Status)
	}
}

// assertCascadeProposalArchived verifies that the change proposal was archived after execution.
func assertCascadeProposalArchived(t *testing.T, ctx context.Context, m *workflow.Manager, slug string) {
	t.Helper()
	proposals, err := m.LoadChangeProposals(ctx, slug)
	if err != nil {
		t.Fatalf("LoadChangeProposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].Status != workflow.ChangeProposalStatusArchived {
		t.Errorf("expected proposal status %q, got %q", workflow.ChangeProposalStatusArchived, proposals[0].Status)
	}
}

// assertCascadeKVFinalState verifies the KV state reached cascade_complete with the expected
// number of affected task IDs.
func assertCascadeKVFinalState(t *testing.T, ctx context.Context, kv *simpleKV, stateKey string, wantAffectedCount int) {
	t.Helper()
	entry, err := kv.Get(ctx, stateKey)
	if err != nil {
		t.Fatalf("get KV state: %v", err)
	}
	var finalState ChangeProposalState
	if err := json.Unmarshal(entry.Value(), &finalState); err != nil {
		t.Fatalf("unmarshal final state: %v", err)
	}
	if finalState.Phase != phases.ChangeProposalCascadeComplete {
		t.Errorf("expected phase %q, got %q", phases.ChangeProposalCascadeComplete, finalState.Phase)
	}
	if len(finalState.AffectedTaskIDs) != wantAffectedCount {
		t.Errorf("expected %d affected task IDs, got %d: %v", wantAffectedCount, len(finalState.AffectedTaskIDs), finalState.AffectedTaskIDs)
	}
}

func TestCascadeExecutor_Execute_NoScenariosForRequirement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := workflow.NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "no-scenarios-test", "No Scenarios Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now()
	req := workflow.Requirement{
		ID:        "requirement.no-scenarios-test.1",
		PlanID:    plan.ID,
		Title:     "Orphan requirement",
		Status:    workflow.RequirementStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.SaveRequirements(ctx, []workflow.Requirement{req}, "no-scenarios-test"); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}
	// No scenarios saved.
	// No tasks saved.

	proposal := workflow.ChangeProposal{
		ID:             "change-proposal.no-scenarios-test.1",
		PlanID:         plan.ID,
		Title:          "Change orphan req",
		Status:         workflow.ChangeProposalStatusAccepted,
		ProposedBy:     "user",
		AffectedReqIDs: []string{req.ID},
		CreatedAt:      now,
	}
	if err := m.SaveChangeProposals(ctx, []workflow.ChangeProposal{proposal}, "no-scenarios-test"); err != nil {
		t.Fatalf("SaveChangeProposals: %v", err)
	}

	kv := newSimpleKV()
	stateKey := "change-proposal.no-scenarios-test.change-proposal.no-scenarios-test.1"
	initState := ChangeProposalState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:     stateKey,
			Phase:  phases.ChangeProposalCascading,
			Status: reactiveEngine.StatusRunning,
		},
		Slug:       "no-scenarios-test",
		ProposalID: proposal.ID,
	}
	stateData, _ := json.Marshal(initState)
	if _, err := kv.Put(ctx, stateKey, stateData); err != nil {
		t.Fatalf("put initial state: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	executor := NewCascadeExecutor(m, kv, logger)

	cascadeReq := &ChangeProposalCascadeRequest{
		ExecutionID: stateKey,
		ProposalID:  proposal.ID,
		Slug:        "no-scenarios-test",
	}

	// Should succeed even with no scenarios (just archives the proposal and transitions).
	if err := executor.Execute(ctx, cascadeReq); err != nil {
		t.Fatalf("Execute with no scenarios: %v", err)
	}

	// Proposal archived.
	proposals, err := m.LoadChangeProposals(ctx, "no-scenarios-test")
	if err != nil {
		t.Fatalf("LoadChangeProposals: %v", err)
	}
	if proposals[0].Status != workflow.ChangeProposalStatusArchived {
		t.Errorf("expected archived, got %q", proposals[0].Status)
	}

	// State is cascade_complete with empty affected task IDs.
	entry, err := kv.Get(ctx, stateKey)
	if err != nil {
		t.Fatalf("get KV state: %v", err)
	}
	var finalState ChangeProposalState
	if err := json.Unmarshal(entry.Value(), &finalState); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if finalState.Phase != phases.ChangeProposalCascadeComplete {
		t.Errorf("expected %q, got %q", phases.ChangeProposalCascadeComplete, finalState.Phase)
	}
	if len(finalState.AffectedTaskIDs) != 0 {
		t.Errorf("expected 0 affected task IDs, got %d", len(finalState.AffectedTaskIDs))
	}
}

func TestCascadeExecutor_Execute_ProposalNotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := workflow.NewManager(tmpDir)

	_, err := m.CreatePlan(ctx, "missing-prop-test", "Missing Proposal Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	kv := newSimpleKV()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	executor := NewCascadeExecutor(m, kv, logger)

	req := &ChangeProposalCascadeRequest{
		ExecutionID: "change-proposal.missing-prop-test.nonexistent",
		ProposalID:  "change-proposal.missing-prop-test.nonexistent",
		Slug:        "missing-prop-test",
	}

	if err := executor.Execute(ctx, req); err == nil {
		t.Fatal("expected error for nonexistent proposal, got nil")
	}
}

// ---------------------------------------------------------------------------
// Payload serialization tests
// ---------------------------------------------------------------------------

func TestChangeProposalPayloads_Schema(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		category string
	}{
		{"accepted", "workflow", "change-proposal-accepted"},
		{"rejected", "workflow", "change-proposal-rejected"},
		{"escalate", "workflow", "change-proposal-escalate"},
		{"error", "workflow", "change-proposal-error"},
	}

	payloads := []interface{ Schema() message.Type }{
		&ChangeProposalAcceptedPayload{ProposalID: "test"},
		&ChangeProposalRejectedPayload{ProposalID: "test"},
		&ChangeProposalEscalatePayload{},
		&ChangeProposalErrorPayload{},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := payloads[i].Schema()
			if schema.Domain != tt.domain {
				t.Errorf("Domain = %q, want %q", schema.Domain, tt.domain)
			}
			if schema.Category != tt.category {
				t.Errorf("Category = %q, want %q", schema.Category, tt.category)
			}
			if schema.Version != "v1" {
				t.Errorf("Version = %q, want %q", schema.Version, "v1")
			}
		})
	}
}

func TestChangeProposalAcceptedPayload_Roundtrip(t *testing.T) {
	original := &ChangeProposalAcceptedPayload{
		ProposalID:      "change-proposal.test.1",
		PlanID:          "plan-id-123",
		Slug:            "test-plan",
		AffectedTaskIDs: []string{"task.test.1", "task.test.2"},
	}

	data, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var restored ChangeProposalAcceptedPayload
	if err := restored.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	if restored.ProposalID != original.ProposalID {
		t.Errorf("ProposalID mismatch: got %q, want %q", restored.ProposalID, original.ProposalID)
	}
	if len(restored.AffectedTaskIDs) != len(original.AffectedTaskIDs) {
		t.Errorf("AffectedTaskIDs length mismatch: got %d, want %d", len(restored.AffectedTaskIDs), len(original.AffectedTaskIDs))
	}
}

func TestChangeProposalRejectedPayload_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		p := &ChangeProposalRejectedPayload{ProposalID: "change-proposal.x.1", Slug: "x"}
		if err := p.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("missing proposal_id", func(t *testing.T) {
		p := &ChangeProposalRejectedPayload{Slug: "x"}
		if err := p.Validate(); err == nil {
			t.Error("expected error for missing proposal_id")
		}
	})
}
