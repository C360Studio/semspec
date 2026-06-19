package executionmanager

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// These tests pin the claim compare-and-swap guard from #157:
// handleExecClaimMutation must reject a claim whose entity is not at the
// expected source stage, and two concurrent claims from the same source must
// resolve to exactly one winner (no last-writer-wins double-dispatch).

func claimData(t *testing.T, req ExecClaimRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal ExecClaimRequest: %v", err)
	}
	return data
}

func seedTask(t *testing.T, c *Component, key, stage string) {
	t.Helper()
	if err := c.store.saveTask(context.Background(), key, &workflow.TaskExecution{Stage: stage}); err != nil {
		t.Fatalf("seed task %s: %v", key, err)
	}
}

func TestHandleExecClaimMutation_SourceStageGuard(t *testing.T) {
	ctx := context.Background()

	t.Run("correct source claims successfully", func(t *testing.T) {
		c := newTestComponent(t)
		const key = "task.demo.node-ok"
		seedTask(t, c, key, "pending")

		resp := c.handleExecClaimMutation(ctx, claimData(t, ExecClaimRequest{
			Key: key, Stage: "developing", ExpectedFromStage: "pending",
		}))
		if !resp.Success {
			t.Fatalf("claim from correct source rejected: %s", resp.Error)
		}
		got, _ := c.store.getTask(key)
		if got.Stage != "developing" {
			t.Errorf("stage = %q, want developing", got.Stage)
		}
	})

	t.Run("wrong source is rejected and leaves stage unchanged", func(t *testing.T) {
		c := newTestComponent(t)
		const key = "task.demo.node-wrong"
		seedTask(t, c, key, "developing") // already past pending

		resp := c.handleExecClaimMutation(ctx, claimData(t, ExecClaimRequest{
			Key: key, Stage: "reviewing", ExpectedFromStage: "pending",
		}))
		if resp.Success {
			t.Fatal("claim from wrong source stage succeeded; want rejected (CAS precondition)")
		}
		got, _ := c.store.getTask(key)
		if got.Stage != "developing" {
			t.Errorf("stage = %q, want developing (unchanged after rejected claim)", got.Stage)
		}
	})

	t.Run("legacy claim without ExpectedFromStage still works and rejects dup", func(t *testing.T) {
		c := newTestComponent(t)
		const key = "task.demo.node-legacy"
		seedTask(t, c, key, "pending")

		// No ExpectedFromStage → legacy path: advances on a different target.
		resp := c.handleExecClaimMutation(ctx, claimData(t, ExecClaimRequest{Key: key, Stage: "developing"}))
		if !resp.Success {
			t.Fatalf("legacy claim rejected: %s", resp.Error)
		}
		// Re-claiming the same (now current) target is rejected as a duplicate.
		dup := c.handleExecClaimMutation(ctx, claimData(t, ExecClaimRequest{Key: key, Stage: "developing"}))
		if dup.Success {
			t.Fatal("duplicate legacy claim succeeded; want rejected (already at stage)")
		}
	})
}

// TestHandleExecClaimMutation_ConcurrentSameSource is the acceptance test for
// #157(b): N goroutines race to claim the same entity from the same source
// stage; exactly one must win. Without the claimMu + CAS guard, multiple read
// "pending" and all save "developing" (double-dispatch). The get-check-save race
// is probabilistic per round, so we run many rounds and require EVERY round to
// have exactly one winner — a missing mutex then fails deterministically in a
// single run rather than flaking. (Verified: with the lock removed this fails.)
func TestHandleExecClaimMutation_ConcurrentSameSource(t *testing.T) {
	const (
		rounds   = 50
		claimers = 16
	)
	c := newTestComponent(t)
	const key = "task.demo.node-race"

	for round := 0; round < rounds; round++ {
		seedTask(t, c, key, "pending") // reset to the source stage each round

		var wins int64
		var start, done sync.WaitGroup
		start.Add(1)
		for i := 0; i < claimers; i++ {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait() // release together to maximize interleaving
				resp := c.handleExecClaimMutation(context.Background(), claimData(t, ExecClaimRequest{
					Key: key, Stage: "developing", ExpectedFromStage: "pending",
				}))
				if resp.Success {
					atomic.AddInt64(&wins, 1)
				}
			}()
		}
		start.Done()
		done.Wait()

		if wins != 1 {
			t.Fatalf("round %d: %d winners, want exactly 1 (double-dispatch guard)", round, wins)
		}
		if got, _ := c.store.getTask(key); got.Stage != "developing" {
			t.Fatalf("round %d: final stage = %q, want developing", round, got.Stage)
		}
	}
}

// TestHandleExecClaimMutation_ReqSourceStageGuard covers the requirement-execution
// branch of the same guard (the handler tries task then req).
func TestHandleExecClaimMutation_ReqSourceStageGuard(t *testing.T) {
	ctx := context.Background()
	c := newTestComponent(t)
	const key = "req.demo.feature"
	if err := c.store.saveReq(ctx, key, &workflow.RequirementExecution{Stage: "pending"}); err != nil {
		t.Fatalf("seed req: %v", err)
	}

	// Wrong source rejected.
	bad := c.handleExecClaimMutation(ctx, claimData(t, ExecClaimRequest{
		Key: key, Stage: "decomposing", ExpectedFromStage: "executing",
	}))
	if bad.Success {
		t.Fatal("req claim from wrong source succeeded; want rejected")
	}
	// Correct source succeeds.
	ok := c.handleExecClaimMutation(ctx, claimData(t, ExecClaimRequest{
		Key: key, Stage: "decomposing", ExpectedFromStage: "pending",
	}))
	if !ok.Success {
		t.Fatalf("req claim from correct source rejected: %s", ok.Error)
	}
	got, _ := c.store.getReq(key)
	if got.Stage != "decomposing" {
		t.Errorf("req stage = %q, want decomposing", got.Stage)
	}
}
