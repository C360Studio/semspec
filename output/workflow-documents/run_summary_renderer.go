package workflowdocuments

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// RenderRunSummary produces a single-page synthesis of a completed plan
// run — the document you'd hand a sponsor or non-technical reviewer to
// answer "what happened on this run." Returns "" when the plan is still
// in-flight (no terminal status reached). Links to the sibling phase
// artifacts (architecture.md, requirements.md, scenarios.md,
// qa-summary.md) by relative path so a reader navigating the
// `.semspec/plans/<slug>/` directory can drill in.
//
// This is the BMAD/OpenSpec-style "run report" — orientation +
// outcome + pointers, not the full content of each phase. The phase
// artifacts hold the content; this file holds the narrative.
func RenderRunSummary(plan *workflow.Plan) string {
	if plan == nil {
		return ""
	}
	status := plan.EffectiveStatus()
	if !isTerminalStatus(status) {
		return ""
	}

	var b strings.Builder

	title := plan.Title
	if title == "" {
		title = plan.Slug
	}
	b.WriteString(fmt.Sprintf("# Run summary: %s\n\n", title))
	b.WriteString(fmt.Sprintf("**Slug:** `%s` | **Terminal status:** `%s` | **QA level:** `%s`\n\n",
		plan.Slug, status, plan.EffectiveQALevel()))

	renderRunTimeline(&b, plan)
	renderRunCounts(&b, plan)
	renderRunQAOutcome(&b, plan)
	renderRunArtifactLinks(&b, plan)
	renderRunAssembledBranch(&b, plan)

	return b.String()
}

func isTerminalStatus(s workflow.Status) bool {
	switch s {
	case workflow.StatusComplete, workflow.StatusAwaitingReview, workflow.StatusRejected:
		return true
	}
	return false
}

func renderRunTimeline(b *strings.Builder, plan *workflow.Plan) {
	b.WriteString("## Timeline\n\n")
	rows := [][2]string{
		{"Created", formatTime(plan.CreatedAt)},
		{"Approved", formatTimePtr(plan.ApprovedAt)},
	}
	if plan.QARun != nil && !plan.QARun.CompletedAt.IsZero() {
		rows = append(rows, [2]string{"QA completed", formatTime(plan.QARun.CompletedAt)})
	}
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- **%s:** %s\n", r[0], r[1]))
	}
	b.WriteString("\n")
}

func renderRunCounts(b *strings.Builder, plan *workflow.Plan) {
	b.WriteString("## Plan shape\n\n")
	reqCount := len(plan.Requirements)
	scenCount := len(plan.Scenarios)
	upstreamCount := 0
	integrationTargets := 0
	if plan.Architecture != nil {
		upstreamCount = len(plan.Architecture.UpstreamResolutions)
		for _, r := range plan.Architecture.UpstreamResolutions {
			if r.Role == "integration_target" {
				integrationTargets++
			}
		}
	}
	b.WriteString(fmt.Sprintf("- Requirements: **%d**\n", reqCount))
	b.WriteString(fmt.Sprintf("- Scenarios: **%d**\n", scenCount))
	b.WriteString(fmt.Sprintf("- Upstream resolutions: **%d**", upstreamCount))
	if integrationTargets > 0 {
		b.WriteString(fmt.Sprintf(" (of which **%d** are integration_targets with declared TestHarness)", integrationTargets))
	}
	b.WriteString("\n")
	if plan.ReviewIteration > 0 {
		b.WriteString(fmt.Sprintf("- Plan-review iterations: **%d**\n", plan.ReviewIteration))
	}
	b.WriteString("\n")
}

func renderRunQAOutcome(b *strings.Builder, plan *workflow.Plan) {
	b.WriteString("## QA outcome\n\n")
	if plan.QARun != nil {
		passed := "FAILED"
		if plan.QARun.Passed {
			passed = "PASSED"
		}
		b.WriteString(fmt.Sprintf("- **Executor verdict:** %s\n", passed))
		if plan.QARun.DurationMs > 0 {
			b.WriteString(fmt.Sprintf("- **Duration:** %.1fs\n", float64(plan.QARun.DurationMs)/1000.0))
		}
		if len(plan.QARun.Failures) > 0 {
			b.WriteString(fmt.Sprintf("- **Failures:** %d (see `qa-summary.md`)\n", len(plan.QARun.Failures)))
		}
		if len(plan.QARun.Artifacts) > 0 {
			b.WriteString(fmt.Sprintf("- **Artifacts captured:** %d\n", len(plan.QARun.Artifacts)))
		}
	} else {
		b.WriteString(fmt.Sprintf("*No executor run on this plan (QA level `%s`).*\n",
			plan.EffectiveQALevel()))
	}
	if len(plan.PlanDecisions) > 0 {
		b.WriteString(fmt.Sprintf("- **Plan-decisions raised:** %d (see `qa-summary.md`)\n",
			len(plan.PlanDecisions)))
	}
	b.WriteString("\n")
}

func renderRunArtifactLinks(b *strings.Builder, plan *workflow.Plan) {
	b.WriteString("## Phase artifacts\n\n")
	b.WriteString("Drill into the per-phase markdown for full content:\n\n")
	b.WriteString("- [`plan.md`](./plan.md) — full plan view (goal, context, scope, architecture, requirements, scenarios)\n")
	if plan.Architecture != nil {
		b.WriteString("- [`architecture.md`](./architecture.md) — architectural deliverable in detail\n")
	}
	if len(plan.Requirements) > 0 {
		b.WriteString("- [`requirements.md`](./requirements.md) — requirements with files_owned + dependency graph\n")
	}
	if len(plan.Scenarios) > 0 {
		b.WriteString("- [`scenarios.md`](./scenarios.md) — BDD scenarios grouped by requirement\n")
	}
	if plan.QARun != nil || len(plan.PlanDecisions) > 0 {
		b.WriteString("- [`qa-summary.md`](./qa-summary.md) — QA executor result + plan-decisions raised\n")
	}
	b.WriteString("- [`plan.json`](./plan.json) — structured plan data for programmatic access\n")
	b.WriteString("\n")
}

func renderRunAssembledBranch(b *strings.Builder, plan *workflow.Plan) {
	if plan.AssembledBranch == "" {
		return
	}
	b.WriteString("## Assembled output\n\n")
	b.WriteString(fmt.Sprintf("- **Branch:** `%s`\n", plan.AssembledBranch))
	if plan.AssembledMergeCommit != "" {
		b.WriteString(fmt.Sprintf("- **Merge commit:** `%s`\n", plan.AssembledMergeCommit))
	}
	b.WriteString("\n")
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}
