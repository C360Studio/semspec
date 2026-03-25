package executionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
)

// ---------------------------------------------------------------------------
// mockAgentKV — minimal in-memory KVStore for agent-roster unit tests.
// Satisfies the agentgraph.KVStore interface without requiring NATS.
// ---------------------------------------------------------------------------

type mockAgentKV struct {
	data map[string][]byte
}

func newMockAgentKV() *mockAgentKV {
	return &mockAgentKV{data: make(map[string][]byte)}
}

func (m *mockAgentKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, errors.New("kv: key not found")
	}
	return &natsclient.KVEntry{Key: key, Value: v, Revision: 1}, nil
}

func (m *mockAgentKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	m.data[key] = value
	return 1, nil
}

func (m *mockAgentKV) UpdateWithRetry(_ context.Context, key string, updateFn func(current []byte) ([]byte, error)) error {
	current := m.data[key] // nil when key is absent — matches NATS "not found" semantics
	updated, err := updateFn(current)
	if err != nil {
		return err
	}
	m.data[key] = updated
	return nil
}

func (m *mockAgentKV) KeysByPrefix(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// categoriesJSON mirrors the constant in workflow/error_category_test.go.
// Duplicated here because the workflow package's test-only constant is not
// exported. The category set is the same authoritative 7-entry list.
// ---------------------------------------------------------------------------

const agentTestCategoriesJSON = `{
	"categories": [
		{
			"id": "missing_tests",
			"label": "Missing Tests",
			"description": "No tests submitted with implementation.",
			"signals": ["No test file created"],
			"guidance": "Create test files alongside implementation."
		},
		{
			"id": "wrong_pattern",
			"label": "Wrong Pattern",
			"description": "Uses a non-idiomatic pattern.",
			"signals": ["Shared memory where channels expected"],
			"guidance": "Follow established project conventions."
		},
		{
			"id": "sop_violation",
			"label": "SOP Violation",
			"description": "Violates a standard operating procedure.",
			"signals": ["SOP rule referenced in feedback"],
			"guidance": "Re-read each SOP rule in the task context."
		},
		{
			"id": "incomplete_implementation",
			"label": "Incomplete Implementation",
			"description": "Missing required components.",
			"signals": ["TODO left in code"],
			"guidance": "All criteria must be fully addressed."
		},
		{
			"id": "edge_case_missed",
			"label": "Edge Case Missed",
			"description": "Boundary conditions not handled.",
			"signals": ["No nil guard"],
			"guidance": "Handle nil, empty, and boundary values."
		},
		{
			"id": "api_contract_mismatch",
			"label": "API Contract Mismatch",
			"description": "Diverges from the API contract.",
			"signals": ["Wrong function signature"],
			"guidance": "Cross-reference against the API contract."
		},
		{
			"id": "scope_violation",
			"label": "Scope Violation",
			"description": "Changes outside the defined scope.",
			"signals": ["Files modified outside task scope"],
			"guidance": "Only modify files in task scope."
		}
	]
}`

// ---------------------------------------------------------------------------
// newAgentTestComponent builds a Component with an in-memory agentHelper and
// a loaded errorCategories registry. The BenchingThreshold is set to the
// workflow default (3) so benching fires after the third matching increment.
// ---------------------------------------------------------------------------

func newAgentTestComponent(t *testing.T) (*Component, *agentgraph.Helper) {
	t.Helper()

	c := newTestComponent(t)

	kv := newMockAgentKV()
	helper := agentgraph.NewHelper(kv)
	c.agentHelper = helper

	reg, err := workflow.LoadErrorCategoriesFromBytes([]byte(agentTestCategoriesJSON))
	if err != nil {
		t.Fatalf("newAgentTestComponent: load error categories: %v", err)
	}
	c.errorCategories = reg
	c.config.BenchingThreshold = workflow.DefaultBenchingThreshold // 3

	return c, helper
}

// newTestAgent creates a workflow.Agent and persists it via helper.CreateAgent.
// Returns the created Agent value for convenient field access in assertions.
func newTestAgent(t *testing.T, ctx context.Context, helper *agentgraph.Helper, id, role, model string) workflow.Agent {
	t.Helper()
	now := time.Now()
	agent := workflow.Agent{
		ID:        id,
		Name:      role + "-" + id,
		Role:      role,
		Model:     model,
		Status:    workflow.AgentAvailable,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := helper.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("newTestAgent: create %q: %v", id, err)
	}
	return agent
}

// ---------------------------------------------------------------------------
// TestCheckAgentBenching_ClassifiesAndBenches verifies that calling
// checkAgentBenching with feedback that matches the "missing_tests" category
// signal increments the error count for that category. After DefaultBenchingThreshold
// (3) calls the agent is benched and the function returns true.
// ---------------------------------------------------------------------------

func TestCheckAgentBenching_ClassifiesAndBenches(t *testing.T) {
	ctx := testCtx(t)
	c, helper := newAgentTestComponent(t)

	agent := newTestAgent(t, ctx, helper, "agent-bench-01", "developer", "default")

	exec := newTestExec("plan-bench", "task-bench")
	exec.AgentID = agent.ID

	// "No test file created" matches the "missing_tests" signal.
	feedback := "No test file created alongside this change."

	// First two calls should not bench the agent (counts 1, 2 — threshold is 3).
	for i := 0; i < workflow.DefaultBenchingThreshold-1; i++ {
		benched := c.checkAgentBenching(ctx, exec, feedback)
		if benched {
			t.Fatalf("call %d: expected not benched yet (threshold %d), got benched",
				i+1, workflow.DefaultBenchingThreshold)
		}
	}

	// Third call reaches the threshold — agent should be benched.
	benched := c.checkAgentBenching(ctx, exec, feedback)
	if !benched {
		t.Error("expected agent to be benched after reaching threshold, got false")
	}

	// Confirm status persisted in the graph.
	stored, err := helper.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent after benching: %v", err)
	}
	if stored.Status != workflow.AgentBenched {
		t.Errorf("stored agent status = %q, want %q", stored.Status, workflow.AgentBenched)
	}
}

