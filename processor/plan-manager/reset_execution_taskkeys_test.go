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
		"req.demo.1",                           // requirement-level execution
		"task.demo.node-eb2926e6097b66c7-aaaa", // approved task node
		"task.demo.node-0ea0bba39b5a2ac6-bbbb", // escalated task node (the orphan)
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

// TestResetRequirementExecutionsByID_SendsTypedFamilyReset pins the #294 typed
// boundary: the requirement-scoped reset names {slug, reqID} per requirement and
// lets execution-manager (the EXECUTION_STATES owner) enumerate the families. So
// plan-manager must send a typed descriptor for EXACTLY the requested reqID (not
// "unrelated") and return the entry count execution-manager reports. The family
// enumeration itself — that "contract"'s task node is deleted but "unrelated"'s
// is not — is now asserted in execution-manager's
// TestResetRequirementFamily_DeletesBothFamiliesScoped, not here.
func TestResetRequirementExecutionsByID_SendsTypedFamilyReset(t *testing.T) {
	c := setupTestComponent(t)

	var sent []string // "slug/reqID" descriptors plan-manager issued
	c.reqFamilyResetSender = func(_ context.Context, slug, reqID string) (int, error) {
		sent = append(sent, slug+"/"+reqID)
		return 2, nil // execution-manager deleted the req row + 1 task node
	}

	n, err := c.resetRequirementExecutionsByID(context.Background(), "demo", []string{"contract"})
	if err != nil {
		t.Fatalf("resetRequirementExecutionsByID returned error: %v", err)
	}
	if n != 2 {
		t.Fatalf("reset count = %d, want 2 (summed from execution-manager's family delete)", n)
	}
	if len(sent) != 1 || sent[0] != "demo/contract" {
		t.Fatalf("typed resets sent = %v, want [demo/contract] (execution-manager owns family enumeration)", sent)
	}
}

func TestResetRequirementExecutions_Failed_ClearsEscalatedTaskNodeKeys(t *testing.T) {
	c := setupTestComponent(t)
	c.execBucket = resetKVStub{
		keys: []string{
			"req.demo.contract",
			"task.demo.node-contract",
			"req.demo.unrelated",
			"task.demo.node-unrelated",
		},
		values: map[string][]byte{
			"req.demo.contract":       []byte(`{"stage":"failed"}`),
			"task.demo.node-contract": []byte(`{"requirement_id":"contract","stage":"escalated"}`),
			"req.demo.unrelated":      []byte(`{"stage":"approved"}`),
			"task.demo.node-unrelated": []byte(
				`{"requirement_id":"unrelated","stage":"approved"}`,
			),
		},
	}
	var reset []string
	c.reqResetSender = func(_ context.Context, key string) error {
		reset = append(reset, key)
		return nil
	}

	n, err := c.resetRequirementExecutions(context.Background(), "demo", "failed", nil)
	if err != nil {
		t.Fatalf("resetRequirementExecutions returned error: %v", err)
	}
	if n != 2 {
		t.Fatalf("reset count = %d, want 2 (failed req + escalated task)", n)
	}
	assertResetKeys(t, reset, []string{
		"req.demo.contract",
		"task.demo.node-contract",
	})
	assertNoResetKeys(t, reset, []string{
		"req.demo.unrelated",
		"task.demo.node-unrelated",
	})
}
