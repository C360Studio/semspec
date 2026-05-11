package executionmanager

import (
	"encoding/json"
	"testing"
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
