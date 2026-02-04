package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ConstitutionScenario tests constitution enforcement during approval.
type ConstitutionScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewConstitutionScenario creates a new constitution scenario.
func NewConstitutionScenario(cfg *config.Config) *ConstitutionScenario {
	return &ConstitutionScenario{
		name:        "constitution",
		description: "Tests constitution enforcement: changes missing required elements should be rejected",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ConstitutionScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ConstitutionScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ConstitutionScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Write a strict constitution that requires tests
	constitution := StrictConstitution()
	if err := s.fs.WriteConstitution(constitution); err != nil {
		return fmt.Errorf("write constitution: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the constitution scenario.
func (s *ConstitutionScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	// Stage-based execution
	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-constitution", s.stageVerifyConstitution},
		{"propose-change", s.stageProposeChange},
		{"write-minimal-spec", s.stageWriteMinimalSpec},
		{"approve-should-fail", s.stageApproveShouldFail},
		{"write-compliant-spec", s.stageWriteCompliantSpec},
		{"approve-should-succeed", s.stageApproveShouldSucceed},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_ms", stage.name), stageDuration.Milliseconds())

		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}

		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *ConstitutionScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stageVerifyConstitution verifies the constitution file exists.
func (s *ConstitutionScenario) stageVerifyConstitution(ctx context.Context, result *Result) error {
	if !s.fs.ConstitutionExists() {
		return fmt.Errorf("constitution.md not found")
	}

	content, err := s.fs.ReadFileRelative(".semspec/constitution.md")
	if err != nil {
		return fmt.Errorf("read constitution: %w", err)
	}

	if !strings.Contains(content, "Testing Required") {
		return fmt.Errorf("constitution doesn't contain expected principles")
	}

	result.SetDetail("constitution_exists", true)
	return nil
}

// stageProposeChange creates a new change for testing.
func (s *ConstitutionScenario) stageProposeChange(ctx context.Context, result *Result) error {
	result.SetDetail("test_change", "test-constitution-enforcement")

	resp, err := s.http.SendMessage(ctx, "/propose test constitution enforcement")
	if err != nil {
		return fmt.Errorf("send propose command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("propose returned error: %s", resp.Content)
	}

	// Wait for change to be created
	slug := "test-constitution-enforcement"
	if err := s.fs.WaitForChange(ctx, slug); err != nil {
		return fmt.Errorf("change not created: %w", err)
	}

	return nil
}

// stageWriteMinimalSpec writes a spec that doesn't meet constitution requirements.
func (s *ConstitutionScenario) stageWriteMinimalSpec(ctx context.Context, result *Result) error {
	slug := "test-constitution-enforcement"

	// Write a spec without testing section (violates constitution)
	specContent := `# Test Feature Specification

## Overview

This is a minimal spec without testing requirements.

## Implementation

- Some implementation details
`

	specPath := fmt.Sprintf(".semspec/changes/%s/spec.md", slug)
	if err := s.fs.WriteFileRelative(specPath, specContent); err != nil {
		return fmt.Errorf("write minimal spec: %w", err)
	}

	result.SetDetail("minimal_spec_written", true)
	return nil
}

// stageApproveShouldFail verifies that approval fails due to constitution violation.
func (s *ConstitutionScenario) stageApproveShouldFail(ctx context.Context, result *Result) error {
	slug := "test-constitution-enforcement"

	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/approve %s", slug))
	if err != nil {
		return fmt.Errorf("send approve command: %w", err)
	}

	// Approval should fail due to constitution check
	if resp.Type != "error" {
		return fmt.Errorf("expected approval to fail, but it succeeded")
	}

	// Verify the failure mentions constitution
	if !strings.Contains(strings.ToLower(resp.Content), "constitution") {
		return fmt.Errorf("approval failed but not due to constitution: %s", resp.Content)
	}

	result.SetDetail("constitution_rejection", resp.Content)
	return nil
}

// stageWriteCompliantSpec writes a spec that meets constitution requirements.
func (s *ConstitutionScenario) stageWriteCompliantSpec(ctx context.Context, result *Result) error {
	slug := "test-constitution-enforcement"

	// Write a compliant spec with testing section
	specContent := `# Test Feature Specification

## Overview

This spec includes all required sections per the constitution.

## Requirements

1. Feature must work correctly
2. Feature must be tested

## Implementation

- Implementation detail 1
- Implementation detail 2

## Testing

All changes must include comprehensive testing:

- Unit tests for core logic
- Integration tests for API endpoints
- E2E tests for critical paths

### Test Plan

1. Write unit tests before implementation
2. Achieve 80% coverage on critical paths
3. Include edge case testing
`

	specPath := fmt.Sprintf(".semspec/changes/%s/spec.md", slug)
	if err := s.fs.WriteFileRelative(specPath, specContent); err != nil {
		return fmt.Errorf("write compliant spec: %w", err)
	}

	// Small delay to allow Docker volume mount to sync on macOS
	time.Sleep(100 * time.Millisecond)

	result.SetDetail("compliant_spec_written", true)
	return nil
}

// stageApproveShouldSucceed verifies that approval succeeds with compliant spec.
func (s *ConstitutionScenario) stageApproveShouldSucceed(ctx context.Context, result *Result) error {
	slug := "test-constitution-enforcement"

	// Debug: Verify the spec was updated correctly before approving
	specPath := fmt.Sprintf(".semspec/changes/%s/spec.md", slug)
	specContent, err := s.fs.ReadFileRelative(specPath)
	if err != nil {
		return fmt.Errorf("read spec for verification: %w", err)
	}
	if !strings.Contains(specContent, "## Testing") {
		return fmt.Errorf("spec does not contain '## Testing' section. Content:\n%s", specContent)
	}
	result.SetDetail("spec_has_testing_section", true)

	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/approve %s", slug))
	if err != nil {
		return fmt.Errorf("send approve command: %w", err)
	}

	if resp.Type == "error" {
		// Debug: Include the spec content in error for debugging
		return fmt.Errorf("approval failed unexpectedly: %s\n\nSpec content was:\n%s", resp.Content, specContent)
	}

	// Verify the change is now approved
	if err := s.fs.WaitForChangeStatus(ctx, slug, "approved"); err != nil {
		return fmt.Errorf("change not approved: %w", err)
	}

	result.SetDetail("approval_succeeded", true)
	return nil
}

// StrictConstitution returns a constitution that requires testing documentation.
func StrictConstitution() string {
	return `# Project Constitution

Version: 1.0.0
Ratified: 2025-01-01

## Principles

### 1. Testing Required

All changes must include a Testing section in their specification.
Specifications without testing documentation will not be approved.

Rationale: Testing ensures code quality and prevents regressions.

### 2. Clear Requirements

All changes must clearly document their requirements.
Vague or missing requirements will cause approval to fail.

Rationale: Clear requirements enable proper implementation and review.

### 3. Implementation Details

All changes must describe their implementation approach.
This helps reviewers understand the technical approach.

Rationale: Technical clarity improves review quality and knowledge sharing.
`
}
