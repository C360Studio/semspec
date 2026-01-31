package workflow

import (
	"fmt"
	"strings"
	"time"
)

// ProposalTemplate generates a proposal.md template.
func ProposalTemplate(title, description string) string {
	return fmt.Sprintf(`# %s

## Why

%s

## What Changes

- [ ] Describe the specific changes to be made
- [ ] List affected components
- [ ] Note any new dependencies

## Impact

### Code Affected
- (List files/modules that will be modified)

### Specs Affected
- (List specs that will be created or modified)

### Testing Required
- (Describe testing approach)
`, title, description)
}

// DesignTemplate generates a design.md template.
func DesignTemplate(title string) string {
	return fmt.Sprintf(`# Design: %s

## Technical Approach

Describe the technical approach to implementing this change.

## Components Affected

| Component | Change Type | Description |
|-----------|-------------|-------------|
| | added/modified/removed | |

## Data Flow

Describe how data flows through the affected components.

## Dependencies

### New Dependencies
- (None)

### Removed Dependencies
- (None)

## Alternatives Considered

### Alternative 1: (Name)
- **Pros**:
- **Cons**:
- **Why not chosen**:

## Security Considerations

- (List any security implications)

## Performance Considerations

- (List any performance implications)
`, title)
}

// SpecTemplate generates a spec.md template.
func SpecTemplate(title string) string {
	return fmt.Sprintf(`# %s Specification

## Overview

Brief description of what this specification covers.

## Requirements

### Requirement 1: (Name)

The system SHALL (describe requirement).

#### Scenario: (Scenario Name)

- GIVEN (initial context)
- WHEN (action occurs)
- THEN (expected outcome)
- AND (additional expectations)

### Requirement 2: (Name)

The system SHALL (describe requirement).

## Constraints

- (List any constraints or limitations)

## Dependencies

- (List dependencies on other specs or systems)

## Version History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0.0 | %s | | Initial specification |
`, title, time.Now().Format("2006-01-02"))
}

// TasksTemplate generates a tasks.md template from spec requirements.
func TasksTemplate(title string, sections []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Tasks: %s\n\n", title))

	if len(sections) == 0 {
		// Default sections
		sections = []string{"Setup", "Implementation", "Testing", "Documentation"}
	}

	for i, section := range sections {
		sb.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, section))
		sb.WriteString(fmt.Sprintf("- [ ] %d.1 Task description\n", i+1))
		sb.WriteString(fmt.Sprintf("- [ ] %d.2 Task description\n", i+1))
		sb.WriteString("\n")
	}

	return sb.String()
}

// ConstitutionTemplate generates a constitution.md template.
func ConstitutionTemplate(projectName string) string {
	return fmt.Sprintf(`# Project Constitution

Version: 1.0.0
Ratified: %s

## Principles

### 1. Test-First Development

All features MUST have tests written before implementation.

Rationale: Ensures testability and catches design issues early.

### 2. No Direct Database Access

All data access MUST go through repository interfaces.

Rationale: Enables testing and future storage changes.

### 3. Clear Error Handling

All errors MUST be handled explicitly with context.

Rationale: Improves debugging and user experience.

### 4. Documentation Required

All public APIs MUST have documentation.

Rationale: Ensures maintainability and usability.
`, time.Now().Format("2006-01-02"))
}

// FormatChangeStatus formats a change for display.
func FormatChangeStatus(change *Change) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("**%s** (%s)\n", change.Title, change.Status))
	sb.WriteString(fmt.Sprintf("  Slug: %s\n", change.Slug))
	sb.WriteString(fmt.Sprintf("  Author: %s\n", change.Author))
	sb.WriteString(fmt.Sprintf("  Created: %s\n", change.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("  Updated: %s\n", change.UpdatedAt.Format("2006-01-02 15:04")))

	// Files
	var files []string
	if change.Files.HasProposal {
		files = append(files, "proposal.md")
	}
	if change.Files.HasDesign {
		files = append(files, "design.md")
	}
	if change.Files.HasSpec {
		files = append(files, "spec.md")
	}
	if change.Files.HasTasks {
		files = append(files, "tasks.md")
	}
	if len(files) > 0 {
		sb.WriteString(fmt.Sprintf("  Files: %s\n", strings.Join(files, ", ")))
	}

	return sb.String()
}

// FormatChangesList formats a list of changes for display.
func FormatChangesList(changes []*Change) string {
	if len(changes) == 0 {
		return "No active changes."
	}

	var sb strings.Builder
	sb.WriteString("## Active Changes\n\n")

	for _, change := range changes {
		sb.WriteString(FormatChangeStatus(change))
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatCheckResult formats a check result for display.
func FormatCheckResult(result *CheckResult) string {
	var sb strings.Builder

	if result.Passed {
		sb.WriteString("✓ All constitution checks passed\n")
	} else {
		sb.WriteString("✗ Constitution check failed\n\n")
		sb.WriteString("Violations:\n")
		for _, v := range result.Violations {
			sb.WriteString(fmt.Sprintf("  - Principle %d (%s): %s\n",
				v.Principle.Number, v.Principle.Title, v.Message))
		}
	}

	return sb.String()
}
