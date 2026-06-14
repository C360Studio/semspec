package health

import (
	"strings"
	"testing"
)

// TestRepeatToolFailure_RejectionTypeWedge reproduces the 2026-05-03 v4
// reviewer wedge: same submit_work validation error 35+ times in a row
// across one loop. With the threshold at 3, the detector must fire
// after the 3rd failure on the same (loop, tool, error class) tuple.
func TestRepeatToolFailure_RejectionTypeWedge(t *testing.T) {
	const errStr = "validation failed: rejection_type is required when verdict is rejected — must be one of: fixable, restructure"
	dispatch := mustRawJSON(t, map[string]any{
		"payload": map[string]any{"id": "call-x", "name": "submit_work"},
	})
	mkResult := func(callID string, err string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{
				"call_id": callID,
				"name":    "submit_work",
				"error":   err,
				"loop_id": "loop-rev-1",
			},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 10, Subject: "tool.execute.submit_work", RawData: dispatch},
		{Sequence: 11, Subject: "tool.result.call-x", RawData: mkResult("call-x", errStr)},
		{Sequence: 12, Subject: "tool.result.call-y", RawData: mkResult("call-y", errStr)},
		{Sequence: 13, Subject: "tool.result.call-z", RawData: mkResult("call-z", errStr)},
		// 4th + 5th failures shouldn't re-emit
		{Sequence: 14, Subject: "tool.result.call-q", RawData: mkResult("call-q", errStr)},
		{Sequence: 15, Subject: "tool.result.call-w", RawData: mkResult("call-w", errStr)},
	}}

	got := RepeatToolFailure{}.Run(b)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 diagnosis (one emit per streak), got %d", len(got))
	}
	d := got[0]
	if d.Severity != SeverityCritical {
		t.Errorf("Severity = %q, want critical", d.Severity)
	}
	if !strings.Contains(d.Remediation, "submit_work") {
		t.Errorf("Remediation should name the wedged tool")
	}
	if !strings.Contains(d.Remediation, "instruction-following") {
		t.Errorf("Remediation should reference the failure class")
	}
}

// TestRepeatToolFailure_BelowThreshold confirms a streak of 2 doesn't fire.
// Two repeats is bad luck, three is a wedge.
func TestRepeatToolFailure_BelowThreshold(t *testing.T) {
	dispatch := mustRawJSON(t, map[string]any{
		"payload": map[string]any{"id": "c1", "name": "bash"},
	})
	mk := func(id string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "bash", "error": "exit code 1: cat: missing", "loop_id": "loop-d"},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.execute.bash", RawData: dispatch},
		{Sequence: 2, Subject: "tool.result.c1", RawData: mk("c1")},
		{Sequence: 3, Subject: "tool.result.c2", RawData: mk("c2")},
	}}
	if got := (RepeatToolFailure{}).Run(b); got != nil {
		t.Errorf("expected no diagnosis below threshold, got %d", len(got))
	}
}

// TestRepeatToolFailure_SuccessResetsStreak: an intervening successful
// tool result on the same (loop, tool) clears the streak. The model
// made forward progress; whatever was wedging it is no longer wedging.
func TestRepeatToolFailure_SuccessResetsStreak(t *testing.T) {
	mkErr := func(id string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "bash", "error": "exit 1", "loop_id": "loop-s"},
		})
	}
	mkOk := func(id string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "bash", "content": "ok", "loop_id": "loop-s"},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.a", RawData: mkErr("a")},
		{Sequence: 2, Subject: "tool.result.b", RawData: mkErr("b")},
		{Sequence: 3, Subject: "tool.result.ok", RawData: mkOk("ok")}, // resets
		{Sequence: 4, Subject: "tool.result.c", RawData: mkErr("c")},
		{Sequence: 5, Subject: "tool.result.d", RawData: mkErr("d")},
		// Only 2 failures since reset → should NOT fire
	}}
	if got := (RepeatToolFailure{}).Run(b); got != nil {
		t.Errorf("expected no diagnosis after success-reset, got %d", len(got))
	}
}

