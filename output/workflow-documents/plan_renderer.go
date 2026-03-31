package workflowdocuments

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// milestoneStatuses defines which plan statuses trigger document generation.
var milestoneStatuses = map[workflow.Status]bool{
	workflow.StatusDrafted:               true,
	workflow.StatusRequirementsGenerated: true,
	workflow.StatusScenariosGenerated:    true,
	workflow.StatusReviewed:              true,
	workflow.StatusScenariosReviewed:     true,
	workflow.StatusReadyForExecution:     true,
	workflow.StatusComplete:              true,
}

// isMilestoneStatus returns true if the status should trigger document generation.
func isMilestoneStatus(status workflow.Status) bool {
	return milestoneStatuses[status]
}

// RenderPlan produces a human-readable markdown representation of a plan,
// including requirements, scenarios, and review history.
func RenderPlan(plan *workflow.Plan) string {
	var b strings.Builder

	renderHeader(&b, plan)
	renderGoal(&b, plan)
	renderContext(&b, plan)
	renderScope(&b, plan)
	renderRequirements(&b, plan)
	renderReviewHistory(&b, plan)
	renderFooter(&b)

	return b.String()
}

func renderHeader(b *strings.Builder, plan *workflow.Plan) {
	title := plan.Title
	if title == "" {
		title = plan.Slug
	}
	b.WriteString(fmt.Sprintf("# %s\n\n", title))

	b.WriteString(fmt.Sprintf("**Status:** %s", plan.EffectiveStatus()))
	if !plan.CreatedAt.IsZero() {
		b.WriteString(fmt.Sprintf(" | **Created:** %s", plan.CreatedAt.Format(time.RFC3339)))
	}
	if plan.ApprovedAt != nil {
		b.WriteString(fmt.Sprintf(" | **Approved:** %s", plan.ApprovedAt.Format(time.RFC3339)))
	}
	b.WriteString("\n\n")
}

func renderGoal(b *strings.Builder, plan *workflow.Plan) {
	if plan.Goal == "" {
		return
	}
	b.WriteString("## Goal\n\n")
	b.WriteString(plan.Goal)
	b.WriteString("\n\n")
}

func renderContext(b *strings.Builder, plan *workflow.Plan) {
	if plan.Context == "" {
		return
	}
	b.WriteString("## Context\n\n")
	b.WriteString(plan.Context)
	b.WriteString("\n\n")
}

func renderScope(b *strings.Builder, plan *workflow.Plan) {
	scope := plan.Scope
	if len(scope.Include) == 0 && len(scope.Exclude) == 0 && len(scope.DoNotTouch) == 0 {
		return
	}
	b.WriteString("## Scope\n\n")
	if len(scope.Include) > 0 {
		b.WriteString("**Include:**\n")
		for _, p := range scope.Include {
			b.WriteString(fmt.Sprintf("- `%s`\n", p))
		}
		b.WriteString("\n")
	}
	if len(scope.Exclude) > 0 {
		b.WriteString("**Exclude:**\n")
		for _, p := range scope.Exclude {
			b.WriteString(fmt.Sprintf("- `%s`\n", p))
		}
		b.WriteString("\n")
	}
	if len(scope.DoNotTouch) > 0 {
		b.WriteString("**Do Not Touch:**\n")
		for _, p := range scope.DoNotTouch {
			b.WriteString(fmt.Sprintf("- `%s`\n", p))
		}
		b.WriteString("\n")
	}
}

func renderRequirements(b *strings.Builder, plan *workflow.Plan) {
	if len(plan.Requirements) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Requirements (%d)\n\n", len(plan.Requirements)))

	for _, req := range plan.Requirements {
		b.WriteString(fmt.Sprintf("### %s\n\n", req.Title))
		if req.Description != "" {
			b.WriteString(req.Description)
			b.WriteString("\n\n")
		}

		var meta []string
		if req.Status != "" {
			meta = append(meta, fmt.Sprintf("**Status:** %s", req.Status))
		}
		if len(req.DependsOn) > 0 {
			meta = append(meta, fmt.Sprintf("**Dependencies:** %s", strings.Join(req.DependsOn, ", ")))
		}
		if len(meta) > 0 {
			b.WriteString(strings.Join(meta, " | "))
			b.WriteString("\n\n")
		}

		// Render scenarios for this requirement.
		renderScenariosForRequirement(b, plan, req.ID)
	}
}

func renderScenariosForRequirement(b *strings.Builder, plan *workflow.Plan, reqID string) {
	var scenarios []workflow.Scenario
	for _, s := range plan.Scenarios {
		if s.RequirementID == reqID {
			scenarios = append(scenarios, s)
		}
	}
	if len(scenarios) == 0 {
		return
	}

	b.WriteString("#### Scenarios\n\n")
	for _, s := range scenarios {
		b.WriteString(fmt.Sprintf("**Given** %s\n", s.Given))
		b.WriteString(fmt.Sprintf("**When** %s\n", s.When))
		b.WriteString("**Then**\n")
		for _, t := range s.Then {
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
		b.WriteString("\n")
	}
}

func renderReviewHistory(b *strings.Builder, plan *workflow.Plan) {
	if plan.ReviewIteration == 0 {
		return
	}
	b.WriteString("## Review History\n\n")
	b.WriteString(fmt.Sprintf("**Iteration:** %d", plan.ReviewIteration))
	if plan.ReviewVerdict != "" {
		b.WriteString(fmt.Sprintf(" | **Verdict:** %s", plan.ReviewVerdict))
	}
	b.WriteString("\n\n")

	if plan.ReviewSummary != "" {
		b.WriteString(fmt.Sprintf("**Summary:** %s\n\n", plan.ReviewSummary))
	}

	if plan.ReviewFormattedFindings != "" {
		b.WriteString("### Findings\n\n")
		b.WriteString(plan.ReviewFormattedFindings)
		b.WriteString("\n")
	}
}

func renderFooter(b *strings.Builder) {
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*Generated at %s*\n", time.Now().UTC().Format(time.RFC3339)))
}
