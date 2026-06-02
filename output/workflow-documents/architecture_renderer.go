package workflowdocuments

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderArchitecture produces a BMAD/OpenSpec-style markdown view of the
// plan's ArchitectureDocument. Returns the empty string when the plan has
// no architecture attached (pre-architecture-generated phase, or
// SkipArchitecture=true plans). The caller is expected to skip writing
// when the result is empty.
//
// Sections rendered: technology_choices, component_boundaries (with
// upstream_refs), data_flow, decisions, actors, integrations,
// upstream_resolutions, harness_profiles, test_surface. Mirrors the structure the sponsor package's
// architecture.md uses, but rendered inline as a per-phase artifact
// at the architecture_generated milestone.
func RenderArchitecture(plan *workflow.Plan) string {
	if plan == nil || plan.Architecture == nil {
		return ""
	}
	arch := plan.Architecture
	var b strings.Builder

	renderArchitectureHeader(&b, plan)
	renderArchTechnologyChoices(&b, arch.TechnologyChoices)
	renderArchComponentBoundaries(&b, arch.ComponentBoundaries)
	renderArchDataFlow(&b, arch.DataFlow)
	renderArchDecisions(&b, arch.Decisions)
	renderArchActors(&b, arch.Actors)
	renderArchIntegrations(&b, arch.Integrations)
	renderArchUpstreamResolutions(&b, arch.UpstreamResolutions)
	renderArchHarnessProfiles(&b, arch.HarnessProfiles)
	renderArchTestSurface(&b, arch.TestSurface)

	return b.String()
}

func renderArchitectureHeader(b *strings.Builder, plan *workflow.Plan) {
	title := displayTitle(plan)
	b.WriteString(fmt.Sprintf("# Architecture: %s\n\n", title))
	b.WriteString("*Generated from the architect role's structured deliverable. The architecture is the bridge between the goal and the implementation.*\n\n")
}

func renderArchTechnologyChoices(b *strings.Builder, choices []workflow.TechChoice) {
	if len(choices) == 0 {
		return
	}
	b.WriteString("## Technology choices\n\n")
	b.WriteString("| Category | Choice | Rationale |\n")
	b.WriteString("|---|---|---|\n")
	for _, c := range choices {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
			escapePipe(c.Category), escapePipe(c.Choice), escapePipe(c.Rationale)))
	}
	b.WriteString("\n")
}

func renderArchComponentBoundaries(b *strings.Builder, comps []workflow.ComponentDef) {
	if len(comps) == 0 {
		return
	}
	b.WriteString("## Component boundaries\n\n")
	for _, c := range comps {
		b.WriteString(fmt.Sprintf("### %s\n\n", c.Name))
		if c.Responsibility != "" {
			b.WriteString(c.Responsibility)
			b.WriteString("\n\n")
		}
		if len(c.Dependencies) > 0 {
			b.WriteString(fmt.Sprintf("- **Internal dependencies:** %s\n",
				strings.Join(c.Dependencies, ", ")))
		}
		if len(c.UpstreamRefs) > 0 {
			b.WriteString(fmt.Sprintf("- **Upstream refs:** %s\n",
				strings.Join(c.UpstreamRefs, ", ")))
		}
		b.WriteString("\n")
	}
}

func renderArchDataFlow(b *strings.Builder, flow string) {
	if flow == "" {
		return
	}
	b.WriteString("## Data flow\n\n")
	b.WriteString(flow)
	b.WriteString("\n\n")
}

func renderArchDecisions(b *strings.Builder, decisions []workflow.ArchDecision) {
	if len(decisions) == 0 {
		return
	}
	b.WriteString("## Architectural decisions\n\n")
	for _, d := range decisions {
		b.WriteString(fmt.Sprintf("### %s: %s\n\n", d.ID, d.Title))
		if d.Decision != "" {
			b.WriteString(fmt.Sprintf("**Decision:** %s\n\n", d.Decision))
		}
		if d.Rationale != "" {
			b.WriteString(fmt.Sprintf("**Rationale:** %s\n\n", d.Rationale))
		}
	}
}

