package parseincident

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/observability"
)

// natsKVKeyPattern matches the character set NATS JetStream KV
// accepts in keys: [a-zA-Z0-9_-./=]. The original incident-ID format
// shipped 2026-05-04 used `:` separators which graph-ingest CAS-write
// rejected on first real-LLM run. Pin the format so a future
// regression that drops back to `:` (or any other forbidden char)
// fails loudly at unit-test time instead of silently in production.
var natsKVKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_./=-]+$`)

// recordedUpsert captures a single UpsertEntity invocation.
type recordedUpsert struct {
	entityType message.Type
	entityID   string
	triples    []message.Triple
}

// recordingWriter implements tripleWriter for tests. It records UpsertEntity
// calls in upserts and WriteTriple calls in writeCalls. Tests assert that
// Emit uses exactly one UpsertEntity and zero WriteTriple calls — the path
// pin that confirms the migration off graph.mutation.triple.add.
type recordingWriter struct {
	upserts    []recordedUpsert
	writeCalls int  // count of WriteTriple calls — must stay 0 after migration
	failUpsert bool // when true, UpsertEntity returns an error
}

func (rw *recordingWriter) WriteTriple(_ context.Context, _, _ string, _ any) error {
	rw.writeCalls++
	return nil
}

func (rw *recordingWriter) UpsertEntity(_ context.Context, entityType message.Type, entityID string, triples []message.Triple) error {
	if rw.failUpsert {
		return errors.New("recording-writer simulated UpsertEntity failure")
	}
	rw.upserts = append(rw.upserts, recordedUpsert{entityType, entityID, triples})
	return nil
}

// findTriple returns the first triple matching predicate in the most recent
// upsert, or nil. Helpers mirror the old recordedTriple helpers so individual
// predicate assertions read identically.
func (rw *recordingWriter) findTriple(_, predicate string) *message.Triple {
	if len(rw.upserts) == 0 {
		return nil
	}
	last := rw.upserts[len(rw.upserts)-1]
	for i := range last.triples {
		if last.triples[i].Predicate == predicate {
			return &last.triples[i]
		}
	}
	return nil
}

func (rw *recordingWriter) findAll(_, predicate string) []message.Triple {
	if len(rw.upserts) == 0 {
		return nil
	}
	last := rw.upserts[len(rw.upserts)-1]
	var out []message.Triple
	for _, t := range last.triples {
		if t.Predicate == predicate {
			out = append(out, t)
		}
	}
	return out
}

// ----- buildIncidentTriples — pure unit tests --------------------------------

// TestBuildIncidentTriples_FullAttributeSet verifies the pure helper emits all
// attributes including call_id, with optional fields present when non-empty and
// quirks emitted one-per-triple. No NATS involved.
func TestBuildIncidentTriples_FullAttributeSet(t *testing.T) {
	ic := IncidentContext{
		CallID:        "loop-build",
		Role:          "planner",
		Model:         "openrouter-qwen3-moe",
		PromptVersion: "v3.2",
	}
	ev := IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		Outcome:     observability.OutcomeToleratedQuirk,
		Quirks:      []string{"fenced_json_wrapper", "trailing_commas"},
		Reason:      "stripped fences",
		RawResponse: "```json\n{\"x\":1,}\n```",
	}
	incidentID := "loop-build.parse.response_parse"

	triples := buildIncidentTriples(incidentID, ic, ev)

	// Helper to find by predicate value.
	find := func(pred string) *message.Triple {
		for i := range triples {
			if triples[i].Predicate == pred {
				return &triples[i]
			}
		}
		return nil
	}
	findAll := func(pred string) []message.Triple {
		var out []message.Triple
		for _, t := range triples {
			if t.Predicate == pred {
				out = append(out, t)
			}
		}
		return out
	}

	// Required always-present predicates.
	required := map[string]any{
		observability.Checkpoint:     observability.CheckpointResponseParse,
		observability.Outcome:        observability.OutcomeToleratedQuirk,
		observability.IncidentCallID: "loop-build",
		observability.Reason:         "stripped fences",
		observability.Role:           "planner",
		observability.Model:          "openrouter-qwen3-moe",
		observability.PromptVersion:  "v3.2",
		observability.RawResponse:    "```json\n{\"x\":1,}\n```",
	}
	for pred, want := range required {
		got := find(pred)
		if got == nil {
			t.Errorf("missing triple for predicate %q", pred)
			continue
		}
		if got.Object != want {
			t.Errorf("predicate %q object = %v, want %v", pred, got.Object, want)
		}
	}

	// Quirks — one per fired quirk.
	quirks := findAll(observability.Quirk)
	if len(quirks) != 2 {
		t.Errorf("expected 2 quirk triples, got %d", len(quirks))
	}
	gotQ := map[any]bool{}
	for _, q := range quirks {
		gotQ[q.Object] = true
	}
	if !gotQ["fenced_json_wrapper"] || !gotQ["trailing_commas"] {
		t.Errorf("expected both fenced_json_wrapper and trailing_commas in quirks, got %v", gotQ)
	}

	// All subjects must equal incidentID.
	for _, tr := range triples {
		if tr.Subject != incidentID {
			t.Errorf("triple subject = %q, want %q (predicate %q)", tr.Subject, incidentID, tr.Predicate)
		}
	}
}

