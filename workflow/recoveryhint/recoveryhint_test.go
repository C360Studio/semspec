package recoveryhint

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/observability"
)

// natsKVKeyPattern matches the character set NATS JetStream KV
// accepts in keys: [a-zA-Z0-9_-./=]. Same constraint pin as
// workflow/parseincident — incident IDs must be valid KV keys
// because graph-ingest stores them via CAS write.
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
		return errors.New("simulated UpsertEntity failure")
	}
	rw.upserts = append(rw.upserts, recordedUpsert{entityType, entityID, triples})
	return nil
}

// findAll returns all triples matching predicate in the most recent upsert.
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

// findOne returns the first triple matching predicate in the most recent upsert.
func (rw *recordingWriter) findOne(_, predicate string) *message.Triple {
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

// ----- Suggest --------------------------------------------------------

func TestSuggest_PicksClosestByEditDistance(t *testing.T) {
	target := "semspec.semsource.code.workspace.file.main-go"
	candidates := []string{
		"semspec.semsource.golang.workspace.file.main-go",            // closest — 2 segments differ slightly
		"semspec.semsource.code.workspace.folder.internal-auth",      // farther
		"completely.different.entity.id.elsewhere.foo",               // farthest
		"semspec.semsource.golang.workspace.file.internal-auth-auth", // close-ish
	}
	got := Suggest(target, candidates, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 suggestions, got %d: %v", len(got), got)
	}
	// The closest match for "code.workspace.file.main-go" target with
	// "golang.workspace.file.main-go" available SHOULD be that golang
	// variant (one segment differs vs many).
	if got[0] != "semspec.semsource.golang.workspace.file.main-go" {
		t.Errorf("first suggestion = %q, want golang.workspace.file.main-go", got[0])
	}
}

func TestSuggest_HandlesEmptyAndZero(t *testing.T) {
	if got := Suggest("x", nil, 5); got != nil {
		t.Errorf("empty candidates should return nil, got %v", got)
	}
	if got := Suggest("x", []string{"a", "b"}, 0); got != nil {
		t.Errorf("n=0 should return nil, got %v", got)
	}
	if got := Suggest("x", []string{"a", "b"}, -1); got != nil {
		t.Errorf("n<0 should return nil, got %v", got)
	}
}

func TestSuggest_NCapsAtCandidatesLength(t *testing.T) {
	got := Suggest("foo", []string{"a", "b"}, 5)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (capped at candidates len)", len(got))
	}
}

func TestSuggest_SkipsEmptyCandidates(t *testing.T) {
	got := Suggest("x", []string{"", "valid", ""}, 5)
	if len(got) != 1 || got[0] != "valid" {
		t.Errorf("expected [valid], got %v", got)
	}
}

func TestSuggest_StableOnTies(t *testing.T) {
	// Both candidates have identical distance from "x" — preserve input order.
	got := Suggest("x", []string{"first", "second"}, 2)
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("stable order broken: got %v, want [first second]", got)
	}
}

// ----- buildIncidentTriples — pure unit tests --------------------------------

// TestBuildIncidentTriples_FullAttributeSet verifies the pure helper emits all
// attributes including call_id, with optional fields present when non-empty and
// candidates emitted one-per-triple. No NATS involved.
func TestBuildIncidentTriples_FullAttributeSet(t *testing.T) {
	rc := RecoveryContext{
		CallID:   "loop-build",
		Role:     "developer",
		Model:    "openrouter-qwen3-moe",
		ToolName: "graph_query",
	}
	re := RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: `{ entity(id: "semspec.semsource.code.workspace.file.main-go") }`,
		Candidates:    []string{"semspec.semsource.golang.workspace.file.main-go", "semspec.semsource.code.workspace.folder.internal-auth"},
	}
	incidentID := "loop-build.tool-recovery.graph_query.deadbeef"

	triples := buildIncidentTriples(incidentID, rc, re)

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
		observability.ToolRecoveryOutcome:       observability.ToolRecoveryOutcomeSuggested,
		observability.ToolRecoveryToolName:      "graph_query",
		observability.IncidentCallID:            "loop-build",
		observability.ToolRecoveryOriginalQuery: re.OriginalQuery,
		observability.ToolRecoveryRole:          "developer",
		observability.ToolRecoveryModel:         "openrouter-qwen3-moe",
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

	// Candidates — one per suggested ID.
	cands := findAll(observability.ToolRecoveryCandidate)
	if len(cands) != 2 {
		t.Errorf("expected 2 candidate triples, got %d", len(cands))
	}
	gotCands := map[any]bool{}
	for _, c := range cands {
		gotCands[c.Object] = true
	}
	for _, want := range re.Candidates {
		if !gotCands[want] {
			t.Errorf("missing candidate triple %q", want)
		}
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
	rc := RecoveryContext{CallID: "loop-min", ToolName: "graph_query"} // Role/Model empty
	re := RecoveryEvent{Outcome: observability.ToolRecoveryOutcomeNotSuggested}
	incidentID := "loop-min.tool-recovery.graph_query.00000000"

	triples := buildIncidentTriples(incidentID, rc, re)

	find := func(pred string) *message.Triple {
		for i := range triples {
			if triples[i].Predicate == pred {
				return &triples[i]
			}
		}
		return nil
	}

	for _, pred := range []string{
		observability.ToolRecoveryOriginalQuery,
		observability.ToolRecoveryRole,
		observability.ToolRecoveryModel,
		observability.ToolRecoveryCandidate,
	} {
		if t2 := find(pred); t2 != nil {
			t.Errorf("empty optional field %q must not generate a triple; got %+v", pred, t2)
		}
	}

	// call_id always present.
	if t2 := find(observability.IncidentCallID); t2 == nil {
		t.Error("call_id triple must always be present")
	}
}

