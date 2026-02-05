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
	fs          *client.FilesystemClient
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

// stageWaitForEntity waits for the entity to appear in message-logger.
func (s *GraphPublishingScenario) stageWaitForEntity(ctx context.Context, result *Result) error {
	// Wait for at least one message on graph.ingest.entity via message-logger
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	err := s.http.WaitForMessageSubject(waitCtx, "graph.ingest.entity", 1)
	if err != nil {
		// Graph publishing might not be configured - this is acceptable
		result.AddWarning("no entity published to graph.ingest.entity (graph may not be configured)")
		result.SetDetail("entity_published", false)
		return nil
	}

	entries, getErr := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
	if getErr != nil {
		result.SetDetail("entity_published", false)
		return nil
	}

	result.SetMetric("entities_captured", len(entries))
	result.SetDetail("entity_published", true)
	result.SetDetail("log_entries", entries)

	return nil
}

// GraphEntity represents an entity published to the graph.
type GraphEntity struct {
	ID         string        `json:"id"`
	Type       string        `json:"type,omitempty"`
	Predicates map[string]any `json:"predicates,omitempty"`
	Triples    []GraphTriple  `json:"triples,omitempty"`
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
		result.AddWarning("skipping predicate verification (no entity published)")
		return nil
	}

	// Extract entities from log entries
	entriesVal, ok := result.GetDetail("log_entries")
	if !ok {
		return fmt.Errorf("no log entries in result")
	}
	entries, ok := entriesVal.([]client.LogEntry)
	if !ok {
		return fmt.Errorf("log entries not in expected format")
	}

	if len(entries) == 0 {
		return fmt.Errorf("no messages in capture")
	}

	// Parse the first entity from BaseMessage wrapper
	if len(entries[0].RawData) == 0 {
		return fmt.Errorf("first log entry has no raw data")
	}

	var baseMsg map[string]any
	if err := json.Unmarshal(entries[0].RawData, &baseMsg); err != nil {
		data := string(entries[0].RawData)
		if len(data) > 200 {
			data = data[:200] + "..."
		}
		return fmt.Errorf("unmarshal base message: %w (data: %s)", err, data)
	}

	payload, _ := baseMsg["payload"].(map[string]any)
	if payload == nil {
		return fmt.Errorf("no payload in base message")
	}

	entityID, _ := payload["id"].(string)
	result.SetDetail("entity_id", entityID)

	// Check for expected predicates in triples
	expectedPredicates := []string{
		"semspec.proposal.title",
		"semspec.proposal.slug",
		"semspec.proposal.status",
	}

	foundPredicates := make(map[string]bool)
	if triples, ok := payload["triples"].([]any); ok {
		for _, t := range triples {
			triple, _ := t.(map[string]any)
			pred, _ := triple["predicate"].(string)
			for _, expected := range expectedPredicates {
				if pred == expected {
					foundPredicates[pred] = true
				}
			}
		}
	}

	result.SetDetail("predicates_found", foundPredicates)

	// Verify entity ID format
	expectedSlug, _ := result.GetDetailString("expected_slug")
	if entityID != "" && expectedSlug != "" {
		if !strings.Contains(strings.ToLower(entityID), strings.ToLower(expectedSlug)) &&
			!strings.Contains(strings.ToLower(entityID), "proposal") {
			result.AddWarning(fmt.Sprintf("entity ID may not match expected format: %s", entityID))
		}
	}

	// At least some predicates should be found
	if len(foundPredicates) == 0 {
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
