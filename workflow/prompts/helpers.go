package prompts

import (
	"fmt"
	"strings"
)

// formatScopeList formats a scope list for display in the prompt.
func formatScopeList(items []string, defaultValue string) string {
	if len(items) == 0 {
		return defaultValue
	}
	return strings.Join(items, ", ")
}

// FormatSOPRequirements formats SOP requirements as a prompt section.
// Returns empty string if no requirements are present.
// Used by task-generator to inject graph-sourced SOP requirements into prompts.
func FormatSOPRequirements(requirements []string) string {
	if len(requirements) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## SOP Requirements\n\n")
	sb.WriteString("The following Standard Operating Procedure requirements MUST be reflected in the generated tasks.\n")
	sb.WriteString("Ensure at least one task addresses each requirement:\n\n")
	for _, req := range requirements {
		sb.WriteString(fmt.Sprintf("- %s\n", req))
	}
	sb.WriteString("\nFor example, if a requirement mandates migration notes for model changes, include a dedicated migration/documentation task.\n")
	sb.WriteString("If a requirement mandates type synchronization across backend and frontend, ensure tasks cover both.\n\n")
	return sb.String()
}