// ----- Emit — path-pin tests -------------------------------------------------

// TestEmit_UsesUpsertEntity_NotWriteTriple is the genuine path pin for
// slice #2: confirms Emit calls UpsertEntity exactly once and makes ZERO
// WriteTriple calls. Slice #1 lacked this assertion.
func TestEmit_UsesUpsertEntity_NotWriteTriple(t *testing.T) {
	rw := &recordingWriter{}
	rc := RecoveryContext{CallID: "loop-pin", Role: "developer", Model: "m1", ToolName: "graph_query"}
	re := RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: "some query",
	}
	id, err := Emit(context.Background(), rw, rc, re)
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
	if rw.upserts[0].entityType != RecoveryIncidentEntityType {
		t.Errorf("UpsertEntity entity type = %+v, want %+v", rw.upserts[0].entityType, RecoveryIncidentEntityType)
	}
	if rw.upserts[0].entityID != id {
		t.Errorf("UpsertEntity entity ID = %q, want %q", rw.upserts[0].entityID, id)
	}
}

// ----- Existing guard tests (preserved, updated to new writer shape) ---------

func TestEmit_NilWriter_NoOp(t *testing.T) {
	id, err := Emit(context.Background(), nil, RecoveryContext{CallID: "x", ToolName: "graph_query"}, RecoveryEvent{
		Outcome: observability.ToolRecoveryOutcomeSuggested,
	})
	if err != nil {
		t.Errorf("nil writer should be no-op, got error: %v", err)
	}
	if id != "" {
		t.Errorf("nil writer should return empty ID, got %q", id)
	}
}

func TestEmit_EmptyOutcome_NoOp(t *testing.T) {
	rw := &recordingWriter{}
	id, err := Emit(context.Background(), rw, RecoveryContext{CallID: "x", ToolName: "graph_query"}, RecoveryEvent{Outcome: ""})
	if err != nil {
		t.Errorf("empty outcome should be no-op, got error: %v", err)
	}
	if id != "" || len(rw.upserts) != 0 {
		t.Errorf("empty outcome must emit nothing")
	}
}

func TestEmit_MissingCallID_Error(t *testing.T) {
	rw := &recordingWriter{}
	_, err := Emit(context.Background(), rw, RecoveryContext{ToolName: "graph_query"}, RecoveryEvent{
		Outcome: observability.ToolRecoveryOutcomeSuggested,
	})
	if err == nil {
		t.Error("expected error when CallID is empty")
	}
}

func TestEmit_MissingToolName_Error(t *testing.T) {
	rw := &recordingWriter{}
	_, err := Emit(context.Background(), rw, RecoveryContext{CallID: "x"}, RecoveryEvent{
		Outcome: observability.ToolRecoveryOutcomeSuggested,
	})
	if err == nil {
		t.Error("expected error when ToolName is empty")
	}
}

