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
// Structure:
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
//	#### Scenarios
//	- **GIVEN** ... **WHEN** ... **THEN** ...
//
// applies_to is derived from the union of FilesOwned across all
// Requirements owning this capability.
func RenderSpec(plan *workflow.Plan, capName string) string {
	if plan == nil || plan.Exploration == nil {
		return ""
	}
	cap, _ := plan.Exploration.FindCapability(capName)
	if cap == nil {
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
	fmt.Fprintf(&sb, "# Spec: %s\n\n", cap.Name)

	if cap.Description != "" {
		sb.WriteString("## Overview\n\n")
		sb.WriteString(cap.Description)
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
		if len(scenarios) > 0 {
			sb.WriteString("#### Scenarios\n\n")
			for _, s := range scenarios {
				writeScenario(&sb, s)
			}
		}
	}
	return sb.String()
}

// writeScenario emits one BDD scenario in OpenSpec's GIVEN/WHEN/THEN
// markdown shape. THEN clauses are multi-valued; each is rendered on its
// own line.
func writeScenario(sb *strings.Builder, s workflow.Scenario) {
	fmt.Fprintf(sb, "- **GIVEN** %s\n", s.Given)
	fmt.Fprintf(sb, "  **WHEN** %s\n", s.When)
	for i, then := range s.Then {
		if i == 0 {
			fmt.Fprintf(sb, "  **THEN** %s\n", then)
		} else {
			fmt.Fprintf(sb, "  **AND** %s\n", then)
		}
	}
	sb.WriteString("\n")
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
