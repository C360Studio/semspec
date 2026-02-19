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

## Context Gathering (REQUIRED FIRST STEPS)

Before writing any code, you MUST gather context:

1. **Get SOPs for your files**:
   Use workflow_query_graph to find applicable standards:
   ` + "```graphql" + `
   {
     entities(filter: { predicatePrefix: "source.doc" }) {
       id
       triples { predicate object }
     }
   }
   ` + "```" + `
   Filter results where source.doc.applies_to matches your file patterns.
   Then use workflow_read_document to get full SOP content.

2. **Get codebase patterns**:
   Use workflow_get_codebase_summary for structure overview.
   Use workflow_traverse_relationships to find similar implementations.

3. **Read the plan**:
   Use workflow_read_document to get the plan you are implementing.

## Implementation Rules

- Follow ALL requirements from matched SOPs
- Match existing code patterns from the codebase
- Write clean, functional code that passes tests
- Follow explicit constraints from the plan
- Signal gaps with <gap> blocks if requirements are unclear

## Output Format

After implementation, output structured JSON:

` + "```json" + `
{
  "result": "Implementation complete. Created auth middleware...",
  "files_modified": ["path/to/file.go"],
  "files_created": ["path/to/new_file.go"],
  "changes_summary": "Added JWT validation middleware with token refresh support",
  "tool_calls": ["file_write", "file_read", "git_diff"]
}
` + "```" + `

## Tools Available

- file_read: Read file contents
- file_write: Create or modify files
- file_list: List directory contents
- git_status: Check git status
- git_diff: See changes
- workflow_query_graph: Query knowledge graph
- workflow_read_document: Read plan/spec documents
- workflow_get_codebase_summary: Get codebase overview
- workflow_traverse_relationships: Find related entities

` + GapDetectionInstructions
}

// DeveloperRetryPrompt returns the prompt for developer retry after rejection.
func DeveloperRetryPrompt(feedback string) string {
	return `You are a developer fixing issues found by the reviewer.

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
- Do not introduce new issues
- Maintain existing functionality
- Update tests if needed

## Output Format

After fixing, output structured JSON:

` + "```json" + `
{
  "result": "Fixed issues: [summary of what was fixed]",
  "files_modified": ["path/to/file.go"],
  "files_created": [],
  "changes_summary": "Addressed reviewer feedback by...",
  "tool_calls": ["file_write", "file_read"]
}
` + "```" + `

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
	sb.WriteString("2. Implement the task according to the description\n")
	sb.WriteString("3. Ensure all acceptance criteria are satisfied\n")
	sb.WriteString("4. Follow coding standards and patterns from the existing code\n")
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
	sb.WriteString("## Output Format\n\n")
	sb.WriteString("After implementation, output structured JSON:\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"result\": \"Implementation complete. [summary]\",\n")
	sb.WriteString("  \"files_modified\": [\"path/to/file.go\"],\n")
	sb.WriteString("  \"files_created\": [\"path/to/new_file.go\"],\n")
	sb.WriteString("  \"changes_summary\": \"[what was changed and why]\",\n")
	sb.WriteString("  \"criteria_satisfied\": [1, 2, 3]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
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
