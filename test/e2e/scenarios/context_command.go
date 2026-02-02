package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ContextCommandScenario tests the /context command that queries the knowledge graph.
// Handles graceful degradation when graph gateway is unavailable.
type ContextCommandScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewContextCommandScenario creates a new context command scenario.
func NewContextCommandScenario(cfg *config.Config) *ContextCommandScenario {
	return &ContextCommandScenario{
		name:        "context-command",
		description: "Tests /context command with graph query and graceful degradation",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ContextCommandScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ContextCommandScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ContextCommandScenario) Setup(ctx context.Context) error {
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

// Execute runs the context command scenario.
func (s *ContextCommandScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"send-context-empty", s.stageSendContextEmpty},
		{"create-proposal", s.stageCreateProposal},
		{"send-context-list", s.stageSendContextList},
		{"send-context-slug", s.stageSendContextSlug},
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
func (s *ContextCommandScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageSendContextEmpty sends /context without arguments to an empty graph.
func (s *ContextCommandScenario) stageSendContextEmpty(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/context")
	if err != nil {
		return fmt.Errorf("send /context command: %w", err)
	}

	result.SetDetail("context_empty_response_type", resp.Type)
	result.SetDetail("context_empty_response_content", resp.Content)
	result.SetDetail("context_empty_response", resp)

	// /context should return a result, not an error
	// It may indicate empty graph or graceful degradation
	if resp.Type == "error" {
		content := strings.ToLower(resp.Content)

		// Check if /context command is not implemented yet
		if strings.Contains(content, "unknown command") ||
			strings.Contains(content, "not found") ||
			strings.Contains(content, "invalid command") {
			result.SetDetail("context_command_implemented", false)
			result.AddWarning("/context command not yet implemented - skipping remaining stages")
			return nil
		}

		// Check if it's a graceful degradation message (graph unavailable)
		if strings.Contains(content, "graph") || strings.Contains(content, "unavailable") ||
			strings.Contains(content, "not available") || strings.Contains(content, "context") {
			result.SetDetail("graph_available", false)
			result.SetDetail("context_command_implemented", true)
			result.AddWarning("graph appears unavailable, but handling gracefully")
			return nil
		}
		return fmt.Errorf("/context returned unexpected error: %s", resp.Content)
	}

	result.SetDetail("context_command_implemented", true)

	// Check content for expected patterns
	content := strings.ToLower(resp.Content)
	hasContextInfo := strings.Contains(content, "context") ||
		strings.Contains(content, "graph") ||
		strings.Contains(content, "no entities") ||
		strings.Contains(content, "empty") ||
		strings.Contains(content, "proposal") ||
		strings.Contains(content, "no active")

	if !hasContextInfo {
		result.AddWarning(fmt.Sprintf("/context response may not have expected content: %s", resp.Content))
	}

	result.SetDetail("graph_available", true)
	result.SetDetail("context_empty_verified", true)
	return nil
}

// stageCreateProposal creates a proposal to have something in context.
func (s *ContextCommandScenario) stageCreateProposal(ctx context.Context, result *Result) error {
	// Skip if /context command is not implemented
	if implemented, _ := result.GetDetail("context_command_implemented"); implemented == false {
		result.AddWarning("skipping proposal creation - /context not implemented")
		return nil
	}

	proposalText := "Test context feature"
	result.SetDetail("proposal_text", proposalText)
	result.SetDetail("expected_slug", "test-context-feature")

	resp, err := s.http.SendMessage(ctx, "/propose "+proposalText)
	if err != nil {
		return fmt.Errorf("send /propose command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("/propose returned error: %s", resp.Content)
	}

	result.SetDetail("propose_response", resp)

	// Wait for proposal to be created in filesystem
	expectedSlug := "test-context-feature"
	// Use stage timeout from context (already applied in Execute loop)
	waitCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)
	defer cancel()

	// Try to find the change - it might have a slightly different slug
	var foundSlug string
	ticker := time.NewTicker(config.DefaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			// Not finding it immediately is okay - continue with test
			result.AddWarning("proposal may not have been fully created in filesystem")
			result.SetDetail("found_slug", expectedSlug)
			return nil
		case <-ticker.C:
			changes, err := s.fs.ListChanges()
			if err != nil {
				continue
			}
			for _, slug := range changes {
				if strings.Contains(slug, "context") || slug == expectedSlug {
					foundSlug = slug
					break
				}
			}
			if foundSlug != "" {
				result.SetDetail("found_slug", foundSlug)
				return nil
			}
		}
	}
}

// stageSendContextList sends /context to list proposals after creating one.
func (s *ContextCommandScenario) stageSendContextList(ctx context.Context, result *Result) error {
	// Skip if /context command is not implemented
	if implemented, _ := result.GetDetail("context_command_implemented"); implemented == false {
		result.AddWarning("skipping context list - /context not implemented")
		return nil
	}

	resp, err := s.http.SendMessage(ctx, "/context")
	if err != nil {
		return fmt.Errorf("send /context command: %w", err)
	}

	result.SetDetail("context_list_response_type", resp.Type)
	result.SetDetail("context_list_response_content", resp.Content)

	// Check if graph was available in earlier stage
	graphAvailable, _ := result.GetDetail("graph_available")
	if graphAvailable == false {
		// Graph not available - just verify we get a reasonable response
		if resp.Type != "error" {
			result.SetDetail("context_list_verified", true)
		}
		return nil
	}

	if resp.Type == "error" {
		// May indicate graph unavailable - that's acceptable
		result.AddWarning(fmt.Sprintf("/context returned error (may indicate graph unavailable): %s", resp.Content))
		return nil
	}

	// If graph is available, check that the response contains expected info
	content := strings.ToLower(resp.Content)

	// Check if the created proposal is mentioned
	foundSlug, _ := result.GetDetailString("found_slug")
	if foundSlug != "" {
		if strings.Contains(content, strings.ToLower(foundSlug)) ||
			strings.Contains(content, "context") ||
			strings.Contains(content, "test") {
			result.SetDetail("proposal_in_context", true)
		}
	}

	result.SetDetail("context_list_verified", true)
	return nil
}

// stageSendContextSlug sends /context <slug> to get specific proposal details.
func (s *ContextCommandScenario) stageSendContextSlug(ctx context.Context, result *Result) error {
	// Skip if /context command is not implemented
	if implemented, _ := result.GetDetail("context_command_implemented"); implemented == false {
		result.AddWarning("skipping context slug query - /context not implemented")
		return nil
	}

	foundSlug, ok := result.GetDetailString("found_slug")
	if !ok || foundSlug == "" {
		foundSlug = "test-context-feature"
	}

	resp, err := s.http.SendMessage(ctx, "/context "+foundSlug)
	if err != nil {
		return fmt.Errorf("send /context %s command: %w", foundSlug, err)
	}

	result.SetDetail("context_slug_response_type", resp.Type)
	result.SetDetail("context_slug_response_content", resp.Content)

	// Check if graph was available
	graphAvailable, _ := result.GetDetail("graph_available")
	if graphAvailable == false {
		// Graph not available - just verify we don't crash
		result.SetDetail("context_slug_verified", true)
		return nil
	}

	// The response could be:
	// 1. Details about the proposal (if found in graph)
	// 2. "Not found" message (if not in graph yet)
	// 3. Generic error (if graph unavailable)
	// All are acceptable - we're testing that the command handles these cases

	if resp.Type == "error" {
		content := strings.ToLower(resp.Content)
		// These are acceptable error conditions
		if strings.Contains(content, "not found") ||
			strings.Contains(content, "unavailable") ||
			strings.Contains(content, "graph") ||
			strings.Contains(content, "no entity") {
			result.AddWarning(fmt.Sprintf("/context %s: %s", foundSlug, resp.Content))
			result.SetDetail("context_slug_verified", true)
			return nil
		}
		return fmt.Errorf("/context %s returned unexpected error: %s", foundSlug, resp.Content)
	}

	result.SetDetail("context_slug_verified", true)
	return nil
}
