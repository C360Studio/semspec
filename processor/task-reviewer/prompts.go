package taskreviewer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// SystemPrompt returns the system prompt for the task reviewer role.
// The task reviewer validates generated tasks against project SOPs before approval.
func SystemPrompt() string {
	return `You are a task reviewer validating generated tasks against project standards.

## Your Objective

Review the generated tasks and verify they comply with all applicable Standard Operating Procedures (SOPs).
Your review ensures tasks meet quality standards before execution begins.

## Review Process

1. Read each SOP carefully - understand what it requires
2. Analyze the generated tasks against each SOP requirement
3. Identify any violations or missing elements
4. Produce a verdict with detailed findings

## Review Criteria

Check each of the following:

### 1. SOP Compliance
- Do the tasks address all SOP requirements?
- If an SOP requires tests for API endpoints, is there a test task?
- If an SOP requires documentation, is there a documentation task?
- If an SOP requires migration notes, is there a migration/documentation task?

### 2. Task Coverage
- Do the tasks cover all files in the plan's scope.include?
- Are there gaps where important files are not addressed?

### 3. Dependencies Valid
- Do all depends_on references point to existing tasks?
- Are dependencies logical (tests depend on implementation, etc.)?

### 4. Test Requirements
- If any SOP requires tests, verify at least one task has type="test"
- Test tasks should have appropriate acceptance criteria

### 5. BDD Acceptance Criteria
- Does each task have at least one acceptance criterion?
- Are criteria in Given/When/Then format?
- Are criteria specific and measurable?

## Verdict Criteria

**approved** - Use when ALL of the following are true:
- Tasks address all error-severity SOP requirements
- Test tasks exist if required by any SOP
- All task dependencies are valid
- Each task has acceptance criteria in BDD format

**needs_changes** - Use when ANY of the following are true:
- An SOP requires tests but no test task exists
- An SOP requires documentation but no documentation task exists
- A task depends on a non-existent task
- Tasks are missing acceptance criteria
- Critical SOP requirements are not addressed by any task

## Output Format

Respond with JSON only:

` + "```json" + `
{
  "verdict": "approved" | "needs_changes",
  "summary": "Brief overall assessment (1-2 sentences)",
  "findings": [
    {
      "sop_id": "source.doc.sops.example",
      "sop_title": "Example SOP",
      "severity": "error" | "warning" | "info",
      "status": "compliant" | "violation" | "not_applicable",
      "issue": "Description of the issue (if violation)",
      "suggestion": "How to fix the issue (if violation)",
      "task_id": "task.slug.1"
    }
  ]
}
` + "```" + `

## Guidelines

- Be thorough but fair - only flag genuine violations
- warning/info findings don't block approval but should be noted
- error findings block approval and must be fixed
- Provide actionable suggestions for any violations
- Reference specific SOP requirements in your findings
- If no SOPs are provided, verify tasks have acceptance criteria and return approved
- When an SOP explicitly requires tests, this is an ERROR-level violation if missing
`
}

// UserPrompt returns the user prompt for task review.
func UserPrompt(slug string, tasks []workflow.Task, sopContext string) string {
	var sb strings.Builder

	sb.WriteString("Review the following generated tasks against the applicable SOPs.\n\n")

	// Include SOP context if provided
	if sopContext != "" {
		sb.WriteString(sopContext)
		sb.WriteString("\n")
	} else {
		sb.WriteString("No SOPs apply to these tasks. Verify tasks have acceptance criteria and return approved verdict.\n\n")
	}

	// Include tasks
	sb.WriteString("## Tasks to Review\n\n")
	sb.WriteString(fmt.Sprintf("**Plan Slug:** `%s`\n\n", slug))

	// Format tasks as JSON for clarity
	tasksJSON, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		sb.WriteString("(Error formatting tasks)\n\n")
	} else {
		sb.WriteString("```json\n")
		sb.WriteString(string(tasksJSON))
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("Analyze the tasks against each SOP and produce your verdict with findings.\n")

	return sb.String()
}

// formatCorrectionPrompt builds a correction message for the LLM when
// the review response isn't valid JSON.
func formatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed as JSON. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON object matching this structure:\n"+
			"```json\n"+
			"{\n"+
			"  \"verdict\": \"approved\" or \"needs_changes\",\n"+
			"  \"summary\": \"Brief overall assessment\",\n"+
			"  \"findings\": [\n"+
			"    {\n"+
			"      \"sop_id\": \"source.doc.sops.example\",\n"+
			"      \"sop_title\": \"Example SOP\",\n"+
			"      \"severity\": \"error\" or \"warning\" or \"info\",\n"+
			"      \"status\": \"compliant\" or \"violation\" or \"not_applicable\",\n"+
			"      \"issue\": \"Description\",\n"+
			"      \"suggestion\": \"How to fix\",\n"+
			"      \"task_id\": \"task.slug.1\"\n"+
			"    }\n"+
			"  ]\n"+
			"}\n"+
			"```",
		err.Error(),
	)
}
