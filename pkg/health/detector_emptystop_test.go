package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractLoopIDFromSubject(t *testing.T) {
	cases := map[string]string{
		"agent.response.abc-123:req:xyz": "abc-123",
		"agent.response.abc-123":         "abc-123",
		"agent.response.":                "",
		"agent.request.foo:req:bar":      "", // not agent.response
		"":                               "",
	}
	for subject, want := range cases {
		if got := extractLoopIDFromSubject(subject); got != want {
			t.Errorf("extractLoopIDFromSubject(%q) = %q, want %q", subject, got, want)
		}
	}
}

func TestEmptyStopAfterToolCalls_SyntheticPositive(t *testing.T) {
	// Synthetic two-message loop: a tool_call response followed by an
	// empty stop. The detector should fire exactly once.
	bundle := &Bundle{Messages: []Message{
		{
			Sequence: 1,
			Subject:  "agent.response.loop-A:req:r1",
			RawData:  json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}`),
		},
		{
			Sequence: 2,
			Subject:  "agent.response.loop-A:req:r2",
			RawData:  json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
	}}
	got := EmptyStopAfterToolCalls{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("want 1 diagnosis, got %d: %+v", len(got), got)
	}
	if got[0].Shape != EmptyStopShape {
		t.Errorf("shape = %q", got[0].Shape)
	}
	if got[0].Severity != SeverityCritical {
		t.Errorf("severity = %q", got[0].Severity)
	}
	if got[0].Evidence[0].ID != "2" {
		t.Errorf("evidence ID = %q, want sequence 2", got[0].Evidence[0].ID)
	}
}

func TestEmptyStopAfterToolCalls_NoToolCallsBeforeIsSilent(t *testing.T) {
	// First-turn empty stop is a different shape (model just refused).
	// EmptyStopAfterToolCalls should NOT fire on it — the "after"
	// predicate is load-bearing.
	bundle := &Bundle{Messages: []Message{
		{
			Sequence: 1,
			Subject:  "agent.response.loop-A:req:r1",
			RawData:  json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
	}}
	if got := (EmptyStopAfterToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("expected no diagnoses for first-turn empty stop, got %+v", got)
	}
}

func TestEmptyStopAfterToolCalls_ContentNotEmptyIsSilent(t *testing.T) {
	// finish_reason=stop with non-empty content is a different shape
	// (likely JSONInText). EmptyStopAfterToolCalls should not match.
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}`),
		},
		{Sequence: 2, Subject: "agent.response.loop-A:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"some text","tool_calls":[]}}}`),
		},
	}}
	if got := (EmptyStopAfterToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("expected no diagnoses for non-empty content, got %+v", got)
	}
}

func TestEmptyStopAfterToolCalls_LoopIsolation(t *testing.T) {
	// Tool call in loop-A must NOT satisfy the "prior tool_call"
	// predicate for an empty stop in loop-B.
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}`),
		},
		{Sequence: 2, Subject: "agent.response.loop-B:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
	}}
	if got := (EmptyStopAfterToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("loop isolation broken: detector matched across loops: %+v", got)
	}
}

func TestEmptyStopAfterToolCalls_BundleMessagesAreNewestFirst(t *testing.T) {
	// Bundle.Messages comes back newest-first from the message-logger.
	// The detector must reorder by sequence before applying the
	// "prior tool_call" predicate, otherwise it would match in
	// arrival order and produce false positives or negatives.
	bundle := &Bundle{Messages: []Message{
		// stop response listed FIRST (newest-first ordering)
		{Sequence: 2, Subject: "agent.response.loop-A:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
		// tool_call response listed SECOND but earlier in time
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}`),
		},
	}}
	got := EmptyStopAfterToolCalls{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnosis (newest-first reorder); got %d: %+v", len(got), got)
	}
}

// TestEmptyStopAfterToolCalls_FixtureV107Rerun is the fixture-driven
// canary for the detector pattern. The plan calls out that
// local-easy-v107-rerun-0930/messages-final.json is a real-world
// shape match; running the detector against it should match the
// 4 known stop responses (one per architecture-gen loop attempt
// before the retry-exhaustion bailout).
func TestEmptyStopAfterToolCalls_FixtureV107Rerun(t *testing.T) {
	path := filepath.Join("testdata", "fixtures", "local-easy-v107-rerun-0930", "messages-final.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var entries []messageEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	bundle := &Bundle{Messages: make([]Message, 0, len(entries))}
	for _, e := range entries {
		bundle.Messages = append(bundle.Messages, Message{
			Sequence:    e.Sequence,
			Subject:     e.Subject,
			MessageType: e.MessageType,
			RawData:     e.RawData,
		})
	}

	got := EmptyStopAfterToolCalls{}.Run(bundle)
	if len(got) == 0 {
		t.Fatal("expected at least one diagnosis from v107-rerun fixture")
	}
	for _, d := range got {
		if d.Shape != EmptyStopShape {
			t.Errorf("unexpected shape %q", d.Shape)
		}
		if d.Severity != SeverityCritical {
			t.Errorf("unexpected severity %q", d.Severity)
		}
		if len(d.Evidence) == 0 || d.Evidence[0].Kind != EvidenceAgentResponse {
			t.Errorf("missing agent_response evidence: %+v", d.Evidence)
		}
		if !strings.Contains(d.Remediation, "feedback_retries_must_inject_failure_context") {
			t.Errorf("remediation should reference feedback memory: %q", d.Remediation)
		}
	}
}

func TestEmptyStopAfterToolCalls_RunsViaRunAll(t *testing.T) {
	// Belt-and-braces: confirm the detector slots into the standard
	// RunAll machinery without surprises.
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}`),
		},
		{Sequence: 2, Subject: "agent.response.loop-A:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
	}}
	RunAll(bundle, []Detector{EmptyStopAfterToolCalls{}})
	if len(bundle.Diagnoses) != 1 {
		t.Errorf("RunAll: expected 1 diagnosis, got %d", len(bundle.Diagnoses))
	}
}
