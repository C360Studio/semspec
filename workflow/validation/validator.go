// Package validation provides document validation for workflow-generated documents.
// It checks that generated documents contain required sections and structure,
// enabling auto-retry with feedback when validation fails.
package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// Pre-compiled regex patterns for performance
var (
	// nextSectionRe matches markdown section headers (# or ##)
	nextSectionRe = regexp.MustCompile(`(?m)^#{1,2}\s+`)
	// emptySectionRe matches empty sections (## header followed immediately by another ##)
	emptySectionRe = regexp.MustCompile(`(?m)^##\s+[^\n]+\n\s*\n##`)
)

// DocumentType identifies the type of workflow document.
type DocumentType string

const (
	// DocumentTypePlan identifies a workflow plan document.
	DocumentTypePlan DocumentType = "plan"
	// DocumentTypeSpec identifies a specification document.
	DocumentTypeSpec DocumentType = "spec"
	// DocumentTypeTasks identifies a tasks document.
	DocumentTypeTasks DocumentType = "tasks"
)

// Result contains the result of document validation.
type Result struct {
	Valid           bool              `json:"valid"`
	DocumentType    DocumentType      `json:"document_type"`
	MissingSections []string          `json:"missing_sections,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
	SectionDetails  map[string]string `json:"section_details,omitempty"`
}

// ValidationResult is an alias for Result for backward compatibility.
type ValidationResult = Result //revive:disable-line

// Validator validates workflow documents.
type Validator struct {
	// RequiredSections maps document types to their required section patterns.
	RequiredSections map[DocumentType][]SectionRequirement
}

// SectionRequirement defines a required section.
type SectionRequirement struct {
	Name        string         // Human-readable name
	Pattern     *regexp.Regexp // Regex pattern to match section header
	MinContent  int            // Minimum content length after header (0 = just header required)
	Description string         // Description for feedback
}

// NewValidator creates a new document validator with default requirements.
func NewValidator() *Validator {
	return &Validator{
		RequiredSections: map[DocumentType][]SectionRequirement{
			DocumentTypePlan: {
				{
					Name:        "Title",
					Pattern:     regexp.MustCompile(`(?m)^#\s+.+`),
					MinContent:  0,
					Description: "Document title (# heading)",
				},
				{
					Name:        "Why",
					Pattern:     regexp.MustCompile(`(?mi)^##\s+why\b`),
					MinContent:  50,
					Description: "Why section explaining rationale",
				},
				{
					Name:        "What Changes",
					Pattern:     regexp.MustCompile(`(?mi)^##\s+what\s+changes?\b`),
					MinContent:  50,
					Description: "What Changes section listing modifications",
				},
				{
					Name:        "Impact",
					Pattern:     regexp.MustCompile(`(?mi)^##\s+impact\b`),
					MinContent:  30,
					Description: "Impact section describing affected areas",
				},
			},
			DocumentTypeSpec: {
				{
					Name:        "Title",
					Pattern:     regexp.MustCompile(`(?m)^#\s+.+`),
					MinContent:  0,
					Description: "Specification title",
				},
				{
					Name:        "Overview",
					Pattern:     regexp.MustCompile(`(?mi)^##\s+overview\b`),
					MinContent:  30,
					Description: "Overview section",
				},
				{
					Name:        "Requirements",
					Pattern:     regexp.MustCompile(`(?mi)^##\s+requirements?\b`),
					MinContent:  100,
					Description: "Requirements section with formal specs",
				},
				{
					Name:        "GIVEN/WHEN/THEN",
					Pattern:     regexp.MustCompile(`(?mis)\*\*GIVEN\*\*.*\*\*WHEN\*\*.*\*\*THEN\*\*`),
					MinContent:  0,
					Description: "At least one GIVEN/WHEN/THEN scenario",
				},
				{
					Name:        "Constraints",
					Pattern:     regexp.MustCompile(`(?mi)^##\s+constraints?\b`),
					MinContent:  20,
					Description: "Constraints section",
				},
			},
			DocumentTypeTasks: {
				{
					Name:        "Title",
					Pattern:     regexp.MustCompile(`(?m)^#\s+.+`),
					MinContent:  0,
					Description: "Tasks title",
				},
				{
					Name:        "Task Checkboxes",
					Pattern:     regexp.MustCompile(`(?m)^-\s+\[\s*\]\s+\d+\.\d+`),
					MinContent:  0,
					Description: "Task items with checkboxes (- [ ] N.N format)",
				},
				{
					Name:        "Multiple Sections",
					Pattern:     regexp.MustCompile(`(?m)^##\s+\d+\.`),
					MinContent:  0,
					Description: "Numbered task sections (## N. format)",
				},
			},
		},
	}
}

