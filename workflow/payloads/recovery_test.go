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

// TestRecoveryCompleteValidate pins the closed action set + per-action
// required fields. The recovery agent's submit_work output gets validated
// here before mutation; an invalid action or missing required field
// surfaces at the parse boundary, not deep in the reconciliation path.
func TestRecoveryCompleteValidate(t *testing.T) {
	base := func() *RecoveryComplete {
		return &RecoveryComplete{
			RecoveryID:          "rec-123",
			Layer:               RecoveryLayerPhaseLocal,
			Slug:                "my-plan",
			Action:              RecoveryActionEscalateHuman,
			Diagnosis:           "Wedged agent kept trying to patch with sed; correct path is heredoc rewrite",
			RecoverySucceeded:   false,
			RecoveryAgentLoopID: "rec-loop-xyz",
		}
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("base happy-path (escalate_human) failed validate: %v", err)
	}

	t.Run("refine_prompt requires refined_prompt", func(t *testing.T) {
		r := base()
		r.Action = RecoveryActionRefinePrompt
		r.RefinedPrompt = ""
		err := r.Validate()
		if err == nil || !strings.Contains(err.Error(), "refined_prompt") {
			t.Errorf("expected refined_prompt-required error, got %v", err)
		}
		r.RefinedPrompt = "Use a heredoc instead of sed"
		if err := r.Validate(); err != nil {
			t.Errorf("refine_prompt with refined_prompt populated should validate: %v", err)
		}
	})

	t.Run("invalid action rejected", func(t *testing.T) {
		r := base()
		r.Action = "bump_model" // explicitly out of the closed set per ADR-037
		err := r.Validate()
		if err == nil {
			t.Fatal("expected error for invalid action, got nil")
		}
		if !strings.Contains(err.Error(), "action must be one of") {
			t.Errorf("expected closed-set error, got %v", err)
		}
	})

	t.Run("each closed-set action is accepted", func(t *testing.T) {
		actions := []RecoveryActionKind{
			RecoveryActionRefinePrompt,
			RecoveryActionNarrowScope,
			RecoveryActionSplitReq,
			RecoveryActionEscalateHuman,
			RecoveryActionMarkUnrecoverable,
		}
		for _, a := range actions {
			r := base()
			r.Action = a
			if a == RecoveryActionRefinePrompt {
				r.RefinedPrompt = "rewritten task prompt"
			}
			if err := r.Validate(); err != nil {
				t.Errorf("action %q should validate: %v", a, err)
			}
		}
	})

	t.Run("diagnosis required for every action", func(t *testing.T) {
		// Per ADR-037: diagnosis is the deliverable for escalate_human and
		// mark_unrecoverable too — those aren't "no analysis" outcomes,
		// they're "analysis says no programmatic action fits."
		for _, a := range []RecoveryActionKind{
			RecoveryActionEscalateHuman,
			RecoveryActionMarkUnrecoverable,
			RecoveryActionRefinePrompt,
		} {
			r := base()
			r.Action = a
			r.Diagnosis = ""
			if a == RecoveryActionRefinePrompt {
				r.RefinedPrompt = "rewritten"
			}
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), "diagnosis") {
				t.Errorf("action %q without diagnosis should fail validate, got %v", a, err)
			}
		}
	})

	t.Run("recovery_agent_loop_id required", func(t *testing.T) {
		r := base()
		r.RecoveryAgentLoopID = ""
		err := r.Validate()
		if err == nil || !strings.Contains(err.Error(), "recovery_agent_loop_id") {
			t.Errorf("missing recovery_agent_loop_id should fail validate, got %v", err)
		}
	})
}

// TestRecoverySubjectPrefixes pins the wire convention. Subject prefixes
// are load-bearing across producer + consumer; renaming requires a
// coordinated change. This test locks the strings at PR time.
func TestRecoverySubjectPrefixes(t *testing.T) {
	if RecoveryRequestedSubjectPrefix != "recovery.requested." {
		t.Errorf("RecoveryRequestedSubjectPrefix changed; producers + consumers must update together")
	}
	if RecoveryCompleteSubjectPrefix != "recovery.complete." {
		t.Errorf("RecoveryCompleteSubjectPrefix changed; producers + consumers must update together")
	}
	if RecoveryStatesBucket != "RECOVERY_STATES" {
		t.Errorf("RecoveryStatesBucket changed; KV reconciliation paths must update together")
	}
}
