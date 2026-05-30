package openspec

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderTasks produces the OpenSpec tasks.md for a plan with live
// `- [ ]` / `- [x]` checkbox state derived from the per-requirement
// execution stages.
//
// Tasks are grouped by capability so the checklist matches the proposal's
// shape. Each requirement is rendered as one top-level task. When the
// req has scenarios, those become sub-tasks with their own checkbox state.
//
// Checkbox semantics:
//   - Requirement: checked when the matching RequirementExecution.Stage is
//     "completed". Anything else (executing, reviewing, failed, error,
//     unstarted) renders as unchecked.
//   - Scenario: checked when Scenario.Status is "passing".
//
// Empty execs map = pre-execution rendering; everything renders unchecked.
// Returns "" when the plan has no Exploration (legacy plans).
func RenderTasks(plan *workflow.Plan, execs map[string]workflow.RequirementExecution) string {
	if plan == nil || plan.Exploration == nil || len(plan.Exploration.Capabilities) == 0 {
		return ""
	}
	var sb strings.Builder
	title := plan.Title
	if title == "" {
		title = plan.Slug
	}
	fmt.Fprintf(&sb, "# Tasks: %s\n\n", title)

	for _, c := range plan.Exploration.Capabilities {
		var reqs []workflow.Requirement
		for _, r := range plan.Requirements {
			if r.CapabilityName == c.Name {
				reqs = append(reqs, r)
			}
		}
		if len(reqs) == 0 {
			// Capability without an implementing requirement — render the
			// section header anyway so the missing-task gap is visible.
			//
			// Note: plan-reviewer's capability_orphan rule (PR 2) is
			// supposed to reject plans in this shape before they reach
			// emission. This branch is defensive — fires only when a plan
			// slipped through (e.g. operator-edited PLAN_STATES bypassing
			// the mutation handler) and we still want tasks.md to flag the
			// gap rather than silently omit the capability.
			fmt.Fprintf(&sb, "## %s\n\n_(no implementing requirement yet)_\n\n", c.Name)
			continue
		}
		fmt.Fprintf(&sb, "## %s\n\n", c.Name)
		for _, r := range reqs {
			reqChecked := isRequirementComplete(execs, r.ID)
			fmt.Fprintf(&sb, "- %s %s (`%s`)\n", checkbox(reqChecked), r.Title, r.ID)
			scenarios := plan.ScenariosForRequirement(r.ID)
			for _, s := range scenarios {
				scenChecked := s.Status == workflow.ScenarioStatusPassing
				fmt.Fprintf(&sb, "  - %s %s\n", checkbox(scenChecked), describeScenario(s))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func isRequirementComplete(execs map[string]workflow.RequirementExecution, reqID string) bool {
	if execs == nil {
		return false
	}
	e, ok := execs[reqID]
	if !ok {
		return false
	}
	return e.Stage == "completed"
}

func checkbox(checked bool) string {
	if checked {
		return "[x]"
	}
	return "[ ]"
}

// describeScenario returns a one-line label for a scenario. Prefers the
// WHEN clause as the most descriptive action; falls back to GIVEN or the
// scenario ID when WHEN is empty.
func describeScenario(s workflow.Scenario) string {
	switch {
	case s.When != "":
		return s.When
	case s.Given != "":
		return s.Given
	default:
		return s.ID
	}
}
