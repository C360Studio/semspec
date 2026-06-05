package workflowdocuments

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderQASummary_NoQARunNoDecisionsReturnsEmpty(t *testing.T) {
	if got := RenderQASummary(nil); got != "" {
		t.Errorf("RenderQASummary(nil) = %q, want empty", got)
	}
	plan := &workflow.Plan{Slug: "no-qa"}
	if got := RenderQASummary(plan); got != "" {
		t.Errorf("RenderQASummary(plan without QARun or decisions) = %q, want empty", got)
	}
}

func TestRenderQASummary_PassedRun(t *testing.T) {
	plan := &workflow.Plan{
		Slug:    "qa-pass",
		Title:   "QA test",
		QALevel: workflow.QALevelUnit,
		QARun: &workflow.QARun{
			RunID:       "run-123",
			Passed:      true,
			DurationMs:  47500,
			CompletedAt: time.Date(2026, 5, 16, 1, 30, 0, 0, time.UTC),
			Artifacts: []workflow.QAArtifactRef{
				{Path: "build/reports/tests/test", Type: "report", Purpose: "JUnit"},
			},
		},
	}
	md := RenderQASummary(plan)
	checks := map[string]bool{
		"# QA Summary: QA test": true,
		"`unit`":                true,
		"## Executor result":    true,
		"PASSED":                true,
		"`run-123`":             true,
		"47.5s":                 true,
		"### Artifacts (1)":     true,
		"build/reports":         true,
	}
	for needle, want := range checks {
		got := strings.Contains(md, needle)
		if got != want {
			t.Errorf("contains(%q) = %v, want %v", needle, got, want)
		}
	}
}

func TestRenderQASummary_FailedRunWithFailures(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "qa-fail",
		QARun: &workflow.QARun{
			RunID:  "r1",
			Passed: false,
			Failures: []workflow.QAFailure{
				{JobName: "integration", Message: "assertion failed", LogExcerpt: "expected 200, got 404"},
			},
		},
	}
	md := RenderQASummary(plan)
	if !strings.Contains(md, "FAILED") {
		t.Error("should mark verdict FAILED")
	}
	if !strings.Contains(md, "### Failures (1)") {
		t.Error("should render failures section")
	}
	if !strings.Contains(md, "expected 200, got 404") {
		t.Error("should include log excerpt")
	}
}

func TestRenderQASummary_WithPlanDecisions(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "decisions",
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "dec-1",
				Title:          "Add error-path test",
				Rationale:      "Coverage gap on 5xx responses",
				Status:         workflow.PlanDecisionStatusProposed,
				ProposedBy:     "qa-reviewer",
				AffectedReqIDs: []string{"req-1"},
			},
		},
	}
	md := RenderQASummary(plan)
	if !strings.Contains(md, "## Plan decisions raised (1)") {
		t.Error("should render plan-decisions section")
	}
	if !strings.Contains(md, "### Add error-path test") {
		t.Error("should render decision title")
	}
	if !strings.Contains(md, "**Rationale:** Coverage gap on 5xx responses") {
		t.Error("should render rationale")
	}
}

func TestRenderQASummary_WithVerdictSummary(t *testing.T) {
	plan := &workflow.Plan{
		Slug:    "with-verdict",
		Title:   "Verdict carrier",
		QALevel: workflow.QALevelUnit,
		QAVerdictSummary: &workflow.QAVerdictSummary{
			Verdict:    workflow.QAVerdictApproved,
			Level:      workflow.QALevelUnit,
			Summary:    "Plan satisfies the change request and tests cover the new surface.",
			RecordedAt: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
			Dimensions: workflow.QAVerdictDimensions{
				RequirementFulfillment: "All four requirements implemented.",
				Coverage:               "Unit + integration cover the new surface.",
				AssertionQuality:       "Assertions check semantic outcomes.",
				RegressionSurface:      "No untouched code paths broken.",
				// FlakeJudgment intentionally empty.
			},
		},
	}
	md := RenderQASummary(plan)
	checks := []struct {
		needle string
		want   bool
	}{
		{"## Reviewer verdict", true},
		{"**Verdict:** `approved`", true},
		{"**Level assessed:** `unit`", true},
		{"2026-05-16T12:00:00Z", true},
		{"Plan satisfies the change request", true},
		{"### Dimensions", true},
		{"**Requirement fulfillment.**", true},
		{"All four requirements implemented.", true},
		{"**Coverage.**", true},
		{"**Assertion quality.**", true},
		{"**Regression surface.**", true},
		{"**Flake judgment.**", false}, // empty body → header suppressed
	}
	for _, c := range checks {
		got := strings.Contains(md, c.needle)
		if got != c.want {
			t.Errorf("contains(%q) = %v, want %v\ngot markdown:\n%s", c.needle, got, c.want, md)
		}
	}
}

func TestRenderQASummary_VerdictSummaryOnlyNoExecutor(t *testing.T) {
	// Plan with only a verdict — no executor run, no plan-decisions.
	// Represents the synthesis-level path where qa-reviewer produces a
	// verdict directly without an executor. Renderer must still produce
	// output (not return "" as it would have pre-Plan.QAVerdictSummary).
	plan := &workflow.Plan{
		Slug:    "verdict-only",
		QALevel: workflow.QALevelSynthesis,
		QAVerdictSummary: &workflow.QAVerdictSummary{
			Verdict: workflow.QAVerdictApproved,
			Level:   workflow.QALevelSynthesis,
			Summary: "Synthesis review: change is coherent.",
			Dimensions: workflow.QAVerdictDimensions{
				RequirementFulfillment: "Change matches the requested behavior.",
			},
		},
	}
	md := RenderQASummary(plan)
	if md == "" {
		t.Fatal("expected non-empty render with QAVerdictSummary only")
	}
	if !strings.Contains(md, "## Reviewer verdict") {
		t.Error("should render reviewer verdict section")
	}
	if !strings.Contains(md, "Synthesis review: change is coherent.") {
		t.Error("should include verdict summary text")
	}
	if !strings.Contains(md, "### Dimensions") {
		t.Error("dimensions header should appear when at least one dimension body is set")
	}
}

func TestRenderQASummary_VerdictSummaryAllDimensionsEmpty(t *testing.T) {
	// Confirms the dimensions header is suppressed when no dimension body
	// is set (avoids empty "### Dimensions" headings).
	plan := &workflow.Plan{
		Slug: "empty-dims",
		QAVerdictSummary: &workflow.QAVerdictSummary{
			Verdict: workflow.QAVerdictApproved,
			Level:   workflow.QALevelSynthesis,
			Summary: "no dimensions assessed",
		},
	}
	md := RenderQASummary(plan)
	if strings.Contains(md, "### Dimensions") {
		t.Errorf("expected no Dimensions header when all dimension bodies empty, got:\n%s", md)
	}
}

func TestRenderQASummary_SynthesisLevelNoExecutor(t *testing.T) {
	plan := &workflow.Plan{
		Slug:    "synth",
		QALevel: workflow.QALevelSynthesis,
		PlanDecisions: []workflow.PlanDecision{
			{ID: "d1", Title: "Synthesis decision"},
		},
	}
	md := RenderQASummary(plan)
	if !strings.Contains(md, "No executor run on this plan") {
		t.Errorf("synthesis level should note absent executor. got:\n%s", md)
	}
}
