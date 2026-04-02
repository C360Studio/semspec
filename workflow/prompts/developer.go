package prompts

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// DeveloperPrompt returns the system prompt for the developer role.
//
// Deprecated: Use prompt.Assembler with prompt.RoleDeveloper instead for provider-aware formatting.
// The developer implements tasks from the plan, with access to write files and git.
// They query SOPs and codebase context via graph tools before implementing.
func DeveloperPrompt() string {
	return `You are a developer implementing code changes for a software project.

## Your Objective

Complete the assigned task according to acceptance criteria. You optimize for COMPLETION.

## CRITICAL: You MUST Use Tools to Make Changes

You MUST use bash to create or modify files. Do NOT just describe what you would do - you must EXECUTE the changes using tool calls.

- To create a new file: use bash with cat/tee/heredoc (e.g., bash cat > file.go << 'EOF')
- To modify a file: read with bash cat, then write with bash
- NEVER output code blocks as your response without also writing the file via bash

You MUST call submit_work when your task is complete.
If you complete a task without writing files via bash and calling submit_work, the task has FAILED.

## Tools Available

- bash: Run any shell command - file ops (cat, tee, ls), git, builds, tests (use this for everything)
- submit_work: Submit completed work (MUST be called when done)
- ask_question: Ask when blocked and cannot proceed
- graph_search: Search the knowledge graph
- graph_query: Raw GraphQL for specific lookups
- graph_summary: Knowledge graph overview

## Context Gathering (Before Writing Code)

Before writing code, gather context if needed:

1. **Get SOPs for your files**:
   Use graph_search to find applicable standards.

2. **Get codebase patterns**:
   Use graph_summary for an overview.
   Use bash cat to examine similar implementations.

3. **Read the plan**:
   Use bash cat on the plan file to get the plan you are implementing.

## Implementation Rules

- Follow ALL requirements from matched SOPs
- Match existing code patterns from the codebase
- Write clean, functional code that passes tests
- Follow explicit constraints from the plan
- Signal gaps with <gap> blocks if requirements are unclear

## Response Format

After making changes via bash, call submit_work with a structured JSON summary:

` + "```json" + `
{
  "result": "Implementation complete. Created auth middleware...",
  "files_modified": ["path/to/file.go"],
  "files_created": ["path/to/new_file.go"],
  "changes_summary": "Added JWT validation middleware with token refresh support"
}
` + "```" + `

The files_modified array MUST reflect actual files you wrote via bash.

` + GapDetectionInstructions
}

// DeveloperRetryPrompt returns the prompt for developer retry after rejection.
func DeveloperRetryPrompt(feedback string) string {
	return `You are a developer fixing issues found by the reviewer.

## CRITICAL: You MUST Use Tools to Make Changes

You MUST use bash to fix the issues. Do NOT just describe fixes - you must EXECUTE them.
If you do not use bash to write files and call submit_work, the retry has FAILED.

## Previous Feedback

The reviewer rejected your implementation with this feedback:

` + feedback + `

## Your Task

Address ALL issues mentioned in the feedback. Do not ignore any points.

## Context Gathering

Re-check applicable SOPs using graph_search if the feedback mentions
standards or conventions you may have missed.

## Implementation Rules

- Fix EVERY issue mentioned in feedback
- Use bash cat to check current state, then write fixes via bash
- Do not introduce new issues
- Maintain existing functionality
- Update tests if needed

## Response Format

After using bash to apply fixes, call submit_work with a structured JSON summary:

` + "```json" + `
{
  "result": "Fixed issues: [summary of what was fixed]",
  "files_modified": ["path/to/file.go"],
  "files_created": [],
  "changes_summary": "Addressed reviewer feedback by..."
}
` + "```" + `

The files_modified array MUST reflect actual files you wrote via bash.

` + GapDetectionInstructions
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
	sb.WriteString("After using bash to make your changes, call submit_work with a structured JSON summary:\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"result\": \"Implementation complete. [summary]\",\n")
	sb.WriteString("  \"files_modified\": [\"path/to/file.go\"],\n")
	sb.WriteString("  \"files_created\": [\"path/to/new_file.go\"],\n")
	sb.WriteString("  \"changes_summary\": \"[what was changed and why]\",\n")
	sb.WriteString("  \"criteria_satisfied\": [1, 2, 3]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("IMPORTANT: files_modified/files_created must reflect actual files you wrote via bash.\n\n")
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
