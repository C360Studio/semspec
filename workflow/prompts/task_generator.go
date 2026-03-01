package prompts

import (
	"fmt"
	"strings"
)

// PhaseInfo contains phase details for phase-aware task generation.
type PhaseInfo struct {
	ID          string
	Sequence    int
	Name        string
	Description string
}

// TaskGeneratorParams contains the plan data needed to generate tasks.
type TaskGeneratorParams struct {
	// Goal describes what we're building or fixing
	Goal string

	// Context describes the current state and why this matters
	Context string

	// ScopeInclude lists files/directories in scope
	ScopeInclude []string

	// ScopeExclude lists files/directories explicitly out of scope
	ScopeExclude []string

	// ScopeProtected lists files/directories that must not be modified
	ScopeProtected []string

	// Title is the plan title (for context)
	Title string

	// Phases provides phase information for phase-aware task generation.
	// When non-empty, the LLM is instructed to assign each task to a phase.
	Phases []PhaseInfo

	// SOPRequirements lists SOP requirements that must be reflected in generated tasks.
	// When non-empty, the prompt includes a section instructing the LLM to create
	// tasks addressing each requirement.
	SOPRequirements []string
}

// TaskGeneratorPrompt returns the system prompt for generating tasks from a plan.
// The LLM generates tasks with BDD-style Given/When/Then acceptance criteria.
// When phases are provided, the LLM assigns each task to a phase via phase_id.
func TaskGeneratorPrompt(params TaskGeneratorParams) string {
	scopeInclude := formatScopeList(params.ScopeInclude, "*")
	scopeExclude := formatScopeList(params.ScopeExclude, "(none)")
	scopeProtected := formatScopeList(params.ScopeProtected, "(none)")

	phaseSection := formatPhaseSection(params.Phases)
	phaseIDField := ""
	phaseIDExample := ""
	phaseIDConstraint := ""
	if len(params.Phases) > 0 {
		phaseIDField = `      "phase_id": "phase.{slug}.1",
`
		phaseIDExample = `      "phase_id": "phase.{slug}.1",
`
		phaseIDConstraint = `- Every task MUST have a "phase_id" field matching one of the phases listed above
- Tasks within a phase should only depend on other tasks in the same or earlier phases
`
	}

	return fmt.Sprintf(`You are a task planner generating actionable development tasks from a plan.

## Plan: %s

**Goal:** %s

**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s
%s
## CRITICAL: Stay On Goal

Every task you generate MUST directly contribute to the goal stated above.
- Do NOT invent features, endpoints, or functionality not mentioned in the goal.
- If the goal says "/goodbye", every task must reference "/goodbye" — not "/health", "/status", or anything else.
- Task descriptions must use the exact names, paths, and terms from the goal.

## Your Task

Generate a list of 3-8 development tasks that accomplish the goal. Each task should:
- Be completable in a single development session
- Have clear, testable acceptance criteria in BDD format (Given/When/Then)
- Reference specific files from the scope when relevant
- Be ordered by dependency (prerequisite tasks first)
- Use the exact feature names from the goal (not synonyms or alternatives)

## Output Format

Return ONLY valid JSON in this exact format:

`+"```json"+`
{
  "tasks": [
    {
%s      "description": "Clear description of what to implement",
      "type": "implement",
      "depends_on": [],
      "acceptance_criteria": [
        {
          "given": "a specific precondition or state",
          "when": "an action is performed",
          "then": "the expected outcome or behavior"
        }
      ],
      "files": ["path/to/relevant/file.go"]
    }
  ]
}
`+"```"+`

## Task Types

Use the appropriate type for each task:
- **implement**: Writing new code or features
- **test**: Writing or updating tests
- **document**: Documentation work
- **review**: Code review tasks
- **refactor**: Restructuring existing code

## Acceptance Criteria Rules

1. Each task MUST have at least one acceptance criterion
2. Use Given/When/Then format for testability:
   - **Given**: The precondition or starting state
   - **When**: The action or trigger
   - **Then**: The expected outcome (observable and verifiable)
3. Be specific and measurable (avoid vague outcomes)
4. Consider edge cases and error conditions

## Example Tasks

`+"```json"+`
{
  "tasks": [
    {
%s      "description": "Create rate limiter struct and configuration",
      "type": "implement",
      "depends_on": [],
      "acceptance_criteria": [
        {
          "given": "a new rate limiter instance",
          "when": "created with config for 5 attempts per 15 minutes",
          "then": "the limiter is properly initialized and ready to track attempts"
        }
      ],
      "files": ["internal/auth/ratelimit.go"]
    }
  ]
}
`+"```"+`

## Dependencies

Use the "depends_on" field to specify which tasks must complete before this task can start:
- Reference tasks by sequence number using the format: "task.{slug}.N" where N is the 1-indexed task number
- Example: the first task is "task.{slug}.1", the second is "task.{slug}.2", etc.
- Tasks with no dependencies should have an empty array: "depends_on": []
- Tasks can depend on multiple other tasks: "depends_on": ["task.{slug}.1", "task.{slug}.2"]
- NEVER use type names as IDs (e.g., "task.implement" or "task.test" are INVALID)
- Dependencies enable parallel execution - independent tasks run concurrently
- Always put foundational/setup tasks first with no dependencies
- Tests typically depend on the implementation they're testing

## Constraints

- Files in the "files" array MUST be within the scope Include paths
- Files in "Protected" MUST NOT appear in any task's files array
- Do not include files from the "Exclude" list
- Keep tasks focused and atomic (one responsibility per task)
- Order tasks so dependencies come first
- No circular dependencies allowed
%s
%sGenerate tasks now. Return ONLY the JSON output, no other text.
`, params.Title, params.Goal, params.Context, scopeInclude, scopeExclude, scopeProtected,
		phaseSection, phaseIDField, phaseIDExample, phaseIDConstraint, FormatSOPRequirements(params.SOPRequirements))
}

