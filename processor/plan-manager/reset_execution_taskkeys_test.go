package planmanager

import (
	"context"
	"testing"
)

// TestResetRequirementExecutions_All_ClearsTaskNodeKeys reproduces the
// 2026-06-16 hybrid-gpt5 mavlink-hard wedge.
//
// Sequence observed live (plan 94dd8f41465d):
//  1. requirement.1 dev node hit a "restructure rejection" → markEscalatedLocked
//     → RecoveryRequested → recovery-agent chose architecture_revise
//     (recovery_succeeded=true).
//  2. applyArchitectureRevise back-transitioned the plan to
//     requirements_generated and called resetRequirementExecutions(scope="all")
//     to "wipe ... all requirement executions", then the pipeline re-planned
//     forward to ready_for_execution.
//  3. The plan then WEDGED at ready_for_execution (idle, never re-dispatched).
//
// Root cause: EXECUTION_STATES carries per-node task executions keyed
// "task.<slug>.node-*" (execution-manager owns them), but
// resetRequirementExecutions only matches the "req.<slug>." prefix. The
// scope="all" reset therefore skips the escalated task-node entries; they
// orphan and block re-dispatch.
//
// This test seeds the bucket with the live key shapes (a req.* entry AND two
// task.*.node-* entries — exactly what the EXECUTION_STATES dump showed) and
// asserts scope="all" clears EVERY execution entry for the plan. It FAILS
// against the current "req." prefix filter and passes once the reset also
// covers task-node keys.
func TestResetRequirementExecutions_All_ClearsTaskNodeKeys(t *testing.T) {
	c := setupTestComponent(t)
	c.execBucket = resetKVStub{keys: []string{
		"req.demo.1",                            // requirement-level execution
		"task.demo.node-eb2926e6097b66c7-aaaa",  // approved task node
		"task.demo.node-0ea0bba39b5a2ac6-bbbb",  // escalated task node (the orphan)
	}}
	var reset []string
	c.reqResetSender = func(_ context.Context, key string) error {
		reset = append(reset, key)
		return nil
	}

	n, err := c.resetRequirementExecutions(context.Background(), "demo", "all", nil)
	if err != nil {
		t.Fatalf("resetRequirementExecutions returned error: %v", err)
	}

	// scope="all" must clear every execution entry for the plan — the req.*
	// entry AND both task.*.node-* entries. The current prefix filter only
	// matches "req.demo." so n==1 and the task nodes survive.
	if n != 3 {
		t.Errorf("reset count = %d, want 3 (1 req + 2 task nodes); task.<slug>.node-* keys are being skipped by the req.-prefix filter", n)
	}
	for _, want := range []string{
		"task.demo.node-eb2926e6097b66c7-aaaa",
		"task.demo.node-0ea0bba39b5a2ac6-bbbb",
	} {
		found := false
		for _, k := range reset {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("task-node execution %q was NOT reset by scope=all — architecture_revise leaves it orphaned, which blocks re-dispatch (the ready_for_execution wedge)", want)
		}
	}
}
