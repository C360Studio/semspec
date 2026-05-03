package health

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGraphToolFailure_EOFOnGraphSearch reproduces the 2026-05-03 v3
// regression: every graph_search call returned EOF on
// http://localhost:8080/graph-gateway/graphql while the watch sidecar
// reported errors=0. The detector must fire on this shape so the
// operator gets a critical alert before another paid run is scheduled.
func TestGraphToolFailure_EOFOnGraphSearch(t *testing.T) {
	dispatch := mustRawJSON(t, map[string]any{
		"payload": map[string]any{
			"id":   "call-123",
			"name": "graph_search",
		},
	})
	result := mustRawJSON(t, map[string]any{
		"payload": map[string]any{
			"call_id": "call-123",
			"error":   `graph search failed: execute request: Post "http://localhost:8080/graph-gateway/graphql": EOF`,
			"loop_id": "loop-abc",
		},
	})

	b := &Bundle{Messages: []Message{
		{Sequence: 10, Subject: "tool.execute.graph_search", RawData: dispatch},
		{Sequence: 11, Subject: "tool.result.call-123", RawData: result},
	}}

	got := GraphToolFailure{}.Run(b)
	if len(got) != 1 {
		t.Fatalf("got %d diagnoses, want 1", len(got))
	}
	d := got[0]
	if d.Shape != GraphToolFailureShape {
		t.Errorf("Shape = %q, want %q", d.Shape, GraphToolFailureShape)
	}
	if d.Severity != SeverityCritical {
		t.Errorf("Severity = %q, want critical", d.Severity)
	}
	if !strings.Contains(d.Remediation, "Semantic Knowledge Graph") {
		t.Errorf("Remediation should reference the SKG; got %q", d.Remediation)
	}
	if !strings.Contains(d.Remediation, "graph_search") {
		t.Errorf("Remediation should name the failing tool; got %q", d.Remediation)
	}
}

// TestGraphToolFailure_NameInResultPayload covers the path where the tool
// name lives directly in the result payload (no need for the dispatch
// pre-pass to resolve it). Some producers populate name; the detector
// must accept either form.
func TestGraphToolFailure_NameInResultPayload(t *testing.T) {
	result := mustRawJSON(t, map[string]any{
		"payload": map[string]any{
			"call_id": "call-x",
			"name":    "graph_query",
			"error":   "graph gateway returned 500: internal server error",
			"loop_id": "loop-y",
		},
	})

	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.call-x", RawData: result},
	}}

	got := GraphToolFailure{}.Run(b)
	if len(got) != 1 {
		t.Fatalf("got %d diagnoses, want 1", len(got))
	}
	if !strings.Contains(got[0].Remediation, "graph_query") {
		t.Errorf("Remediation should name graph_query; got %q", got[0].Remediation)
	}
}

// TestGraphToolFailure_NonGraphToolErrorIgnored ensures we don't fire on
// bash failures, http_request errors, etc. Those have other detectors
// or are operator-debug surface, not SKG breakage.
func TestGraphToolFailure_NonGraphToolErrorIgnored(t *testing.T) {
	result := mustRawJSON(t, map[string]any{
		"payload": map[string]any{
			"call_id": "call-bash",
			"name":    "bash",
			"error":   "exit code 127: command not found",
			"loop_id": "loop-z",
		},
	})

	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.call-bash", RawData: result},
	}}

	if got := (GraphToolFailure{}).Run(b); got != nil {
		t.Errorf("expected no diagnoses for bash error, got %d", len(got))
	}
}

// TestGraphToolFailure_SuccessfulGraphCallIgnored — a graph tool result
// with no error field is the happy path; detector must stay silent.
func TestGraphToolFailure_SuccessfulGraphCallIgnored(t *testing.T) {
	result := mustRawJSON(t, map[string]any{
		"payload": map[string]any{
			"call_id": "call-ok",
			"name":    "graph_summary",
			"content": "115 entities indexed from 1 source.",
			"loop_id": "loop-ok",
		},
	})

	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.call-ok", RawData: result},
	}}

	if got := (GraphToolFailure{}).Run(b); got != nil {
		t.Errorf("expected no diagnoses for successful call, got %d", len(got))
	}
}

// TestGraphToolFailure_MultipleFailures pins ordering: matches return
// in ascending sequence so fixture-driven adopter tools see deterministic
// alert sequencing across runs.
func TestGraphToolFailure_MultipleFailures(t *testing.T) {
	r1 := mustRawJSON(t, map[string]any{
		"payload": map[string]any{"call_id": "c1", "name": "graph_search", "error": "EOF"},
	})
	r2 := mustRawJSON(t, map[string]any{
		"payload": map[string]any{"call_id": "c2", "name": "graph_search", "error": "EOF"},
	})

	b := &Bundle{Messages: []Message{
		{Sequence: 20, Subject: "tool.result.c2", RawData: r2},
		{Sequence: 10, Subject: "tool.result.c1", RawData: r1},
	}}

	got := GraphToolFailure{}.Run(b)
	if len(got) != 2 {
		t.Fatalf("got %d diagnoses, want 2", len(got))
	}
	if got[0].Evidence[0].ID != "10" || got[1].Evidence[0].ID != "20" {
		t.Errorf("expected ascending sequence ordering; got %s, %s",
			got[0].Evidence[0].ID, got[1].Evidence[0].ID)
	}
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
