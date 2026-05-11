package payloads

import (
	"strings"
	"testing"
)

// TestRecoveryRequestedValidate pins the required-field contract for
// RecoveryRequested. The recovery dispatch chain depends on every field
// being present at publish time — missing fields would manifest as
// recovery agents trying to fetch trajectories that don't exist or
// reconcile against KV records they can't find.
func TestRecoveryRequestedValidate(t *testing.T) {
	base := func() *RecoveryRequested {
		return &RecoveryRequested{
			RecoveryID:       "rec-123",
			Layer:            RecoveryLayerPhaseLocal,
			Slug:             "my-plan",
			LoopID:           "loop-abc",
			EscalationReason: "fixable rejections exceeded TDD cycle budget",
		}
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("base happy-path failed validate: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*RecoveryRequested)
		wantErr string
	}{
		{"missing recovery_id", func(r *RecoveryRequested) { r.RecoveryID = "" }, "recovery_id"},
		{"missing layer", func(r *RecoveryRequested) { r.Layer = "" }, "layer"},
		{"invalid layer", func(r *RecoveryRequested) { r.Layer = "wrong" }, "phase_local or coordinator"},
		{"missing slug", func(r *RecoveryRequested) { r.Slug = "" }, "slug"},
		{"missing escalation_reason", func(r *RecoveryRequested) { r.EscalationReason = "" }, "escalation_reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.mutate(r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}

	// Plan-phase wedges may not have a canonical wedged loop in scope yet
	// (e.g. round-2 revision exhaustion where the wedged work was spread
	// across multiple generators). Validate must accept LoopID="" so those
	// paths can publish RecoveryRequested; the recovery agent falls back to
	// feedback + findings when no trajectory is fetchable.
	t.Run("missing loop_id is allowed", func(t *testing.T) {
		r := base()
		r.LoopID = ""
		if err := r.Validate(); err != nil {
			t.Errorf("LoopID is optional; validate should pass, got %v", err)
		}
	})
}

// TestRecoveryActionKindClosedSet pins the action constants. Recovery
// agent output is parsed against this set in
// processor/recovery-agent/result.go; renaming or removing a constant
// requires a coordinated change with that parser + the prompt fragment
// that lists the actions.
func TestRecoveryActionKindClosedSet(t *testing.T) {
	want := []RecoveryActionKind{
		RecoveryActionRefinePrompt,
		RecoveryActionNarrowScope,
		RecoveryActionSplitReq,
		RecoveryActionEscalateHuman,
		RecoveryActionMarkUnrecoverable,
	}
	got := map[RecoveryActionKind]string{
		"refine_prompt":      "RecoveryActionRefinePrompt",
		"narrow_scope":       "RecoveryActionNarrowScope",
		"split_req":          "RecoveryActionSplitReq",
		"escalate_human":     "RecoveryActionEscalateHuman",
		"mark_unrecoverable": "RecoveryActionMarkUnrecoverable",
	}
	if len(want) != len(got) {
		t.Fatalf("closed action set size changed: const list has %d, expected map has %d", len(want), len(got))
	}
	for _, a := range want {
		if _, ok := got[a]; !ok {
			t.Errorf("RecoveryActionKind %q not in expected wire-string map (renamed const?)", a)
		}
	}
}

// TestRecoverySubjectPrefix pins the wire convention. The subject prefix
// is load-bearing across publishers (plan-manager, execution-manager,
// requirement-executor) + the recovery-agent consumer.
func TestRecoverySubjectPrefix(t *testing.T) {
	if RecoveryRequestedSubjectPrefix != "recovery.requested." {
		t.Errorf("RecoveryRequestedSubjectPrefix changed; producers + consumers must update together")
	}
}

// _ keeps the strings import live across the file (used by other tests).
var _ = strings.Contains