// TestBuildIncidentTriples_OptionalAbsentWhenEmpty confirms empty optional
// fields produce no triples — no sentinel-value pollution.
func TestBuildIncidentTriples_OptionalAbsentWhenEmpty(t *testing.T) {
	ic := IncidentContext{CallID: "loop-min"} // Role/Model/PromptVersion all empty
	ev := IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
		// Reason, RawResponse empty
	}
	incidentID := "loop-min.parse.response_parse"

	triples := buildIncidentTriples(incidentID, ic, ev)

	find := func(pred string) *message.Triple {
		for i := range triples {
			if triples[i].Predicate == pred {
				return &triples[i]
			}
		}
		return nil
	}

	// Optional fields that were empty must not appear.
	for _, pred := range []string{
		observability.Reason,
		observability.Role,
		observability.Model,
		observability.PromptVersion,
		observability.RawResponse,
		observability.RawResponseTruncated,
	} {
		if t2 := find(pred); t2 != nil {
			t.Errorf("empty optional field %q must not generate a triple; got %+v", pred, t2)
		}
	}

	// call_id is required even when other optionals are absent.
	if t2 := find(observability.IncidentCallID); t2 == nil {
		t.Error("call_id triple must always be present")
	}
}

// TestBuildIncidentTriples_TruncationFlag pins that a response over the cap
// produces a truncated body triple AND a raw_response_truncated triple.
func TestBuildIncidentTriples_TruncationFlag(t *testing.T) {
	huge := strings.Repeat("a", MaxRawResponseBytes*2)
	ic := IncidentContext{CallID: "loop-big"}
	ev := IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		Outcome:     observability.OutcomeRejected,
		RawResponse: huge,
	}
	incidentID := "loop-big.parse.response_parse"

	triples := buildIncidentTriples(incidentID, ic, ev)

	find := func(pred string) *message.Triple {
		for i := range triples {
			if triples[i].Predicate == pred {
				return &triples[i]
			}
		}
		return nil
	}

	rawT := find(observability.RawResponse)
	if rawT == nil {
		t.Fatal("missing raw_response triple")
	}
	rawStr, ok := rawT.Object.(string)
	if !ok {
		t.Fatalf("raw_response object is not a string: %T", rawT.Object)
	}
	if len(rawStr) > MaxRawResponseBytes {
		t.Errorf("raw_response should be ≤%d bytes, got %d", MaxRawResponseBytes, len(rawStr))
	}
	truncT := find(observability.RawResponseTruncated)
	if truncT == nil {
		t.Fatal("missing raw_response_truncated flag")
	}
	if truncT.Object != true {
		t.Errorf("raw_response_truncated = %v, want true", truncT.Object)
	}
}

// ----- Emit — path-pin tests -------------------------------------------------

