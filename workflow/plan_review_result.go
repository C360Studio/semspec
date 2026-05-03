package workflow

import (
	"fmt"
	"strings"
)

// PlanReviewFinding represents a single finding from plan review. Persisted
// in plan state and consumed by both plan-manager mutations and the
// plan-reviewer parse path.
//
// Migrated 2026-04-28 from workflow/prompts/plan_reviewer.go as part of the
// Plan B persona/fragment consolidation — the type is a domain output, not a
// prompt-content artifact, so it belongs alongside the rest of workflow/.
type PlanReviewFinding struct {
	SOPID      string `json:"sop_id"`
	SOPTitle   string `json:"sop_title"`
	Severity   string `json:"severity"`
	Status     string `json:"status"`
	Category   string `json:"category,omitempty"`  // "sop" or "completeness" (ADR-029)
	Phase      string `json:"phase,omitempty"`     // "plan", "requirements", "architecture", "scenarios"
	TargetID   string `json:"target_id,omitempty"` // specific entity ID (e.g., "REQ-2", "SCEN-3")
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

// NormalizeVerdict makes the verdict structurally consistent with findings.
// The verdict is a function of error-severity violations:
//   - any error finding → "needs_changes" (upgrade if reviewer was lenient)
//   - no error findings → "approved" (downgrade if reviewer panicked)
//
// Two real failure modes this guards against:
//   - Reviewer says "needs_changes" but only has compliant/warning findings — the
//     verdict panics against its own data, so we downgrade to approved.
//   - Reviewer says "approved" but emits error-severity findings (or, in the
//     2026-05-03 openrouter @easy /health run, mentions a critical scope-path
//     mismatch in `summary` while leaving findings clean and verdict=approved).
//     The persona is required to encode plan defects as findings; when that
//     happens the verdict must reject. We upgrade to needs_changes.
func (r *PlanReviewResult) NormalizeVerdict() {
	if len(r.ErrorFindings()) > 0 {
		r.Verdict = "needs_changes"
		return
	}
	if r.Verdict == "needs_changes" {
		r.Verdict = "approved"
	}
}

// FormatFindings formats findings for display, grouped by status.
func (r *PlanReviewResult) FormatFindings() string {
	if len(r.Findings) == 0 {
		return "No findings."
	}

	var sb strings.Builder

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

	if len(violations) > 0 {
		sb.WriteString("### Violations\n\n")
		for _, f := range violations {
			fmt.Fprintf(&sb, "- **[%s]** %s\n", strings.ToUpper(f.Severity), f.SOPTitle)
			if f.Issue != "" {
				fmt.Fprintf(&sb, "  - Issue: %s\n", f.Issue)
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&sb, "  - Suggestion: %s\n", f.Suggestion)
			}
		}
		sb.WriteString("\n")
	}

	if len(compliant) > 0 {
		sb.WriteString("### Compliant\n\n")
		for _, f := range compliant {
			fmt.Fprintf(&sb, "- %s\n", f.SOPTitle)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
