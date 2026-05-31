package openspec

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// RenderSpec produces the OpenSpec specs/<capName>/spec.md for a single
// capability. Returns "" when no Requirements claim the capability (the
// caller skips writing the file).
//
// Structure (per ADR-041 PR 6 — tag-aware round-trip syntax):
//
//	# Spec: <cap name>
//
//	## Overview
//	<capability description>
//
//	## Applies To
//	- path/to/file.go
//	- path/to/test.go
//
//	## Requirements
//	### <req title>
//	SHALL/MUST normative text...
//
//	#### Scenario: <scenario title>
//
//	`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`
//
//	**GIVEN** ...
//	**WHEN** ...
//	**THEN** ...
//	**AND** ...
//
// The tag line is omitted when the scenario carries no tags (legacy
// pre-ADR-041 plans render without the line, preserving back-compat).
// The harness binding (`· harness: <ids>`) is omitted when the scenario
// has no HarnessProfileIDs. Title falls back to a synthesized "When X"
// when the scenario was emitted before workflow.Scenario.Title existed.
//
// applies_to is derived from the union of FilesOwned across all
// Requirements owning this capability.
func RenderSpec(plan *workflow.Plan, capName string) string {
	if plan == nil || plan.Exploration == nil {
		return ""
	}
	capability, _ := plan.Exploration.FindCapability(capName)
	if capability == nil {
		return ""
	}
	var reqs []workflow.Requirement
	for _, r := range plan.Requirements {
		if r.CapabilityName == capName {
			reqs = append(reqs, r)
		}
	}
	if len(reqs) == 0 {
		// No implementing Requirements — caller treats empty return as
		// "skip emitting spec.md for this capability". plan-reviewer's
		// capability_orphan rule will surface this upstream.
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Spec: %s\n\n", capability.Name)

	if capability.Description != "" {
		sb.WriteString("## Overview\n\n")
		sb.WriteString(capability.Description)
		sb.WriteString("\n\n")
	}

	// applies_to: union of FilesOwned, deduplicated, sorted by first
	// appearance for stable diffs.
	seen := make(map[string]bool)
	var appliesTo []string
	for _, r := range reqs {
		for _, f := range r.FilesOwned {
			if !seen[f] {
				seen[f] = true
				appliesTo = append(appliesTo, f)
			}
		}
	}
	if len(appliesTo) > 0 {
		sb.WriteString("## Applies To\n\n")
		for _, f := range appliesTo {
			fmt.Fprintf(&sb, "- `%s`\n", f)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Requirements\n\n")
	for _, r := range reqs {
		fmt.Fprintf(&sb, "### %s\n\n", r.Title)
		if r.Description != "" {
			sb.WriteString(r.Description)
			sb.WriteString("\n\n")
		}
		scenarios := plan.ScenariosForRequirement(r.ID)
		for _, s := range scenarios {
			writeScenario(&sb, s)
		}
	}
	return sb.String()
}

// writeScenario emits one BDD scenario in OpenSpec's tag-aware markdown
// shape (ADR-041 PR 6). Heading line per scenario; tag + harness binding
// on a single inline-code prose line when present; GIVEN/WHEN/THEN as
// non-bulleted bold markers (matches the spec.md adopter parsers).
//
// Tagless scenarios silently omit the tag line — keeps pre-ADR-041 plans
// rendering as standard openspec markdown without spurious empty rows.
func writeScenario(sb *strings.Builder, s workflow.Scenario) {
	fmt.Fprintf(sb, "#### Scenario: %s\n\n", scenarioHeading(s))

	if tagLine := renderScenarioTagLine(s); tagLine != "" {
		sb.WriteString(tagLine)
		sb.WriteString("\n\n")
	}

	fmt.Fprintf(sb, "**GIVEN** %s\n", s.Given)
	fmt.Fprintf(sb, "**WHEN** %s\n", s.When)
	for i, then := range s.Then {
		if i == 0 {
			fmt.Fprintf(sb, "**THEN** %s\n", then)
		} else {
			fmt.Fprintf(sb, "**AND** %s\n", then)
		}
	}
	sb.WriteString("\n")
}

// scenarioHeading returns s.Title when present, otherwise synthesizes a
// short title from s.When for legacy scenarios drafted before
// workflow.Scenario.Title landed. The fallback is truncated to keep the
// markdown heading scannable — full When prose stays in the GIVEN/WHEN/THEN
// block below.
func scenarioHeading(s workflow.Scenario) string {
	if title := strings.TrimSpace(s.Title); title != "" {
		return title
	}
	when := strings.TrimSpace(s.When)
	if when == "" {
		return s.ID
	}
	const maxHeading = 80
	if len(when) <= maxHeading {
		return when
	}
	return when[:maxHeading-1] + "…"
}

// renderScenarioTagLine produces the inline-code tag + harness binding
// prose line per ADR-041's "OpenSpec round-trip syntax" section:
//
//	`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`, `mavlink.raw-mavlink-direct`
//
// Returns "" when the scenario carries no tags — legacy plans get a
// tagless render that's still valid openspec markdown.
func renderScenarioTagLine(s workflow.Scenario) string {
	if len(s.Tags) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, tag := range s.Tags {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "`%s`", tag)
	}
	if len(s.HarnessProfileIDs) > 0 {
		sb.WriteString(" · harness: ")
		for i, id := range s.HarnessProfileIDs {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "`%s`", id)
		}
	}
	return sb.String()
}

// ListCapabilityNames returns the slice of capability names from a plan
// whose Requirements exist. Used by the parent emitter component to know
// which spec.md files to write.
func ListCapabilityNames(plan *workflow.Plan) []string {
	if plan == nil || plan.Exploration == nil {
		return nil
	}
	covered := make(map[string]bool)
	for _, r := range plan.Requirements {
		if r.CapabilityName != "" {
			covered[r.CapabilityName] = true
		}
	}
	var out []string
	for _, c := range plan.Exploration.Capabilities {
		if covered[c.Name] {
			out = append(out, c.Name)
		}
	}
	return out
}