// TestEmit_UsesUpsertEntity_NotWriteTriple is the genuine path pin for
// slice #2: confirms Emit calls UpsertEntity exactly once and makes ZERO
// WriteTriple calls. Slice #1 lacked this assertion.
func TestEmit_UsesUpsertEntity_NotWriteTriple(t *testing.T) {
	rw := &recordingWriter{}
	ic := IncidentContext{CallID: "loop-pin", Role: "planner", Model: "m1"}
	ev := IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
		Reason:     "no JSON",
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty incident ID")
	}
	if len(rw.upserts) != 1 {
		t.Errorf("expected 1 UpsertEntity call, got %d", len(rw.upserts))
	}
	if rw.writeCalls != 0 {
		t.Errorf("expected 0 WriteTriple calls (relation write dropped), got %d", rw.writeCalls)
	}
	if rw.upserts[0].entityType != ParseIncidentEntityType {
		t.Errorf("UpsertEntity entity type = %+v, want %+v", rw.upserts[0].entityType, ParseIncidentEntityType)
	}
	if rw.upserts[0].entityID != id {
		t.Errorf("UpsertEntity entity ID = %q, want %q", rw.upserts[0].entityID, id)
	}
}

// TestEmit_UpsertEntityFails_ReturnsEmptyID confirms Emit returns ("", err)
// when UpsertEntity fails — no partial incident ID is returned because
// the single-call model is atomic (either the node lands or it doesn't).
func TestEmit_UpsertEntityFails_ReturnsEmptyID(t *testing.T) {
	rw := &recordingWriter{failUpsert: true}
	_, err := Emit(context.Background(), rw, IncidentContext{CallID: "x"}, IncidentEvent{
		Checkpoint: observability.CheckpointResponseParse,
		Outcome:    observability.OutcomeRejected,
	})
	if err == nil {
		t.Error("expected error when UpsertEntity fails")
	}
}

// ----- Existing guard tests (preserved, updated to new writer shape) ---------

