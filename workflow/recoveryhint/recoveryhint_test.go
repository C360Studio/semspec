package recoveryhint

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semspec/vocabulary/observability"
)

// recordedTriple captures a single WriteTriple invocation.
type recordedTriple struct {
	subject, predicate string
	object             any
}

type recordingWriter struct {
	triples []recordedTriple
	failOn  string
}

func (rw *recordingWriter) WriteTriple(_ context.Context, subject, predicate string, object any) error {
	if rw.failOn != "" && rw.failOn == predicate {
		return errors.New("simulated failure")
	}
	rw.triples = append(rw.triples, recordedTriple{subject, predicate, object})
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

func (rw *recordingWriter) findOne(subject, predicate string) *recordedTriple {
	for i := range rw.triples {
		if rw.triples[i].subject == subject && rw.triples[i].predicate == predicate {
			return &rw.triples[i]
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

// ----- Emit -----------------------------------------------------------

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
	if id != "" || len(rw.triples) != 0 {
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
	if !strings.HasPrefix(id, "loop-rec:tool-recovery:graph_query:") {
		t.Errorf("incident ID prefix wrong: %q", id)
	}

	// Relation written FIRST (subject is the call ID).
	if len(rw.triples) == 0 || rw.triples[0].subject != "loop-rec" || rw.triples[0].predicate != observability.ToolRecoveryIncident {
		t.Errorf("first write must be the tool.recovery.incident relation; got %+v", rw.triples[0])
	}

	// All required + populated optional predicates present.
	checks := map[string]any{
		observability.ToolRecoveryOutcome:       observability.ToolRecoveryOutcomeSuggested,
		observability.ToolRecoveryToolName:      "graph_query",
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
		if got.object != want {
			t.Errorf("predicate %q object = %v, want %v", pred, got.object, want)
		}
	}

	// Multi-value candidates — one triple per candidate.
	cands := rw.findAll(id, observability.ToolRecoveryCandidate)
	if len(cands) != 2 {
		t.Errorf("expected 2 candidate triples, got %d", len(cands))
	}
	gotCands := map[any]bool{}
	for _, c := range cands {
		gotCands[c.object] = true
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
	if out == nil || out.object != observability.ToolRecoveryOutcomeNotSuggested {
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
	rw.triples = nil // reset
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

// Mid-write failure (e.g. on the ToolName attribute) should still
// return the partial incident ID — the relation triple landed
// successfully, so a graph reader can find the partial-state node.
// Documents the partial-write contract called out in Emit's
// godoc at emit.go:46-48.
func TestEmit_MidAttributeWriteFails_ReturnsPartialID(t *testing.T) {
	rw := &recordingWriter{failOn: observability.ToolRecoveryToolName}
	rc := RecoveryContext{CallID: "loop-mid", ToolName: "graph_query"}
	re := RecoveryEvent{
		Outcome:       observability.ToolRecoveryOutcomeSuggested,
		OriginalQuery: "x",
	}
	id, err := Emit(context.Background(), rw, rc, re)
	if err == nil {
		t.Error("expected error when mid-attribute write fails")
	}
	if id == "" {
		t.Error("expected partial incident ID when relation already landed")
	}
	if !strings.HasPrefix(id, "loop-mid:tool-recovery:graph_query:") {
		t.Errorf("partial ID prefix wrong: %q", id)
	}
	// Relation triple should still be in the writer's recorded set.
	if rw.findOne("loop-mid", observability.ToolRecoveryIncident) == nil {
		t.Error("relation triple should have landed before the mid-write failure")
	}
}

func TestEmit_RelationWriteFails_ShortCircuits(t *testing.T) {
	rw := &recordingWriter{failOn: observability.ToolRecoveryIncident}
	_, err := Emit(context.Background(), rw, RecoveryContext{CallID: "x", ToolName: "graph_query"}, RecoveryEvent{
		Outcome: observability.ToolRecoveryOutcomeSuggested,
	})
	if err == nil {
		t.Error("expected error when relation write fails")
	}
	if len(rw.triples) != 0 {
		t.Errorf("no triples should land when relation write fails; got %d", len(rw.triples))
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