// Validate validates a document against its type requirements.
func (v *Validator) Validate(content string, docType DocumentType) *Result {
	result := &Result{
		Valid:          true,
		DocumentType:   docType,
		SectionDetails: make(map[string]string),
	}

	requirements, ok := v.RequiredSections[docType]
	if !ok {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Unknown document type: %s", docType))
		return result
	}

	for _, req := range requirements {
		match := req.Pattern.FindStringIndex(content)
		if match == nil {
			result.Valid = false
			result.MissingSections = append(result.MissingSections,
				fmt.Sprintf("%s: %s", req.Name, req.Description))
			continue
		}

		// Check minimum content length after the section header
		if req.MinContent > 0 {
			// Find the next section or end of document
			sectionStart := match[1]
			nextSection := findNextSection(content[sectionStart:])
			sectionContent := ""
			if nextSection == -1 {
				sectionContent = content[sectionStart:]
			} else {
				sectionContent = content[sectionStart : sectionStart+nextSection]
			}

			// Check content length (excluding whitespace)
			trimmedContent := strings.TrimSpace(sectionContent)
			if len(trimmedContent) < req.MinContent {
				result.Valid = false
				result.MissingSections = append(result.MissingSections,
					fmt.Sprintf("%s: Section too short (min %d chars, got %d)",
						req.Name, req.MinContent, len(trimmedContent)))
			} else {
				result.SectionDetails[req.Name] = fmt.Sprintf("OK (%d chars)", len(trimmedContent))
			}
		} else {
			result.SectionDetails[req.Name] = "OK"
		}
	}

	// Add warnings for common issues
	result.Warnings = append(result.Warnings, v.checkCommonIssues(content, docType)...)

	return result
}

// findNextSection finds the index of the next markdown section header.
func findNextSection(content string) int {
	match := nextSectionRe.FindStringIndex(content)
	if match == nil {
		return -1
	}
	return match[0]
}

// checkCommonIssues checks for common document quality issues.
func (v *Validator) checkCommonIssues(content string, docType DocumentType) []string {
	var warnings []string

	// Check for placeholder text
	placeholders := []string{
		"TODO", "FIXME", "XXX", "TBD",
		"[placeholder]", "[insert", "[add",
		"Lorem ipsum", "example text",
	}
	for _, p := range placeholders {
		if strings.Contains(strings.ToLower(content), strings.ToLower(p)) {
			warnings = append(warnings, fmt.Sprintf("Contains placeholder text: %s", p))
		}
	}

	// Check minimum document length
	minLengths := map[DocumentType]int{
		DocumentTypePlan:  500,
		DocumentTypeSpec:  600,
		DocumentTypeTasks: 400,
	}
	if minLen, ok := minLengths[docType]; ok {
		if len(content) < minLen {
			warnings = append(warnings,
				fmt.Sprintf("Document may be too short (%d chars, recommend at least %d)",
					len(content), minLen))
		}
	}

	// Check for empty sections
	if emptySectionRe.MatchString(content) {
		warnings = append(warnings, "Contains empty sections")
	}

	return warnings
}

// FormatFeedback formats validation results as feedback for retry.
func (r *Result) FormatFeedback() string {
	if r.Valid {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Validation Failed\n\n")
	sb.WriteString("The generated document is missing required sections or content.\n\n")

	if len(r.MissingSections) > 0 {
		sb.WriteString("### Missing or Incomplete Sections\n\n")
		for _, section := range r.MissingSections {
			sb.WriteString(fmt.Sprintf("- %s\n", section))
		}
		sb.WriteString("\n")
	}

	if len(r.Warnings) > 0 {
		sb.WriteString("### Warnings\n\n")
		for _, warning := range r.Warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", warning))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Please regenerate the document addressing these issues.\n")

	return sb.String()
}

// ValidateDocument is a convenience function for validating a document.
func ValidateDocument(content string, docType DocumentType) *Result {
	v := NewValidator()
	return v.Validate(content, docType)
}
