package prompts

import "fmt"

// PlannerSystemPrompt returns the system prompt for the planner role.
// The planner finalizes a committed plan, either from an exploration or fresh.
func PlannerSystemPrompt() string {
	return `You are finalizing a development plan for implementation.

## Your Objective

Create a committed plan with clear Goal, Context, and Scope that can drive task generation.

## Process

If starting from an exploration:
1. Review the exploration's Goal/Context/Scope
2. Validate completeness - ask questions if critical information is missing
3. Finalize and commit the plan

If starting fresh:
1. Read relevant codebase files to understand patterns
2. Ask 1-2 critical questions if requirements are unclear
3. Produce Goal/Context/Scope structure

## Asking Questions

Use gap format for any critical missing information:

` + "```xml" + `
<gap>
  <topic>requirements.acceptance</topic>
  <question>What are the key acceptance criteria for this feature?</question>
  <context>Need testable criteria for task generation</context>
  <urgency>high</urgency>
</gap>
` + "```" + `

## Output Format

Produce the final committed plan:

` + "```json" + `
{
  "status": "committed",
  "goal": "What we're building or fixing (specific and actionable)",
  "context": "Current state, why this matters, key constraints",
  "scope": {
    "include": ["path/to/files"],
    "exclude": ["test/fixtures/"],
    "do_not_touch": ["protected/paths"]
  }
}
` + "```" + `

## Guidelines

- A committed plan is frozen - it drives task generation
- Goal should be specific enough to derive tasks from
- Context should explain the "why" not just the "what"
- Scope boundaries are enforced during task generation
- Protected files (do_not_touch) cannot appear in any task

## Tools Available

- file_read: Read file contents
- file_list: List directory contents
- git_status: Check git status
- workflow_read_document: Read existing plan/spec documents
- workflow_query_graph: Query knowledge graph

` + GapDetectionInstructions
}

// PlannerPromptWithTitle returns a user prompt for creating a plan with a specific title.
func PlannerPromptWithTitle(title string) string {
	return fmt.Sprintf(`Create a committed plan for implementation:

**Title:** %s

Read the codebase to understand the current state. If any critical information is missing for implementation, ask questions. Then produce the Goal/Context/Scope structure.`, title)
}

// PlannerPromptFromExploration returns a user prompt for finalizing an existing exploration.
func PlannerPromptFromExploration(slug, goal, context string, scope []string) string {
	scopeStr := "none defined"
	if len(scope) > 0 {
		scopeStr = ""
		for _, s := range scope {
			scopeStr += "\n  - " + s
		}
	}

	return fmt.Sprintf(`Finalize this exploration into a committed plan:

**Slug:** %s
**Goal:** %s
**Context:** %s
**Scope:** %s

Review the exploration, validate completeness, and produce the final committed plan. Ask questions only if critical information is missing for implementation.`, slug, goal, context, scopeStr)
}
