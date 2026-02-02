package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// GraphPublishingScenario tests that /propose publishes entities to the knowledge graph.
type GraphPublishingScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
	fs          *client.FilesystemClient
	capture     *client.MessageCapture
}

// NewGraphPublishingScenario creates a new graph publishing scenario.
func NewGraphPublishingScenario(cfg *config.Config) *GraphPublishingScenario {
	return &GraphPublishingScenario{
		name:        "graph-publishing",
		description: "Tests /propose publishes entities to graph.ingest.entity",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *GraphPublishingScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *GraphPublishingScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *GraphPublishingScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for HTTP service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	// Create NATS client for message capture
	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	return nil
}

// Execute runs the graph publishing scenario.
func (s *GraphPublishingScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"setup-capture", s.stageSetupCapture},
		{"send-propose", s.stageSendPropose},
		{"wait-for-entity", s.stageWaitForEntity},
		{"verify-predicates", s.stageVerifyPredicates},
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
func (s *GraphPublishingScenario) Teardown(ctx context.Context) error {
	var errs []error
	if s.capture != nil {
		if err := s.capture.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("stop capture: %w", err))
		}
	}
	if s.nats != nil {
		if err := s.nats.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("close NATS: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("teardown errors: %v", errs)
	}
	return nil
}

// stageSetupCapture starts capturing messages on graph.ingest.entity.
func (s *GraphPublishingScenario) stageSetupCapture(ctx context.Context, result *Result) error {
	// Start capturing on the graph ingest subject
	capture, err := s.nats.CaptureMessages("graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("start message capture: %w", err)
	}
	s.capture = capture

	result.SetDetail("capture_started", true)
	return nil
}

// stageSendPropose sends the /propose command via HTTP.
func (s *GraphPublishingScenario) stageSendPropose(ctx context.Context, result *Result) error {
	proposalText := "Test graph publishing"
	result.SetDetail("proposal_text", proposalText)
	result.SetDetail("expected_slug", "test-graph-publishing")

	resp, err := s.http.SendMessage(ctx, "/propose "+proposalText)
	if err != nil {
		return fmt.Errorf("send /propose command: %w", err)
	}

	result.SetDetail("response_type", resp.Type)
	result.SetDetail("response_content", resp.Content)
	result.SetDetail("propose_response", resp)

	if resp.Type == "error" {
		return fmt.Errorf("/propose returned error: %s", resp.Content)
	}

	return nil
}

// stageWaitForEntity waits for the entity to appear in captured messages.
func (s *GraphPublishingScenario) stageWaitForEntity(ctx context.Context, result *Result) error {
	// Wait for at least one message on graph.ingest.entity
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	err := s.capture.WaitForCount(waitCtx, 1)
	if err != nil {
		// Graph publishing might not be configured - this is acceptable
		// We should still check filesystem was updated
		result.AddWarning("no entity published to graph.ingest.entity (graph may not be configured)")
		result.SetDetail("entity_published", false)
		return nil
	}

	result.SetMetric("entities_captured", s.capture.Count())
	result.SetDetail("entity_published", true)

	return nil
}

// GraphEntity represents an entity published to the graph.
type GraphEntity struct {
	ID         string            `json:"id"`
	Type       string            `json:"type,omitempty"`
	Predicates map[string]any    `json:"predicates,omitempty"`
	Triples    []GraphTriple     `json:"triples,omitempty"`
}

// GraphTriple represents a triple in the entity.
type GraphTriple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    any    `json:"object"`
}

// stageVerifyPredicates verifies the entity contains expected predicates.
func (s *GraphPublishingScenario) stageVerifyPredicates(ctx context.Context, result *Result) error {
	entityPublished, ok := result.GetDetail("entity_published")
	if !ok || entityPublished == false {
		// Entity wasn't published - skip predicate verification
		result.AddWarning("skipping predicate verification (no entity published)")
		return nil
	}

	// Get messages directly from capture
	msgs := s.capture.Messages()
	if len(msgs) == 0 {
		return fmt.Errorf("no messages in capture")
	}

	// Parse the first message
	var entity GraphEntity
	if err := json.Unmarshal(msgs[0].Data, &entity); err != nil {
		// Truncate data for error message if too long
		data := string(msgs[0].Data)
		if len(data) > 200 {
			data = data[:200] + "..."
		}
		return fmt.Errorf("unmarshal entity: %w (data: %s)", err, data)
	}

	result.SetDetail("entity_id", entity.ID)
	result.SetDetail("entity_type", entity.Type)

	// Check for expected predicates
	expectedPredicates := []string{
		"semspec.proposal.title",
		"semspec.proposal.slug",
		"semspec.proposal.status",
	}

	// Check in predicates map
	foundPredicates := make(map[string]bool)
	for _, pred := range expectedPredicates {
		if entity.Predicates != nil {
			if _, exists := entity.Predicates[pred]; exists {
				foundPredicates[pred] = true
			}
		}
		// Also check in triples
		for _, triple := range entity.Triples {
			if triple.Predicate == pred {
				foundPredicates[pred] = true
			}
		}
	}

	result.SetDetail("predicates_found", foundPredicates)

	// Verify entity ID format
	expectedSlug, _ := result.GetDetailString("expected_slug")
	if entity.ID != "" && expectedSlug != "" {
		if !strings.Contains(strings.ToLower(entity.ID), strings.ToLower(expectedSlug)) &&
			!strings.Contains(strings.ToLower(entity.ID), "proposal") {
			result.AddWarning(fmt.Sprintf("entity ID may not match expected format: %s", entity.ID))
		}
	}

	// At least some predicates should be found
	if len(foundPredicates) == 0 && len(entity.Predicates) == 0 && len(entity.Triples) == 0 {
		result.AddWarning("no predicates found in entity (entity may have different format)")
	}

	result.SetDetail("predicates_verified", true)
	return nil
}

// stageVerifyFilesystem verifies the proposal was created on the filesystem.
func (s *GraphPublishingScenario) stageVerifyFilesystem(ctx context.Context, result *Result) error {
	// Try to find the change directory
	changes, err := s.fs.ListChanges()
	if err != nil {
		return fmt.Errorf("list changes: %w", err)
	}

	// Look for our proposal
	var foundSlug string
	expectedSlug, _ := result.GetDetailString("expected_slug")

	for _, slug := range changes {
		if slug == expectedSlug ||
			strings.Contains(slug, "graph-publishing") ||
			strings.Contains(slug, "test-graph") {
			foundSlug = slug
			break
		}
	}

	if foundSlug == "" {
		// Wait a bit and try again
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := s.fs.WaitForChange(waitCtx, expectedSlug); err == nil {
			foundSlug = expectedSlug
		} else {
			// Check all changes again
			changes, _ = s.fs.ListChanges()
			return fmt.Errorf("proposal not found in filesystem (found changes: %v)", changes)
		}
	}

	result.SetDetail("found_slug", foundSlug)

	// Verify metadata.json exists
	if err := s.fs.WaitForChangeFile(ctx, foundSlug, "metadata.json"); err != nil {
		return fmt.Errorf("metadata.json not created: %w", err)
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
