package openspec

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderDesign produces the OpenSpec design.md for a plan. Returns "" when
// the plan has no Architecture (the architecture-generator phase hasn't run
// or was skipped via Plan.SkipArchitecture).
//
// Structure:
//
//	# Design: <plan title>
//
//	## Technology Choices
//	| Category | Choice | Rationale |
//
//	## Components
//	### <component name>
//	Responsibility / Dependencies / Upstream refs
//
//	## Decisions
//	### <id>: <title>
//	**Decision**: <text>
//	**Rationale**: <text>
//
//	## Test Harness Profiles
//	- `<profile-id>` — used by <component>: <purpose>
//
//	## Integration Points
//	| Name | Direction | Protocol | Contract | Error Mode |
//
// Each section only emits when its source slice is non-empty so the
// document stays readable on partial architectures.
func RenderDesign(plan *workflow.Plan) string {
	if plan == nil || plan.Architecture == nil {
		return ""
	}
	arch := plan.Architecture

	var sb strings.Builder
	title := plan.Title
	if title == "" {
		title = plan.Slug
	}
	fmt.Fprintf(&sb, "# Design: %s\n\n", title)

	if len(arch.TechnologyChoices) > 0 {
		sb.WriteString("## Technology Choices\n\n")
		sb.WriteString("| Category | Choice | Rationale |\n")
		sb.WriteString("|---|---|---|\n")
		for _, t := range arch.TechnologyChoices {
			fmt.Fprintf(&sb, "| %s | %s | %s |\n", t.Category, t.Choice, t.Rationale)
		}
		sb.WriteString("\n")
	}

	if arch.DataFlow != "" {
		sb.WriteString("## Data Flow\n\n")
		sb.WriteString(arch.DataFlow)
		sb.WriteString("\n\n")
	}

	if len(arch.ComponentBoundaries) > 0 {
		sb.WriteString("## Components\n\n")
		for _, c := range arch.ComponentBoundaries {
			fmt.Fprintf(&sb, "### %s\n\n", c.Name)
			if c.Responsibility != "" {
				fmt.Fprintf(&sb, "**Responsibility**: %s\n\n", c.Responsibility)
			}
			if len(c.Dependencies) > 0 {
				fmt.Fprintf(&sb, "**Dependencies**: %s\n\n", strings.Join(c.Dependencies, ", "))
			}
			if len(c.UpstreamRefs) > 0 {
				fmt.Fprintf(&sb, "**Upstream refs**: %s\n\n", strings.Join(c.UpstreamRefs, ", "))
			}
		}
	}

	if len(arch.Decisions) > 0 {
		sb.WriteString("## Decisions\n\n")
		for _, d := range arch.Decisions {
			fmt.Fprintf(&sb, "### %s: %s\n\n", d.ID, d.Title)
			if d.Decision != "" {
				fmt.Fprintf(&sb, "**Decision**: %s\n\n", d.Decision)
			}
			if d.Rationale != "" {
				fmt.Fprintf(&sb, "**Rationale**: %s\n\n", d.Rationale)
			}
		}
	}

	if len(arch.HarnessProfiles) > 0 {
		sb.WriteString("## Test Harness Profiles\n\n")
		for _, h := range arch.HarnessProfiles {
			line := fmt.Sprintf("- `%s`", h.ProfileID)
			if len(h.UsedBy) > 0 {
				line += " — used by " + strings.Join(h.UsedBy, ", ")
			}
			if h.Purpose != "" {
				line += ": " + h.Purpose
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(arch.Integrations) > 0 {
		sb.WriteString("## Integration Points\n\n")
		sb.WriteString("| Name | Direction | Protocol | Contract | Error Mode |\n")
		sb.WriteString("|---|---|---|---|---|\n")
		for _, ip := range arch.Integrations {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n",
				ip.Name, ip.Direction, ip.Protocol, ip.Contract, ip.ErrorMode)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
