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
	workflow.StatusArchitectureGenerated: true,
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
	renderArchitecture(&b, plan)
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

func renderArchitecture(b *strings.Builder, plan *workflow.Plan) {
	if plan.Architecture == nil {
		return
	}
	arch := plan.Architecture

	b.WriteString("## Architecture\n\n")

	if len(arch.TechnologyChoices) > 0 {
		b.WriteString("### Technology Choices\n\n")
		b.WriteString("| Category | Choice | Rationale |\n")
		b.WriteString("|----------|--------|-----------|\n")
		for _, tc := range arch.TechnologyChoices {
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", tc.Category, tc.Choice, tc.Rationale))
		}
		b.WriteString("\n")
	}

	if len(arch.ComponentBoundaries) > 0 {
		b.WriteString("### Component Boundaries\n\n")
		for _, cb := range arch.ComponentBoundaries {
			b.WriteString(fmt.Sprintf("**%s** — %s", cb.Name, cb.Responsibility))
			if len(cb.Dependencies) > 0 {
				b.WriteString(fmt.Sprintf(" (depends on: %s)", strings.Join(cb.Dependencies, ", ")))
			}
			b.WriteString("\n\n")
		}
	}

	if arch.DataFlow != "" {
		b.WriteString("### Data Flow\n\n")
		b.WriteString(arch.DataFlow)
		b.WriteString("\n\n")
	}

	if len(arch.Decisions) > 0 {
		b.WriteString("### Architecture Decisions\n\n")
		for _, d := range arch.Decisions {
			b.WriteString(fmt.Sprintf("**%s: %s**\n\n", d.ID, d.Title))
			b.WriteString(fmt.Sprintf("*Decision:* %s\n\n", d.Decision))
			b.WriteString(fmt.Sprintf("*Rationale:* %s\n\n", d.Rationale))
		}
	}

	if len(arch.Actors) > 0 {
		b.WriteString("### Actors\n\n")
		b.WriteString("| Name | Type | Triggers |\n")
		b.WriteString("|------|------|----------|\n")
		for _, a := range arch.Actors {
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", a.Name, a.Type, strings.Join(a.Triggers, ", ")))
		}
		b.WriteString("\n")
	}

	if len(arch.Integrations) > 0 {
		b.WriteString("### Integration Points\n\n")
		b.WriteString("| Name | Direction | Protocol |\n")
		b.WriteString("|------|-----------|----------|\n")
		for _, ip := range arch.Integrations {
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", ip.Name, ip.Direction, ip.Protocol))
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
