package planmanager

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// TestApplyRecoveryHint pins the (a3) apply logic: when a recovery
// PlanDecision is accepted, its rationale lands on each affected
// requirement's RecoveryHint field. Execution-manager reads this at
// next-cycle developer dispatch (see lookupRecoveryHint).
func TestApplyRecoveryHint(t *testing.T) {
	mkPlan := func() *workflow.Plan {
		now := time.Now().Add(-1 * time.Hour) // stale UpdatedAt to detect the write
		return &workflow.Plan{
			Slug: "test-plan",
			Requirements: []workflow.Requirement{
				{ID: "req.test-plan.1", Title: "first", UpdatedAt: now},
				{ID: "req.test-plan.2", Title: "second", UpdatedAt: now},
				{ID: "req.test-plan.3", Title: "third", UpdatedAt: now},
			},
		}
	}

	t.Run("writes hint to single affected req", func(t *testing.T) {
		plan := mkPlan()
		decision := &workflow.PlanDecision{
			Rationale:      "Diagnosis: agent was thrashing on bash quoting. Use heredoc, not sed.",
			AffectedReqIDs: []string{"req.test-plan.2"},
		}
		applyRecoveryHint(plan, decision)

		// Targeted req should have the hint.
		if plan.Requirements[1].RecoveryHint != decision.Rationale {
			t.Errorf("affected req's RecoveryHint = %q, want %q",
				plan.Requirements[1].RecoveryHint, decision.Rationale)
		}
		// Untargeted reqs must NOT receive the hint — cross-req leakage
		// would cause the dev to see another req's recovery context on
		// retry, which is misleading and a real Goodhart risk for the
		// hint mechanism.
		if plan.Requirements[0].RecoveryHint != "" {
			t.Errorf("untargeted req 1 got hint: %q (cross-req leakage)", plan.Requirements[0].RecoveryHint)
		}
		if plan.Requirements[2].RecoveryHint != "" {
			t.Errorf("untargeted req 3 got hint: %q (cross-req leakage)", plan.Requirements[2].RecoveryHint)
		}
	})

	t.Run("writes hint to multiple affected reqs", func(t *testing.T) {
		plan := mkPlan()
		decision := &workflow.PlanDecision{
			Rationale:      "Split this work across two reqs to narrow scope.",
			AffectedReqIDs: []string{"req.test-plan.1", "req.test-plan.3"},
		}
		applyRecoveryHint(plan, decision)
		if plan.Requirements[0].RecoveryHint == "" {
			t.Error("req 1 should have hint set")
		}
		if plan.Requirements[1].RecoveryHint != "" {
			t.Errorf("req 2 should NOT have hint; got %q", plan.Requirements[1].RecoveryHint)
		}
		if plan.Requirements[2].RecoveryHint == "" {
			t.Error("req 3 should have hint set")
		}
	})

	t.Run("bumps UpdatedAt on affected req", func(t *testing.T) {
		plan := mkPlan()
		oldUpdate := plan.Requirements[0].UpdatedAt
		decision := &workflow.PlanDecision{
			Rationale:      "hint",
			AffectedReqIDs: []string{"req.test-plan.1"},
		}
		applyRecoveryHint(plan, decision)
		if !plan.Requirements[0].UpdatedAt.After(oldUpdate) {
			t.Error("UpdatedAt was not bumped after applying recovery hint")
		}
		// Untouched reqs keep their old UpdatedAt.
		if !plan.Requirements[1].UpdatedAt.Equal(oldUpdate) {
			t.Error("untargeted req's UpdatedAt was bumped (should be unchanged)")
		}
	})

	t.Run("nil proposal is a no-op", func(t *testing.T) {
		plan := mkPlan()
		applyRecoveryHint(plan, nil)
		for i, r := range plan.Requirements {
			if r.RecoveryHint != "" {
				t.Errorf("req %d got hint from nil proposal: %q", i, r.RecoveryHint)
			}
		}
	})

	t.Run("empty rationale is a no-op", func(t *testing.T) {
		plan := mkPlan()
		decision := &workflow.PlanDecision{
			Rationale:      "",
			AffectedReqIDs: []string{"req.test-plan.1"},
		}
		applyRecoveryHint(plan, decision)
		if plan.Requirements[0].RecoveryHint != "" {
			t.Errorf("empty rationale wrote hint: %q", plan.Requirements[0].RecoveryHint)
		}
	})

	t.Run("unknown affected req is silently skipped", func(t *testing.T) {
		plan := mkPlan()
		decision := &workflow.PlanDecision{
			Rationale:      "hint for missing req",
			AffectedReqIDs: []string{"req.does-not-exist"},
		}
		// Should not panic, should not affect any existing req.
		applyRecoveryHint(plan, decision)
		for i, r := range plan.Requirements {
			if r.RecoveryHint != "" {
				t.Errorf("req %d got hint despite unknown affected ID: %q", i, r.RecoveryHint)
			}
		}
	})

	t.Run("repeated accepts overwrite hint idempotently", func(t *testing.T) {
		plan := mkPlan()
		decision := &workflow.PlanDecision{
			Rationale:      "first hint",
			AffectedReqIDs: []string{"req.test-plan.2"},
		}
		applyRecoveryHint(plan, decision)
		if plan.Requirements[1].RecoveryHint != "first hint" {
			t.Fatalf("first apply did not set hint")
		}

		decision.Rationale = "second hint"
		applyRecoveryHint(plan, decision)
		if plan.Requirements[1].RecoveryHint != "second hint" {
			t.Errorf("second apply did not overwrite: got %q, want %q",
				plan.Requirements[1].RecoveryHint, "second hint")
		}
		// Must not contain the first hint anymore — stale hint accumulation
		// would cause the dev to see contradictory recovery guidance on
		// successive retries.
		if strings.Contains(plan.Requirements[1].RecoveryHint, "first hint") {
			t.Error("stale hint leaked across overwrite")
		}
	})
}
