package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
// canary for the detector pattern. local-easy-v107-rerun-0930/
// messages-final.json captures qwen3:14b architecture-gen exhausting
// 3 retries with empty-stop-after-tool-calls; the detector should
// match exactly those 3 sequences.
func TestEmptyStopAfterToolCalls_FixtureV107Rerun(t *testing.T) {
	bundle := loadMessagesFixture(t, "local-easy-v107-rerun-0930", "messages-final.json")
	got := EmptyStopAfterToolCalls{}.Run(bundle)

	// Exact pin: 3 diagnoses at sequences 746, 947, 1103. If a future
	// detector change drops one or starts double-firing, this test
	// catches the regression where a >=1 assertion would silently
	// hide it.
	wantSequences := []string{"746", "947", "1103"}
	if len(got) != len(wantSequences) {
		t.Fatalf("len = %d, want %d (sequences %v); got %v",
			len(got), len(wantSequences), wantSequences, diagSeqs(got))
	}
	for i, want := range wantSequences {
		if got[i].Shape != EmptyStopShape {
			t.Errorf("got[%d].Shape = %q", i, got[i].Shape)
		}
		if got[i].Severity != SeverityCritical {
			t.Errorf("got[%d].Severity = %q", i, got[i].Severity)
		}
		if got[i].Evidence[0].ID != want {
			t.Errorf("got[%d].Evidence.ID = %q, want %q", i, got[i].Evidence[0].ID, want)
		}
		if !strings.Contains(got[i].Remediation, "feedback_retries_must_inject_failure_context") {
			t.Errorf("got[%d].Remediation should reference feedback memory: %q", i, got[i].Remediation)
		}
	}
}

func TestEmptyStopAfterToolCalls_NumericSeqOrder(t *testing.T) {
	// Regression: the prior implementation sorted Diagnoses
	// lexicographically by Evidence[0].ID (which is FormatInt'd
	// sequence). Lex-order interleaves "1103" between "110" and
	// "111" — invisible at small fixture sizes, broken at scale.
	// Pin numeric order with sequences that span a power-of-ten
	// boundary.
	bundle := &Bundle{Messages: []Message{
		respWithTC("loop-A:req:r1", 99),
		respEmptyStop("loop-A:req:r2", 100),
		respWithTC("loop-A:req:r3", 999),
		respEmptyStop("loop-A:req:r4", 1000),
		respWithTC("loop-A:req:r5", 9),
		respEmptyStop("loop-A:req:r6", 10),
	}}
	got := EmptyStopAfterToolCalls{}.Run(bundle)
	wantSeqs := []string{"10", "100", "1000"}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range wantSeqs {
		if got[i].Evidence[0].ID != want {
			t.Errorf("got[%d].ID = %q, want %q (full=%v)", i, got[i].Evidence[0].ID, want, diagSeqs(got))
		}
	}
}

func TestEmptyStopAfterToolCalls_ToolCallsNullEqualsEmpty(t *testing.T) {
	// JSON `null` vs `[]` both decode to a zero-length slice and
	// must be treated equivalently. Pin with both shapes to confirm
	// the detector doesn't panic on null and matches the empty-stop
	// shape correctly when prior turn used null vs empty.
	bundle := &Bundle{Messages: []Message{
		// Prior turn used `null` as tool_calls — should NOT count
		// as a "prior tool_call" since len(null) == 0.
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"text","tool_calls":null}}}`),
		},
		// Empty stop — should NOT match because there's no prior tool_call.
		{Sequence: 2, Subject: "agent.response.loop-A:req:r2",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":null}}}`),
		},
	}}
	if got := (EmptyStopAfterToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("tool_calls:null should not satisfy 'prior tool_call' predicate; got %+v", got)
	}
}

func TestEmptyStopAfterToolCalls_InterleavedFiresPerEvent(t *testing.T) {
	// A loop with multiple tool_call -> empty-stop episodes should
	// fire once per empty stop. After the first match, seenToolCall
	// stays true (the model HAD called tools earlier in the loop),
	// so a second tool_call -> empty stop pair also fires. Important
	// for ThinkingSpiral, which expects multiple violations per loop.
	bundle := &Bundle{Messages: []Message{
		respWithTC("loop-A:req:r1", 1),
		respEmptyStop("loop-A:req:r2", 2),
		respWithTC("loop-A:req:r3", 3),
		respEmptyStop("loop-A:req:r4", 4),
	}}
	got := EmptyStopAfterToolCalls{}.Run(bundle)
	if len(got) != 2 {
		t.Fatalf("interleaved: want 2 diagnoses, got %d (seqs %v)", len(got), diagSeqs(got))
	}
	if got[0].Evidence[0].ID != "2" || got[1].Evidence[0].ID != "4" {
		t.Errorf("got seqs %v, want [2 4]", diagSeqs(got))
	}
}

func TestEmptyStopAfterToolCalls_MalformedSurfacesUndetermined(t *testing.T) {
	// Malformed agent.response RawData should bump the malformed
	// counter and surface as a SeverityUndetermined diagnosis so the
	// bundle reader sees "we couldn't decide because input was
	// corrupt" rather than "found nothing."
	bundle := &Bundle{Messages: []Message{
		respWithTC("loop-A:req:r1", 1),
		respEmptyStop("loop-A:req:r2", 2),
		// Garbage payload — agent.response subject prefix, but
		// RawData isn't valid JSON.
		{Sequence: 3, Subject: "agent.response.loop-B:req:rX", RawData: json.RawMessage(`{not json`)},
	}}
	got := EmptyStopAfterToolCalls{}.Run(bundle)
	var sawCritical, sawUndetermined bool
	for _, d := range got {
		switch d.Severity {
		case SeverityCritical:
			sawCritical = true
		case SeverityUndetermined:
			sawUndetermined = true
		}
	}
	if !sawCritical {
		t.Error("expected the synthetic empty-stop to fire")
	}
	if !sawUndetermined {
		t.Errorf("expected SeverityUndetermined diagnosis for malformed entries; got %+v", got)
	}
}

// loadMessagesFixture reads testdata/fixtures/<run>/<file> and returns
// a Bundle populated with Messages. Fixture file is the wire JSON from
// /message-logger/entries — same shape FetchMessages decodes.
func loadMessagesFixture(t *testing.T, run, file string) *Bundle {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", run, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s/%s: %v", run, file, err)
	}
	var entries []messageEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decode fixture %s/%s: %v", run, file, err)
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
	return bundle
}

// diagSeqs returns just the Evidence[0].ID strings from a diagnosis
// slice — useful for terse t.Errorf output.
func diagSeqs(diags []Diagnosis) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		if len(d.Evidence) == 0 {
			out = append(out, "<no-evidence>")
			continue
		}
		out = append(out, d.Evidence[0].ID)
	}
	return out
}

// respWithTC builds a message-logger entry for an agent.response that
// contains a tool_call. Test helper.
func respWithTC(subjectTail string, seq int64) Message {
	return Message{
		Sequence: seq,
		Subject:  "agent.response." + subjectTail,
		RawData:  json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}`),
	}
}

// respEmptyStop builds an empty-stop agent.response entry. Test helper.
func respEmptyStop(subjectTail string, seq int64) Message {
	return Message{
		Sequence: seq,
		Subject:  "agent.response." + subjectTail,
		RawData:  json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
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
