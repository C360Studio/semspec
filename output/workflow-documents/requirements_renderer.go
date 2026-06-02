package workflowdocuments

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderRequirements produces a markdown view of the plan's requirements
// with files_owned + depends_on relationships surfaced as a flat list and
// a mermaid dependency graph. Returns "" when no requirements are
// attached.
//
// Each requirement is rendered as a top-level section so that downstream
// readers (BMAD/OpenSpec consumers, manual reviewers) can deep-link to a
// specific requirement by its ID.
func RenderRequirements(plan *workflow.Plan) string {
	if plan == nil || len(plan.Requirements) == 0 {
		return ""
	}
	var b strings.Builder

	title := displayTitle(plan)
	b.WriteString(fmt.Sprintf("# Requirements: %s\n\n", title))
	b.WriteString(fmt.Sprintf("*Generated from the requirement-generator role's output. **%d requirements** partition the implementation work.*\n\n",
		len(plan.Requirements)))

	renderRequirementsDependencyGraph(&b, plan.Requirements)

	for _, req := range plan.Requirements {
		renderSingleRequirement(&b, plan, req)
	}

	return b.String()
}

func renderRequirementsDependencyGraph(b *strings.Builder, reqs []workflow.Requirement) {
	hasDeps := false
	for _, r := range reqs {
		if len(r.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	if !hasDeps {
		return
	}
	b.WriteString("## Dependency graph\n\n")
	b.WriteString("```mermaid\ngraph TD\n")
	for _, r := range reqs {
		label := r.ID
		if r.Title != "" {
			label = fmt.Sprintf("%s[\"%s\"]", r.ID, escapeMermaid(r.Title))
		}
		b.WriteString(fmt.Sprintf("  %s\n", label))
	}
	for _, r := range reqs {
		for _, dep := range r.DependsOn {
			b.WriteString(fmt.Sprintf("  %s --> %s\n", dep, r.ID))
		}
	}
	b.WriteString("```\n\n")
}

func renderSingleRequirement(b *strings.Builder, plan *workflow.Plan, req workflow.Requirement) {
	title := req.Title
	if title == "" {
		title = req.ID
	}
	b.WriteString(fmt.Sprintf("## %s\n\n", title))
	if req.ID != "" {
		b.WriteString(fmt.Sprintf("**ID:** `%s`", req.ID))
		if req.Status != "" {
			b.WriteString(fmt.Sprintf(" | **Status:** `%s`", req.Status))
		}
		b.WriteString("\n\n")
	}
	if req.Description != "" {
		b.WriteString(req.Description)
		b.WriteString("\n\n")
	}
	// ADR-043 Move 4 — file ownership moved to Story; the renderer derives
	// the file list per Story instead of per Requirement. Stories are
	// rendered under their parent Requirement in the post-PR-4b
	// tasks.md / spec.md surfaces.
	if len(req.DependsOn) > 0 {
		b.WriteString(fmt.Sprintf("**Depends on:** %s\n\n",
			strings.Join(req.DependsOn, ", ")))
	}
	// Scenario count for this requirement — link to scenarios.md anchor.
	scenarioCount := 0
	for _, s := range plan.Scenarios {
		if s.RequirementID == req.ID {
			scenarioCount++
		}
	}
	if scenarioCount > 0 {
		b.WriteString(fmt.Sprintf("**Verified by %d scenario(s)** — see `scenarios.md`.\n\n", scenarioCount))
	}
}

// escapeMermaid replaces characters that break mermaid node labels.
// Double quotes inside the label string are the most common breakage.
func escapeMermaid(s string) string {
	s = strings.ReplaceAll(s, "\"", "'")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
