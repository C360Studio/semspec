package executionmanager

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestResetRequirementFamily_DeletesBothFamiliesScoped is the #294 coverage that
// MIGRATED from plan-manager: a typed {slug, reqID} reset must delete EVERY key
// family the requirement owns — its req.<slug>.<reqID> row AND every
// task.<slug>.<id> node whose RequirementID matches — while leaving an unrelated
// requirement's row and task nodes untouched. Enumerating families here (the
// EXECUTION_STATES owner) is the fix: when plan-manager hand-filtered key
// prefixes, a stranded task node blocked re-dispatch and wedged the plan idle at
// ready_for_execution (the recovery-redispatch wedge class).
func TestResetRequirementFamily_DeletesBothFamiliesScoped(t *testing.T) {
	c := newTestComponent(t)
	ctx := testCtx(t)
	slug := "demo"

	seedReq := func(reqID string) {
		key := workflow.RequirementExecutionKey(slug, reqID)
		if err := c.store.saveReq(ctx, key, &workflow.RequirementExecution{Slug: slug, RequirementID: reqID}); err != nil {
			t.Fatalf("seed req %s: %v", reqID, err)
		}
	}
	seedTask := func(taskID, reqID string) {
		key := workflow.TaskExecutionKey(slug, taskID)
		if err := c.store.saveTask(ctx, key, &workflow.TaskExecution{Slug: slug, TaskID: taskID, RequirementID: reqID}); err != nil {
			t.Fatalf("seed task %s: %v", taskID, err)
		}
	}

	seedReq("contract")
	seedTask("node-contract-1", "contract")
	seedTask("node-contract-2", "contract")
	seedReq("unrelated")
	seedTask("node-unrelated", "unrelated")

	data, err := json.Marshal(ReqResetRequest{Slug: slug, RequirementID: "contract"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp := c.handleReqResetMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("typed family reset failed: %s", resp.Error)
	}
	if resp.ResetCount != 3 {
		t.Fatalf("ResetCount = %d, want 3 (contract req row + 2 task nodes)", resp.ResetCount)
	}

	// contract's families are gone.
	if _, ok := c.store.getReq(workflow.RequirementExecutionKey(slug, "contract")); ok {
		t.Error("contract requirement row should be deleted")
	}
	for _, taskID := range []string{"node-contract-1", "node-contract-2"} {
		if _, ok := c.store.getTask(workflow.TaskExecutionKey(slug, taskID)); ok {
			t.Errorf("contract task %s should be deleted — a stranded task node is the re-dispatch wedge (#294)", taskID)
		}
	}

	// unrelated's families are untouched: a scoped reset must not over-reach.
	if _, ok := c.store.getReq(workflow.RequirementExecutionKey(slug, "unrelated")); !ok {
		t.Error("unrelated requirement row must be preserved")
	}
	if _, ok := c.store.getTask(workflow.TaskExecutionKey(slug, "node-unrelated")); !ok {
		t.Error("unrelated task node must be preserved")
	}
}

// TestResetRequirementFamily_CancelsChildLoops confirms the typed path keeps the
// #224 guarantee: cancel the requirement's non-terminal child loops before
// deleting rows, and leave an unrelated requirement's child untouched.
func TestResetRequirementFamily_CancelsChildLoops(t *testing.T) {
	c := newTestComponent(t)
	ctx := testCtx(t)

	live := newTestExec("demo", "task-live")
	live.RequirementID = "contract"
	live.Stage = phaseDeveloping
	c.activeExecs.Set(live.EntityID, live)

	other := newTestExec("demo", "task-other")
	other.RequirementID = "unrelated"
	other.Stage = phaseDeveloping
	c.activeExecs.Set(other.EntityID, other)

	data, err := json.Marshal(ReqResetRequest{Slug: "demo", RequirementID: "contract"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if resp := c.handleReqResetMutation(ctx, data); !resp.Success {
		t.Fatalf("reset failed: %s", resp.Error)
	}

	live.mu.Lock()
	liveTerminated := live.terminated
	live.mu.Unlock()
	if !liveTerminated {
		t.Error("typed reset must cancel the requirement's child loop before deleting rows (#224)")
	}

	other.mu.Lock()
	otherTerminated := other.terminated
	other.mu.Unlock()
	if otherTerminated {
		t.Error("an unrelated requirement's child loop must not be cancelled")
	}
}

// TestResetRequirementFamily_NoEntriesIsIdempotent guards the recovery re-fire
// case: resetting a requirement with no execution rows succeeds with ResetCount
// 0 rather than erroring, so an already-clean requirement doesn't fail the
// recovery accept.
func TestResetRequirementFamily_NoEntriesIsIdempotent(t *testing.T) {
	c := newTestComponent(t)
	data, err := json.Marshal(ReqResetRequest{Slug: "demo", RequirementID: "ghost"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp := c.handleReqResetMutation(testCtx(t), data)
	if !resp.Success {
		t.Fatalf("reset of a requirement with no executions should succeed: %s", resp.Error)
	}
	if resp.ResetCount != 0 {
		t.Fatalf("ResetCount = %d, want 0", resp.ResetCount)
	}
}
