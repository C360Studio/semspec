package prompts

import (
	"fmt"
	"strings"
)

// PhaseGeneratorParams contains the parameters for building the phase generation prompt.
type PhaseGeneratorParams struct {
	Goal           string
	Context        string
	Title          string
	ScopeInclude   []string
	ScopeExclude   []string
	ScopeProtected []string
}

// PhaseGeneratorResponse is the expected JSON output from the LLM.
type PhaseGeneratorResponse struct {
	Phases []GeneratedPhase `json:"phases"`
}

// GeneratedPhase is a single phase from the LLM response.
type GeneratedPhase struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	DependsOn        []int    `json:"depends_on,omitempty"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
}

// PhaseGeneratorPrompt builds the prompt for phase generation from an approved plan.
func PhaseGeneratorPrompt(params PhaseGeneratorParams) string {
	scopeInclude := "all files"
	if len(params.ScopeInclude) > 0 {
		scopeInclude = strings.Join(params.ScopeInclude, ", ")
	}
	scopeExclude := "none"
	if len(params.ScopeExclude) > 0 {
		scopeExclude = strings.Join(params.ScopeExclude, ", ")
	}
	scopeProtected := "none"
	if len(params.ScopeProtected) > 0 {
		scopeProtected = strings.Join(params.ScopeProtected, ", ")
	}

	return fmt.Sprintf(`You are a development project planner specializing in decomposing plans into logical execution phases.

## Plan: %s

**Goal:** %s
**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s

## Your Task

Decompose this plan into 2-7 logical execution phases. Each phase groups related work that can be completed before moving to the next stage.

**Phase Design Guidelines:**
- Start with a foundation/setup phase (dependencies, data models, infrastructure)
- Follow with implementation phases (core logic, API endpoints, integrations)
- Include a testing/review phase if the plan involves significant new functionality
- End with integration/deployment phases if applicable
- Each phase should have a clear name (e.g., "Phase 1: Foundation", "Phase 2: Core Implementation")
- Phases should be sequentially ordered — later phases depend on earlier ones
- Consider whether any phases need human approval before proceeding (e.g., architecture decisions, breaking changes)

**Dependency Rules:**
- Use 1-based sequence numbers for depends_on (e.g., phase 3 depends on phase 1 → "depends_on": [1])
- A phase can depend on multiple earlier phases
- No circular dependencies allowed
- Phase 1 should have no dependencies

## Output Format

Return ONLY valid JSON matching this exact structure:

`+"`"+`json
{
  "phases": [
    {
      "name": "Phase 1: Foundation",
      "description": "Set up base types, data models, and infrastructure needed by later phases",
      "depends_on": [],
      "requires_approval": false
    },
    {
      "name": "Phase 2: Core Implementation",
      "description": "Implement the main business logic and API endpoints",
      "depends_on": [1],
      "requires_approval": false
    }
  ]
}
`+"`"+`

**Important:** Return ONLY the JSON object, no additional text or explanation.
`, params.Title, params.Goal, params.Context, scopeInclude, scopeExclude, scopeProtected)
}

// PhaseGeneratorWithGapDetection enriches a phase generation prompt with gap detection instructions.
func PhaseGeneratorWithGapDetection(basePrompt string) string {
	return basePrompt + `

## Gap Detection

After generating phases, review them for common gaps:
- Is there a testing phase or at least testing coverage within implementation phases?
- Is there a documentation phase if the plan involves public APIs?
- Are integration points between components covered in their own phase or within implementation?
- Is there a review/validation phase for significant architectural changes?

If you detect gaps, add appropriate phases to address them.
`
}