func TestEmit_Suggested_FullTripleSet(t *testing.T) {
	rw := &recordingWriter{}
	rc := RecoveryContext{
		CallID:   "loop-rec",
		Role:     "developer",
		Model:    "openrouter-qwen3-moe",
		ToolName: "graph_query",
	}
	re := RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: `{ entity(id: "semspec.semsource.code.workspace.file.main-go") }`,
		Candidates:    []string{"semspec.semsource.golang.workspace.file.main-go", "semspec.semsource.code.workspace.folder.internal-auth"},
	}
	id, err := Emit(context.Background(), rw, rc, re)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Deterministic ID shape.
	if !strings.HasPrefix(id, "loop-rec.tool-recovery.graph_query.") {
		t.Errorf("incident ID prefix wrong: %q", id)
	}
	// NATS KV key safety — see natsKVKeyPattern godoc.
	if !natsKVKeyPattern.MatchString(id) {
		t.Errorf("incident ID %q contains chars NATS KV won't accept (allowed: [a-zA-Z0-9_./=-])", id)
	}

	// All required + populated optional predicates present.
	checks := map[string]any{
		observability.ToolRecoveryOutcome:       observability.ToolRecoveryOutcomeSuggested,
		observability.ToolRecoveryToolName:      "graph_query",
		observability.IncidentCallID:            "loop-rec",
		observability.ToolRecoveryOriginalQuery: re.OriginalQuery,
		observability.ToolRecoveryRole:          "developer",
		observability.ToolRecoveryModel:         "openrouter-qwen3-moe",
	}
	for pred, want := range checks {
		got := rw.findOne(id, pred)
		if got == nil {
			t.Errorf("missing triple for %q", pred)
			continue
		}
		if got.Object != want {
			t.Errorf("predicate %q object = %v, want %v", pred, got.Object, want)
		}
	}

	// Multi-value candidates — one triple per candidate.
	cands := rw.findAll(id, observability.ToolRecoveryCandidate)
	if len(cands) != 2 {
		t.Errorf("expected 2 candidate triples, got %d", len(cands))
	}
	gotCands := map[any]bool{}
	for _, c := range cands {
		gotCands[c.Object] = true
	}
	for _, want := range re.Candidates {
		if !gotCands[want] {
			t.Errorf("missing candidate triple %q", want)
		}
	}
}

func TestEmit_NotSuggested_NoCandidateTriples(t *testing.T) {
	rw := &recordingWriter{}
	rc := RecoveryContext{CallID: "loop-x", ToolName: "graph_query"}
	re := RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeNotSuggested,
		OriginalQuery: "...",
	}
	id, err := Emit(context.Background(), rw, rc, re)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rw.findAll(id, observability.ToolRecoveryCandidate)) != 0 {
		t.Error("not_suggested outcome must not emit candidate triples")
	}
	// Outcome triple still written.
	out := rw.findOne(id, observability.ToolRecoveryOutcome)
	if out == nil || out.Object != observability.ToolRecoveryOutcomeNotSuggested {
		t.Error("missing or wrong outcome triple")
	}
}

func TestEmit_DeterministicIDOnSameQuery(t *testing.T) {
	// Two emits with the same OriginalQuery must produce identical
	// incident IDs — idempotent SKG state on retry replay.
	rw := &recordingWriter{}
	rc := RecoveryContext{CallID: "loop-d", ToolName: "graph_query"}
	re := RecoveryEvent{Outcome: observability.ToolRecoveryOutcomeSuggested, OriginalQuery: "same query"}
	id1, _ := Emit(context.Background(), rw, rc, re)
	rw.upserts = nil // reset
	id2, _ := Emit(context.Background(), rw, rc, re)
	if id1 != id2 {
		t.Errorf("expected deterministic IDs, got %q and %q", id1, id2)
	}
}

func TestEmit_DifferentQueries_DifferentIDs(t *testing.T) {
	// Two emits with different OriginalQuery on the SAME loop must
	// produce different incident IDs — recovery has no checkpoint
	// cap, so a single loop can fire many recoveries on different IDs
	// (today's wedge had 28 occurrences). Hash suffix dedupes
	// per-query naturally.
	rw := &recordingWriter{}
	rc := RecoveryContext{CallID: "loop-d", ToolName: "graph_query"}
	id1, _ := Emit(context.Background(), rw, rc, RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: "query 1",
	})
	id2, _ := Emit(context.Background(), rw, rc, RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: "query 2",
	})
	if id1 == id2 {
		t.Errorf("different queries should yield different IDs, got %q == %q", id1, id2)
	}
}

// TestEmit_UpsertEntityFails_ReturnsError confirms Emit propagates UpsertEntity
// errors and returns empty ID (single-call atomic model — either the node lands
// or it doesn't, no partial state).
func TestEmit_UpsertEntityFails_ReturnsError(t *testing.T) {
	rw := &recordingWriter{failUpsert: true}
	rc := RecoveryContext{CallID: "loop-mid", ToolName: "graph_query"}
	re := RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: "x",
	}
	id, err := Emit(context.Background(), rw, rc, re)
	if err == nil {
		t.Error("expected error when UpsertEntity fails")
	}
	if id != "" {
		t.Errorf("expected empty ID on upsert failure, got %q", id)
	}
}

func TestEmit_EmptyOptionalFields_Skipped(t *testing.T) {
	rw := &recordingWriter{}
	rc := RecoveryContext{CallID: "loop-min", ToolName: "graph_query"}
	re := RecoveryEvent{Outcome: observability.ToolRecoveryOutcomeNotSuggested}
	id, err := Emit(context.Background(), rw, rc, re)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, pred := range []string{
		observability.ToolRecoveryOriginalQuery,
		observability.ToolRecoveryRole,
		observability.ToolRecoveryModel,
	} {
		if rw.findOne(id, pred) != nil {
			t.Errorf("empty optional field %q must not generate a triple", pred)
		}
	}
}
