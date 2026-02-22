package prompts

import (
	"fmt"
	"strings"
)

// PlanReviewerSystemPrompt returns the system prompt for the plan reviewer role.
// The plan reviewer validates plans against project SOPs before approval.
func PlanReviewerSystemPrompt() string {
	return `You are a plan reviewer validating development plans against project standards.

## Your Objective

Review the plan and verify it complies with all applicable Standard Operating Procedures (SOPs).
Your review ensures plans meet quality standards before implementation begins.

## Review Process

1. Read each SOP carefully - understand what it requires
2. Analyze the plan against each SOP requirement
3. Identify any violations or missing elements
4. Produce a verdict with detailed findings

## Verdict Criteria

**approved** - Use when ALL of the following are true:
- Plan addresses all error-severity SOP requirements
- No critical gaps in scope, goal, or context
- Migration strategies exist if required by SOPs
- Architecture decisions align with documented standards

**needs_changes** - Use when ANY of the following are true:
- Plan violates an error-severity SOP requirement
- Missing elements that are EXPLICITLY mandated by an applicable SOP (only flag what SOPs actually require — do not invent requirements like migration strategies unless an SOP explicitly demands one)
- Scope boundaries conflict with SOP constraints
- Architectural decisions contradict established patterns
- Scope includes file paths that do NOT exist in the project file tree (hallucination) — EXCEPT in greenfield projects where scope paths are files the plan intends to create (this is expected and correct)

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
      "evidence": "Quote or reference from plan supporting this finding"
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
- If no SOPs are provided, return approved with no findings
- Compare scope.include file paths against the project file tree (if provided in context)
- If scope references files that don't exist in the project, flag as an error-severity violation
- Suggest replacing non-existent scope paths with actual project files from the file tree
`
}

// PlanReviewerUserPrompt returns the user prompt for plan review.
func PlanReviewerUserPrompt(planSlug string, planContent string, sopContext string) string {
	var sb strings.Builder

	sb.WriteString("Review the following plan against the applicable SOPs.\n\n")

	// Include SOP context if provided
	if sopContext != "" {
		sb.WriteString(sopContext)
		sb.WriteString("\n")
	} else {
		sb.WriteString("No SOPs apply to this plan. Return approved verdict.\n\n")
	}

	// Include plan content
	sb.WriteString("## Plan to Review\n\n")
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n\n", planSlug))
	sb.WriteString("```json\n")
	sb.WriteString(planContent)
	sb.WriteString("\n```\n\n")

	sb.WriteString("Analyze the plan against each SOP and produce your verdict with findings.\n")

	return sb.String()
}

// PlanReviewFinding represents a single finding from plan review.
type PlanReviewFinding struct {
	SOPID      string `json:"sop_id"`
	SOPTitle   string `json:"sop_title"`
	Severity   string `json:"severity"`
	Status     string `json:"status"`
	Issue      string `json:"issue,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// PlanReviewResult is the structured output from plan review.
type PlanReviewResult struct {
	Verdict  string              `json:"verdict"`
	Summary  string              `json:"summary"`
	Findings []PlanReviewFinding `json:"findings"`
}

// IsApproved returns true if the verdict is "approved".
func (r *PlanReviewResult) IsApproved() bool {
	return r.Verdict == "approved"
}

// ErrorFindings returns only error-severity findings that are violations.
func (r *PlanReviewResult) ErrorFindings() []PlanReviewFinding {
	var errors []PlanReviewFinding
	for _, f := range r.Findings {
		if f.Severity == "error" && f.Status == "violation" {
			errors = append(errors, f)
		}
	}
	return errors
}

// FormatFindings formats findings for display.
func (r *PlanReviewResult) FormatFindings() string {
	if len(r.Findings) == 0 {
		return "No findings."
	}

	var sb strings.Builder

	// Group by status
	var violations, compliant, notApplicable []PlanReviewFinding
	for _, f := range r.Findings {
		switch f.Status {
		case "violation":
			violations = append(violations, f)
		case "compliant":
			compliant = append(compliant, f)
		default:
			notApplicable = append(notApplicable, f)
		}
	}

	// Show violations first
	if len(violations) > 0 {
		sb.WriteString("### Violations\n\n")
		for _, f := range violations {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s\n", strings.ToUpper(f.Severity), f.SOPTitle))
			if f.Issue != "" {
				sb.WriteString(fmt.Sprintf("  - Issue: %s\n", f.Issue))
			}
			if f.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("  - Suggestion: %s\n", f.Suggestion))
			}
		}
		sb.WriteString("\n")
	}

	// Show compliant items
	if len(compliant) > 0 {
		sb.WriteString("### Compliant\n\n")
		for _, f := range compliant {
			sb.WriteString(fmt.Sprintf("- %s\n", f.SOPTitle))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
