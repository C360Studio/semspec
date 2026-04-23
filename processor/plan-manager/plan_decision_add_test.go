package planmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func newPlanDecisionTestComponent(t *testing.T) *Component {
	t.Helper()
	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}
	return &Component{
		logger: slog.Default(),
		plans:  ps,
	}
}

func TestHandlePlanDecisionAddMutation_AppendsToPlan(t *testing.T) {
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{ID: workflow.PlanEntityID(slug), Slug: slug, Title: slug}
	_ = c.plans.save(context.Background(), plan)

	body, _ := json.Marshal(planDecisionAddRequest{
		Slug: slug,
		Decision: workflow.PlanDecision{
			ID:             "plan-decision.demo.exhaust.r1.1",
			PlanID:         workflow.PlanEntityID(slug),
			Kind:           workflow.PlanDecisionKindExecutionExhausted,
			Title:          "R1 exhausted",
			Rationale:      "retries=3/3",
			AffectedReqIDs: []string{"R1"},
			ProposedBy:     "requirement-executor",
		},
	})
	resp := c.handlePlanDecisionAddMutation(context.Background(), body)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got, _ := c.plans.get(slug)
	if len(got.PlanDecisions) != 1 {
		t.Fatalf("len(PlanDecisions) = %d, want 1", len(got.PlanDecisions))
	}
	d := got.PlanDecisions[0]
	if d.Kind != workflow.PlanDecisionKindExecutionExhausted {
		t.Errorf("kind = %q, want execution_exhausted", d.Kind)
	}
	if d.Status != workflow.PlanDecisionStatusProposed {
		t.Errorf("status = %q, want proposed (default)", d.Status)
	}
	if d.CreatedAt.IsZero() {
		t.Error("CreatedAt should default to now when zero")
	}
}

func TestHandlePlanDecisionAddMutation_SupersedesOpenExhaustionOnSameReq(t *testing.T) {
	// Two retry cycles on R1 each exhaust → executor emits a decision each
	// time. The newer emission should archive the earlier open record, not
	// stack parallel decisions for the same stuck requirement.
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID(slug),
		Slug:  slug,
		Title: slug,
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "plan-decision.demo.exhaust.r1.old",
				PlanID:         workflow.PlanEntityID(slug),
				Kind:           workflow.PlanDecisionKindExecutionExhausted,
				Status:         workflow.PlanDecisionStatusProposed,
				AffectedReqIDs: []string{"R1"},
			},
		},
	}
	_ = c.plans.save(context.Background(), plan)

	body, _ := json.Marshal(planDecisionAddRequest{
		Slug: slug,
		Decision: workflow.PlanDecision{
			ID:             "plan-decision.demo.exhaust.r1.new",
			PlanID:         workflow.PlanEntityID(slug),
			Kind:           workflow.PlanDecisionKindExecutionExhausted,
			AffectedReqIDs: []string{"R1"},
		},
	})
	resp := c.handlePlanDecisionAddMutation(context.Background(), body)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got, _ := c.plans.get(slug)
	var old, updated workflow.PlanDecision
	for _, d := range got.PlanDecisions {
		if d.ID == "plan-decision.demo.exhaust.r1.old" {
			old = d
		}
		if d.ID == "plan-decision.demo.exhaust.r1.new" {
			updated = d
		}
	}
	if old.Status != workflow.PlanDecisionStatusArchived {
		t.Errorf("old.Status = %q, want archived (superseded)", old.Status)
	}
	if old.DecidedAt == nil {
		t.Error("old.DecidedAt should be set on supersession")
	}
	if updated.Status != workflow.PlanDecisionStatusProposed {
		t.Errorf("new.Status = %q, want proposed", updated.Status)
	}
}

func TestHandlePlanDecisionAddMutation_DoesNotSupersedeOtherRequirements(t *testing.T) {
	// Open exhaustion on R1 must NOT be archived when a new exhaustion lands
	// on R2 — they're independent stuck points.
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID(slug),
		Slug:  slug,
		Title: slug,
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "plan-decision.demo.exhaust.r1.1",
				PlanID:         workflow.PlanEntityID(slug),
				Kind:           workflow.PlanDecisionKindExecutionExhausted,
				Status:         workflow.PlanDecisionStatusProposed,
				AffectedReqIDs: []string{"R1"},
			},
		},
	}
	_ = c.plans.save(context.Background(), plan)

	body, _ := json.Marshal(planDecisionAddRequest{
		Slug: slug,
		Decision: workflow.PlanDecision{
			ID:             "plan-decision.demo.exhaust.r2.1",
			PlanID:         workflow.PlanEntityID(slug),
			Kind:           workflow.PlanDecisionKindExecutionExhausted,
			AffectedReqIDs: []string{"R2"},
		},
	})
	resp := c.handlePlanDecisionAddMutation(context.Background(), body)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got, _ := c.plans.get(slug)
	for _, d := range got.PlanDecisions {
		if d.ID == "plan-decision.demo.exhaust.r1.1" && d.Status != workflow.PlanDecisionStatusProposed {
			t.Errorf("R1 decision status = %q, want proposed (should not be superseded by R2)", d.Status)
		}
	}
}

