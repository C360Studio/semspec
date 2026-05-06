package health

import (
	"encoding/json"
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

func TestWalkAgentResponses_GroupsAndSorts(t *testing.T) {
	// Two loops, two messages each. Bundle order is newest-first;
	// walkAgentResponses must reorder each loop ascending by seq.
	messages := []Message{
		{Sequence: 4, Subject: "agent.response.loop-A:req:r4",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
		{Sequence: 3, Subject: "agent.response.loop-B:req:r3",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"x","tool_calls":[]}}}`),
		},
		{Sequence: 2, Subject: "agent.response.loop-A:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t"}]}}}`),
		},
		{Sequence: 1, Subject: "agent.response.loop-B:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t"}]}}}`),
		},
	}
	byLoop, malformed := walkAgentResponses(messages)
	if malformed != 0 {
		t.Errorf("malformed = %d, want 0", malformed)
	}
	if len(byLoop) != 2 {
		t.Fatalf("len(byLoop) = %d, want 2 (got %v)", len(byLoop), byLoop)
	}
	a := byLoop["loop-A"]
	if len(a) != 2 || a[0].Sequence != 2 || a[1].Sequence != 4 {
		t.Errorf("loop-A out of order: %v", seqs(a))
	}
	b := byLoop["loop-B"]
	if len(b) != 2 || b[0].Sequence != 1 || b[1].Sequence != 3 {
		t.Errorf("loop-B out of order: %v", seqs(b))
	}
	if !a[0].HasToolCalls() || a[1].HasToolCalls() {
		t.Errorf("loop-A predicate flags wrong: %+v", a)
	}
	if !a[1].IsStop() {
		t.Errorf("loop-A[1].IsStop = false")
	}
}

func TestWalkAgentResponses_SkipsNonAgentResponseAndMalformed(t *testing.T) {
	messages := []Message{
		// Wrong subject prefix — skip silently.
		{Sequence: 1, Subject: "agent.request.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls"}}`),
		},
		// Empty subject after prefix — skip.
		{Sequence: 2, Subject: "agent.response."},
		// Malformed JSON — bump counter, skip.
		{Sequence: 3, Subject: "agent.response.loop-X:req:rX", RawData: json.RawMessage(`{garbage`)},
		// Real one, decodes cleanly.
		{Sequence: 4, Subject: "agent.response.loop-Y:req:rY",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"hi","tool_calls":[]}}}`),
		},
	}
	byLoop, malformed := walkAgentResponses(messages)
	if malformed != 1 {
		t.Errorf("malformed = %d, want 1", malformed)
	}
	if _, ok := byLoop["loop-Y"]; !ok {
		t.Errorf("loop-Y missing from result: %v", byLoop)
	}
	if _, ok := byLoop["loop-X"]; ok {
		t.Errorf("malformed loop should not appear in byLoop: %v", byLoop)
	}
}

// TestWalkAgentResponses_EmptyRawDataNotMalformed pins the 2026-05-06
// fix: agent.response messages with nil/empty/literal-null RawData
// (valid markers without a payload body) must not be counted as
// malformed. Pre-fix every hybrid @hard run fired a spurious
// SeverityUndetermined diagnosis with evidence_id="" because the
// message-logger emits some response markers without raw_data on the
// wire (omitempty), and json.Unmarshal(nil) errors with "unexpected
// end of JSON input".
func TestWalkAgentResponses_EmptyRawDataNotMalformed(t *testing.T) {
	messages := []Message{
		// nil raw_data — must not bump counter.
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1"},
		// empty bytes — must not bump counter.
		{Sequence: 2, Subject: "agent.response.loop-B:req:r1", RawData: json.RawMessage("")},
		// literal "null" — must not bump counter.
		{Sequence: 3, Subject: "agent.response.loop-C:req:r1", RawData: json.RawMessage("null")},
		// real one for sanity.
		{Sequence: 4, Subject: "agent.response.loop-D:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"hi","tool_calls":[]}}}`),
		},
	}
	byLoop, malformed := walkAgentResponses(messages)
	if malformed != 0 {
		t.Errorf("malformed = %d, want 0 (empty raw_data must be silent skip)", malformed)
	}
	if _, ok := byLoop["loop-D"]; !ok {
		t.Errorf("loop-D should be present: %v", byLoop)
	}
	for _, absentLoop := range []string{"loop-A", "loop-B", "loop-C"} {
		if _, ok := byLoop[absentLoop]; ok {
			t.Errorf("%s had no payload — must not appear in byLoop: %v", absentLoop, byLoop)
		}
	}
}

func TestWalkAgentResponses_NullToolCallsTreatedAsEmpty(t *testing.T) {
	// JSON null and [] both decode to a zero-len slice. HasToolCalls
	// must treat them equivalently — pinning here so future struct
	// tag changes don't break the equivalence silently.
	messages := []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":null}}}`),
		},
		{Sequence: 2, Subject: "agent.response.loop-A:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
	}
	byLoop, _ := walkAgentResponses(messages)
	a := byLoop["loop-A"]
	if len(a) != 2 {
		t.Fatalf("len(loop-A) = %d, want 2", len(a))
	}
	if a[0].HasToolCalls() || a[1].HasToolCalls() {
		t.Errorf("null and [] should both report HasToolCalls=false: %+v", a)
	}
}

// seqs returns the Sequence field of each entry — terser test output.
func seqs(rs []agentResponse) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.Sequence
	}
	return out
}