// FormatSOPRequirements formats SOP requirements as a prompt section.
// Returns empty string if no requirements are present.
// Used by task-generator to inject graph-sourced SOP requirements into prompts.
func FormatSOPRequirements(requirements []string) string {
	if len(requirements) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## SOP Requirements\n\n")
	sb.WriteString("The following Standard Operating Procedure requirements MUST be reflected in the generated tasks.\n")
	sb.WriteString("Ensure at least one task addresses each requirement:\n\n")
	for _, req := range requirements {
		sb.WriteString(fmt.Sprintf("- %s\n", req))
	}
	sb.WriteString("\nFor example, if a requirement mandates migration notes for model changes, include a dedicated migration/documentation task.\n")
	sb.WriteString("If a requirement mandates type synchronization across backend and frontend, ensure tasks cover both.\n\n")
	return sb.String()
}

// formatPhaseSection builds the phases section for the task generation prompt.
// When phases are available, instructs the LLM to assign each task to a phase.
func formatPhaseSection(phases []PhaseInfo) string {
	if len(phases) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Execution Phases\n\n")
	sb.WriteString("This plan has been decomposed into the following phases. Each task MUST be assigned to exactly one phase using its phase_id.\n\n")
	for _, p := range phases {
		sb.WriteString(fmt.Sprintf("- **%s** (ID: `%s`): %s\n", p.Name, p.ID, p.Description))
	}
	sb.WriteString("\nAssign tasks to the phase where they logically belong. Earlier phases should contain foundational work, later phases contain integration/testing.\n")
	return sb.String()
}

// formatScopeList formats a scope list for display in the prompt.
func formatScopeList(items []string, defaultValue string) string {
	if len(items) == 0 {
		return defaultValue
	}
	return strings.Join(items, ", ")
}

// TaskGeneratorResponse represents the expected JSON response from task generation.
type TaskGeneratorResponse struct {
	Tasks []GeneratedTask `json:"tasks"`
}

// GeneratedTask represents a task generated by the LLM.
type GeneratedTask struct {
	Description        string               `json:"description"`
	Type               string               `json:"type"`
	PhaseID            string               `json:"phase_id,omitempty"`
	DependsOn          []string             `json:"depends_on,omitempty"`
	AcceptanceCriteria []GeneratedCriterion `json:"acceptance_criteria"`
	Files              []string             `json:"files,omitempty"`
}

// GeneratedCriterion represents a BDD acceptance criterion from LLM output.
type GeneratedCriterion struct {
	Given string `json:"given"`
	When  string `json:"when"`
	Then  string `json:"then"`
}
