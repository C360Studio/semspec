package prompts

import (
	"fmt"
	"strings"
)

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

If revising after reviewer rejection:
1. Read the Review Summary and Specific Findings carefully - this is your PRIMARY input
2. Address EVERY issue raised - do not skip any findings
3. For scope violations: add the specific files or patterns mentioned in suggestions
4. For missing elements: add them exactly as suggested by the reviewer
5. Preserve parts of the plan that were NOT flagged as violations
6. Produce an updated Goal/Context/Scope that resolves ALL findings

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

// PlannerRevisionPrompt returns the prompt for revising a plan after reviewer rejection.
// This follows the same pattern as DeveloperRetryPrompt — structured feedback drives revision.
func PlannerRevisionPrompt(summary string, findings string) string {
	return `Your previous plan was rejected by the reviewer. Address ALL issues to produce an improved plan.

## Review Summary

` + summary + `

## Specific Findings

` + findings + `

## Instructions

1. Read each finding carefully
2. Address EVERY issue raised — do not skip any
3. Re-examine the codebase if the reviewer identified missing context
4. Produce an updated Goal/Context/Scope that resolves all findings
5. Maintain the same output format as before

Focus on fixing what the reviewer identified. Do not change parts of the plan that were not flagged.`
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

// PlannerFocusedSystemPrompt returns the system prompt for a focused planner.
// Focused planners concentrate on a specific area and produce partial plans
// that will be synthesized by the coordinator.
func PlannerFocusedSystemPrompt(focusArea string) string {
	return fmt.Sprintf(`You are analyzing a development task with focus on: **%s**

## Your Objective

Examine the codebase from your specialized perspective and produce a partial plan covering:
- **Goal**: What needs to happen from the %s perspective
- **Context**: Current state of %s-related code and patterns
- **Scope**: Files and directories relevant to %s concerns

## Process

1. Use the pre-loaded context (entities, files) provided by the coordinator
2. Use file_read to examine specific files in detail if needed
3. Focus deeply on your area - other planners cover different aspects
4. Produce a partial Goal/Context/Scope that can be synthesized with other planners

## Output Format

Produce your focused analysis:

`+"`"+`json
{
  "goal": "What needs to happen from %s perspective",
  "context": "Current state of %s-related code",
  "scope": {
    "include": ["paths relevant to %s"],
    "exclude": ["paths outside %s concern"],
    "do_not_touch": ["protected paths"]
  }
}
`+"`"+`

## Guidelines

- Stay focused on your area - don't try to cover everything
- Be thorough within your scope
- Your output will be combined with other planners' outputs
- Flag any dependencies or concerns that other focus areas should know about
`, focusArea, focusArea, focusArea, focusArea, focusArea, focusArea, focusArea, focusArea)
}

// PlannerFocusedPrompt returns the user prompt for a focused planner.
func PlannerFocusedPrompt(focusArea, description, title string, hints []string, context *PlannerContextInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Analyze this task from the **%s** perspective:\n\n", focusArea))
	sb.WriteString(fmt.Sprintf("**Task Title:** %s\n", title))
	sb.WriteString(fmt.Sprintf("**Focus Area:** %s\n", focusArea))
	sb.WriteString(fmt.Sprintf("**Focus Description:** %s\n\n", description))

	if len(hints) > 0 {
		sb.WriteString("**Hints:**\n")
		for _, hint := range hints {
			sb.WriteString(fmt.Sprintf("- %s\n", hint))
		}
		sb.WriteString("\n")
	}

	if context != nil {
		sb.WriteString("## Pre-loaded Context from Knowledge Graph\n\n")

		if context.Summary != "" {
			sb.WriteString(fmt.Sprintf("**Summary:** %s\n\n", context.Summary))
		}

		if len(context.Entities) > 0 {
			sb.WriteString("**Relevant Entities:**\n")
			for _, e := range context.Entities {
				sb.WriteString(fmt.Sprintf("- %s\n", e))
			}
			sb.WriteString("\n")
		}

		if len(context.Files) > 0 {
			sb.WriteString("**Files in Scope:**\n")
			for _, f := range context.Files {
				sb.WriteString(fmt.Sprintf("- %s\n", f))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("Produce a focused Goal/Context/Scope for your area.\n")

	return sb.String()
}

// PlannerContextInfo contains context information for focused planners.
// Used for prompt generation.
type PlannerContextInfo struct {
	Entities []string
	Files    []string
	Summary  string
}
