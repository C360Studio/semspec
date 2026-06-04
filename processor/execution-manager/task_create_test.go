package executionmanager

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestTaskCreateRequest_RequirementIDRoundTrip pins the wire shape that
// requirement-executor uses to dispatch tasks to execution-manager.
// req-executor sends a map[string]any, execution-manager unmarshals into
// TaskCreateRequest. Take 7 gemini @hard surfaced requirement_id=None on
// every task in EXECUTION_STATES KV despite req-executor logging the
// field as populated — this test isolates whether the wire round-trip
// is the lossy step.
func TestTaskCreateRequest_RequirementIDRoundTrip(t *testing.T) {
	// Simulate exactly what requirement-executor.dispatchNextNodeLocked
	// builds at component.go:1275 — a map[string]any payload.
	wire := map[string]any{
		"slug":           "test-plan",
		"task_id":        "node-abc-123",
		"requirement_id": "requirement.test-plan.1",
		"title":          "Configure Gradle build",
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal wire payload: %v", err)
	}

	// Sanity check the JSON contains the field as we expect.
	wantSub := `"requirement_id":"requirement.test-plan.1"`
	if !contains(string(data), wantSub) {
		t.Errorf("marshaled JSON missing %q\nfull:\n%s", wantSub, data)
	}

	// Now what execution-manager.handleTaskCreateMutation does at
	// mutations.go:193 — unmarshal into TaskCreateRequest.
	var req TaskCreateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal into TaskCreateRequest: %v", err)
	}

	if req.RequirementID != "requirement.test-plan.1" {
		t.Errorf("TaskCreateRequest.RequirementID = %q, want %q (wire round-trip dropped it)",
			req.RequirementID, "requirement.test-plan.1")
	}
	if req.Slug != "test-plan" {
		t.Errorf("Slug round-trip broken: got %q", req.Slug)
	}
	if req.TaskID != "node-abc-123" {
		t.Errorf("TaskID round-trip broken: got %q", req.TaskID)
	}
}

// TestTaskCreateRequest_ScenariosRoundTrip pins the wire shape for the
// scenarios field added 2026-06-03 to thread the per-task scenario
// contract through dev + per-task reviewer prompts (closes Cline
// blindness). req-executor sends taskReq["scenarios"] = []workflow.Scenario;
// execution-manager unmarshals into TaskCreateRequest.Scenarios; the
// mutation handler then persists onto workflow.TaskExecution.Scenarios.
//
// Paid mavlink-hard 2026-06-03 showed task entities with scenarios=null
// despite req KV having 4 scenarios and node.ScenarioIDs having 18 — so
// production must be dropping the field somewhere on the dispatch path.
// This test pins the wire stage to localize the bug.
func TestTaskCreateRequest_ScenariosRoundTrip(t *testing.T) {
	scenarios := []workflow.Scenario{
		{
			ID:    "scenario.x.1.1.1",
			Given: "the system is in state X",
			When:  "the user submits Y",
			Then:  []string{"the response is Z"},
			Tags:  []string{"@unit"},
		},
		{
			ID:    "scenario.x.1.1.2",
			Given: "the system is in state X2",
			When:  "the user submits Y2",
			Then:  []string{"the response is Z2"},
			Tags:  []string{"@unit"},
		},
	}

	// Exact shape dispatchNextNodeLocked builds (component.go:1411-1426).
	wire := map[string]any{
		"slug":           "test-plan",
		"task_id":        "node-abc-123",
		"requirement_id": "requirement.test-plan.1",
		"title":          "Implement test-first",
		"prompt":         "Write failing test for the goodbye endpoint",
		"file_scope":     []string{"src/api/goodbye.go"},
		"scenarios":      scenarios,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal wire payload: %v", err)
	}

	// Sanity: scenarios key MUST be present in marshaled JSON.
	if !contains(string(data), `"scenarios":[`) {
		t.Fatalf("marshaled JSON missing scenarios array — JSON encoder dropped the slice. payload:\n%s", string(data))
	}

	var req TaskCreateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal into TaskCreateRequest: %v", err)
	}

	if len(req.Scenarios) != 2 {
		t.Errorf("TaskCreateRequest.Scenarios = %d items, want 2 — unmarshaling dropped the slice", len(req.Scenarios))
	}
	if len(req.Scenarios) > 0 {
		if req.Scenarios[0].ID != "scenario.x.1.1.1" {
			t.Errorf("first scenario lost ID: got %q want %q", req.Scenarios[0].ID, "scenario.x.1.1.1")
		}
		if req.Scenarios[0].Given != "the system is in state X" {
			t.Errorf("first scenario lost Given: got %q", req.Scenarios[0].Given)
		}
	}
}

// contains is a no-import-strings helper. Stdlib avoidance keeps the
// test surface minimal so a failure in a related test file doesn't
// mask whatever's breaking here.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
