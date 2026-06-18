package requirementexecutor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

type mockTaskCompletionKVEntry struct {
	key   string
	value []byte
	op    jetstream.KeyValueOp
}

func (e *mockTaskCompletionKVEntry) Bucket() string                  { return "EXECUTION_STATES" }
func (e *mockTaskCompletionKVEntry) Key() string                     { return e.key }
func (e *mockTaskCompletionKVEntry) Value() []byte                   { return e.value }
func (e *mockTaskCompletionKVEntry) Revision() uint64                { return 1 }
func (e *mockTaskCompletionKVEntry) Created() time.Time              { return time.Time{} }
func (e *mockTaskCompletionKVEntry) Delta() uint64                   { return 0 }
func (e *mockTaskCompletionKVEntry) Operation() jetstream.KeyValueOp { return e.op }

func TestHandleTaskStateChange_RehydratesReqExecAndAdvancesRecoveredQANode(t *testing.T) {
	c := newTestComponent(t)

	const (
		slug          = "plan-qa-recovery"
		requirementID = "req.plan.1"
		node0TaskID   = "task-node-0"
	)

	dagRaw, err := json.Marshal(TaskDAG{Nodes: []TaskNode{
		{ID: "node-0", Prompt: "fix the reviewed gap", Role: "developer", FileScope: []string{"src/a.go"}},
		{ID: "node-1", Prompt: "cover the reviewed gap", Role: "developer", FileScope: []string{"src/a_test.go"}},
	}})
	if err != nil {
		t.Fatalf("marshal dag: %v", err)
	}

	persisted := &workflow.RequirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.recovered",
		Slug:              slug,
		RequirementID:     requirementID,
		Stage:             phaseExecuting,
		CurrentNodeIdx:    0,
		CurrentNodeTaskID: node0TaskID,
		DAGRaw:            dagRaw,
		SortedNodeIDs:     []string{"node-0", "node-1"},
		SortedStoryIDs:    []string{"story.plan.1"},
		CurrentStoryIdx:   0,
		MaxRetries:        1,
	}
	recovered := c.rebuildExecFromKV(workflow.RequirementExecutionKey(slug, requirementID), persisted)

	c.taskCompletionReqExecLoader = func(_ context.Context, gotSlug, gotRequirementID string) (*requirementExecution, error) {
		if gotSlug != slug {
			t.Fatalf("loader slug = %q, want %q", gotSlug, slug)
		}
		if gotRequirementID != requirementID {
			t.Fatalf("loader requirement = %q, want %q", gotRequirementID, requirementID)
		}
		return recovered, nil
	}

	taskExec := workflow.TaskExecution{
		Slug:          slug,
		RequirementID: requirementID,
		TaskID:        node0TaskID,
		Stage:         "approved",
		FilesModified: []string{"src/a.go"},
		MergeCommit:   "abc123",
		UpdatedAt:     time.Now(),
	}
	entryValue, err := json.Marshal(taskExec)
	if err != nil {
		t.Fatalf("marshal task execution: %v", err)
	}

	c.handleTaskStateChange(context.Background(), &mockTaskCompletionKVEntry{
		key:   "task." + node0TaskID,
		value: entryValue,
		op:    jetstream.KeyValuePut,
	})

	if got := recovered.CurrentNodeIdx; got != 1 {
		t.Fatalf("CurrentNodeIdx = %d, want 1 after recovered node completion", got)
	}
	if recovered.CurrentNodeTaskID == "" {
		t.Fatal("CurrentNodeTaskID is empty; node 1 was not dispatched")
	}
	if recovered.CurrentNodeTaskID == node0TaskID {
		t.Fatal("CurrentNodeTaskID still points at node 0; node 1 was not dispatched")
	}
	if !recovered.VisitedNodes["node-0"] {
		t.Fatal("node-0 was not marked visited")
	}
	if len(recovered.NodeResults) != 1 {
		t.Fatalf("NodeResults = %d, want 1", len(recovered.NodeResults))
	}
	if got := recovered.NodeResults[0].CommitSHA; got != "abc123" {
		t.Fatalf("NodeResults[0].CommitSHA = %q, want abc123", got)
	}
	if _, ok := c.activeExecs.Get(recovered.EntityID); !ok {
		t.Fatalf("recovered execution %q was not installed in activeExecs", recovered.EntityID)
	}
}
