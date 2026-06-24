package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestMergeReviewFindings covers the pure accumulation helper: findings carry
// forward across rounds (so the regenerating agent sees the cumulative
// constraint set), cross-phase residue is dropped, evidence drift dedups, and an
// unparseable incoming payload preserves the accumulated set.
const roundArch = 3 // architecture review round

func TestMergeReviewFindings(t *testing.T) {
	mk := func(t *testing.T, fs ...workflow.PlanReviewFinding) json.RawMessage {
		t.Helper()
		b, err := json.Marshal(fs)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}
	a := workflow.PlanReviewFinding{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "scoped_include_unowned", TargetField: "scope.include", TargetValue: "README.md"}
	b := workflow.PlanReviewFinding{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "component_placement", TargetID: "action-controlstreams"}

	parse := func(t *testing.T, raw json.RawMessage) []workflow.PlanReviewFinding {
		t.Helper()
		var out []workflow.PlanReviewFinding
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal merged: %v", err)
		}
		return out
	}

	t.Run("empty existing returns incoming", func(t *testing.T) {
		got := parse(t, mergeReviewFindings(nil, mk(t, a), roundArch))
		if len(got) != 1 || got[0] != a {
			t.Fatalf("got %+v, want [a]", got)
		}
	})

	t.Run("distinct rounds accumulate (union, order preserved)", func(t *testing.T) {
		got := parse(t, mergeReviewFindings(mk(t, a), mk(t, b), roundArch))
		if len(got) != 2 || got[0] != a || got[1] != b {
			t.Fatalf("got %+v, want [a, b]", got)
		}
	})

	t.Run("exact duplicate across rounds collapses", func(t *testing.T) {
		got := parse(t, mergeReviewFindings(mk(t, a), mk(t, a, b), roundArch))
		if len(got) != 2 || got[0] != a || got[1] != b {
			t.Fatalf("got %+v, want [a, b] (a deduped)", got)
		}
	})

	t.Run("free-text drift (evidence/suggestion/issue) dedups to one", func(t *testing.T) {
		aDrift := a
		aDrift.Evidence = "a different verbatim quote"
		aDrift.Suggestion = "reworded suggestion"
		aDrift.Issue = "the same violation, paraphrased"
		got := parse(t, mergeReviewFindings(mk(t, a), mk(t, aDrift), roundArch))
		if len(got) != 1 {
			t.Fatalf("got %d findings, want 1 (drift on volatile free-text must not survive twice): %+v", len(got), got)
		}
	})

	t.Run("cross-phase residue is dropped (the R1->R-req bleed)", func(t *testing.T) {
		planFinding := workflow.PlanReviewFinding{Severity: "error", Status: "violation", Phase: "plan", SOPID: "goal_too_vague"}
		// existing carries a leaked draft-phase finding; the current round is
		// architecture (3). The bled plan finding must NOT accumulate.
		got := parse(t, mergeReviewFindings(mk(t, planFinding), mk(t, a), roundArch))
		if len(got) != 1 || got[0] != a {
			t.Fatalf("got %+v, want only the architecture finding (plan residue dropped)", got)
		}
	})

	t.Run("unparseable incoming preserves accumulated set", func(t *testing.T) {
		bad := json.RawMessage(`{not-an-array`)
		got := mergeReviewFindings(mk(t, a), bad, roundArch)
		if string(got) != string(mk(t, a)) {
			t.Fatalf("got %q, want the preserved existing set (not the corrupt payload)", string(got))
		}
	})
}

// TestHandleRevisionMutation_AccumulatesAcrossRounds is the regression for the
// R-arch oscillation: two needs_changes rounds in the same review phase must
// leave BOTH rounds' findings on the plan, so the architect revises against the
// full constraint set instead of only the latest round's.
func TestHandleRevisionMutation_AccumulatesAcrossRounds(t *testing.T) {
	ctx := context.Background()
	c := setupRevisionComponent(t, 5) // cap high so neither round escalates
	plan := setupTestPlan(t, c, "arch-accum")
	plan.Status = workflow.StatusReviewingArchitecture
	plan.ReviewIteration = 0
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	round1 := workflow.PlanReviewFinding{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "scoped_include_unowned", TargetValue: "build.gradle"}
	resp := c.handleRevisionMutation(ctx, marshalRevision(t, RevisionMutationRequest{
		Slug: "arch-accum", Round: 3, Verdict: "needs_changes", Summary: "ownership",
		Findings: makeFindings(t, []workflow.PlanReviewFinding{round1}),
	}))
	if !resp.Success {
		t.Fatalf("round 1 revision failed: %s", resp.Error)
	}

	// Architect regenerates; the reviewer re-reviews → plan back in R-arch.
	got, _ := c.plans.get("arch-accum")
	got.Status = workflow.StatusReviewingArchitecture
	if err := c.plans.save(ctx, got); err != nil {
		t.Fatalf("save: %v", err)
	}

	round2 := workflow.PlanReviewFinding{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "component_placement", TargetID: "action-controlstreams"}
	resp = c.handleRevisionMutation(ctx, marshalRevision(t, RevisionMutationRequest{
		Slug: "arch-accum", Round: 3, Verdict: "needs_changes", Summary: "placement",
		Findings: makeFindings(t, []workflow.PlanReviewFinding{round2}),
	}))
	if !resp.Success {
		t.Fatalf("round 2 revision failed: %s", resp.Error)
	}

	got, _ = c.plans.get("arch-accum")
	var findings []workflow.PlanReviewFinding
	if err := json.Unmarshal(got.ReviewFindings, &findings); err != nil {
		t.Fatalf("unmarshal accumulated findings: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("accumulated findings = %d, want 2 (round 1 + round 2)", len(findings))
	}
	haveR1, haveR2 := false, false
	for _, f := range findings {
		if f == round1 {
			haveR1 = true
		}
		if f == round2 {
			haveR2 = true
		}
	}
	if !haveR1 || !haveR2 {
		t.Fatalf("expected both rounds' findings; haveR1=%v haveR2=%v (%+v)", haveR1, haveR2, findings)
	}
	// The cumulative set must also reach the architect via the formatted text.
	if got.ReviewFormattedFindings == "" {
		t.Fatal("ReviewFormattedFindings empty — architect would see no feedback")
	}
}
