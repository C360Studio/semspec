package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// WorkflowBasicScenario tests the full propose → approve workflow.
type WorkflowBasicScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewWorkflowBasicScenario creates a new workflow basic scenario.
func NewWorkflowBasicScenario(cfg *config.Config) *WorkflowBasicScenario {
	return &WorkflowBasicScenario{
		name:        "workflow-basic",
		description: "Tests the full propose → status → design → spec → check → approve workflow",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *WorkflowBasicScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *WorkflowBasicScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *WorkflowBasicScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the workflow basic scenario.
func (s *WorkflowBasicScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	// Stage-based execution
	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"propose", s.stagePropose},
		{"verify-created", s.stageVerifyCreated},
		{"status-check", s.stageStatusCheck},
		{"write-spec", s.stageWriteSpec},
		{"approve", s.stageApprove},
		{"verify-approved", s.stageVerifyApproved},
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
func (s *WorkflowBasicScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stagePropose sends the /propose command.
func (s *WorkflowBasicScenario) stagePropose(ctx context.Context, result *Result) error {
	result.SetDetail("test_change", "add-auth-refresh")

	resp, err := s.http.SendMessage(ctx, "/propose add auth refresh")
	if err != nil {
		return fmt.Errorf("send propose command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("propose returned error: %s", resp.Content)
	}

	result.SetDetail("propose_response", resp.Content)
	return nil
}

// stageVerifyCreated verifies the change was created.
func (s *WorkflowBasicScenario) stageVerifyCreated(ctx context.Context, result *Result) error {
	slug := "add-auth-refresh"

	// Wait for change directory
	if err := s.fs.WaitForChange(ctx, slug); err != nil {
		return fmt.Errorf("change directory not created: %w", err)
	}

	// Wait for metadata.json
	if err := s.fs.WaitForChangeFile(ctx, slug, "metadata.json"); err != nil {
		return fmt.Errorf("metadata.json not created: %w", err)
	}

	// Wait for proposal.md
	if err := s.fs.WaitForChangeFile(ctx, slug, "proposal.md"); err != nil {
		return fmt.Errorf("proposal.md not created: %w", err)
	}

	// Load and verify metadata
	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	if metadata.Status != "created" {
		return fmt.Errorf("expected status 'created', got '%s'", metadata.Status)
	}

	result.SetDetail("metadata_status", metadata.Status)
	result.SetDetail("metadata_slug", metadata.Slug)
	return nil
}

// stageStatusCheck sends the /changes command and verifies the response.
func (s *WorkflowBasicScenario) stageStatusCheck(ctx context.Context, result *Result) error {
	slug := "add-auth-refresh"

	// Use /changes command (not /status - that's reserved for loop status in semstreams)
	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/changes %s", slug))
	if err != nil {
		return fmt.Errorf("send changes command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("changes returned error: %s", resp.Content)
	}

	// Verify the response contains expected content
	if !strings.Contains(resp.Content, slug) {
		return fmt.Errorf("changes response doesn't contain slug")
	}
	if !strings.Contains(resp.Content, "created") {
		return fmt.Errorf("changes response doesn't show 'created' status")
	}

	result.SetDetail("changes_response", resp.Content)
	return nil
}

// stageWriteSpec writes a spec.md file for the change.
func (s *WorkflowBasicScenario) stageWriteSpec(ctx context.Context, result *Result) error {
	slug := "add-auth-refresh"
	specContent := `# Auth Refresh Specification

## Overview

This spec defines the auth refresh token mechanism.

## Requirements

1. Refresh tokens must be rotated on each use
2. Tokens expire after 7 days of inactivity
3. Secure storage in HttpOnly cookies

## Implementation

- Add refresh endpoint at /api/auth/refresh
- Implement token rotation logic
- Add cookie handling middleware

## Testing

- Unit tests for token rotation
- Integration tests for refresh flow
- Security tests for token leakage
`

	specPath := fmt.Sprintf(".semspec/changes/%s/spec.md", slug)
	if err := s.fs.WriteFileRelative(specPath, specContent); err != nil {
		return fmt.Errorf("write spec.md: %w", err)
	}

	// Verify the file was written
	if !s.fs.ChangeHasFile(slug, "spec.md") {
		return fmt.Errorf("spec.md not found after write")
	}

	result.SetDetail("spec_written", true)
	return nil
}

// stageApprove sends the /approve command.
func (s *WorkflowBasicScenario) stageApprove(ctx context.Context, result *Result) error {
	slug := "add-auth-refresh"

	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/approve %s", slug))
	if err != nil {
		return fmt.Errorf("send approve command: %w", err)
	}

	// Approve might fail if constitution check fails - that's acceptable
	if resp.Type == "error" {
		if strings.Contains(resp.Content, "constitution") {
			result.AddWarning("approve failed due to constitution check")
			return nil
		}
		return fmt.Errorf("approve returned error: %s", resp.Content)
	}

	result.SetDetail("approve_response", resp.Content)
	return nil
}

// stageVerifyApproved verifies the change status is approved.
func (s *WorkflowBasicScenario) stageVerifyApproved(ctx context.Context, result *Result) error {
	slug := "add-auth-refresh"

	// Wait for status to be approved
	if err := s.fs.WaitForChangeStatus(ctx, slug, "approved"); err != nil {
		// Check if there was a warning about constitution
		if len(result.Warnings) > 0 && strings.Contains(result.Warnings[0], "constitution") {
			// Verify it's at least in reviewed state after constitution check
			metadata, loadErr := s.fs.LoadChangeMetadata(slug)
			if loadErr != nil {
				return fmt.Errorf("load metadata: %w", loadErr)
			}
			// Any valid state is acceptable if constitution blocked approval
			validStates := []string{"created", "drafted", "reviewed"}
			for _, state := range validStates {
				if metadata.Status == state {
					result.SetDetail("final_status", metadata.Status)
					result.SetDetail("blocked_by_constitution", true)
					return nil
				}
			}
		}
		return fmt.Errorf("change not approved: %w", err)
	}

	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load final metadata: %w", err)
	}

	result.SetDetail("final_status", metadata.Status)
	return nil
}
