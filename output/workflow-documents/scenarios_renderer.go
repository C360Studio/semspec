package workflowdocuments

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderScenarios produces a BDD-style (Given/When/Then) markdown view of
// the plan's scenarios, grouped by requirement. Returns "" when the plan
// has no scenarios.
//
// Scenarios appear under H2 headings keyed to their owning requirement.
// Each scenario block uses bold labels for Given/When/Then and a bullet
// list for the Then-clauses (multiple post-conditions are the common case).
func RenderScenarios(plan *workflow.Plan) string {
	if plan == nil || len(plan.Scenarios) == 0 {
		return ""
	}
	var b strings.Builder

	title := displayTitle(plan)
	b.WriteString(fmt.Sprintf("# Scenarios: %s\n\n", title))
	b.WriteString(fmt.Sprintf("*Generated from the scenario-generator role's output. **%d scenarios** verify the implementation, grouped by the requirement they cover.*\n\n",
		len(plan.Scenarios)))

	// Group scenarios by requirement ID, preserving requirement order.
	byReq := make(map[string][]workflow.Scenario)
	var orphans []workflow.Scenario
	for _, s := range plan.Scenarios {
		if s.RequirementID == "" {
			orphans = append(orphans, s)
			continue
		}
		byReq[s.RequirementID] = append(byReq[s.RequirementID], s)
	}

	for _, req := range plan.Requirements {
		scenarios := byReq[req.ID]
		if len(scenarios) == 0 {
			continue
		}
		reqLabel := req.Title
		if reqLabel == "" {
			reqLabel = req.ID
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", reqLabel))
		if req.ID != "" {
			b.WriteString(fmt.Sprintf("*Requirement `%s` — %d scenario(s)*\n\n", req.ID, len(scenarios)))
		}
		for _, s := range scenarios {
			renderSingleScenario(&b, s)
		}
	}

	if len(orphans) > 0 {
		b.WriteString("## Unassigned scenarios\n\n")
		b.WriteString(fmt.Sprintf("*%d scenario(s) without a requirement_id link — likely an upstream defect.*\n\n",
			len(orphans)))
		for _, s := range orphans {
			renderSingleScenario(&b, s)
		}
	}

	return b.String()
}

func renderSingleScenario(b *strings.Builder, s workflow.Scenario) {
	// Scenario has no Title field — derive a heading from the When clause
	// (the imperative phrase that anchors the scenario), falling back to ID.
	heading := s.When
	if heading == "" {
		heading = s.ID
	}
	if heading == "" {
		heading = "(unnamed scenario)"
	}
	b.WriteString(fmt.Sprintf("### %s\n\n", heading))
	if s.ID != "" {
		b.WriteString(fmt.Sprintf("*ID: `%s`*\n\n", s.ID))
	}
	if s.Given != "" {
		b.WriteString(fmt.Sprintf("**Given** %s\n\n", s.Given))
	}
	if s.When != "" {
		b.WriteString(fmt.Sprintf("**When** %s\n\n", s.When))
	}
	if len(s.Then) > 0 {
		b.WriteString("**Then:**\n\n")
		for _, t := range s.Then {
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
		b.WriteString("\n")
	}
}
