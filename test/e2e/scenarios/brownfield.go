package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// BrownfieldScenario tests the workflow on an existing codebase with history.
// It simulates proposing and approving changes to an established project.
type BrownfieldScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewBrownfieldScenario creates a new brownfield scenario.
func NewBrownfieldScenario(cfg *config.Config) *BrownfieldScenario {
	return &BrownfieldScenario{
		name:        "brownfield",
		description: "Tests workflow on existing codebase: propose changes referencing existing code",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *BrownfieldScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *BrownfieldScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment with an established codebase.
func (s *BrownfieldScenario) Setup(ctx context.Context) error {
	// Create filesystem client
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)

	// Clean workspace completely
	if err := s.fs.CleanWorkspaceAll(); err != nil {
		return fmt.Errorf("clean workspace: %w", err)
	}

	// Setup .semspec directory
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Copy Go fixture to create established codebase
	fixturePath := s.config.GoFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy fixture: %w", err)
	}

	// Initialize git and create history to simulate established project
	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}

	// Create initial commit
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add initial: %w", err)
	}
	if err := s.fs.GitCommit("feat: initial auth package implementation"); err != nil {
		return fmt.Errorf("git commit initial: %w", err)
	}

	// Add a second commit to show history
	if err := s.fs.WriteFileRelative("internal/auth/doc.go", `// Package auth provides authentication utilities.
package auth
`); err != nil {
		return fmt.Errorf("write doc.go: %w", err)
	}
	if err := s.fs.GitAdd("internal/auth/doc.go"); err != nil {
		return fmt.Errorf("git add doc.go: %w", err)
	}
	if err := s.fs.GitCommit("docs: add package documentation"); err != nil {
		return fmt.Errorf("git commit doc: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the brownfield scenario.
func (s *BrownfieldScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-history", s.stageVerifyHistory},
		{"propose-enhancement", s.stageProposeEnhancement},
		{"verify-proposal", s.stageVerifyProposal},
		{"write-spec-with-context", s.stageWriteSpecWithContext},
		{"approve-change", s.stageApproveChange},
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
func (s *BrownfieldScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stageVerifyHistory verifies the existing codebase has history.
func (s *BrownfieldScenario) stageVerifyHistory(ctx context.Context, result *Result) error {
	// Verify git repo exists
	if !s.fs.IsGitRepo() {
		return fmt.Errorf("workspace is not a git repository")
	}

	// Verify commit history
	log, err := s.fs.GitLog(3)
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}

	if !strings.Contains(log, "initial auth package") {
		return fmt.Errorf("initial commit not found in history")
	}
	if !strings.Contains(log, "package documentation") {
		return fmt.Errorf("documentation commit not found in history")
	}

	// Verify existing code
	if !s.fs.FileExistsRelative("internal/auth/auth.go") {
		return fmt.Errorf("existing auth.go not found")
	}

	authContent, err := s.fs.ReadFileRelative("internal/auth/auth.go")
	if err != nil {
		return fmt.Errorf("read auth.go: %w", err)
	}

	// Verify existing types we'll reference in the proposal
	if !strings.Contains(authContent, "type User struct") {
		return fmt.Errorf("User type not found in existing code")
	}
	if !strings.Contains(authContent, "func Authenticate") {
		return fmt.Errorf("Authenticate function not found in existing code")
	}

	result.SetDetail("commit_count", strings.Count(log, "\n")+1)
	result.SetDetail("existing_types", []string{"User", "Token"})
	return nil
}

// stageProposeEnhancement proposes a change to the existing auth module.
func (s *BrownfieldScenario) stageProposeEnhancement(ctx context.Context, result *Result) error {
	result.SetDetail("test_change", "add-session-management")

	// Propose an enhancement that builds on existing code
	resp, err := s.http.SendMessage(ctx, "/propose add session management to auth package")
	if err != nil {
		return fmt.Errorf("send propose command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("propose returned error: %s", resp.Content)
	}

	result.SetDetail("propose_response", resp.Content)
	return nil
}

// stageVerifyProposal verifies the proposal was created.
func (s *BrownfieldScenario) stageVerifyProposal(ctx context.Context, result *Result) error {
	slug := "add-session-management"

	// Wait for change directory
	if err := s.fs.WaitForChange(ctx, slug); err != nil {
		return fmt.Errorf("change directory not created: %w", err)
	}

	// Wait for metadata and proposal
	if err := s.fs.WaitForChangeFile(ctx, slug, "metadata.json"); err != nil {
		return fmt.Errorf("metadata.json not created: %w", err)
	}
	if err := s.fs.WaitForChangeFile(ctx, slug, "proposal.md"); err != nil {
		return fmt.Errorf("proposal.md not created: %w", err)
	}

	// Verify metadata
	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	if metadata.Status != "created" {
		return fmt.Errorf("expected status 'created', got '%s'", metadata.Status)
	}

	result.SetDetail("proposal_status", metadata.Status)
	return nil
}

// stageWriteSpecWithContext writes a spec that references existing code.
func (s *BrownfieldScenario) stageWriteSpecWithContext(ctx context.Context, result *Result) error {
	slug := "add-session-management"

	// Write a spec that explicitly references existing types and functions
	specContent := `# Session Management Specification

## Overview

Add session management capabilities to the existing auth package, building on
the current User and Token types.

## Existing Code Context

This enhancement builds on:
- **User type** (internal/auth/auth.go:12) - Will be extended with session association
- **Token type** (internal/auth/auth.go:19) - Sessions will reference tokens
- **Authenticate function** (internal/auth/auth.go:29) - Will create sessions on successful auth

## Requirements

1. Add Session type to track active user sessions
2. Integrate session creation with existing Authenticate function
3. Add session validation middleware
4. Support session revocation

## Implementation

### New Types

` + "```go" + `
type Session struct {
    ID        string
    UserID    string
    Token     *Token
    CreatedAt time.Time
    ExpiresAt time.Time
    Metadata  map[string]string
}
` + "```" + `

### Modified Functions

1. Modify Authenticate to return session alongside user
2. Add CreateSession, GetSession, RevokeSession functions

## Testing

- Unit tests for session CRUD operations
- Integration tests for auth + session flow
- Tests for session expiration handling
`

	specPath := fmt.Sprintf(".semspec/changes/%s/spec.md", slug)
	if err := s.fs.WriteFileRelative(specPath, specContent); err != nil {
		return fmt.Errorf("write spec.md: %w", err)
	}

	if !s.fs.ChangeHasFile(slug, "spec.md") {
		return fmt.Errorf("spec.md not found after write")
	}

	result.SetDetail("spec_written", true)
	result.SetDetail("references_existing_code", true)
	return nil
}

// stageApproveChange approves the brownfield change.
func (s *BrownfieldScenario) stageApproveChange(ctx context.Context, result *Result) error {
	slug := "add-session-management"

	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/approve %s", slug))
	if err != nil {
		return fmt.Errorf("send approve command: %w", err)
	}

	// Accept either success or constitution-based rejection
	if resp.Type == "error" {
		if strings.Contains(strings.ToLower(resp.Content), "constitution") {
			result.AddWarning("approve blocked by constitution")
			return nil
		}
		return fmt.Errorf("approve returned error: %s", resp.Content)
	}

	result.SetDetail("approve_response", resp.Content)
	return nil
}

// stageVerifyApproved verifies the change status.
func (s *BrownfieldScenario) stageVerifyApproved(ctx context.Context, result *Result) error {
	slug := "add-session-management"

	// Check if constitution blocked
	if len(result.Warnings) > 0 {
		metadata, err := s.fs.LoadChangeMetadata(slug)
		if err != nil {
			return fmt.Errorf("load metadata: %w", err)
		}
		result.SetDetail("final_status", metadata.Status)
		result.SetDetail("blocked_by_constitution", true)
		return nil
	}

	// Wait for approved status
	if err := s.fs.WaitForChangeStatus(ctx, slug, "approved"); err != nil {
		return fmt.Errorf("change not approved: %w", err)
	}

	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load final metadata: %w", err)
	}

	result.SetDetail("final_status", metadata.Status)
	return nil
}
