package requirementexecutor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestAbandonExecsForSlug verifies the architecture_revise teardown
// (go-reviewer C2/H1): every active exec for the target slug is marked
// terminated and removed from activeExecs, while execs for OTHER slugs are
// left untouched. This is what stops the wedged exec from being resumed on
// the accepted event and prevents the abandoned exec from racing the fresh
// architect-driven re-run on the shared req.<slug>.<reqID> key.
func TestAbandonExecsForSlug(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)

	// Distinct EntityIDs — newAwaitingExec derives EntityID from reqID alone,
	// so a same-reqID exec on a different slug would otherwise collide in the
	// cache.
	target1 := newAwaitingExec("mavlink-hard", "req-0")
	target1.EntityID = "entity-mavlink-req-0"
	target1.awaitingRecovery = true
	target2 := newAwaitingExec("mavlink-hard", "req-1") // sibling, still "running"
	target2.EntityID = "entity-mavlink-req-1"
	other := newAwaitingExec("other-plan", "req-0") // different slug — must survive
	other.EntityID = "entity-other-req-0"

	c.activeExecs.Set(target1.EntityID, target1)
	c.activeExecs.Set(target2.EntityID, target2)
	c.activeExecs.Set(other.EntityID, other)

	abandoned := c.abandonExecsForSlug("mavlink-hard")

	if abandoned != 2 {
		t.Errorf("abandoned = %d, want 2 (both mavlink-hard execs)", abandoned)
	}
	if _, ok := c.activeExecs.Get(target1.EntityID); ok {
		t.Error("target1 should have been removed from activeExecs")
	}
	if _, ok := c.activeExecs.Get(target2.EntityID); ok {
		t.Error("target2 should have been removed from activeExecs")
	}
	if !target1.terminated {
		t.Error("target1 should be marked terminated")
	}
	if target1.awaitingRecovery {
		t.Error("target1.awaitingRecovery should be cleared (won't be resumed)")
	}

	// The other-plan exec must be untouched.
	if _, ok := c.activeExecs.Get(other.EntityID); !ok {
		t.Error("other-plan exec must NOT be removed")
	}
	if other.terminated {
		t.Error("other-plan exec must NOT be marked terminated")
	}
}

// TestAbandonExecsForSlug_Empty verifies no panic / zero count when there are
// no execs for the slug.
func TestAbandonExecsForSlug_Empty(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	if got := c.abandonExecsForSlug("nonexistent"); got != 0 {
		t.Errorf("abandoned = %d, want 0", got)
	}
}

func TestAbandonExecsForRequirements_Scoped(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)

	contract := newAwaitingExec("generic-plan", "contract")
	contract.EntityID = "entity-contract"
	consumer := newAwaitingExec("generic-plan", "consumer")
	consumer.EntityID = "entity-consumer"
	unrelated := newAwaitingExec("generic-plan", "unrelated")
	unrelated.EntityID = "entity-unrelated"
	otherPlan := newAwaitingExec("other-plan", "contract")
	otherPlan.EntityID = "entity-other-contract"

	c.activeExecs.Set(contract.EntityID, contract)
	c.activeExecs.Set(consumer.EntityID, consumer)
	c.activeExecs.Set(unrelated.EntityID, unrelated)
	c.activeExecs.Set(otherPlan.EntityID, otherPlan)

	abandoned := c.abandonExecsForRequirements("generic-plan", []string{"contract", "consumer", "contract"})

	if abandoned != 2 {
		t.Fatalf("abandoned = %d, want 2", abandoned)
	}
	for _, exec := range []*requirementExecution{contract, consumer} {
		if _, ok := c.activeExecs.Get(exec.EntityID); ok {
			t.Fatalf("%s should have been removed from activeExecs", exec.RequirementID)
		}
		if !exec.terminated {
			t.Fatalf("%s should be marked terminated", exec.RequirementID)
		}
	}
	for _, exec := range []*requirementExecution{unrelated, otherPlan} {
		if _, ok := c.activeExecs.Get(exec.EntityID); !ok {
			t.Fatalf("%s should remain active", exec.EntityID)
		}
		if exec.terminated {
			t.Fatalf("%s should not be terminated", exec.EntityID)
		}
	}
}

func TestHandlePlanDecisionAccepted_StoryReprepareAbandonsInsteadOfResuming(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)

	exec := newAwaitingExec("mavlink-hard", "req-1")
	exec.EntityID = "entity-mavlink-req-1"
	exec.awaitingRecovery = true
	c.activeExecs.Set(exec.EntityID, exec)

	evt := payloads.PlanDecisionAcceptedEvent{
		ProposalID:             "plan-decision.mavlink-hard.recovery.story",
		Slug:                   "mavlink-hard",
		Kind:                   workflow.PlanDecisionKindStoryReprepare,
		AffectedRequirementIDs: []string{"req-1"},
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	envelope, err := json.Marshal(map[string]json.RawMessage{"payload": payload})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	msg := &mockMsg{data: envelope}

	c.handlePlanDecisionAccepted(context.Background(), context.Background(), msg)

	if !msg.acked {
		t.Fatal("accepted event should be acked")
	}
	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Fatal("story_reprepare should abandon stale active exec instead of resuming it")
	}
	if !exec.terminated {
		t.Fatal("abandoned exec should be marked terminated")
	}
}

func TestHandlePlanDecisionAccepted_ScopeIncompleteAbandonsInsteadOfQAResume(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)

	exec := newAwaitingExec("mavlink-hard", "req-1")
	exec.EntityID = "entity-mavlink-scope-req-1"
	exec.awaitingRecovery = true
	c.activeExecs.Set(exec.EntityID, exec)

	evt := payloads.PlanDecisionAcceptedEvent{
		ProposalID:             "plan-decision.mavlink-hard.scope-incomplete",
		Slug:                   "mavlink-hard",
		Kind:                   workflow.PlanDecisionKindScopeIncomplete,
		AffectedRequirementIDs: []string{"req-1"},
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	envelope, err := json.Marshal(map[string]json.RawMessage{"payload": payload})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	msg := &mockMsg{data: envelope}

	c.handlePlanDecisionAccepted(context.Background(), context.Background(), msg)

	if !msg.acked {
		t.Fatal("accepted event should be acked")
	}
	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Fatal("scope_incomplete should abandon stale active exec because plan-manager owns the retry")
	}
	if !exec.terminated {
		t.Fatal("abandoned exec should be marked terminated")
	}
}
