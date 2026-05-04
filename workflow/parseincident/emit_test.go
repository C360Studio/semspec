package parseincident

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/c360studio/semspec/vocabulary/observability"
)

// recordedTriple captures a single WriteTriple invocation for
// table-driven assertions on the emitted triple set.
type recordedTriple struct {
	subject   string
	predicate string
	object    any
}

// recordingWriter implements tripleWriter by appending every call to
// an in-memory slice. Tests assert on the slice's length, ordering,
// and predicate/object pairs.
type recordingWriter struct {
	triples []recordedTriple
	failOn  string // when set, return an error on the matching predicate
}

func (rw *recordingWriter) WriteTriple(_ context.Context, subject, predicate string, object any) error {
	if rw.failOn != "" && rw.failOn == predicate {
		return errors.New("recording-writer simulated failure")
	}
	rw.triples = append(rw.triples, recordedTriple{subject, predicate, object})
	return nil
}

// findTriple returns the first triple matching subject+predicate, or nil.
func (rw *recordingWriter) findTriple(subject, predicate string) *recordedTriple {
	for i := range rw.triples {
		if rw.triples[i].subject == subject && rw.triples[i].predicate == predicate {
			return &rw.triples[i]
		}
	}
	return nil
}

func (rw *recordingWriter) findAll(subject, predicate string) []recordedTriple {
	var out []recordedTriple
	for _, t := range rw.triples {
		if t.subject == subject && t.predicate == predicate {
			out = append(out, t)
		}
	}
	return out
}

func TestEmit_StrictOutcome_NoTriples(t *testing.T) {
	rw := &recordingWriter{}
	ic := IncidentContext{CallID: "loop-1", Role: "planner", Model: "test"}
	ev := IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeStrict,
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("incident ID should be empty for strict outcome, got %q", id)
	}
	if len(rw.triples) != 0 {
		t.Errorf("strict outcome must emit ZERO triples; got %d: %+v", len(rw.triples), rw.triples)
	}
}

func TestEmit_NilWriter_NoOp(t *testing.T) {
	id, err := Emit(context.Background(), nil, IncidentContext{CallID: "x"}, IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
	})
	if err != nil {
		t.Errorf("nil writer should be a no-op, got error: %v", err)
	}
	if id != "" {
		t.Errorf("nil writer should return empty ID, got %q", id)
	}
}

func TestEmit_EmptyOutcome_NoOp(t *testing.T) {
	rw := &recordingWriter{}
	id, err := Emit(context.Background(), rw, IncidentContext{CallID: "x"}, IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    "",
	})
	if err != nil {
		t.Errorf("empty outcome should be a no-op, got error: %v", err)
	}
	if id != "" {
		t.Errorf("empty outcome should return empty ID, got %q", id)
	}
	if len(rw.triples) != 0 {
		t.Errorf("empty outcome must emit ZERO triples; got %d", len(rw.triples))
	}
}

func TestEmit_MissingCallID_Error(t *testing.T) {
	rw := &recordingWriter{}
	_, err := Emit(context.Background(), rw, IncidentContext{}, IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
	})
	if err == nil {
		t.Error("expected error when CallID is empty")
	}
}

func TestEmit_MissingCheckpoint_Error(t *testing.T) {
	rw := &recordingWriter{}
	_, err := Emit(context.Background(), rw, IncidentContext{CallID: "x"}, IncidentEvent{
		Outcome: observability.OutcomeRejected,
	})
	if err == nil {
		t.Error("expected error when Checkpoint is empty")
	}
}

