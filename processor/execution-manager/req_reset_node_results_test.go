package executionmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleReqResetNodeResultsMutation_WipesSliceAndPreservesOtherFields
// pins the contract for the new execution.mutation.req.reset_node_results
// mutation introduced to close go-reviewer Pass-1 finding H4.
//
// Pre-mutation, requirement-executor wiped NodeResults in memory during
// recovery resume and restructure retry but left the KV-side slice
// intact (handleReqNodeMutation only appends). On the next restart,
// rebuildExecFromKV restored the stale entries, inflating the
// claim/observation gate's surface and the requirement-final file
// aggregation. This mutation lets the producer mirror the in-memory
// wipe to KV without touching any other field.
func TestHandleReqResetNodeResultsMutation_WipesSliceAndPreservesOtherFields(t *testing.T) {
	c := newTestComponent(t)
	ctx := context.Background()
	key := "req.demo.req.demo.1"

	// Seed an existing req entry with some NodeResults and other fields.
	exec := &workflow.RequirementExecution{
		EntityID:          "entity-1",
		Slug:              "demo",
		RequirementID:     "req.demo.1",
		Stage:             "executing",
		CurrentNodeIdx:    2,
		SortedNodeIDs:     []string{"n.A", "n.B", "n.C"},
		SortedStoryIDs:    []string{"story.demo.1.1"},
		CurrentStoryIdx:   0,
		RequirementBranch: "semspec/requirement-req.demo.1",
		NodeResults: []workflow.NodeResult{
			{NodeID: "n.A", FilesModified: []string{"src/a.go"}, CommitSHA: "aa"},
			{NodeID: "n.B", FilesModified: []string{"src/b.go"}, CommitSHA: "bb"},
		},
	}
	if err := c.store.saveReq(ctx, key, exec); err != nil {
		t.Fatalf("seed saveReq: %v", err)
	}

	// Invoke the reset_node_results mutation.
	reqBytes, _ := json.Marshal(ReqResetNodeResultsRequest{Key: key})
	resp := c.handleReqResetNodeResultsMutation(ctx, reqBytes)

	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp)
	}

	// NodeResults should be empty / nil.
	after, ok := c.store.getReq(key)
	if !ok {
		t.Fatal("entry disappeared after reset_node_results")
	}
	if len(after.NodeResults) != 0 {
		t.Errorf("NodeResults = %v, want empty", after.NodeResults)
	}

	// Every other field must be preserved — the reset is scoped narrowly.
	if after.Stage != "executing" {
		t.Errorf("Stage = %q, want executing (preserved)", after.Stage)
	}
	if after.CurrentNodeIdx != 2 {
		t.Errorf("CurrentNodeIdx = %d, want 2 (preserved)", after.CurrentNodeIdx)
	}
	if len(after.SortedNodeIDs) != 3 {
		t.Errorf("SortedNodeIDs = %v, want preserved", after.SortedNodeIDs)
	}
	if len(after.SortedStoryIDs) != 1 || after.SortedStoryIDs[0] != "story.demo.1.1" {
		t.Errorf("SortedStoryIDs = %v, want preserved", after.SortedStoryIDs)
	}
	if after.RequirementBranch != "semspec/requirement-req.demo.1" {
		t.Errorf("RequirementBranch = %q, want preserved", after.RequirementBranch)
	}
}

// TestHandleReqResetNodeResultsMutation_MissingKeyIsIdempotentSuccess pins
// the "no entry to reset = success" semantic. Callers (recovery-resume,
// restructure-retry) may fire the wipe before the exec is even persisted
// in unit-test mode or after a pre-create failure — returning Success=true
// for the missing case keeps the call site simple.
func TestHandleReqResetNodeResultsMutation_MissingKeyIsIdempotentSuccess(t *testing.T) {
	c := newTestComponent(t)
	ctx := context.Background()

	reqBytes, _ := json.Marshal(ReqResetNodeResultsRequest{Key: "req.nonexistent.req.nope.1"})
	resp := c.handleReqResetNodeResultsMutation(ctx, reqBytes)

	if !resp.Success {
		t.Errorf("expected idempotent Success=true for missing key, got %+v", resp)
	}
}

// TestHandleReqResetNodeResultsMutation_EmptyKeyRejected pins the input-
// validation contract — empty key is a wire-shape bug and must fail loud.
func TestHandleReqResetNodeResultsMutation_EmptyKeyRejected(t *testing.T) {
	c := newTestComponent(t)
	ctx := context.Background()

	reqBytes, _ := json.Marshal(ReqResetNodeResultsRequest{Key: ""})
	resp := c.handleReqResetNodeResultsMutation(ctx, reqBytes)

	if resp.Success {
		t.Errorf("expected failure on empty key, got Success=true")
	}
	if resp.Error == "" {
		t.Errorf("expected error message on empty key")
	}
}