// TestRepeatToolFailure_DifferentErrorClassResets: a different error
// class on the same loop+tool resets the streak. The model's behavior
// changed; whatever the new failure is, it's a separate signal.
func TestRepeatToolFailure_DifferentErrorClassResets(t *testing.T) {
	mk := func(id, errStr string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "submit_work", "error": errStr, "loop_id": "loop-x"},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.a", RawData: mk("a", "validation failed: rejection_type is required")},
		{Sequence: 2, Subject: "tool.result.b", RawData: mk("b", "validation failed: rejection_type is required")},
		{Sequence: 3, Subject: "tool.result.c", RawData: mk("c", "validation failed: feedback is required")}, // different class, resets
		{Sequence: 4, Subject: "tool.result.d", RawData: mk("d", "validation failed: feedback is required")},
		// Two of each class, neither reaches threshold
	}}
	if got := (RepeatToolFailure{}).Run(b); got != nil {
		t.Errorf("expected no diagnosis with class switch, got %d", len(got))
	}
}

// TestRepeatToolFailure_DifferentLoopsDontMerge: the same error on
// DIFFERENT loops should NOT combine into one streak. Each loop is
// its own context.
func TestRepeatToolFailure_DifferentLoopsDontMerge(t *testing.T) {
	mk := func(id, loopID string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "submit_work", "error": "validation failed: foo", "loop_id": loopID},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.a", RawData: mk("a", "loop-1")},
		{Sequence: 2, Subject: "tool.result.b", RawData: mk("b", "loop-2")},
		{Sequence: 3, Subject: "tool.result.c", RawData: mk("c", "loop-3")},
	}}
	if got := (RepeatToolFailure{}).Run(b); got != nil {
		t.Errorf("expected no diagnosis with cross-loop spread, got %d", len(got))
	}
}

// TestRepeatToolFailure_OutOfOrderMessages: the bundle isn't guaranteed
// chronological. Detector must sort by sequence internally so streaks
// detect correctly regardless of message-bundle ordering.
func TestRepeatToolFailure_OutOfOrderMessages(t *testing.T) {
	mk := func(id string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "submit_work", "error": "validation failed: x", "loop_id": "loop-o"},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 30, Subject: "tool.result.c", RawData: mk("c")},
		{Sequence: 10, Subject: "tool.result.a", RawData: mk("a")},
		{Sequence: 20, Subject: "tool.result.b", RawData: mk("b")},
	}}
	got := RepeatToolFailure{}.Run(b)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnosis with out-of-order messages, got %d", len(got))
	}
	// First-seq evidence should be the earliest-seq failure (10), not message order
	var firstSeqEvidence string
	for _, e := range got[0].Evidence {
		if e.Field == "first_failure" {
			firstSeqEvidence = e.ID
		}
	}
	if firstSeqEvidence != "10" {
		t.Errorf("first_failure evidence ID = %q, want \"10\" (chronologically earliest)", firstSeqEvidence)
	}
}

// TestRepeatToolFailure_GraphSearchEOFAlsoTrips: the detector is tool-agnostic,
// so the v3 graph_search EOF wedge would have tripped this too (in addition to
// the dedicated GraphToolFailure detector). Defense in depth — different
// detector classes catch overlapping but differently-shaped failures.
func TestRepeatToolFailure_GraphSearchEOFAlsoTrips(t *testing.T) {
	mk := func(id string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{"call_id": id, "name": "graph_search", "error": "graph search failed: ... EOF", "loop_id": "loop-g"},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.a", RawData: mk("a")},
		{Sequence: 2, Subject: "tool.result.b", RawData: mk("b")},
		{Sequence: 3, Subject: "tool.result.c", RawData: mk("c")},
	}}
	got := RepeatToolFailure{}.Run(b)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnosis, got %d", len(got))
	}
	if !strings.Contains(got[0].Remediation, "graph_search") {
		t.Errorf("Remediation should name graph_search")
	}
}

