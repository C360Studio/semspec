package prompts

import (
	"fmt"
	"strings"
)

// RequirementSummary is a lightweight view of a requirement for prompt injection.
type RequirementSummary struct {
	Title       string
	Description string
}

// ArchitectParams contains the plan data needed to generate architecture decisions.
type ArchitectParams struct {
	// PlanGoal describes what we're building or fixing.
	PlanGoal string

	// PlanContext describes the current state and why this matters.
	PlanContext string

	// ScopeInclude lists files/directories in scope.
	ScopeInclude []string

	// ScopeExclude lists files/directories explicitly out of scope.
	ScopeExclude []string

	// ScopeProtected lists files/directories that must not be modified.
	ScopeProtected []string

	// Requirements lists the plan's requirements for architectural analysis.
	Requirements []RequirementSummary

	// PreviousError holds the error message from a prior failed generation attempt.
	// When set, the prompt includes a section instructing the model to fix the issue.
	PreviousError string
}

// ArchitectPrompt builds the user prompt for architecture generation.
// The architect agent explores the codebase and submits an ArchitectureDocument
// via submit_work with a structured deliverable.
func ArchitectPrompt(params ArchitectParams) string {
	scopeInclude := formatScopeList(params.ScopeInclude, "all files")
	scopeExclude := formatScopeList(params.ScopeExclude, "none")
	scopeProtected := formatScopeList(params.ScopeProtected, "none")

	var reqSection strings.Builder
	if len(params.Requirements) > 0 {
		reqSection.WriteString("\n## Requirements\n\n")
		for i, r := range params.Requirements {
			reqSection.WriteString(fmt.Sprintf("%d. **%s**: %s\n", i+1, r.Title, r.Description))
		}
	}

	previousErrorSection := ""
	if params.PreviousError != "" {
		previousErrorSection = fmt.Sprintf(`

## Previous Attempt Failed

Your previous output could not be processed: %s

Please fix the issue and ensure your deliverable matches the required structure.`, params.PreviousError)
	}

	return fmt.Sprintf(`Analyze the following plan and its requirements to produce architecture decisions.

**Goal:** %s

**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s
%s
## Your Task

1. Use bash and graph tools to explore the codebase — understand the existing technology stack, project structure, and patterns already in use.
2. Identify technology choices, component boundaries, data flows, and key architecture decisions.
3. Call submit_work with a summary and a structured deliverable.

**Guidelines:**
- Reuse existing technology stack where possible — do not propose new frameworks when the project already has one
- Focus on structure and boundaries, not implementation details
- Justify every decision with a rationale
- Flag architectural risks or trade-offs
- Component boundaries should reflect natural module/service divisions in the codebase

## Deliverable Structure

Your deliverable must contain:
- **technology_choices**: array of {category, choice, rationale} — e.g., framework, database, messaging
- **component_boundaries**: array of {name, responsibility, dependencies[]} — logical modules or services
- **data_flow**: string describing how data moves between components
- **decisions**: array of {id, title, decision, rationale} — architecture decision records (use IDs like ARCH-001)
%s`, params.PlanGoal, params.PlanContext, scopeInclude, scopeExclude, scopeProtected,
		reqSection.String(), previousErrorSection)
}