func renderArchActors(b *strings.Builder, actors []workflow.ActorDef) {
	if len(actors) == 0 {
		return
	}
	b.WriteString("## Actors\n\n")
	for _, a := range actors {
		b.WriteString(fmt.Sprintf("- **%s** (%s)", a.Name, a.Type))
		if len(a.Triggers) > 0 {
			b.WriteString(fmt.Sprintf(" — triggers: %s", strings.Join(a.Triggers, ", ")))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func renderArchIntegrations(b *strings.Builder, ints []workflow.IntegrationPoint) {
	b.WriteString("## Integrations\n\n")
	if len(ints) == 0 {
		b.WriteString("*None declared — pure-library shape on this plan itself (no external boundaries to map).*\n\n")
		return
	}
	b.WriteString("| Name | Direction | Protocol | Contract | Error mode |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, i := range ints {
		contract := i.Contract
		if contract == "" {
			contract = "—"
		}
		errMode := i.ErrorMode
		if errMode == "" {
			errMode = "—"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			escapePipe(i.Name), i.Direction, escapePipe(i.Protocol),
			escapePipe(contract), escapePipe(errMode)))
	}
	b.WriteString("\n")
}

func renderArchUpstreamResolutions(b *strings.Builder, ur []workflow.UpstreamResolution) {
	if len(ur) == 0 {
		return
	}
	b.WriteString("## Upstream resolutions\n\n")
	b.WriteString("Every external library, API, or framework the implementation depends on. The architect classifies each one's role; service-style integrations are covered by catalog-backed harness profiles.\n\n")
	for _, r := range ur {
		b.WriteString(fmt.Sprintf("### %s\n\n", r.Name))
		b.WriteString(fmt.Sprintf("- **Coordinate:** `%s`\n", r.Coordinate))
		if r.Role != "" {
			b.WriteString(fmt.Sprintf("- **Role:** `%s`\n", r.Role))
		}
		if r.SourceRef != "" {
			b.WriteString(fmt.Sprintf("- **Source ref:** %s\n", r.SourceRef))
		}
		if len(r.UsedBy) > 0 {
			b.WriteString(fmt.Sprintf("- **Used by:** %s\n", strings.Join(r.UsedBy, ", ")))
		}
		if len(r.APIs) > 0 {
			b.WriteString(fmt.Sprintf("- **API surfaces consumed (%d):**\n", len(r.APIs)))
			const maxSurfacesShown = 5
			shown := r.APIs
			truncated := false
			if len(shown) > maxSurfacesShown {
				shown = shown[:maxSurfacesShown]
				truncated = true
			}
			for _, a := range shown {
				b.WriteString(fmt.Sprintf("  - `%s` (%s)", a.Symbol, a.Kind))
				if a.Signature != "" {
					b.WriteString(fmt.Sprintf(" — `%s`", a.Signature))
				}
				b.WriteString("\n")
				if a.Citation != "" {
					b.WriteString(fmt.Sprintf("    *cited from %s*\n", a.Citation))
				}
			}
			if truncated {
				b.WriteString(fmt.Sprintf("  - *(+ %d more)*\n", len(r.APIs)-maxSurfacesShown))
			}
		}
		b.WriteString("\n")
	}
}

func renderArchHarnessProfiles(b *strings.Builder, profiles []workflow.HarnessProfileSelection) {
	b.WriteString("## Harness profiles\n\n")
	if len(profiles) == 0 {
		b.WriteString("*None selected — no catalog-backed integration harness needed for this architecture.*\n\n")
		return
	}
	b.WriteString("| Profile ID | Used by | Purpose | Covers |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, p := range profiles {
		usedBy := "—"
		if len(p.UsedBy) > 0 {
			usedBy = strings.Join(p.UsedBy, ", ")
		}
		covers := "—"
		if len(p.Covers) > 0 {
			covers = strings.Join(p.Covers, ", ")
		}
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
			escapePipe(p.ProfileID), escapePipe(usedBy), escapePipe(p.Purpose), escapePipe(covers)))
	}
	b.WriteString("\n")
}

func renderArchTestSurface(b *strings.Builder, ts *workflow.TestSurface) {
	if ts == nil {
		return
	}
	intf := ts.IntegrationFlows
	e2ef := ts.E2EFlows
	if len(intf) == 0 && len(e2ef) == 0 {
		return
	}
	b.WriteString("## Test surface\n\n")
	if len(intf) > 0 {
		b.WriteString("### Integration flows\n\n")
		for _, f := range intf {
			b.WriteString(fmt.Sprintf("- **%s**", f.Name))
			if len(f.ComponentsInvolved) > 0 {
				b.WriteString(fmt.Sprintf(" — components: %s",
					strings.Join(f.ComponentsInvolved, ", ")))
			}
			b.WriteString("\n")
			if f.Description != "" {
				b.WriteString(fmt.Sprintf("  - %s\n", f.Description))
			}
		}
		b.WriteString("\n")
	}
	if len(e2ef) > 0 {
		b.WriteString("### End-to-end flows\n\n")
		for _, f := range e2ef {
			b.WriteString(fmt.Sprintf("- Actor **%s** — %d step(s), %d success criteria\n",
				f.Actor, len(f.Steps), len(f.SuccessCriteria)))
		}
		b.WriteString("\n")
	}
}

// escapePipe replaces "|" in cell content with "\|" so markdown tables
// don't break on values containing pipes. Newlines collapse to spaces.
func escapePipe(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