// TestRepeatToolFailure_BenignWorktree404Suppressed covers issue #179: a
// redelivered duplicate loop for an already-completed node hits a GC'd worktree
// and the sandbox returns "worktree not found". These are benign and must NOT
// count toward a failure streak.
func TestRepeatToolFailure_BenignWorktree404Suppressed(t *testing.T) {
	const errStr = "sandbox exec failed: exec: server error 404: worktree not found"
	mk := func(callID string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{
				"call_id": callID, "name": "bash", "error": errStr, "loop_id": "loop-dup-1",
			},
		})
	}
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.a", RawData: mk("a")},
		{Sequence: 2, Subject: "tool.result.b", RawData: mk("b")},
		{Sequence: 3, Subject: "tool.result.c", RawData: mk("c")},
		{Sequence: 4, Subject: "tool.result.d", RawData: mk("d")},
	}}
	if got := (RepeatToolFailure{}).Run(b); len(got) != 0 {
		t.Fatalf("worktree-404 repeats must not raise a diagnosis, got %d: %+v", len(got), got)
	}
}

// TestRepeatToolFailure_RecoveredLoopSuppressed covers issue #179: a loop that
// repeated a real tool error 3+ times but ultimately RECOVERED (outcome=success
// in the AGENT_LOOPS entry) must not raise a critical wedge alert in a post-hoc
// bundle.
func TestRepeatToolFailure_RecoveredLoopSuppressed(t *testing.T) {
	const errStr = "validation failed: ./gradlew test exit 1"
	mk := func(callID string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{
				"call_id": callID, "name": "bash", "error": errStr, "loop_id": "loop-grad-1",
			},
		})
	}
	loopVal := mustRawJSON(t, map[string]any{"id": "loop-grad-1", "state": "complete", "outcome": "success"})
	b := &Bundle{
		Messages: []Message{
			{Sequence: 1, Subject: "tool.result.a", RawData: mk("a")},
			{Sequence: 2, Subject: "tool.result.b", RawData: mk("b")},
			{Sequence: 3, Subject: "tool.result.c", RawData: mk("c")},
		},
		Loops: []KVEntry{{Key: "loop-grad-1", Value: loopVal}},
	}
	if got := (RepeatToolFailure{}).Run(b); len(got) != 0 {
		t.Fatalf("a recovered (outcome=success) loop must not raise a diagnosis, got %d: %+v", len(got), got)
	}
}

// TestRepeatToolFailure_NonSuccessLoopStillFires confirms the outcome gate only
// suppresses success: a loop that terminally failed (or whose outcome is
// unknown / still running) still raises the critical so live --bail-on works.
func TestRepeatToolFailure_NonSuccessLoopStillFires(t *testing.T) {
	const errStr = "validation failed: rejection_type is required"
	mk := func(callID string) []byte {
		return mustRawJSON(t, map[string]any{
			"payload": map[string]any{
				"call_id": callID, "name": "submit_work", "error": errStr, "loop_id": "loop-fail-1",
			},
		})
	}
	loopVal := mustRawJSON(t, map[string]any{"id": "loop-fail-1", "state": "complete", "outcome": "failure"})
	b := &Bundle{
		Messages: []Message{
			{Sequence: 1, Subject: "tool.result.a", RawData: mk("a")},
			{Sequence: 2, Subject: "tool.result.b", RawData: mk("b")},
			{Sequence: 3, Subject: "tool.result.c", RawData: mk("c")},
		},
		Loops: []KVEntry{{Key: "loop-fail-1", Value: loopVal}},
	}
	got := (RepeatToolFailure{}).Run(b)
	if len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("a terminally-failed loop must still raise the critical, got %+v", got)
	}
}

// TestRepeatToolFailure_MalformedPayloadNoEmptyEvidenceEmit covers issue #179:
// tool.result payloads that won't decode (e.g. beta.107 payload summarization)
// are an observer-side condition, not a tool failure — they must NOT surface as
// a RepeatToolFailure with an empty evidence_id.
func TestRepeatToolFailure_MalformedPayloadNoEmptyEvidenceEmit(t *testing.T) {
	b := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "tool.result.a", RawData: []byte(`{not valid json`)},
		{Sequence: 2, Subject: "tool.result.b", RawData: []byte(`also broken`)},
		{Sequence: 3, Subject: "tool.result.c", RawData: []byte(`{"payload": <bad>}`)},
	}}
	if got := (RepeatToolFailure{}).Run(b); len(got) != 0 {
		t.Fatalf("malformed payloads must not emit a diagnosis (no empty evidence_id), got %d: %+v", len(got), got)
	}
}
