package workflowdocuments

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderRunSummary_NonTerminalReturnsEmpty(t *testing.T) {
	if got := RenderRunSummary(nil); got != "" {
		t.Errorf("RenderRunSummary(nil) = %q, want empty", got)
	}
	plan := &workflow.Plan{Slug: "wip", Status: workflow.StatusImplementing}
	if got := RenderRunSummary(plan); got != "" {
		t.Errorf("RenderRunSummary(non-terminal) = %q, want empty", got)
	}
}

func TestRenderRunSummary_CompletePlan(t *testing.T) {
	approved := time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC)
	plan := &workflow.Plan{
		Slug:       "summary-test",
		Title:      "Driver build",
		Status:     workflow.StatusComplete,
		QALevel:    workflow.QALevelIntegration,
		CreatedAt:  time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		ApprovedAt: &approved,
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "Implement driver"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "s1"}, {ID: "s2"}, {ID: "s3"},
		},
		Architecture: &workflow.ArchitectureDocument{
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "Lib X", Role: "runtime_dep"},
				{Name: "Daemon Y", Role: "integration_target",
					TestHarness: &workflow.TestHarness{Library: "testcontainers-java", Image: "y:latest", AccessMethod: "tcp:8080"}},
			},
		},
		QARun: &workflow.QARun{
			RunID:       "r1",
			Passed:      true,
			DurationMs:  30000,
			CompletedAt: time.Date(2026, 5, 16, 1, 18, 0, 0, time.UTC),
		},
		AssembledBranch:      "semspec/plan-summary-test",
		AssembledMergeCommit: "abc123def",
	}
	md := RenderRunSummary(plan)
	checks := map[string]bool{
		"# Run summary: Driver build":          true,
		"**Terminal status:** `complete`":      true,
		"**QA level:** `integration`":          true,
		"## Timeline":                          true,
		"## Plan shape":                        true,
		"Requirements: **1**":                  true,
		"Scenarios: **3**":                     true,
		"Upstream resolutions: **2**":          true,
		"**1** are integration_targets":        true,
		"## QA outcome":                        true,
		"**Executor verdict:** PASSED":         true,
		"30.0s":                                true,
		"## Phase artifacts":                   true,
		"[`plan.md`](./plan.md)":               true,
		"[`architecture.md`](./architecture.md)": true,
		"[`requirements.md`](./requirements.md)": true,
		"[`scenarios.md`](./scenarios.md)":     true,
		"[`qa-summary.md`](./qa-summary.md)":   true,
		"## Assembled output":                  true,
		"`semspec/plan-summary-test`":          true,
		"`abc123def`":                          true,
	}
	for needle, want := range checks {
		got := strings.Contains(md, needle)
		if got != want {
			t.Errorf("contains(%q) = %v, want %v", needle, got, want)
		}
	}
}

func TestRenderRunSummary_TerminalStatuses(t *testing.T) {
	cases := []workflow.Status{
		workflow.StatusComplete,
		workflow.StatusAwaitingReview,
		workflow.StatusRejected,
	}
	for _, s := range cases {
		plan := &workflow.Plan{Slug: "t", Status: s}
		if got := RenderRunSummary(plan); got == "" {
			t.Errorf("status %q should be terminal and produce output", s)
		}
	}
}