func TestHandlePlanDecisionAddMutation_RequirementChangeKindDoesNotSupersede(t *testing.T) {
	// Only ExecutionExhausted decisions supersede. A requirement_change
	// decision (qa-reviewer needs_changes) should never archive anything.
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID(slug),
		Slug:  slug,
		Title: slug,
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "plan-decision.demo.exhaust.r1.1",
				PlanID:         workflow.PlanEntityID(slug),
				Kind:           workflow.PlanDecisionKindExecutionExhausted,
				Status:         workflow.PlanDecisionStatusProposed,
				AffectedReqIDs: []string{"R1"},
			},
		},
	}
	_ = c.plans.save(context.Background(), plan)

	body, _ := json.Marshal(planDecisionAddRequest{
		Slug: slug,
		Decision: workflow.PlanDecision{
			ID:             "plan-decision.demo.qa.xyz",
			PlanID:         workflow.PlanEntityID(slug),
			Kind:           workflow.PlanDecisionKindRequirementChange,
			AffectedReqIDs: []string{"R1"},
		},
	})
	resp := c.handlePlanDecisionAddMutation(context.Background(), body)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got, _ := c.plans.get(slug)
	for _, d := range got.PlanDecisions {
		if d.ID == "plan-decision.demo.exhaust.r1.1" && d.Status != workflow.PlanDecisionStatusProposed {
			t.Errorf("exhaustion decision on R1 was archived by a requirement_change add; should not have been")
		}
	}
}

func TestHandlePlanDecisionAddMutation_ValidationErrors(t *testing.T) {
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{ID: workflow.PlanEntityID(slug), Slug: slug, Title: slug}
	_ = c.plans.save(context.Background(), plan)

	cases := []struct {
		name string
		body []byte
		want string
	}{
		{
			name: "missing slug",
			body: mustJSON(planDecisionAddRequest{
				Decision: workflow.PlanDecision{ID: "x", PlanID: "y"},
			}),
			want: "slug is required",
		},
		{
			name: "missing id",
			body: mustJSON(planDecisionAddRequest{
				Slug:     slug,
				Decision: workflow.PlanDecision{PlanID: "y"},
			}),
			want: "decision.id is required",
		},
		{
			name: "missing plan_id",
			body: mustJSON(planDecisionAddRequest{
				Slug:     slug,
				Decision: workflow.PlanDecision{ID: "x"},
			}),
			want: "decision.plan_id is required",
		},
		{
			name: "invalid kind",
			body: mustJSON(planDecisionAddRequest{
				Slug: slug,
				Decision: workflow.PlanDecision{
					ID:     "x",
					PlanID: "y",
					Kind:   "bogus",
				},
			}),
			want: "invalid decision kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := c.handlePlanDecisionAddMutation(context.Background(), tc.body)
			if resp.Success {
				t.Fatalf("expected failure for %q", tc.name)
			}
			if tc.want != "" && !contains(resp.Error, tc.want) {
				t.Errorf("error = %q, want substring %q", resp.Error, tc.want)
			}
		})
	}
}

func TestAutoArchiveExhaustionDecisions_ArchivesOpenOnMatch(t *testing.T) {
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID(slug),
		Slug:  slug,
		Title: slug,
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "plan-decision.demo.exhaust.r1",
				Kind:           workflow.PlanDecisionKindExecutionExhausted,
				Status:         workflow.PlanDecisionStatusProposed,
				AffectedReqIDs: []string{"R1"},
			},
			{
				ID:             "plan-decision.demo.exhaust.r2",
				Kind:           workflow.PlanDecisionKindExecutionExhausted,
				Status:         workflow.PlanDecisionStatusProposed,
				AffectedReqIDs: []string{"R2"},
			},
		},
	}
	_ = c.plans.save(context.Background(), plan)

	// R1 resolved — only the R1 decision should archive.
	c.autoArchiveExhaustionDecisions(context.Background(), slug, "R1", "requirement completed after exhaustion")

	got, _ := c.plans.get(slug)
	var r1, r2 workflow.PlanDecision
	for _, d := range got.PlanDecisions {
		if d.ID == "plan-decision.demo.exhaust.r1" {
			r1 = d
		}
		if d.ID == "plan-decision.demo.exhaust.r2" {
			r2 = d
		}
	}
	if r1.Status != workflow.PlanDecisionStatusArchived {
		t.Errorf("R1.Status = %q, want archived", r1.Status)
	}
	if r1.DecidedAt == nil {
		t.Error("R1.DecidedAt should be set after auto-archive")
	}
	if r1.RejectionReasons["auto_archive"] == "" {
		t.Error("R1 should carry auto_archive reason for audit")
	}
	if r2.Status != workflow.PlanDecisionStatusProposed {
		t.Errorf("R2.Status = %q, want proposed (R2 still stuck)", r2.Status)
	}
}

func TestAutoArchiveExhaustionDecisions_SkipsRequirementChangeKind(t *testing.T) {
	// A QA requirement_change decision on R1 should NOT be auto-archived
	// when R1 resolves — that kind has its own accept/reject lifecycle and
	// the auto-archive is scoped to exhaustion only.
	c := newPlanDecisionTestComponent(t)
	slug := "demo"
	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID(slug),
		Slug:  slug,
		Title: slug,
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "plan-decision.demo.qa.r1",
				Kind:           workflow.PlanDecisionKindRequirementChange,
				Status:         workflow.PlanDecisionStatusProposed,
				AffectedReqIDs: []string{"R1"},
			},
		},
	}
	_ = c.plans.save(context.Background(), plan)

	c.autoArchiveExhaustionDecisions(context.Background(), slug, "R1", "test")

	got, _ := c.plans.get(slug)
	if got.PlanDecisions[0].Status != workflow.PlanDecisionStatusProposed {
		t.Errorf("QA decision was archived by exhaustion auto-archive; should not happen")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
