package prompts

import (
	"fmt"
	"strings"
)

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
}

// TaskGeneratorPrompt returns the system prompt for generating tasks from a plan.
// The LLM generates tasks with BDD-style Given/When/Then acceptance criteria.
func TaskGeneratorPrompt(params TaskGeneratorParams) string {
	scopeInclude := formatScopeList(params.ScopeInclude, "*")
	scopeExclude := formatScopeList(params.ScopeExclude, "(none)")
	scopeProtected := formatScopeList(params.ScopeProtected, "(none)")

	return fmt.Sprintf(`You are a task planner generating actionable development tasks from a plan.

## Plan: %s

**Goal:** %s

**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s

## Your Task

Generate a list of 3-8 development tasks that accomplish the goal. Each task should:
- Be completable in a single development session
- Have clear, testable acceptance criteria in BDD format (Given/When/Then)
- Reference specific files from the scope when relevant
- Be ordered by dependency (prerequisite tasks first)

## Output Format

Return ONLY valid JSON in this exact format:

`+"```json"+`
{
  "tasks": [
    {
      "description": "Clear description of what to implement",
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
      "description": "Create rate limiter struct and configuration",
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
    },
    {
      "description": "Add rate limiting to login endpoint",
      "type": "implement",
      "depends_on": ["task.{slug}.1"],
      "acceptance_criteria": [
        {
          "given": "5 failed login attempts from the same IP in 15 minutes",
          "when": "the user attempts a 6th login",
          "then": "return 429 Too Many Requests and block the IP for 15 minutes"
        },
        {
          "given": "a blocked IP after rate limit triggered",
          "when": "15 minutes have passed",
          "then": "the IP can attempt login again normally"
        }
      ],
      "files": ["internal/auth/login.go", "internal/auth/ratelimit.go"]
    },
    {
      "description": "Write tests for rate limiting",
      "type": "test",
      "depends_on": ["task.{slug}.1", "task.{slug}.2"],
      "acceptance_criteria": [
        {
          "given": "a test environment with a rate limiter",
          "when": "test scenarios for rate limiting are executed",
          "then": "all rate limiting behaviors are verified"
        }
      ],
      "files": ["internal/auth/ratelimit_test.go"]
    }
  ]
}
`+"```"+`

## Dependencies

Use the "depends_on" field to specify which tasks must complete before this task can start:
- Reference tasks by their ID format: "task.{slug}.{sequence}" where sequence is 1-indexed
- Tasks with no dependencies should have an empty array: "depends_on": []
- Tasks can depend on multiple other tasks: "depends_on": ["task.{slug}.1", "task.{slug}.2"]
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

Generate tasks now. Return ONLY the JSON output, no other text.
`, params.Title, params.Goal, params.Context, scopeInclude, scopeExclude, scopeProtected)
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
