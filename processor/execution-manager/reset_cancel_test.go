package executionmanager

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleReqResetMutation_CancelsChildLoops pins #224: a requirement reset
// (architecture_revise / story_reprepare / scope_incomplete / retry) must cancel
// the requirement's non-terminal child loops before deleting the row, so a stale
// dev/reviewer/validator loop doesn't keep burning tokens or write stale
// artifacts after its execution row is gone. An unrelated requirement's child
// must be left untouched.
func TestHandleReqResetMutation_CancelsChildLoops(t *testing.T) {
	c := newTestComponent(t)

	// Live child task exec for the requirement being reset (not terminated).
	live := newTestExec("plan-reset", "task-live")
	live.RequirementID = "req-a"
	live.Stage = phaseDeveloping
	c.activeExecs.Set(live.EntityID, live)

	// A child for a DIFFERENT requirement must NOT be touched by this reset.
	other := newTestExec("plan-reset", "task-other")
	other.RequirementID = "req-b"
	other.Stage = phaseDeveloping
	c.activeExecs.Set(other.EntityID, other)

	// Seed the requirement execution row so the handler resolves slug+reqID
	// from the stored entry (no fragile key parsing).
	reqKey := workflow.RequirementExecutionKey("plan-reset", "req-a")
	if err := c.store.saveReq(testCtx(t), reqKey, &workflow.RequirementExecution{
		Slug: "plan-reset", RequirementID: "req-a",
	}); err != nil {
		t.Fatalf("seed req exec: %v", err)
	}

	data, err := json.Marshal(ReqResetRequest{Key: reqKey})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if resp := c.handleReqResetMutation(testCtx(t), data); !resp.Success {
		t.Fatalf("reset failed: %s", resp.Error)
	}

	live.mu.Lock()
	liveTerminated := live.terminated
	live.mu.Unlock()
	if !liveTerminated {
		t.Error("child loop for the reset requirement should be cancelled/terminated (#224)")
	}

	other.mu.Lock()
	otherTerminated := other.terminated
	other.mu.Unlock()
	if otherTerminated {
		t.Error("child loop for an UNRELATED requirement must not be touched by the reset")
	}
}

// TestHandleReqResetMutation_NoReqEntryStillSucceeds confirms the cancellation is
// best-effort: a reset whose key has no stored requirement entry (e.g. a task.*
// key, or an already-evicted row) still deletes and returns success without
// cancelling anything.
func TestHandleReqResetMutation_NoReqEntryStillSucceeds(t *testing.T) {
	c := newTestComponent(t)
	data, err := json.Marshal(ReqResetRequest{Key: "req.plan-x.req-missing"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if resp := c.handleReqResetMutation(testCtx(t), data); !resp.Success {
		t.Fatalf("reset of a missing requirement should still succeed (idempotent), got: %s", resp.Error)
	}
}