// ---------------------------------------------------------------------------
// TestCheckAgentBenching_NilHelper verifies that checkAgentBenching returns
// false without panicking when c.agentHelper is nil.
// ---------------------------------------------------------------------------

func TestCheckAgentBenching_NilHelper(t *testing.T) {
	c := newTestComponent(t)
	// agentHelper is nil by default from newTestComponent.

	exec := newTestExec("plan-nil", "task-nil")
	exec.AgentID = "some-agent-id"

	// Must not panic; must return false.
	benched := c.checkAgentBenching(testCtx(t), exec, "No test file created")
	if benched {
		t.Error("checkAgentBenching with nil helper should return false, got true")
	}
}

// ---------------------------------------------------------------------------
// TestCheckAgentBenching_EmptyFeedback verifies that empty feedback text does
// not trigger any error-count increment or benching, even when an agent exists.
// ---------------------------------------------------------------------------

func TestCheckAgentBenching_EmptyFeedback(t *testing.T) {
	ctx := testCtx(t)
	c, helper := newAgentTestComponent(t)

	agent := newTestAgent(t, ctx, helper, "agent-empty-01", "developer", "default")

	exec := newTestExec("plan-empty", "task-empty")
	exec.AgentID = agent.ID

	// Empty feedback must not increment counts or bench the agent.
	benched := c.checkAgentBenching(ctx, exec, "")
	if benched {
		t.Error("checkAgentBenching with empty feedback should not bench, got true")
	}

	// Confirm no error counts were written.
	stored, err := helper.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if total := stored.TotalErrorCount(); total != 0 {
		t.Errorf("TotalErrorCount = %d, want 0 after empty feedback", total)
	}
}

// ---------------------------------------------------------------------------
// TestSelectReplacementAgent_FindsAvailable verifies that when one agent is
// benched and another is available, selectReplacementAgent returns the available
// one (without creating a new agent).
// ---------------------------------------------------------------------------

func TestSelectReplacementAgent_FindsAvailable(t *testing.T) {
	ctx := testCtx(t)
	c, helper := newAgentTestComponent(t)

	// Create a benched agent and an available agent, both with role "developer".
	newTestAgent(t, ctx, helper, "agent-benched-01", "developer", "default")
	newTestAgent(t, ctx, helper, "agent-avail-01", "developer", "default")

	// Bench the first agent.
	if err := helper.SetAgentStatus(ctx, "agent-benched-01", workflow.AgentBenched); err != nil {
		t.Fatalf("SetAgentStatus benched: %v", err)
	}

	exec := newTestExec("plan-replace", "task-replace")

	result := c.selectReplacementAgent(ctx, exec)
	if result == nil {
		t.Fatal("selectReplacementAgent: expected non-nil result, got nil")
	}
	if result.ID != "agent-avail-01" {
		t.Errorf("selected agent ID = %q, want %q", result.ID, "agent-avail-01")
	}
	if result.Status != workflow.AgentAvailable {
		t.Errorf("selected agent status = %q, want %q", result.Status, workflow.AgentAvailable)
	}
}

// ---------------------------------------------------------------------------
// TestSelectReplacementAgent_NilHelper verifies that selectReplacementAgent
// returns nil without panicking when c.agentHelper is nil.
// ---------------------------------------------------------------------------

func TestSelectReplacementAgent_NilHelper(t *testing.T) {
	c := newTestComponent(t)
	// agentHelper is nil by default.

	exec := newTestExec("plan-nil-replace", "task-nil-replace")

	result := c.selectReplacementAgent(testCtx(t), exec)
	if result != nil {
		t.Errorf("selectReplacementAgent with nil helper should return nil, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// TestSelectReplacementAgent_AllBenchedNoModels verifies that when all
// existing agents are benched and no model registry is configured, the
// function returns nil rather than creating a new agent.
// ---------------------------------------------------------------------------

func TestSelectReplacementAgent_AllBenchedNoModels(t *testing.T) {
	ctx := testCtx(t)
	c, helper := newAgentTestComponent(t)

	// Create a single agent and immediately bench it.
	newTestAgent(t, ctx, helper, "agent-only-01", "developer", "default")
	if err := helper.SetAgentStatus(ctx, "agent-only-01", workflow.AgentBenched); err != nil {
		t.Fatalf("SetAgentStatus benched: %v", err)
	}

	// Clear the model registry so the fallback chain is empty.
	c.modelRegistry = nil

	exec := newTestExec("plan-no-model", "task-no-model")

	result := c.selectReplacementAgent(ctx, exec)
	if result != nil {
		t.Errorf("selectReplacementAgent with all benched and no model registry should return nil, got %+v", result)
	}
}
