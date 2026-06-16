package planmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
)

// TestHandleQAVerdictMutation_PersistsVerdictSummary covers the needs_changes
// path. The persistence logic runs identically in all verdict branches —
// QAVerdictSummary is assigned before the verdict switch so the approved+
// merge-fail path (which has its own save) and the approved+happy path
// both pick it up. See TestHandleQAVerdictMutation_PersistsSummaryOnMergeFail
// for the merge-fail variant.
func TestHandleQAVerdictMutation_PersistsVerdictSummary(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	slug := "verdict-persist"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingQA
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	event := workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   workflow.QALevelUnit,
		Verdict: workflow.QAVerdictNeedsChanges,
		Summary: "Unit tier surfaced a coverage gap.",
		Dimensions: workflow.QAVerdictDimensions{
			RequirementFulfillment: "All four requirements implemented.",
			Coverage:               "Missing assertions for the 5xx path.",
		},
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := c.handleQAVerdictMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	stored, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing from store after mutation")
	}
	if stored.QAVerdictSummary == nil {
		t.Fatal("Plan.QAVerdictSummary was not persisted")
	}
	if stored.QAVerdictSummary.Verdict != workflow.QAVerdictNeedsChanges {
		t.Errorf("Verdict = %q, want needs_changes", stored.QAVerdictSummary.Verdict)
	}
	if stored.QAVerdictSummary.Level != workflow.QALevelUnit {
		t.Errorf("Level = %q, want unit", stored.QAVerdictSummary.Level)
	}
	if stored.QAVerdictSummary.Summary != "Unit tier surfaced a coverage gap." {
		t.Errorf("Summary = %q, want full summary", stored.QAVerdictSummary.Summary)
	}
	if stored.QAVerdictSummary.Dimensions.Coverage != "Missing assertions for the 5xx path." {
		t.Errorf("Dimensions.Coverage = %q, want full coverage text",
			stored.QAVerdictSummary.Dimensions.Coverage)
	}
	if stored.QAVerdictSummary.RecordedAt.IsZero() {
		t.Error("RecordedAt should be set by plan-manager (zero indicates field not populated)")
	}
}

// TestHandleQAVerdictMutation_PersistsSummaryOnMergeFail covers the
// approved-verdict-but-plan-level-merge-conflict path. The reviewer's
// prose narrative MUST survive this failure mode — operators triaging a
// "QA approved but stuck" state need to know WHY the reviewer approved,
// not just that a merge failed afterward. Before the QAVerdictSummary
// assignment was moved above the verdict switch, this case dropped the
// summary because the merge-fail branch has its own ps.save that ran
// before the post-switch summary write.
func TestHandleQAVerdictMutation_PersistsSummaryOnMergeFail(t *testing.T) {
	ctx := context.Background()

	stub := newStubMergeBranchesServer(t, http.StatusInternalServerError, map[string]any{
		"status": "error",
		"error":  "merge conflict in pkg/math/sub.go",
	})

	// We need a Component with both an in-memory plan store and a sandbox
	// pointing at the conflict-returning stub. setupTestComponent wires the
	// store; newTestComponentWithSandbox wires the sandbox. Compose by
	// hand here — neither helper covers both.
	ps, err := newPlanStore(ctx, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}
	c := &Component{
		name:    "plan-manager",
		logger:  slog.Default(),
		plans:   ps,
		sandbox: sandbox.NewClient(stub.server.URL),
	}

	slug := "merge-fail-persists-summary"
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: workflow.StatusReviewingQA,
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "R1"},
		},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	event := workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   workflow.QALevelUnit,
		Verdict: workflow.QAVerdictApproved,
		Summary: "Tests cover all requirements; ready for assembly.",
		Dimensions: workflow.QAVerdictDimensions{
			RequirementFulfillment: "All requirements implemented.",
			Coverage:               "Unit + integration cover the new surface.",
		},
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := c.handleQAVerdictMutation(ctx, data)
	if resp.Success {
		t.Fatal("mutation should have failed on merge conflict")
	}

	stored, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing from store after failed mutation")
	}
	if stored.QAVerdictSummary == nil {
		t.Fatal("Plan.QAVerdictSummary must survive merge-fail save path " +
			"(operators need the reviewer narrative to triage stuck plans)")
	}
	if stored.QAVerdictSummary.Verdict != workflow.QAVerdictApproved {
		t.Errorf("Verdict = %q, want approved", stored.QAVerdictSummary.Verdict)
	}
	if stored.QAVerdictSummary.Summary != "Tests cover all requirements; ready for assembly." {
		t.Errorf("Summary = %q, want full summary", stored.QAVerdictSummary.Summary)
	}
	if stored.LastError == "" {
		t.Error("LastError should record the merge failure for operator visibility")
	}
}

// TestDeterminePlanStage_ConditionallyApproved asserts a conditionally-approved
// terminal plan renders as a distinct yellow stage, never plain green
// "complete" — the user-visible half of the done-with-deferrals contract.
func TestDeterminePlanStage_ConditionallyApproved(t *testing.T) {
	c := setupTestComponent(t)

	plain := &workflow.Plan{Slug: "s", Status: workflow.StatusComplete}
	if got := c.determinePlanStage(plain); got != "complete" {
		t.Errorf("plain complete stage = %q, want complete", got)
	}

	cond := &workflow.Plan{
		Slug:             "s",
		Status:           workflow.StatusComplete,
		QAVerdictSummary: &workflow.QAVerdictSummary{Verdict: workflow.QAVerdictConditionallyApproved},
	}
	if got := c.determinePlanStage(cond); got != "complete_with_deferrals" {
		t.Errorf("conditionally-approved complete stage = %q, want complete_with_deferrals", got)
	}

	// An ordinary approval must remain plain green.
	appr := &workflow.Plan{
		Slug:             "s",
		Status:           workflow.StatusComplete,
		QAVerdictSummary: &workflow.QAVerdictSummary{Verdict: workflow.QAVerdictApproved},
	}
	if got := c.determinePlanStage(appr); got != "complete" {
		t.Errorf("approved complete stage = %q, want complete", got)
	}
}

// TestHandleQAVerdictMutation_AcceptsConditionallyApproved asserts the new
// verdict passes validation (it reaches the plan lookup rather than being
// rejected as an unknown verdict).
func TestHandleQAVerdictMutation_AcceptsConditionallyApproved(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	event := workflow.QAVerdictEvent{Slug: "missing-plan", Verdict: workflow.QAVerdictConditionallyApproved}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp := c.handleQAVerdictMutation(ctx, data)
	if resp.Success {
		t.Fatal("expected failure for a missing plan")
	}
	if resp.Error != "plan not found" {
		t.Errorf("Error = %q, want \"plan not found\" (proves conditionally_approved passed verdict validation)", resp.Error)
	}
}