func TestEmit_RejectedOutcome_FullTripleSet(t *testing.T) {
	rw := &recordingWriter{}
	ic := IncidentContext{
		CallID:        "loop-rej",
		Role:          "planner",
		Model:         "openrouter-qwen3-moe",
		PromptVersion: "v3.2",
	}
	ev := IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		Outcome:     observability.OutcomeRejected,
		Reason:      "no JSON found in result",
		RawResponse: "I'm not sure what to do here",
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantID := "loop-rej:parse:response_parse"
	if id != wantID {
		t.Errorf("incident ID = %q, want %q", id, wantID)
	}

	// Relation first.
	if len(rw.triples) == 0 || rw.triples[0].subject != "loop-rej" || rw.triples[0].predicate != observability.Incident {
		t.Errorf("first write must be the parse.incident relation; got %+v", rw.triples)
	}

	// All required + optional populated predicates present.
	checks := map[string]any{
		observability.Checkpoint:    observability.CheckpointResponseParse,
		observability.Outcome:       observability.OutcomeRejected,
		observability.Reason:        "no JSON found in result",
		observability.Role:          "planner",
		observability.Model:         "openrouter-qwen3-moe",
		observability.PromptVersion: "v3.2",
		observability.RawResponse:   "I'm not sure what to do here",
	}
	for pred, want := range checks {
		got := rw.findTriple(wantID, pred)
		if got == nil {
			t.Errorf("missing triple for predicate %q", pred)
			continue
		}
		if got.object != want {
			t.Errorf("predicate %q object = %v, want %v", pred, got.object, want)
		}
	}

	// No quirk triples on rejection.
	if quirks := rw.findAll(wantID, observability.Quirk); len(quirks) != 0 {
		t.Errorf("rejected outcome must not emit quirk triples; got %d", len(quirks))
	}

	// No truncation flag on a short response.
	if t2 := rw.findTriple(wantID, observability.RawResponseTruncated); t2 != nil {
		t.Errorf("short response should not emit raw_response_truncated; got %+v", t2)
	}
}

func TestEmit_ToleratedQuirk_MultipleQuirkTriples(t *testing.T) {
	rw := &recordingWriter{}
	ic := IncidentContext{CallID: "loop-tol", Role: "planner", Model: "m1"}
	ev := IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		Outcome:     observability.OutcomeToleratedQuirk,
		Quirks:      []string{"fenced_json_wrapper", "trailing_commas"},
		Reason:      "stripped fences and trailing commas",
		RawResponse: "```json\n{\"goal\":\"x\",}\n```",
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// One quirk triple per fired quirk — RDF-correct multi-value, NOT
	// a concatenated string.
	quirks := rw.findAll(id, observability.Quirk)
	if len(quirks) != 2 {
		t.Errorf("expected 2 quirk triples, got %d: %+v", len(quirks), quirks)
	}
	gotQuirks := make(map[any]bool)
	for _, q := range quirks {
		gotQuirks[q.object] = true
	}
	if !gotQuirks["fenced_json_wrapper"] || !gotQuirks["trailing_commas"] {
		t.Errorf("expected both fenced_json_wrapper and trailing_commas, got %v", gotQuirks)
	}
}

func TestEmit_EmptyOptionalFields_SkippedNotEmptyTriples(t *testing.T) {
	rw := &recordingWriter{}
	ic := IncidentContext{
		CallID: "loop-min",
		// Role, Model, PromptVersion all empty
	}
	ev := IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
		// Reason and RawResponse empty
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Required predicates: Incident relation + Checkpoint + Outcome.
	// Optional empty fields should NOT generate empty-string triples.
	for _, pred := range []string{
		observability.Reason,
		observability.Role,
		observability.Model,
		observability.PromptVersion,
		observability.RawResponse,
	} {
		if got := rw.findTriple(id, pred); got != nil {
			t.Errorf("empty optional field %q must not generate a triple, got %+v", pred, got)
		}
	}

	// Required ones present.
	if rw.findTriple(id, observability.Checkpoint) == nil {
		t.Error("missing required Checkpoint triple")
	}
	if rw.findTriple(id, observability.Outcome) == nil {
		t.Error("missing required Outcome triple")
	}
}

func TestEmit_LargeRawResponse_TruncatedWithFlag(t *testing.T) {
	rw := &recordingWriter{}
	huge := strings.Repeat("a", MaxRawResponseBytes*2)
	ic := IncidentContext{CallID: "loop-big"}
	ev := IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		Outcome:     observability.OutcomeRejected,
		RawResponse: huge,
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rawTriple := rw.findTriple(id, observability.RawResponse)
	if rawTriple == nil {
		t.Fatal("missing raw_response triple")
	}
	rawString, ok := rawTriple.object.(string)
	if !ok {
		t.Fatalf("raw_response object is not a string: %T", rawTriple.object)
	}
	if len(rawString) > MaxRawResponseBytes {
		t.Errorf("raw_response should be truncated to ≤%d bytes, got %d", MaxRawResponseBytes, len(rawString))
	}

	truncFlag := rw.findTriple(id, observability.RawResponseTruncated)
	if truncFlag == nil {
		t.Fatal("missing raw_response_truncated flag on truncated response")
	}
	if truncFlag.object != true {
		t.Errorf("raw_response_truncated = %v, want true", truncFlag.object)
	}
}

