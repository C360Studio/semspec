package executionmanager

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// TestHandleRequirementUpdate_NonTerminal_NoOp: stage transitions that aren't
// terminal must not trigger cancellation. Running this path on every KV put
// would thrash the cancel fan-out on every stage blink.
func TestHandleRequirementUpdate_NonTerminal_NoOp(t *testing.T) {
	c := newTestComponent(t)

	// Seed a live child that would be cancelled if the guard failed.
	exec := newTestExec("plan", "task-live")
	exec.RequirementID = "req-a"
	exec.Stage = phaseDeveloping
	c.activeExecs.Set(exec.EntityID, exec)

	reqExec := workflow.RequirementExecution{
		Slug:          "plan",
		RequirementID: "req-a",
		Stage:         "executing", // NOT terminal
	}
	value, _ := json.Marshal(reqExec)

	c.handleRequirementUpdate(testCtx(t), &kvEntryStub{key: "req.plan.req-a", value: value})

	if exec.terminated {
		t.Error("live child must not be terminated for non-terminal parent stage")
	}
}

// TestHandleRequirementUpdate_Completed_NoOp: happy-path completion shouldn't
// trigger the cancel cascade — children merged successfully before parent
// could reach completed. Guards against wasted AGENT_LOOPS scans.
func TestHandleRequirementUpdate_Completed_NoOp(t *testing.T) {
	c := newTestComponent(t)

	exec := newTestExec("plan", "task-done")
	exec.RequirementID = "req-b"
	exec.Stage = phaseApproved
	exec.terminated = true // already done via normal path
	c.activeExecs.Set(exec.EntityID, exec)

	reqExec := workflow.RequirementExecution{
		Slug:          "plan",
		RequirementID: "req-b",
		Stage:         "completed",
	}
	value, _ := json.Marshal(reqExec)

	// Should not crash / do anything visible.
	c.handleRequirementUpdate(testCtx(t), &kvEntryStub{key: "req.plan.req-b", value: value})
}

// TestCancelChildrenForRequirement_ScopesToMatching: only TaskExecutions with
// matching RequirementID (and slug) are cancelled. Tasks belonging to other
// requirements on the same plan, and tasks created outside requirement-executor
// (empty RequirementID), must be untouched.
func TestCancelChildrenForRequirement_ScopesToMatching(t *testing.T) {
	c := newTestComponent(t)

	// Child of the dying requirement — should be cancelled.
	in := newTestExec("plan", "task-in")
	in.RequirementID = "req-x"
	in.Stage = phaseDeveloping
	c.activeExecs.Set(in.EntityID, in)

	// Child of a different requirement — should be untouched.
	otherReq := newTestExec("plan", "task-other-req")
	otherReq.RequirementID = "req-y"
	otherReq.Stage = phaseDeveloping
	c.activeExecs.Set(otherReq.EntityID, otherReq)

	// Different plan, same reqID string — slug filter must still exclude.
	otherSlug := newTestExec("plan-b", "task-other-slug")
	otherSlug.RequirementID = "req-x"
	otherSlug.Stage = phaseDeveloping
	c.activeExecs.Set(otherSlug.EntityID, otherSlug)

	// Adhoc task (no RequirementID) — must never be cancelled by the cascade.
	adhoc := newTestExec("plan", "task-adhoc")
	adhoc.RequirementID = ""
	adhoc.Stage = phaseDeveloping
	c.activeExecs.Set(adhoc.EntityID, adhoc)

	// Already-terminal child — must not be re-marked or double-counted.
	done := newTestExec("plan", "task-done")
	done.RequirementID = "req-x"
	done.Stage = phaseApproved
	done.terminated = true
	c.activeExecs.Set(done.EntityID, done)

	c.cancelChildrenForRequirement(testCtx(t), "plan", "req-x", "parent_requirement_failed")

	if !in.terminated {
		t.Error("matching in-flight child should be terminated by cascade")
	}
	if in.Stage != phaseError {
		t.Errorf("matching child stage: want %q, got %q", phaseError, in.Stage)
	}
	if otherReq.terminated {
		t.Error("child of different requirement must not be terminated")
	}
	if otherSlug.terminated {
		t.Error("child of different slug must not be terminated")
	}
	if adhoc.terminated {
		t.Error("adhoc task (no RequirementID) must not be terminated by the cascade")
	}
	// Pre-existing terminal state preserved.
	if done.Stage != phaseApproved {
		t.Errorf("already-terminal child stage should stay %q, got %q", phaseApproved, done.Stage)
	}
}

// TestCancelChildrenForRequirement_EmptyRequirementID_NoOp: guards the cheap
// early exit that prevents a cascade for a malformed parent entry.
func TestCancelChildrenForRequirement_EmptyRequirementID_NoOp(t *testing.T) {
	c := newTestComponent(t)

	exec := newTestExec("plan", "task-z")
	exec.RequirementID = ""
	exec.Stage = phaseDeveloping
	c.activeExecs.Set(exec.EntityID, exec)

	c.cancelChildrenForRequirement(testCtx(t), "plan", "", "parent_requirement_failed")

	if exec.terminated {
		t.Error("empty requirementID must be a no-op")
	}
}

// kvEntryStub is a minimal jetstream.KeyValueEntry for tests. Only the fields
// handleRequirementUpdate reads are implemented; the rest return zero values.
type kvEntryStub struct {
	key   string
	value []byte
}

func (k *kvEntryStub) Bucket() string     { return "EXECUTION_STATES" }
func (k *kvEntryStub) Key() string        { return k.key }
func (k *kvEntryStub) Value() []byte      { return k.value }
func (k *kvEntryStub) Revision() uint64   { return 1 }
func (k *kvEntryStub) Created() time.Time { return time.Time{} }
func (k *kvEntryStub) Delta() uint64      { return 0 }
func (k *kvEntryStub) Operation() jetstream.KeyValueOp {
	return jetstream.KeyValuePut
}
