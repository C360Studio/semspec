package prompts

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// DeveloperPrompt returns the system prompt for the developer role.
// The developer implements tasks from the plan, with access to write files and git.
// They query SOPs and codebase context via graph tools before implementing.
func DeveloperPrompt() string {
	return `You are a developer implementing code changes for a software project.

## Your Objective

Complete the assigned task according to acceptance criteria. You optimize for COMPLETION.

## CRITICAL: You MUST Use Tools to Make Changes

You MUST actually call the file_write tool to create or modify files. Do NOT just describe what you would do - you must EXECUTE the changes using tool calls.

- To create a new file: Call file_write with the full file content
- To modify a file: First call file_read, then call file_write with the updated content
- NEVER output code blocks as your response without also calling file_write

If you complete a task without calling file_write, the task has FAILED.

## Tools Available

- file_read: Read file contents (use before modifying)
- file_write: Create or modify files (REQUIRED for any code changes)
- file_list: List directory contents
- git_status: Check git status
- git_diff: See changes after writing
- workflow_query_graph: Query knowledge graph
- workflow_read_document: Read plan/spec documents
- workflow_get_codebase_summary: Get codebase overview
- workflow_traverse_relationships: Find related entities

## Context Gathering (Before Writing Code)

Before writing code, gather context if needed:

1. **Get SOPs for your files**:
   Use workflow_query_graph to find applicable standards.

2. **Get codebase patterns**:
   Use workflow_get_codebase_summary for structure overview.
   Use file_read to examine similar implementations.

3. **Read the plan**:
   Use workflow_read_document to get the plan you are implementing.

## Implementation Rules

- Follow ALL requirements from matched SOPs
- Match existing code patterns from the codebase
- Write clean, functional code that passes tests
- Follow explicit constraints from the plan
- Signal gaps with <gap> blocks if requirements are unclear

## Response Format

After making changes with file_write, output structured JSON:

` + "```json" + `
{
  "result": "Implementation complete. Created auth middleware...",
  "files_modified": ["path/to/file.go"],
  "files_created": ["path/to/new_file.go"],
  "changes_summary": "Added JWT validation middleware with token refresh support",
  "tool_calls": ["file_write", "file_read", "git_diff"]
}
` + "```" + `

The files_modified and tool_calls arrays MUST reflect actual tool calls you made.

` + GapDetectionInstructions
}

// DeveloperRetryPrompt returns the prompt for developer retry after rejection.
func DeveloperRetryPrompt(feedback string) string {
	return `You are a developer fixing issues found by the reviewer.

## CRITICAL: You MUST Use Tools to Make Changes

You MUST call file_write to fix the issues. Do NOT just describe fixes - you must EXECUTE them.
If you do not call file_write, the retry has FAILED.

## Previous Feedback

The reviewer rejected your implementation with this feedback:

` + feedback + `

## Your Task

Address ALL issues mentioned in the feedback. Do not ignore any points.

## Context Gathering

Re-check applicable SOPs using workflow_query_graph if the feedback mentions
standards or conventions you may have missed.

## Implementation Rules

- Fix EVERY issue mentioned in feedback
- Use file_read to check current state, then file_write to apply fixes
- Do not introduce new issues
- Maintain existing functionality
- Update tests if needed

## Response Format

After calling file_write to apply fixes, output structured JSON:

` + "```json" + `
{
  "result": "Fixed issues: [summary of what was fixed]",
  "files_modified": ["path/to/file.go"],
  "files_created": [],
  "changes_summary": "Addressed reviewer feedback by...",
  "tool_calls": ["file_write", "file_read"]
}
` + "```" + `

The files_modified and tool_calls MUST reflect actual tool calls you made.

` + GapDetectionInstructions
}

// DeveloperTaskPromptParams contains the parameters for generating a task-specific developer prompt.
type DeveloperTaskPromptParams struct {
	// Task is the task to implement
	Task workflow.Task

	// Context is the pre-built context containing relevant code and documentation
	Context *workflow.ContextPayload

	// PlanTitle is the title of the parent plan (optional, for context)
	PlanTitle string

	// PlanGoal is the goal of the parent plan (optional, for context)
	PlanGoal string
}

