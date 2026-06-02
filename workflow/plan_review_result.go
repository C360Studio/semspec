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
//
// Action / TargetField / TargetValue carry the structured remediation
// directive (added 2026-05-14 after take-24 hybrid/hard escalation). The
// finding's prose Suggestion is human-readable but bidirectional ("ensure
// consistency between scope.create and files_owned" can be satisfied in
// EITHER direction). The downstream regen LLM (requirement-generator,
// scenario-generator, architect, planner) consumes findings as prose and
// must infer both the action verb and the target field — when the prose
// is ambiguous a non-deterministic model picks the smaller mutation, which
// is often the wrong direction. Take-24's reviewer asked to "Add X to
// scope.create AND ensure consistency"; regen REMOVED X from
// requirement.files_owned and the loop escalated. Action+TargetField+
// TargetValue let the reviewer commit to one direction at the source so
// downstream regen has nothing to misinterpret.
type PlanReviewFinding struct {
	SOPID    string `json:"sop_id"`
	SOPTitle string `json:"sop_title"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Category string `json:"category,omitempty"`  // "sop" or "completeness" (ADR-029)
	Phase    string `json:"phase,omitempty"`     // "plan", "requirements", "architecture", "stories", "scenarios"
	TargetID string `json:"target_id,omitempty"` // specific entity ID (e.g., "REQ-2", "SCEN-3")
	// Action is the imperative remediation verb. Required on every error-
	// severity violation when verdict=needs_changes. Allowed values: "add",
	// "remove", "rename", "replace", "move". Empty on compliant/info
	// findings (no remediation needed) and on warnings the reviewer
	// chose not to commit a direction on.
	Action string `json:"action,omitempty"`
	// TargetField is the SINGLE plan field the Action mutates. Examples:
	// "scope.create", "scope.include", "requirement.<id>.files_owned",
	// "architecture.decisions", "scenario.<id>.given". Required whenever
	// Action is set. Multi-field "ensure consistency between A and B"
	// guidance is forbidden — pick ONE side and commit.
	TargetField string `json:"target_field,omitempty"`
	// TargetValue is the value being added/removed/renamed. For "add"
	// it's the new entry; for "remove" the entry to drop; for "rename"
	// or "replace" the format is "old → new". Required whenever Action
	// is set.
	TargetValue string `json:"target_value,omitempty"`
	Issue       string `json:"issue,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
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
//
// The violations block is what downstream generators (req-gen, arch-gen,
// scen-gen) consume on revision rounds — every dropped field is a thread
// of context the next-round model must reconstruct from prose. Take 9
// (2026-05-08) confirmed this: SOPTitle="n/a" for completeness findings
// produced "[ERROR] n/a" bullet headers, and target_id/phase/evidence
// were stripped entirely, so scen-gen could not pin its fix to a
// specific scenario or know which phase to retarget. The format below
// is structured-prose: the bullet header is meaningful even when
// SOPTitle is empty (category + phase carry the topic), and the
// evidence quote is preserved verbatim for the model to anchor on.
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
			writeViolationFinding(&sb, f)
		}
	}

	if len(compliant) > 0 {
		sb.WriteString("### Compliant\n\n")
		for _, f := range compliant {
			fmt.Fprintf(&sb, "- %s\n", findingHeader(f))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// writeViolationFinding renders one violation as structured prose
// preserving every diagnostic field. The order is fixed so model
// attention is consistent across findings within a round and across
// rounds: header → ACTION DIRECTIVE → issue → evidence → suggestion.
// Evidence is the reviewer's verbatim quote of the inconsistency.
//
// Action is rendered FIRST when present so the regen LLM sees the
// committed remediation direction before any prose that might be
// bidirectional. The Suggestion field is still surfaced (it carries
// the reviewer's reasoning) but it's preceded by the directive — so
// even if the suggestion language drifts toward "ensure consistency"
// the regen has an unambiguous instruction to anchor on. Format is
// "Action: ADD `value` TO `field`" — the verb is uppercase and the
// value/field are backtick-quoted to survive copy-paste through the
// model's tokenization.
func writeViolationFinding(sb *strings.Builder, f PlanReviewFinding) {
	fmt.Fprintf(sb, "- **[%s]** %s\n", strings.ToUpper(f.Severity), findingHeader(f))
	if f.Action != "" {
		fmt.Fprintf(sb, "  - Action: %s\n", formatActionDirective(f))
	}
	if f.Issue != "" {
		fmt.Fprintf(sb, "  - Issue: %s\n", f.Issue)
	}
	if f.Evidence != "" {
		fmt.Fprintf(sb, "  - Evidence: %s\n", f.Evidence)
	}
	if f.Suggestion != "" {
		fmt.Fprintf(sb, "  - Suggestion: %s\n", f.Suggestion)
	}
	sb.WriteString("\n")
}

// formatActionDirective renders the Action / TargetField / TargetValue
// triple as an imperative directive. Falls back gracefully when only
// some fields are populated (older payloads or partial reviewer output)
// — the verb alone is still a stronger signal than prose suggestion
// when the field/value couldn't be committed. Examples:
//   - ADD `MeshtasticConnection.java` TO `scope.create`
//   - REMOVE `requirement.X.2.files_owned[2]` (no field/value)
//   - RENAME `old → new` IN `architecture.decisions[ARCH-001]`
//
// The verb is uppercased to make it visually stand out from the
// surrounding prose; downstream regen prompts cite this rendering
// shape in their meta-rule.
func formatActionDirective(f PlanReviewFinding) string {
	verb := strings.ToUpper(strings.TrimSpace(f.Action))
	value := strings.TrimSpace(f.TargetValue)
	field := strings.TrimSpace(f.TargetField)

	switch {
	case value != "" && field != "":
		// Most informative shape — both endpoints committed.
		switch verb {
		case "ADD":
			return fmt.Sprintf("ADD `%s` TO `%s`", value, field)
		case "REMOVE":
			return fmt.Sprintf("REMOVE `%s` FROM `%s`", value, field)
		case "RENAME", "REPLACE":
			return fmt.Sprintf("%s `%s` IN `%s`", verb, value, field)
		case "MOVE":
			return fmt.Sprintf("MOVE `%s` TO `%s`", value, field)
		default:
			return fmt.Sprintf("%s `%s` IN `%s`", verb, value, field)
		}
	case value != "":
		return fmt.Sprintf("%s `%s`", verb, value)
	case field != "":
		return fmt.Sprintf("%s in `%s`", verb, field)
	default:
		return verb
	}
}

// findingHeader composes a meaningful bullet header even when SOPTitle
// is "n/a" (completeness findings, which carry no SOP). Format:
//   - "category=completeness phase=requirements target=scenario.X.1.1"
//   - "<sop-title> [phase=architecture target=integration.health]"
//
// Empty fields are omitted. Falls back to "(no detail)" only when every
// component is empty — which would itself be a malformed finding.
func findingHeader(f PlanReviewFinding) string {
	var parts []string
	switch {
	case f.SOPTitle != "" && f.SOPTitle != "n/a":
		parts = append(parts, f.SOPTitle)
	case f.Category != "":
		parts = append(parts, "category="+f.Category)
	}
	if f.Phase != "" {
		parts = append(parts, "phase="+f.Phase)
	}
	if f.TargetID != "" {
		parts = append(parts, "target="+f.TargetID)
	}
	if len(parts) == 0 {
		return "(no detail)"
	}
	return strings.Join(parts, " ")
}
