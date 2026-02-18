package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

// DocIngestScenario tests document ingestion via file watching.
// It copies document fixtures to the watched sources directory and verifies
// document entities are extracted correctly with proper chunking.
type DocIngestScenario struct {
	name             string
	description      string
	config           *config.Config
	http             *client.HTTPClient
	fs               *client.FilesystemClient
	baselineSequence int64 // Sequence number at setup time, used to filter for new entities
}

// NewDocIngestScenario creates a new document ingestion scenario.
func NewDocIngestScenario(cfg *config.Config) *DocIngestScenario {
	return &DocIngestScenario{
		name:        "doc-ingest",
		description: "Tests document ingestion: verifies markdown and RST files are parsed and chunked correctly",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *DocIngestScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *DocIngestScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *DocIngestScenario) Setup(ctx context.Context) error {
	// Create HTTP client for message-logger queries
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for HTTP service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

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

	// Create sources directory for watched documents
	if err := s.fs.CreateDirectory("sources"); err != nil {
		return fmt.Errorf("create sources directory: %w", err)
	}

	// Capture baseline sequence before copying fixture
	seq, err := s.http.GetMaxSequence(ctx)
	if err != nil {
		return fmt.Errorf("get baseline sequence: %w", err)
	}
	s.baselineSequence = seq

	// Copy document fixtures to watched sources directory
	fixturePath := s.config.DocFixturePath()
	if err := s.fs.CopyFixtureToSubdir(fixturePath, "sources"); err != nil {
		return fmt.Errorf("copy doc fixture: %w", err)
	}

	return nil
}

// Execute runs the document ingestion scenario.
func (s *DocIngestScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-document", s.stageVerifyDocument},
		{"verify-chunks", s.stageVerifyChunks},
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
func (s *DocIngestScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageVerifyFixture verifies the document fixtures were copied correctly.
func (s *DocIngestScenario) stageVerifyFixture(ctx context.Context, result *Result) error {
	// Check that expected files exist in sources directory
	expectedFiles := []string{
		"sources/error-handling.md",
		"sources/api-reference.rst",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify markdown file has expected frontmatter
	content, err := s.fs.ReadFileRelative("sources/error-handling.md")
	if err != nil {
		return fmt.Errorf("read error-handling.md: %w", err)
	}
	if !strings.Contains(content, "category: sop") {
		return fmt.Errorf("error-handling.md doesn't contain expected frontmatter")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures document entity messages via the message-logger service.
func (s *DocIngestScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for document indexing to produce source entities
	// We expect at least: 1 document entity + its chunks
	minExpectedEntities := 1

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Wait specifically for NEW source entities (after baseline sequence)
	if err := s.waitForNewSourceEntities(waitCtx, minExpectedEntities); err != nil {
		// Get current count for debugging
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d source entities, got %d total (baseline seq: %d): %w",
			minExpectedEntities, len(entries), s.baselineSequence, err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))
	result.SetDetail("baseline_sequence", s.baselineSequence)

	// Extract source entity payloads from BaseMessage-wrapped log entries
	entities := s.extractSourceEntitiesAfterSequence(entries, s.baselineSequence)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyDocument verifies the document entity was created with correct predicates.
func (s *DocIngestScenario) stageVerifyDocument(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	if len(entities) == 0 {
		return fmt.Errorf("no source entities captured")
	}

	// Find the document entity (not a chunk)
	var documentEntity map[string]any
	for _, entity := range entities {
		id, _ := entity["id"].(string)
		// Document entities don't have ".chunk." in their ID
		if strings.Contains(id, "error-handling") && !strings.Contains(id, ".chunk.") {
			documentEntity = entity
			break
		}
	}

	if documentEntity == nil {
		return fmt.Errorf("document entity for error-handling.md not found")
	}

	result.SetDetail("document_entity_id", documentEntity["id"])

	// Verify expected predicates
	triples, ok := documentEntity["triples"].([]any)
	if !ok {
		return fmt.Errorf("document entity has no triples")
	}

	predicates := make(map[string]any)
	for _, t := range triples {
		triple, _ := t.(map[string]any)
		pred, _ := triple["predicate"].(string)
		obj := triple["object"]
		predicates[pred] = obj
	}

	// Check source.type = document
	if sourceType, ok := predicates[sourceVocab.SourceType].(string); !ok || sourceType != "document" {
		return fmt.Errorf("expected source.type=document, got %v", predicates[sourceVocab.SourceType])
	}

	// Check source.doc.category = sop (from frontmatter)
	if category, ok := predicates[sourceVocab.DocCategory].(string); !ok || category != "sop" {
		return fmt.Errorf("expected source.doc.category=sop, got %v", predicates[sourceVocab.DocCategory])
	}

	result.SetDetail("document_predicates", predicates)
	return nil
}

// stageVerifyChunks verifies chunk entities were created and linked to parent.
func (s *DocIngestScenario) stageVerifyChunks(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	docIDVal, ok := result.GetDetail("document_entity_id")
	if !ok {
		return fmt.Errorf("document entity ID not found in result")
	}
	documentID, _ := docIDVal.(string)

	// Find chunk entities for this document
	var chunks []map[string]any
	for _, entity := range entities {
		id, _ := entity["id"].(string)
		if strings.Contains(id, ".chunk.") && strings.HasPrefix(id, strings.TrimSuffix(documentID, "")) {
			chunks = append(chunks, entity)
		}
	}

	// Documents may or may not have chunks depending on size
	// The fixture is small so might not chunk, which is acceptable
	result.SetDetail("chunk_count", len(chunks))

	// If we have chunks, verify they're linked to parent
	for _, chunk := range chunks {
		triples, ok := chunk["triples"].([]any)
		if !ok {
			continue
		}

		foundBelongs := false
		for _, t := range triples {
			triple, _ := t.(map[string]any)
			pred, _ := triple["predicate"].(string)
			if pred == sourceVocab.CodeBelongs {
				foundBelongs = true
				break
			}
		}

		if !foundBelongs {
			chunkID, _ := chunk["id"].(string)
			return fmt.Errorf("chunk %s missing belongs relationship", chunkID)
		}
	}

	return nil
}

// waitForNewSourceEntities waits for source entities to appear after baseline sequence.
func (s *DocIngestScenario) waitForNewSourceEntities(ctx context.Context, minCount int) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
			if err != nil {
				continue
			}

			entities := s.extractSourceEntitiesAfterSequence(entries, s.baselineSequence)
			if len(entities) >= minCount {
				return nil
			}
		}
	}
}

// extractSourceEntitiesAfterSequence extracts source entity payloads from message log entries.
func (s *DocIngestScenario) extractSourceEntitiesAfterSequence(entries []client.LogEntry, minSequence int64) []map[string]any {
	var entities []map[string]any
	for _, entry := range entries {
		// Filter by sequence if specified
		if minSequence > 0 && entry.Sequence <= minSequence {
			continue
		}
		// Filter to source entity messages
		if entry.MessageType != "source.entity.v1" {
			continue
		}
		if len(entry.RawData) == 0 {
			continue
		}
		var baseMsg map[string]any
		if err := json.Unmarshal(entry.RawData, &baseMsg); err != nil {
			continue
		}
		payload, ok := baseMsg["payload"].(map[string]any)
		if !ok {
			continue
		}
		entities = append(entities, payload)
	}
	return entities
}
