package github

import (
	"strings"
)

// ParsedIssue contains the structured fields extracted from a GitHub Issue
// Forms template body. The template uses heading-delimited sections.
type ParsedIssue struct {
	Description string
	Scope       string
	Constraints string
	Priority    string
}

// ParseIssueBody extracts structured fields from a GitHub Issue Forms template body.
// The template renders as heading-delimited markdown:
//
//	### Description
//	What should be built or changed?
//
//	### Scope
//	src/api/**, tests/integration/**
//
//	### Constraints
//	Must use existing auth middleware
//
//	### Priority
//	Normal
func ParseIssueBody(body string) ParsedIssue {
	sections := parseSections(body)

	return ParsedIssue{
		Description: sections["description"],
		Scope:       sections["scope"],
		Constraints: sections["constraints"],
		Priority:    normalizePriority(sections["priority"]),
	}
}

// parseSections splits a markdown body into heading-delimited sections.
// Returns a map of lowercase heading → trimmed body text.
func parseSections(body string) map[string]string {
	sections := make(map[string]string)
	var currentHeading string
	var currentBody strings.Builder

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if heading, ok := parseHeading(trimmed); ok {
			// Save previous section.
			if currentHeading != "" {
				sections[currentHeading] = strings.TrimSpace(currentBody.String())
			}
			currentHeading = strings.ToLower(heading)
			currentBody.Reset()
			continue
		}
		if currentHeading != "" {
			currentBody.WriteString(line)
			currentBody.WriteString("\n")
		}
	}

	// Save final section.
	if currentHeading != "" {
		sections[currentHeading] = strings.TrimSpace(currentBody.String())
	}

	return sections
}

// parseHeading returns the heading text if the line is a markdown heading (### Heading).
// Supports h3 (###) which is what GitHub Issue Forms generates.
func parseHeading(line string) (string, bool) {
	if strings.HasPrefix(line, "### ") {
		return strings.TrimSpace(line[4:]), true
	}
	return "", false
}

// normalizePriority normalizes the priority value to lowercase.
// Returns "normal" if empty or unrecognized.
func normalizePriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return "high"
	case "low":
		return "low"
	case "normal", "":
		return "normal"
	default:
		return "normal"
	}
}