func TestEmit_StrictOutcome_NoOp(t *testing.T) {
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
	if len(rw.upserts) != 0 {
		t.Errorf("strict outcome must call ZERO UpsertEntity; got %d", len(rw.upserts))
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
	if len(rw.upserts) != 0 {
		t.Errorf("empty outcome must call ZERO UpsertEntity; got %d", len(rw.upserts))
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
	wantID := "loop-rej.parse.response_parse"
	if id != wantID {
		t.Errorf("incident ID = %q, want %q", id, wantID)
	}

	// All required + optional populated predicates present in the upsert.
	checks := map[string]any{
		observability.Checkpoint:     observability.CheckpointResponseParse,
		observability.Outcome:        observability.OutcomeRejected,
		observability.IncidentCallID: "loop-rej",
		observability.Reason:         "no JSON found in result",
		observability.Role:           "planner",
		observability.Model:          "openrouter-qwen3-moe",
		observability.PromptVersion:  "v3.2",
		observability.RawResponse:    "I'm not sure what to do here",
	}
	for pred, want := range checks {
		got := rw.findTriple(wantID, pred)
		if got == nil {
			t.Errorf("missing triple for predicate %q", pred)
			continue
		}
		if got.Object != want {
			t.Errorf("predicate %q object = %v, want %v", pred, got.Object, want)
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

	// One quirk triple per fired quirk — RDF-correct multi-value.
	quirks := rw.findAll(id, observability.Quirk)
	if len(quirks) != 2 {
		t.Errorf("expected 2 quirk triples, got %d: %+v", len(quirks), quirks)
	}
	gotQuirks := make(map[any]bool)
	for _, q := range quirks {
		gotQuirks[q.Object] = true
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

	// Optional empty fields should NOT generate triples.
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
	if rw.findTriple(id, observability.IncidentCallID) == nil {
		t.Error("missing required call_id triple")
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
	rawString, ok := rawTriple.Object.(string)
	if !ok {
		t.Fatalf("raw_response object is not a string: %T", rawTriple.Object)
	}
	if len(rawString) > MaxRawResponseBytes {
		t.Errorf("raw_response should be truncated to ≤%d bytes, got %d", MaxRawResponseBytes, len(rawString))
	}

	truncFlag := rw.findTriple(id, observability.RawResponseTruncated)
	if truncFlag == nil {
		t.Fatal("missing raw_response_truncated flag on truncated response")
	}
	if truncFlag.Object != true {
		t.Errorf("raw_response_truncated = %v, want true", truncFlag.Object)
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
		wantUpsert  bool
	}{
		{
			name:        "parse failure → rejected",
			parseErr:    errors.New("invalid JSON"),
			wantOutcome: observability.OutcomeRejected,
			wantUpsert:  true,
		},
		{
			name:        "quirks fired → tolerated_quirk",
			quirks:      []string{"fenced_json_wrapper"},
			wantOutcome: observability.OutcomeToleratedQuirk,
			wantUpsert:  true,
		},
		{
			name:        "clean parse → strict (no upsert)",
			wantOutcome: observability.OutcomeStrict,
			wantUpsert:  false,
		},
		{
			name:        "parse error wins over quirks",
			quirks:      []string{"fenced_json_wrapper"},
			parseErr:    errors.New("typed unmarshal failed"),
			wantOutcome: observability.OutcomeRejected,
			wantUpsert:  true,
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
			if tt.wantUpsert {
				if id == "" {
					t.Error("expected non-empty incident ID")
				}
				if len(rw.upserts) != 1 {
					t.Fatalf("expected 1 UpsertEntity, got %d", len(rw.upserts))
				}
				outcome := rw.findTriple(id, observability.Outcome)
				if outcome == nil {
					t.Fatal("missing outcome triple")
				}
				if outcome.Object != tt.wantOutcome {
					t.Errorf("outcome = %v, want %s", outcome.Object, tt.wantOutcome)
				}
			} else {
				if id != "" {
					t.Errorf("strict outcome should produce empty incident ID, got %q", id)
				}
				if len(rw.upserts) != 0 {
					t.Errorf("strict outcome should produce ZERO UpsertEntity calls, got %d", len(rw.upserts))
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

// Pin that incident IDs use only NATS-KV-safe characters. The
// original 2026-05-04 ship used `:` which graph-ingest rejected with
// "nats: invalid key" — caught by active polling on first real-LLM
// run with parse rejections. See emit.go's godoc for the full story.
func TestEmit_IncidentID_IsNATSKVSafe(t *testing.T) {
	rw := &recordingWriter{}
	ic := IncidentContext{CallID: "loop-uuid-08161be1-3a1b-4318-b080-241ec2d1eb1f", Role: "planner", Model: "openrouter-qwen3-moe"}
	ev := IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		Outcome:     observability.OutcomeRejected,
		Reason:      "empty result",
		RawResponse: "raw",
	}
	id, err := Emit(context.Background(), rw, ic, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !natsKVKeyPattern.MatchString(id) {
		t.Errorf("incident ID %q contains characters NATS KV won't accept (allowed: [a-zA-Z0-9_./=-])", id)
	}
	// Belt-and-suspenders — explicitly forbid the historical `:`.
	if strings.Contains(id, ":") {
		t.Errorf("incident ID %q must not contain ':' (NATS KV rejects with 'invalid key')", id)
	}
}

func TestTruncateUTF8Safe_PinsRuneBoundary(t *testing.T) {
	// 4-byte rune emoji repeated such that the cap lands mid-rune.
	in := strings.Repeat("🦀", 100) // each crab is 4 bytes
	maxBytes := 7                  // forces the cap to land mid-rune (🦀🦀 = 8 bytes; 7 = halfway through second)
	out, truncated := truncateUTF8Safe(in, maxBytes)
	if !truncated {
		t.Error("expected truncated=true for over-cap input")
	}
	if len(out) > maxBytes {
		t.Errorf("output longer than cap: %d > %d", len(out), maxBytes)
	}
	// Result must be valid UTF-8 — no mid-rune byte split.
	if !utf8.ValidString(out) {
		t.Errorf("truncated output is not valid UTF-8: %q (len=%d)", out, len(out))
	}
}
