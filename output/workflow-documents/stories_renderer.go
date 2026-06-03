package workflowdocuments

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderStories produces a markdown view of Sarah's per-Story narratives,
// grouped by the parent Requirement. ADR-043 added Stories as the unit of
// dev work between Requirements and Tasks; until this renderer landed
// (smoke 6 follow-up, 2026-06-02) Story data was only visible via
// plan.json and the "Applies To" section of OpenSpec spec.md. The
// stories.md artifact makes Sarah's contribution legible alongside the
// other persona outputs in `.semspec/plans/<slug>/`.
//
// Returns "" when the plan has no Stories — the file is skipped on
// pre-Sarah plans (mock fixtures, ADR-043 PR-3 dormant runs).
func RenderStories(plan *workflow.Plan) string {
	if plan == nil || len(plan.Stories) == 0 {
		return ""
	}
	var b strings.Builder

	title := displayTitle(plan)
	b.WriteString(fmt.Sprintf("# Stories: %s\n\n", title))
	b.WriteString(fmt.Sprintf("*Generated from the story-preparer role (Sarah). **%d stories** ready the per-Requirement work for the executor pipeline, each carrying its own Tasks checklist and FilesOwned scope.*\n\n",
		len(plan.Stories)))

	// Group stories by primary requirement ID, preserving requirement order.
	// ADR-044: Stories carry M:N RequirementIDs; use PrimaryRequirementID for
	// the legacy per-requirement grouping in this renderer.
	// TODO ADR-044 commit 3+: render M:N coverage joins properly.
	byReq := make(map[string][]workflow.Story)
	var orphans []workflow.Story
	for _, s := range plan.Stories {
		rid := s.PrimaryRequirementID()
		if rid == "" {
			orphans = append(orphans, s)
			continue
		}
		byReq[rid] = append(byReq[rid], s)
	}

	for _, req := range plan.Requirements {
		stories := byReq[req.ID]
		if len(stories) == 0 {
			continue
		}
		reqLabel := req.Title
		if reqLabel == "" {
			reqLabel = req.ID
		}
		fmt.Fprintf(&b, "## %s\n\n", reqLabel)
		if req.ID != "" {
			fmt.Fprintf(&b, "*Requirement `%s` — %d story(ies)*\n\n", req.ID, len(stories))
		}
		for _, s := range stories {
			writeStory(&b, s)
		}
	}

	if len(orphans) > 0 {
		b.WriteString("## Unassigned stories\n\n")
		b.WriteString("*These stories have no parent Requirement — investigate, the plan-reviewer R3 round should have rejected them.*\n\n")
		for _, s := range orphans {
			writeStory(&b, s)
		}
	}

	return b.String()
}

// writeStory emits a single Story block: title, intent, components,
// files_owned, depends_on, and the Task checklist.
func writeStory(b *strings.Builder, s workflow.Story) {
	heading := s.Title
	if heading == "" {
		heading = s.ID
	}
	fmt.Fprintf(b, "### %s\n\n", heading)
	if s.ID != "" {
		fmt.Fprintf(b, "*`%s`*\n\n", s.ID)
	}
	if s.Intent != "" {
		fmt.Fprintf(b, "%s\n\n", s.Intent)
	}
	if len(s.Components) > 0 {
		fmt.Fprintf(b, "**Components:** %s\n\n", strings.Join(s.Components, ", "))
	}
	if len(s.FilesOwned) > 0 {
		b.WriteString("**Files owned:**\n\n")
		for _, f := range s.FilesOwned {
			fmt.Fprintf(b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}
	if len(s.DependsOn) > 0 {
		b.WriteString("**Depends on:**\n\n")
		for _, d := range s.DependsOn {
			fmt.Fprintf(b, "- `%s`\n", d)
		}
		b.WriteString("\n")
	}
	if len(s.Tasks) > 0 {
		b.WriteString("**Tasks:**\n\n")
		for _, t := range s.Tasks {
			desc := t.Description
			if desc == "" {
				desc = t.ID
			}
			fmt.Fprintf(b, "- `%s` — %s\n", t.ID, desc)
		}
		b.WriteString("\n")
	}
}
