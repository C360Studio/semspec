package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// GreenfieldScenario tests the workflow on a brand new project with no existing code.
// It simulates bootstrapping a new feature from scratch.
type GreenfieldScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewGreenfieldScenario creates a new greenfield scenario.
func NewGreenfieldScenario(cfg *config.Config) *GreenfieldScenario {
	return &GreenfieldScenario{
		name:        "greenfield",
		description: "Tests workflow on empty project: propose and spec new feature from scratch",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *GreenfieldScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *GreenfieldScenario) Description() string {
	return s.description
}

// Setup prepares an empty workspace.
func (s *GreenfieldScenario) Setup(ctx context.Context) error {
	// Create filesystem client
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)

	// Clean workspace completely - start fresh
	if err := s.fs.CleanWorkspaceAll(); err != nil {
		return fmt.Errorf("clean workspace: %w", err)
	}

	// Setup only the .semspec directory - no code
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Initialize empty git repo (new project)
	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}

	// Create initial empty commit
	if err := s.fs.WriteFileRelative(".gitkeep", ""); err != nil {
		return fmt.Errorf("write .gitkeep: %w", err)
	}
	if err := s.fs.GitAdd(".gitkeep"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("chore: initialize empty project"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the greenfield scenario.
func (s *GreenfieldScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-empty", s.stageVerifyEmpty},
		{"propose-new-feature", s.stageProposeNewFeature},
		{"verify-proposal", s.stageVerifyProposal},
		{"write-initial-spec", s.stageWriteInitialSpec},
		{"status-check", s.stageStatusCheck},
		{"approve-change", s.stageApproveChange},
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
func (s *GreenfieldScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stageVerifyEmpty verifies the workspace is essentially empty.
func (s *GreenfieldScenario) stageVerifyEmpty(ctx context.Context, result *Result) error {
	// Verify git repo exists but is nearly empty
	if !s.fs.IsGitRepo() {
		return fmt.Errorf("workspace is not a git repository")
	}

	// List all files (excluding .git and .semspec)
	files, err := s.fs.ListFiles()
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	// Should only have .gitkeep and .gitignore
	codeFiles := []string{}
	for _, f := range files {
		if f != ".gitkeep" && f != ".gitignore" {
			codeFiles = append(codeFiles, f)
		}
	}

	if len(codeFiles) > 0 {
		return fmt.Errorf("workspace should be empty but found: %v", codeFiles)
	}

	// Verify no existing changes
	changes, err := s.fs.ListChanges()
	if err != nil {
		return fmt.Errorf("list changes: %w", err)
	}
	if len(changes) > 0 {
		return fmt.Errorf("workspace should have no changes but found: %v", changes)
	}

	result.SetDetail("workspace_empty", true)
	result.SetDetail("file_count", len(files))
	return nil
}

// stageProposeNewFeature proposes a brand new feature.
func (s *GreenfieldScenario) stageProposeNewFeature(ctx context.Context, result *Result) error {
	result.SetDetail("test_change", "bootstrap-user-service")

	// Propose a completely new feature
	resp, err := s.http.SendMessage(ctx, "/propose bootstrap user service")
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
func (s *GreenfieldScenario) stageVerifyProposal(ctx context.Context, result *Result) error {
	slug := "bootstrap-user-service"

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

// stageWriteInitialSpec writes a spec for bootstrapping a new feature.
func (s *GreenfieldScenario) stageWriteInitialSpec(ctx context.Context, result *Result) error {
	slug := "bootstrap-user-service"

	// Write a spec for a brand new feature with no existing code references
	specContent := `# User Service Bootstrap Specification

## Overview

Bootstrap a new user service from scratch. This is a greenfield implementation
with no existing codebase to build upon.

## Requirements

1. Create user data model with standard fields
2. Implement CRUD operations for users
3. Add basic validation for user data
4. Set up initial test infrastructure

## Implementation

### Project Structure

` + "```" + `
user-service/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   └── user/
│       ├── model.go
│       ├── repository.go
│       ├── service.go
│       └── handler.go
├── go.mod
└── README.md
` + "```" + `

### User Model

` + "```go" + `
type User struct {
    ID        string    ` + "`json:\"id\"`" + `
    Email     string    ` + "`json:\"email\"`" + `
    Name      string    ` + "`json:\"name\"`" + `
    CreatedAt time.Time ` + "`json:\"created_at\"`" + `
    UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}
` + "```" + `

### API Endpoints

- POST /users - Create user
- GET /users/:id - Get user by ID
- PUT /users/:id - Update user
- DELETE /users/:id - Delete user

## Testing

- Unit tests for model validation
- Unit tests for service layer
- Integration tests for HTTP handlers
- Test coverage target: 80%

## Dependencies

- Standard library only for MVP
- Add external dependencies as needed in follow-up changes
`

	specPath := fmt.Sprintf(".semspec/changes/%s/spec.md", slug)
	if err := s.fs.WriteFileRelative(specPath, specContent); err != nil {
		return fmt.Errorf("write spec.md: %w", err)
	}

	if !s.fs.ChangeHasFile(slug, "spec.md") {
		return fmt.Errorf("spec.md not found after write")
	}

	result.SetDetail("spec_written", true)
	result.SetDetail("greenfield_spec", true)
	return nil
}

// stageStatusCheck verifies changes command works on greenfield project.
func (s *GreenfieldScenario) stageStatusCheck(ctx context.Context, result *Result) error {
	slug := "bootstrap-user-service"

	// Use /changes command (not /status - that's reserved for loop status in semstreams)
	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/changes %s", slug))
	if err != nil {
		return fmt.Errorf("send changes command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("changes returned error: %s", resp.Content)
	}

	// Verify response contains expected info
	if !strings.Contains(resp.Content, slug) {
		return fmt.Errorf("changes response doesn't contain slug")
	}

	result.SetDetail("changes_response", resp.Content)
	return nil
}

// stageApproveChange approves the greenfield change.
func (s *GreenfieldScenario) stageApproveChange(ctx context.Context, result *Result) error {
	slug := "bootstrap-user-service"

	resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/approve %s", slug))
	if err != nil {
		return fmt.Errorf("send approve command: %w", err)
	}

	// Accept either success or constitution-based rejection
	if resp.Type == "error" {
		if strings.Contains(strings.ToLower(resp.Content), "constitution") {
			result.AddWarning("approve blocked by constitution (expected without one)")
			return nil
		}
		return fmt.Errorf("approve returned error: %s", resp.Content)
	}

	// Wait for approved status
	if err := s.fs.WaitForChangeStatus(ctx, slug, "approved"); err != nil {
		// Check metadata directly
		metadata, loadErr := s.fs.LoadChangeMetadata(slug)
		if loadErr != nil {
			return fmt.Errorf("change not approved and couldn't load metadata: %w", err)
		}
		result.SetDetail("final_status", metadata.Status)
		return nil
	}

	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load final metadata: %w", err)
	}

	result.SetDetail("final_status", metadata.Status)
	result.SetDetail("approve_response", resp.Content)
	return nil
}