// EmitForResult derives outcome from (quirks, parseErr) — the most
// common shape callers face at a parse-checkpoint boundary. Pin all
// three branches plus the nil-writer no-op.
func TestEmitForResult_OutcomeBranches(t *testing.T) {
	tests := []struct {
		name        string
		quirks      []string
		parseErr    error
		wantOutcome string
		wantTriples bool
	}{
		{
			name:        "parse failure → rejected",
			parseErr:    errors.New("invalid JSON"),
			wantOutcome: observability.OutcomeRejected,
			wantTriples: true,
		},
		{
			name:        "quirks fired → tolerated_quirk",
			quirks:      []string{"fenced_json_wrapper"},
			wantOutcome: observability.OutcomeToleratedQuirk,
			wantTriples: true,
		},
		{
			name:        "clean parse → strict (no triples)",
			wantOutcome: observability.OutcomeStrict,
			wantTriples: false,
		},
		{
			name:        "parse error wins over quirks",
			quirks:      []string{"fenced_json_wrapper"},
			parseErr:    errors.New("typed unmarshal failed"),
			wantOutcome: observability.OutcomeRejected,
			wantTriples: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rw := &recordingWriter{}
			ic := IncidentContext{CallID: "loop-x", Role: "developer", Model: "m1"}
			id, err := EmitForResult(context.Background(), rw, ic, observability.CheckpointResponseParse, tt.quirks, "raw output", tt.parseErr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantTriples {
				if id == "" {
					t.Error("expected non-empty incident ID")
				}
				outcome := rw.findTriple(id, observability.Outcome)
				if outcome == nil {
					t.Fatal("missing outcome triple")
				}
				if outcome.object != tt.wantOutcome {
					t.Errorf("outcome = %v, want %s", outcome.object, tt.wantOutcome)
				}
			} else {
				if id != "" {
					t.Errorf("strict outcome should produce empty incident ID, got %q", id)
				}
				if len(rw.triples) != 0 {
					t.Errorf("strict outcome should produce ZERO triples, got %d", len(rw.triples))
				}
			}
		})
	}
}

func TestEmitForResult_NilWriter_NoOp(t *testing.T) {
	id, err := EmitForResult(context.Background(), nil, IncidentContext{CallID: "x"}, observability.CheckpointResponseParse, nil, "raw", errors.New("x"))
	if err != nil {
		t.Errorf("nil writer should be no-op, got error: %v", err)
	}
	if id != "" {
		t.Errorf("nil writer should return empty ID, got %q", id)
	}
}

func TestEmit_RelationWriteFails_ShortCircuits(t *testing.T) {
	rw := &recordingWriter{failOn: observability.Incident}
	_, err := Emit(context.Background(), rw, IncidentContext{CallID: "x"}, IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
	})
	if err == nil {
		t.Error("expected error when relation write fails")
	}
	if len(rw.triples) != 0 {
		t.Errorf("no triples should land when relation write fails, got %d", len(rw.triples))
	}
}

func TestTruncateUTF8Safe_PinsRuneBoundary(t *testing.T) {
	// 4-byte rune emoji repeated such that the cap lands mid-rune.
	in := strings.Repeat("🦀", 100) // each crab is 4 bytes
	cap := 7                        // forces the cap to land mid-rune (🦀🦀 = 8 bytes; 7 = halfway through second)
	out, truncated := truncateUTF8Safe(in, cap)
	if !truncated {
		t.Error("expected truncated=true for over-cap input")
	}
	if len(out) > cap {
		t.Errorf("output longer than cap: %d > %d", len(out), cap)
	}
	// Result must be valid UTF-8 — no mid-rune byte split.
	if !utf8.ValidString(out) {
		t.Errorf("truncated output is not valid UTF-8: %q (len=%d)", out, len(out))
	}
}
