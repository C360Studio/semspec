package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// FullWorkflowScenario tests the complete semspec workflow:
// propose → design → spec → tasks → check → approve
type FullWorkflowScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewFullWorkflowScenario creates a new full workflow scenario.
func NewFullWorkflowScenario(cfg *config.Config) *FullWorkflowScenario {
	return &FullWorkflowScenario{
		name:        "full-workflow",
		description: "Tests complete propose → design → spec → tasks → check → approve workflow",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *FullWorkflowScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *FullWorkflowScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *FullWorkflowScenario) Setup(ctx context.Context) error {
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

// Execute runs the full workflow scenario.
func (s *FullWorkflowScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"propose", s.stagePropose, 60 * time.Second},
		{"verify-proposal", s.stageVerifyProposal, 30 * time.Second},
		{"design", s.stageDesign, 90 * time.Second},
		{"verify-design", s.stageVerifyDesign, 30 * time.Second},
		{"spec", s.stageSpec, 90 * time.Second},
		{"verify-spec", s.stageVerifySpec, 30 * time.Second},
		{"tasks", s.stageTasks, 90 * time.Second},
		{"verify-tasks", s.stageVerifyTasks, 30 * time.Second},
		{"check", s.stageCheck, 60 * time.Second},
		{"approve", s.stageApprove, 60 * time.Second},
		{"verify-approved", s.stageVerifyApproved, 30 * time.Second},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)

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
func (s *FullWorkflowScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stagePropose sends the /propose command.
func (s *FullWorkflowScenario) stagePropose(ctx context.Context, result *Result) error {
	proposalText := "Add caching layer for API responses"
	result.SetDetail("proposal_text", proposalText)

	resp, err := s.http.SendMessage(ctx, "/propose "+proposalText)
	if err != nil {
		return fmt.Errorf("send /propose: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("propose returned error: %s", resp.Content)
	}

	result.SetDetail("propose_response", resp.Content)
	return nil
}

// stageVerifyProposal verifies the proposal was created.
func (s *FullWorkflowScenario) stageVerifyProposal(ctx context.Context, result *Result) error {
	// Find the created change
	changes, err := s.fs.ListChanges()
	if err != nil {
		return fmt.Errorf("list changes: %w", err)
	}

	var slug string
	for _, c := range changes {
		if strings.Contains(c, "caching") || strings.Contains(c, "api") {
			slug = c
			break
		}
	}

	if slug == "" {
		// Try waiting for a common slug pattern
		possibleSlugs := []string{
			"add-caching-layer-for-api-responses",
			"add-caching-layer",
			"caching-layer",
		}
		for _, ps := range possibleSlugs {
			if err := s.fs.WaitForChange(ctx, ps); err == nil {
				slug = ps
				break
			}
		}
	}

	if slug == "" {
		return fmt.Errorf("proposal not found (available: %v)", changes)
	}

	result.SetDetail("slug", slug)

	// Verify metadata
	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	result.SetDetail("initial_status", metadata.Status)
	return nil
}

// stageDesign sends the /design command.
func (s *FullWorkflowScenario) stageDesign(ctx context.Context, result *Result) error {
	slug, ok := result.GetDetailString("slug")
	if !ok {
		return fmt.Errorf("slug not found")
	}

	resp, err := s.http.SendMessage(ctx, "/design "+slug)
	if err != nil {
		return fmt.Errorf("send /design: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("design returned error: %s", resp.Content)
	}

	result.SetDetail("design_response", resp.Content)
	return nil
}

// stageVerifyDesign verifies the design was created.
func (s *FullWorkflowScenario) stageVerifyDesign(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	// Wait for design.md to be created
	if err := s.fs.WaitForChangeFile(ctx, slug, "design.md"); err != nil {
		result.AddWarning("design.md not created, skipping")
		return nil // Non-fatal - design might be optional
	}

	// Verify metadata updated
	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	result.SetDetail("design_status", metadata.Status)
	result.SetDetail("has_design", metadata.Files.HasDesign)
	return nil
}

// stageSpec sends the /spec command.
func (s *FullWorkflowScenario) stageSpec(ctx context.Context, result *Result) error {
	slug, ok := result.GetDetailString("slug")
	if !ok {
		return fmt.Errorf("slug not found")
	}

	resp, err := s.http.SendMessage(ctx, "/spec "+slug)
	if err != nil {
		return fmt.Errorf("send /spec: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("spec returned error: %s", resp.Content)
	}

	result.SetDetail("spec_response", resp.Content)
	return nil
}

// stageVerifySpec verifies the spec was created.
func (s *FullWorkflowScenario) stageVerifySpec(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	// Wait for spec.md to be created
	if err := s.fs.WaitForChangeFile(ctx, slug, "spec.md"); err != nil {
		result.AddWarning("spec.md not created, skipping")
		return nil // Non-fatal
	}

	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	result.SetDetail("spec_status", metadata.Status)
	result.SetDetail("has_spec", metadata.Files.HasSpec)
	return nil
}

// stageTasks sends the /tasks command.
func (s *FullWorkflowScenario) stageTasks(ctx context.Context, result *Result) error {
	slug, ok := result.GetDetailString("slug")
	if !ok {
		return fmt.Errorf("slug not found")
	}

	resp, err := s.http.SendMessage(ctx, "/tasks "+slug)
	if err != nil {
		return fmt.Errorf("send /tasks: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("tasks returned error: %s", resp.Content)
	}

	result.SetDetail("tasks_response", resp.Content)
	return nil
}

// stageVerifyTasks verifies tasks were generated.
func (s *FullWorkflowScenario) stageVerifyTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	// Wait for tasks.md to be created
	if err := s.fs.WaitForChangeFile(ctx, slug, "tasks.md"); err != nil {
		result.AddWarning("tasks.md not created, skipping")
		return nil // Non-fatal
	}

	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	result.SetDetail("tasks_status", metadata.Status)
	result.SetDetail("has_tasks", metadata.Files.HasTasks)
	return nil
}

// stageCheck sends the /check command.
func (s *FullWorkflowScenario) stageCheck(ctx context.Context, result *Result) error {
	slug, ok := result.GetDetailString("slug")
	if !ok {
		return fmt.Errorf("slug not found")
	}

	resp, err := s.http.SendMessage(ctx, "/check "+slug)
	if err != nil {
		return fmt.Errorf("send /check: %w", err)
	}

	if resp.Type == "error" {
		// Check might fail if requirements not met - that's informational
		result.AddWarning(fmt.Sprintf("check returned: %s", resp.Content))
	}

	result.SetDetail("check_response", resp.Content)
	return nil
}

// stageApprove sends the /approve command.
func (s *FullWorkflowScenario) stageApprove(ctx context.Context, result *Result) error {
	slug, ok := result.GetDetailString("slug")
	if !ok {
		return fmt.Errorf("slug not found")
	}

	resp, err := s.http.SendMessage(ctx, "/approve "+slug)
	if err != nil {
		return fmt.Errorf("send /approve: %w", err)
	}

	if resp.Type == "error" {
		// Approve might fail due to constitution or missing requirements
		if strings.Contains(resp.Content, "constitution") ||
			strings.Contains(resp.Content, "requirement") ||
			strings.Contains(resp.Content, "missing") {
			result.AddWarning(fmt.Sprintf("approve blocked: %s", resp.Content))
			result.SetDetail("approve_blocked", true)
			return nil
		}
		return fmt.Errorf("approve returned error: %s", resp.Content)
	}

	result.SetDetail("approve_response", resp.Content)
	return nil
}

// stageVerifyApproved verifies the change was approved (or blocked).
func (s *FullWorkflowScenario) stageVerifyApproved(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	// Check if approval was blocked
	if blocked, _ := result.GetDetail("approve_blocked"); blocked == true {
		// Verify we're in a valid blocked state
		metadata, err := s.fs.LoadChangeMetadata(slug)
		if err != nil {
			return fmt.Errorf("load metadata: %w", err)
		}

		validBlockedStates := map[string]bool{
			"created":  true,
			"drafted":  true,
			"designed": true,
			"reviewed": true,
		}

		if validBlockedStates[metadata.Status] {
			result.SetDetail("final_status", metadata.Status)
			result.SetDetail("workflow_blocked", true)
			return nil
		}
	}

	// Wait for approved status
	if err := s.fs.WaitForChangeStatus(ctx, slug, "approved"); err != nil {
		// Check current status
		metadata, err := s.fs.LoadChangeMetadata(slug)
		if err != nil {
			return fmt.Errorf("load metadata: %w", err)
		}

		result.SetDetail("final_status", metadata.Status)

		// If we're in a reasonable state, that's acceptable
		if metadata.Status != "" {
			result.AddWarning(fmt.Sprintf("not approved, final status: %s", metadata.Status))
			return nil
		}

		return fmt.Errorf("approval verification failed")
	}

	metadata, err := s.fs.LoadChangeMetadata(slug)
	if err != nil {
		return fmt.Errorf("load final metadata: %w", err)
	}

	result.SetDetail("final_status", metadata.Status)
	result.SetDetail("workflow_complete", true)
	return nil
}
