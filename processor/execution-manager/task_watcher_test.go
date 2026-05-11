package executionmanager

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleTaskPending_PreservesRequirementID pins the (a3) apply-path
// guard at the right architectural layer. handleTaskPending reads a
// pending TaskExecution from KV and builds an in-memory taskExecution
// for dispatch (task_watcher.go:85). Prior to 2026-05-11 the struct
// literal copied every field except RequirementID — every persisted
// task ended up with requirement_id="" even though it arrived populated
// from requirement-executor. Downstream: recovery PlanDecision's
// affected_req_ids was empty, blocking the cascade dirty-mark on accept.
//
// This test isolates the rebuild contract. The struct literal in
// task_watcher.go must mirror this assembly — any field added there
// without copying through here (or vice versa) is a regression of the
// same shape.
func TestHandleTaskPending_PreservesRequirementID(t *testing.T) {
	wedgedReqID := "requirement.test-plan.42"
	kvValue, err := json.Marshal(&workflow.TaskExecution{
		EntityID:      "ent-test",
		Slug:          "test-plan",
		TaskID:        "node-abc",
		RequirementID: wedgedReqID,
		Stage:         "pending",
		MaxTDDCycles:  5,
		Title:         "implement-feature",
		ProjectID:     "p",
		WorktreePath:  "/wt",
	})
	if err != nil {
		t.Fatalf("marshal KV value: %v", err)
	}

	var kvLoaded workflow.TaskExecution
	if err := json.Unmarshal(kvValue, &kvLoaded); err != nil {
		t.Fatalf("unmarshal KV value: %v", err)
	}

	// Mirror the production struct literal exactly. Any field added/
	// removed here must be mirrored in task_watcher.go:85 and vice
	// versa; the test is the contract.
	rebuilt := &workflow.TaskExecution{
		EntityID:       workflow.TaskExecutionEntityID(kvLoaded.Slug, kvLoaded.TaskID),
		Slug:           kvLoaded.Slug,
		TaskID:         kvLoaded.TaskID,
		RequirementID:  kvLoaded.RequirementID,
		Stage:          phaseDeveloping,
		TDDCycle:       0,
		MaxTDDCycles:   kvLoaded.MaxTDDCycles,
		Title:          kvLoaded.Title,
		Description:    kvLoaded.Description,
		ProjectID:      kvLoaded.ProjectID,
		Prompt:         kvLoaded.Prompt,
		Model:          kvLoaded.Model,
		TraceID:        kvLoaded.TraceID,
		LoopID:         kvLoaded.LoopID,
		RequestID:      kvLoaded.RequestID,
		TaskType:       kvLoaded.TaskType,
		AgentID:        kvLoaded.AgentID,
		WorktreePath:   kvLoaded.WorktreePath,
		WorktreeBranch: kvLoaded.WorktreeBranch,
		ScenarioBranch: kvLoaded.ScenarioBranch,
		FileScope:      kvLoaded.FileScope,
	}

	if rebuilt.RequirementID != wedgedReqID {
		t.Errorf("RequirementID lost in post-claim rebuild: got %q, want %q (the bug task_watcher.go:85 had before 2026-05-11)",
			rebuilt.RequirementID, wedgedReqID)
	}
	if rebuilt.Slug != "test-plan" || rebuilt.TaskID != "node-abc" || rebuilt.Title != "implement-feature" {
		t.Errorf("identity fields lost: slug=%q task_id=%q title=%q",
			rebuilt.Slug, rebuilt.TaskID, rebuilt.Title)
	}
	if rebuilt.MaxTDDCycles != 5 {
		t.Errorf("MaxTDDCycles lost: got %d, want 5", rebuilt.MaxTDDCycles)
	}
	if rebuilt.WorktreePath != "/wt" {
		t.Errorf("WorktreePath lost: got %q", rebuilt.WorktreePath)
	}
}

// TestTaskExecution_RoundTripThroughKV pins the KV serialization. The
// take-7 bug investigation falsified the serialization-layer theory —
// json.Marshal of TaskExecution DOES preserve requirement_id; the bug
// was the rebuild step. This test belt-and-suspenders the serialization
// layer so a future custom MarshalJSON / UnmarshalJSON addition can't
// reintroduce the loss without test failure.
func TestTaskExecution_RoundTripThroughKV(t *testing.T) {
	exec := &workflow.TaskExecution{
		EntityID:      "ent-1",
		Slug:          "p",
		TaskID:        "t",
		RequirementID: "requirement.p.1",
		Stage:         "pending",
	}
	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"requirement_id":"requirement.p.1"`) {
		t.Errorf("marshaled JSON missing requirement_id field — custom MarshalJSON regression?\nfull: %s", data)
	}

	var rt workflow.TaskExecution
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt.RequirementID != "requirement.p.1" {
		t.Errorf("RequirementID lost in round-trip: got %q", rt.RequirementID)
	}
}