// DeveloperTaskPrompt generates a prompt for a development agent to implement a specific task.
// This is used by task-dispatcher to provide inline context so the agent doesn't need to query.
func DeveloperTaskPrompt(params DeveloperTaskPromptParams) string {
	var sb strings.Builder

	sb.WriteString("You are implementing a development task. Follow the instructions carefully.\n\n")

	// CRITICAL tool usage reminder at the top
	sb.WriteString("## CRITICAL: You MUST Use Tools\n\n")
	sb.WriteString("You MUST call file_write to create or modify files. Do NOT describe code without writing it.\n")
	sb.WriteString("If you do not call file_write, the task has FAILED.\n\n")

	// Task header
	sb.WriteString(fmt.Sprintf("## Task: %s\n\n", params.Task.ID))

	if params.PlanGoal != "" {
		sb.WriteString(fmt.Sprintf("**Plan Goal:** %s\n\n", params.PlanGoal))
	}

	sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", params.Task.Description))
	sb.WriteString(fmt.Sprintf("**Type:** %s\n\n", params.Task.Type))

	// Scope files
	if len(params.Task.Files) > 0 {
		sb.WriteString("**Scope Files:**\n")
		for _, f := range params.Task.Files {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
		sb.WriteString("\n")
	}

	// Acceptance criteria
	if len(params.Task.AcceptanceCriteria) > 0 {
		sb.WriteString("## Acceptance Criteria\n\n")
		for i, ac := range params.Task.AcceptanceCriteria {
			sb.WriteString(fmt.Sprintf("### Criterion %d\n", i+1))
			sb.WriteString(fmt.Sprintf("- **Given:** %s\n", ac.Given))
			sb.WriteString(fmt.Sprintf("- **When:** %s\n", ac.When))
			sb.WriteString(fmt.Sprintf("- **Then:** %s\n\n", ac.Then))
		}
	}

	// Context section (inline code and documentation)
	writeContextSection(&sb, params.Context)

	// Implementation instructions
	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Review the context provided above\n")
	sb.WriteString("2. Use file_read if you need to see the current file contents\n")
	sb.WriteString("3. Call file_write to create or modify files (REQUIRED)\n")
	sb.WriteString("4. Ensure all acceptance criteria are satisfied\n")
	sb.WriteString("5. Only modify files within the scope\n\n")

	// Output format
	writeOutputFormat(&sb)

	sb.WriteString(GapDetectionInstructions)

	return sb.String()
}

// writeContextSection appends the relevant context section to the string builder.
func writeContextSection(sb *strings.Builder, ctx *workflow.ContextPayload) {
	if ctx == nil || !hasContext(ctx) {
		return
	}
	sb.WriteString("## Relevant Context\n\n")
	if ctx.TokenCount > 0 {
		sb.WriteString(fmt.Sprintf("*Context includes approximately %d tokens of reference material.*\n\n", ctx.TokenCount))
	}
	if len(ctx.SOPs) > 0 {
		sb.WriteString("### Standard Operating Procedures\n\n")
		sb.WriteString("Follow these guidelines:\n\n")
		for _, sop := range ctx.SOPs {
			sb.WriteString(sop)
			sb.WriteString("\n\n")
		}
	}
	if len(ctx.Entities) > 0 {
		sb.WriteString("### Related Entities\n\n")
		for _, entity := range ctx.Entities {
			if entity.Content != "" {
				sb.WriteString(fmt.Sprintf("#### %s (%s)\n", entity.ID, entity.Type))
				sb.WriteString("```\n")
				sb.WriteString(entity.Content)
				sb.WriteString("\n```\n\n")
			}
		}
	}
	if len(ctx.Documents) > 0 {
		sb.WriteString("### Source Files\n\n")
		for fpath, content := range ctx.Documents {
			ext := getFileExtension(fpath)
			sb.WriteString(fmt.Sprintf("#### %s\n", fpath))
			sb.WriteString(fmt.Sprintf("```%s\n", ext))
			sb.WriteString(content)
			sb.WriteString("\n```\n\n")
		}
	}
}

// writeOutputFormat appends the output format instructions to the string builder.
func writeOutputFormat(sb *strings.Builder) {
	sb.WriteString("## Response Format\n\n")
	sb.WriteString("After calling file_write to make your changes, output structured JSON:\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"result\": \"Implementation complete. [summary]\",\n")
	sb.WriteString("  \"files_modified\": [\"path/to/file.go\"],\n")
	sb.WriteString("  \"files_created\": [\"path/to/new_file.go\"],\n")
	sb.WriteString("  \"changes_summary\": \"[what was changed and why]\",\n")
	sb.WriteString("  \"criteria_satisfied\": [1, 2, 3]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("IMPORTANT: files_modified/files_created must reflect actual file_write calls you made.\n\n")
}

// hasContext returns true if the context payload has any content.
func hasContext(ctx *workflow.ContextPayload) bool {
	if ctx == nil {
		return false
	}
	return len(ctx.Documents) > 0 || len(ctx.Entities) > 0 || len(ctx.SOPs) > 0
}

// getFileExtension extracts the file extension for syntax highlighting.
func getFileExtension(path string) string {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return ""
	}
	ext := parts[len(parts)-1]

	// Map common extensions to language identifiers
	switch ext {
	case "go":
		return "go"
	case "ts", "tsx":
		return "typescript"
	case "js", "jsx":
		return "javascript"
	case "py":
		return "python"
	case "rs":
		return "rust"
	case "java":
		return "java"
	case "md":
		return "markdown"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "sql":
		return "sql"
	case "sh", "bash":
		return "bash"
	case "svelte":
		return "svelte"
	case "html":
		return "html"
	case "css":
		return "css"
	default:
		return ext
	}
}
