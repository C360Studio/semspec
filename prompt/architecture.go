package prompt

import (
	"fmt"
	"strings"
)

// FormatArchitectureContext produces a markdown summary of actors and
// integration points for injection into the scenario-generator prompt.
// Returns the empty string when both slices are empty.
//
// Migrated from workflow/prompts.FormatArchitectureContext as part of the
// Plan B consolidation — pre-rendering happens in component code, not in
// the user-prompt fragment, because the input data lives in workflow types
// the prompt package shouldn't depend on transitively.
func FormatArchitectureContext(actors []ActorInfo, integrations []IntegrationInfo) string {
	if len(actors) == 0 && len(integrations) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Architecture Context\n\n")
	sb.WriteString("Use this architecture context to write more specific scenarios. Scenarios should reference the actors and integration points below where relevant.\n\n")

	if len(actors) > 0 {
		sb.WriteString("### Actors\n\n")
		for _, a := range actors {
			fmt.Fprintf(&sb, "- **%s** (%s)", a.Name, a.Type)
			if len(a.Triggers) > 0 {
				fmt.Fprintf(&sb, ": %s", strings.Join(a.Triggers, ", "))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(integrations) > 0 {
		sb.WriteString("### Integration Points\n\n")
		for _, ip := range integrations {
			fmt.Fprintf(&sb, "- **%s** (%s, %s)\n", ip.Name, ip.Direction, ip.Protocol)
		}
	}

	return sb.String()
}
