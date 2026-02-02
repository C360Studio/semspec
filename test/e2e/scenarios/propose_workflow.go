package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ProposeWorkflowScenario tests the /propose command that creates graph entities.
// This is the primary test for verifying command processing and state creation.
type ProposeWorkflowScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewProposeWorkflowScenario creates a new propose workflow scenario.
func NewProposeWorkflowScenario(cfg *config.Config) *ProposeWorkflowScenario {
	return &ProposeWorkflowScenario{
		name:        "propose-workflow",
		description: "Tests /propose command with graph entity creation and KV state verification",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ProposeWorkflowScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ProposeWorkflowScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ProposeWorkflowScenario) Setup(ctx context.Context) error {
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

// Execute runs the propose workflow scenario.
func (s *ProposeWorkflowScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"send-propose", s.stageSendPropose},
		{"verify-response", s.stageVerifyResponse},
		{"verify-filesystem", s.stageVerifyFilesystem},
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
func (s *ProposeWorkflowScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stageSendPropose sends the /propose command via HTTP.
func (s *ProposeWorkflowScenario) stageSendPropose(ctx context.Context, result *Result) error {
	proposalText := "Add user authentication feature"
	result.SetDetail("proposal_text", proposalText)
	result.SetDetail("expected_slug", "add-user-authentication-feature")

	resp, err := s.http.SendMessage(ctx, "/propose "+proposalText)
	if err != nil {
		return fmt.Errorf("send /propose command: %w", err)
	}

	result.SetDetail("response_type", resp.Type)
	result.SetDetail("response_content", resp.Content)
	result.SetDetail("propose_response", resp)

	return nil
}

// stageVerifyResponse verifies the /propose response content.
func (s *ProposeWorkflowScenario) stageVerifyResponse(ctx context.Context, result *Result) error {
	respVal, ok := result.GetDetail("propose_response")
	if !ok {
		return fmt.Errorf("propose_response not found in result")
	}

	resp, ok := respVal.(*client.MessageResponse)
	if !ok {
		return fmt.Errorf("propose_response has wrong type")
	}

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("propose returned error: %s", resp.Content)
	}

	// Verify response contains proposal confirmation
	content := strings.ToLower(resp.Content)
	hasProposalInfo := strings.Contains(content, "propos") ||
		strings.Contains(content, "creat") ||
		strings.Contains(content, "add-user") ||
		strings.Contains(content, "authentication")

	if !hasProposalInfo {
		return fmt.Errorf("propose response doesn't contain expected info: %s", resp.Content)
	}

	// Try to extract slug from response
	slug := extractSlug(resp.Content)
	if slug != "" {
		result.SetDetail("extracted_slug", slug)
	}

	result.SetDetail("response_validated", true)
	return nil
}

// stageVerifyFilesystem verifies the proposal was created on the filesystem.
func (s *ProposeWorkflowScenario) stageVerifyFilesystem(ctx context.Context, result *Result) error {
	// Try to find the change directory
	changes, err := s.fs.ListChanges()
	if err != nil {
		return fmt.Errorf("list changes: %w", err)
	}

	// Look for our proposal
	var foundSlug string
	expectedSlug, _ := result.GetDetailString("expected_slug")
	extractedSlug, _ := result.GetDetailString("extracted_slug")

	for _, slug := range changes {
		if slug == expectedSlug || slug == extractedSlug ||
			strings.Contains(slug, "user-auth") ||
			strings.Contains(slug, "authentication") {
			foundSlug = slug
			break
		}
	}

	if foundSlug == "" {
		// Wait a bit and try again
		if err := s.fs.WaitForChange(ctx, expectedSlug); err == nil {
			foundSlug = expectedSlug
		} else {
			return fmt.Errorf("proposal not found in filesystem (changes: %v)", changes)
		}
	}

	result.SetDetail("found_slug", foundSlug)

	// Verify metadata.json exists
	if err := s.fs.WaitForChangeFile(ctx, foundSlug, "metadata.json"); err != nil {
		return fmt.Errorf("metadata.json not created: %w", err)
	}

	// Verify proposal.md exists
	if err := s.fs.WaitForChangeFile(ctx, foundSlug, "proposal.md"); err != nil {
		return fmt.Errorf("proposal.md not created: %w", err)
	}

	// Load and verify metadata
	metadata, err := s.fs.LoadChangeMetadata(foundSlug)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	if metadata.Status == "" {
		return fmt.Errorf("metadata has no status")
	}

	result.SetDetail("metadata_status", metadata.Status)
	result.SetDetail("metadata_title", metadata.Title)
	result.SetDetail("filesystem_verified", true)

	return nil
}

// extractSlug attempts to extract a slug from response content.
func extractSlug(content string) string {
	lower := strings.ToLower(content)

	markers := []string{"slug:", "created ", "proposal ", "`"}
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx != -1 {
			start := idx + len(marker)
			if start >= len(content) {
				continue
			}

			rest := content[start:]
			rest = strings.TrimLeft(rest, " `")

			end := strings.IndexAny(rest, " \n\t`")
			if end == -1 {
				end = len(rest)
			}

			slug := rest[:end]
			slug = strings.Trim(slug, ".,!?")

			if len(slug) > 3 && strings.Contains(slug, "-") && slug == strings.ToLower(slug) {
				return slug
			}
		}
	}

	return ""
}
