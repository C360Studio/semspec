package validation

import (
	"strings"
	"testing"
)

func TestValidateProposal(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name           string
		content        string
		expectValid    bool
		expectMissing  []string
		expectWarnings []string
	}{
		{
			name: "valid proposal",
			content: `# Add User Authentication

## Why

We need user authentication to protect sensitive endpoints and provide personalized experiences.
This change will enable users to log in securely and maintain session state across requests.

## What Changes

- Add new auth package with JWT token handling
- Create middleware for protected routes
- Add user session management
- Update API endpoints to check authentication

## Impact

### Code Affected
- api/routes.go - Add auth middleware
- handlers/ - Update protected handlers

### Specs Affected
- Auth specification needs creation

### Testing Required
- Unit tests for token validation
- Integration tests for auth flow
`,
			expectValid:   true,
			expectMissing: nil,
		},
		{
			name: "missing why section",
			content: `# Add Feature

## What Changes

Some changes here that are long enough to pass validation.

## Impact

Impact section with enough content.
`,
			expectValid:   false,
			expectMissing: []string{"Why"},
		},
		{
			name: "short sections",
			content: `# Title

## Why

Short.

## What Changes

Brief.

## Impact

Minimal.
`,
			expectValid:   false,
			expectMissing: []string{"Why", "What Changes"},
		},
		{
			name: "has placeholder text",
			content: `# Add Feature

## Why

TODO: Fill in the rationale for this change. This is placeholder text that needs to be replaced.

## What Changes

- Changes to make
- More changes needed here to meet minimum length requirements

## Impact

- Impact on codebase with sufficient detail
`,
			expectValid:    true, // Placeholders are warnings, not validation failures
			expectWarnings: []string{"TODO"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, DocumentTypePlan)

			if result.Valid != tt.expectValid {
				t.Errorf("expected valid=%v, got valid=%v", tt.expectValid, result.Valid)
				t.Logf("Missing sections: %v", result.MissingSections)
			}

			if tt.expectMissing != nil {
				for _, expected := range tt.expectMissing {
					found := false
					for _, missing := range result.MissingSections {
						if strings.Contains(missing, expected) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected missing section containing %q", expected)
					}
				}
			}

			if tt.expectWarnings != nil {
				for _, expected := range tt.expectWarnings {
					found := false
					for _, warning := range result.Warnings {
						if strings.Contains(warning, expected) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected warning containing %q", expected)
					}
				}
			}
		})
	}
}


func TestValidateSpec(t *testing.T) {
	validator := NewValidator()

	validSpec := `# User Authentication Specification

## Overview

This specification defines the requirements for user authentication in the system.
It references the proposal and design documents for context.

## Requirements

### Requirement 1: Token Validation

The system SHALL validate JWT tokens on protected endpoints.

#### Scenario: Valid Token

- **GIVEN** a user has a valid JWT token
- **WHEN** they access a protected endpoint
- **THEN** the request should proceed successfully
- **AND** the user context should be populated

#### Scenario: Expired Token

- **GIVEN** a user has an expired JWT token
- **WHEN** they access a protected endpoint
- **THEN** they should receive a 401 Unauthorized response

## Constraints

- Tokens must expire within 24 hours
- Refresh tokens must be single-use
- Token signatures must use RS256 algorithm

## Dependencies

- User service must be available for credential validation
- Redis must be available for token blacklisting
`

	result := validator.Validate(validSpec, DocumentTypeSpec)
	if !result.Valid {
		t.Errorf("expected valid spec, got invalid")
		t.Logf("Missing: %v", result.MissingSections)
	}

	// Test missing GIVEN/WHEN/THEN
	specWithoutScenarios := `# Specification

## Overview

Overview text here.

## Requirements

The system should do things.

## Constraints

Some constraints.
`

	result = validator.Validate(specWithoutScenarios, DocumentTypeSpec)
	if result.Valid {
		t.Error("expected invalid for spec without GIVEN/WHEN/THEN")
	}
}

func TestValidateTasks(t *testing.T) {
	validator := NewValidator()

	validTasks := `# Tasks: Add User Authentication

## 1. Setup

- [ ] 1.1 Create feature branch from main
- [ ] 1.2 Add JWT dependency to go.mod
- [ ] 1.3 Create auth/ package structure

## 2. Core Authentication

- [ ] 2.1 Implement token generation in auth/token.go
- [ ] 2.2 Implement token validation middleware
- [ ] 2.3 Add token refresh endpoint

## 3. Testing

- [ ] 3.1 Write unit tests for token validation
- [ ] 3.2 Add integration tests for auth flow
`

	result := validator.Validate(validTasks, DocumentTypeTasks)
	if !result.Valid {
		t.Errorf("expected valid tasks, got invalid")
		t.Logf("Missing: %v", result.MissingSections)
	}

	// Test missing checkboxes
	tasksWithoutCheckboxes := `# Tasks

## 1. Setup

Just some text without proper task format.

## 2. Implementation

More text here.
`

	result = validator.Validate(tasksWithoutCheckboxes, DocumentTypeTasks)
	if result.Valid {
		t.Error("expected invalid for tasks without checkboxes")
	}
}

func TestFormatFeedback(t *testing.T) {
	result := &ValidationResult{
		Valid:        false,
		DocumentType: DocumentTypePlan,
		MissingSections: []string{
			"Why: Why section explaining rationale",
			"Impact: Impact section describing affected areas",
		},
		Warnings: []string{
			"Contains placeholder text: TODO",
		},
	}

	feedback := result.FormatFeedback()

	if !strings.Contains(feedback, "Validation Failed") {
		t.Error("expected 'Validation Failed' in feedback")
	}

	if !strings.Contains(feedback, "Why") {
		t.Error("expected 'Why' section mentioned in feedback")
	}

	if !strings.Contains(feedback, "TODO") {
		t.Error("expected 'TODO' warning in feedback")
	}

	// Test valid result returns empty feedback
	validResult := &ValidationResult{Valid: true}
	if validResult.FormatFeedback() != "" {
		t.Error("expected empty feedback for valid result")
	}
}

func TestValidateDocument(t *testing.T) {
	content := `# Test Proposal

## Why

This is a test proposal with sufficient content to pass the minimum length check for the why section.

## What Changes

We will make significant changes to the codebase including new features and modifications to existing code.

## Impact

The impact will be substantial across multiple components.
`

	result := ValidateDocument(content, DocumentTypePlan)
	if !result.Valid {
		t.Errorf("expected valid document")
		t.Logf("Missing: %v", result.MissingSections)
	}
}

func TestUnknownDocumentType(t *testing.T) {
	validator := NewValidator()
	result := validator.Validate("any content", DocumentType("unknown"))

	// Should return valid with a warning
	if !result.Valid {
		t.Error("expected valid for unknown document type")
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warning for unknown document type")
	}
}
