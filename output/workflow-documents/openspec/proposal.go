package openspec

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderProposal produces the OpenSpec proposal.md for a plan. Returns ""
// when the plan has no Exploration (legacy plans). Structure mirrors
// OpenSpec's adopter expectations:
//
//	# Proposal: <plan title>
//
//	## Why
//	<Plan.Goal — what we're building/fixing and why>
//
//	## What Changes
//	### New Capabilities
//	- `cap-name` — description
//	### Modified Capabilities
//	- `cap-name` — description
//
//	## Open Questions
//	- <analyst-flagged question>
//
//	## Capability Dependencies
//	- `cap-a` depends on `cap-b`
//
// The "What Changes" sections only emit when at least one capability of
// that lifecycle is present. Open Questions and Dependencies sections only
// emit when populated.
func RenderProposal(plan *workflow.Plan) string {
	if plan == nil || plan.Exploration == nil || len(plan.Exploration.Capabilities) == 0 {
		return ""
	}
	var sb strings.Builder

	title := plan.Title
	if title == "" {
		title = plan.Slug
	}
	fmt.Fprintf(&sb, "# Proposal: %s\n\n", title)

	if plan.Goal != "" {
		sb.WriteString("## Why\n\n")
		sb.WriteString(plan.Goal)
		sb.WriteString("\n\n")
	}

	var newCaps, modCaps []workflow.Capability
	for _, c := range plan.Exploration.Capabilities {
		switch c.Lifecycle {
		case workflow.CapabilityModified:
			modCaps = append(modCaps, c)
		default:
			// Default lifecycle is "new" — covers both explicit and empty.
			newCaps = append(newCaps, c)
		}
	}

	if len(newCaps) > 0 || len(modCaps) > 0 {
		sb.WriteString("## What Changes\n\n")
		if len(newCaps) > 0 {
			sb.WriteString("### New Capabilities\n\n")
			for _, c := range newCaps {
				fmt.Fprintf(&sb, "- `%s` — %s\n", c.Name, c.Description)
			}
			sb.WriteString("\n")
		}
		if len(modCaps) > 0 {
			sb.WriteString("### Modified Capabilities\n\n")
			for _, c := range modCaps {
				fmt.Fprintf(&sb, "- `%s` — %s\n", c.Name, c.Description)
			}
			sb.WriteString("\n")
		}
	}

	// Capability dependency edges. Surfaced as a flat list rather than a
	// graph diagram so the markdown stays git-diff friendly.
	var depLines []string
	for _, c := range plan.Exploration.Capabilities {
		for _, dep := range c.DependsOn {
			depLines = append(depLines, fmt.Sprintf("- `%s` depends on `%s`", c.Name, dep))
		}
	}
	if len(depLines) > 0 {
		sb.WriteString("## Capability Dependencies\n\n")
		for _, line := range depLines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(plan.Exploration.OpenQuestions) > 0 {
		sb.WriteString("## Open Questions\n\n")
		for _, q := range plan.Exploration.OpenQuestions {
			fmt.Fprintf(&sb, "- %s\n", q)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
