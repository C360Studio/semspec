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
	specVocab "github.com/c360studio/semspec/vocabulary/spec"
)

// OpenSpecIngestScenario tests OpenSpec specification ingestion via file watching.
// It copies OpenSpec fixtures to the watched sources directory and verifies
// spec, requirement, and scenario entities are extracted correctly.
type OpenSpecIngestScenario struct {
	name             string
	description      string
	config           *config.Config
	http             *client.HTTPClient
	fs               *client.FilesystemClient
	baselineSequence int64 // Sequence number at setup time, used to filter for new entities
}

// NewOpenSpecIngestScenario creates a new OpenSpec ingestion scenario.
func NewOpenSpecIngestScenario(cfg *config.Config) *OpenSpecIngestScenario {
	return &OpenSpecIngestScenario{
		name:        "openspec-ingest",
		description: "Tests OpenSpec ingestion: verifies spec, requirement, and scenario entities are extracted correctly",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *OpenSpecIngestScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *OpenSpecIngestScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *OpenSpecIngestScenario) Setup(ctx context.Context) error {
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

	// Create sources directory for watched files
	if err := s.fs.CreateDirectory("sources"); err != nil {
		return fmt.Errorf("create sources directory: %w", err)
	}

	// Create openspec subdirectory structure
	if err := s.fs.CreateDirectory("sources/openspec/specs"); err != nil {
		return fmt.Errorf("create openspec/specs directory: %w", err)
	}
	if err := s.fs.CreateDirectory("sources/openspec/changes"); err != nil {
		return fmt.Errorf("create openspec/changes directory: %w", err)
	}

	// Capture baseline sequence before copying fixtures
	seq, err := s.http.GetMaxSequence(ctx)
	if err != nil {
		return fmt.Errorf("get baseline sequence: %w", err)
	}
	s.baselineSequence = seq

	// Copy OpenSpec fixtures to watched sources directory
	fixturePath := s.config.OpenSpecFixturePath()
	if err := s.fs.CopyFixtureToSubdir(fixturePath+"/openspec/specs", "sources/openspec/specs"); err != nil {
		return fmt.Errorf("copy specs fixture: %w", err)
	}
	if err := s.fs.CopyFixtureToSubdir(fixturePath+"/openspec/changes", "sources/openspec/changes"); err != nil {
		return fmt.Errorf("copy changes fixture: %w", err)
	}

	return nil
}

// Execute runs the OpenSpec ingestion scenario.
func (s *OpenSpecIngestScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-source-of-truth", s.stageVerifySourceOfTruth},
		{"verify-requirements", s.stageVerifyRequirements},
		{"verify-scenarios", s.stageVerifyScenarios},
		{"verify-delta-spec", s.stageVerifyDeltaSpec},
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
func (s *OpenSpecIngestScenario) Teardown(_ context.Context) error {
	return nil
}

// stageVerifyFixture verifies the OpenSpec fixtures were copied correctly.
func (s *OpenSpecIngestScenario) stageVerifyFixture(_ context.Context, result *Result) error {
	// Check that expected files exist in sources directory
	expectedFiles := []string{
		"sources/openspec/specs/auth.md",
		"sources/openspec/changes/session-timeout.md",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify auth.md has expected frontmatter
	content, err := s.fs.ReadFileRelative("sources/openspec/specs/auth.md")
	if err != nil {
		return fmt.Errorf("read auth.md: %w", err)
	}
	if !strings.Contains(content, "title: Authentication Specification") {
		return fmt.Errorf("auth.md doesn't contain expected frontmatter")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures spec entity messages via the message-logger service.
func (s *OpenSpecIngestScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for spec indexing to produce entities
	// We expect at least: 2 spec entities + requirements + scenarios
	minExpectedEntities := 2

	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	// Wait specifically for NEW source entities (after baseline sequence)
	if err := s.waitForNewSpecEntities(waitCtx, minExpectedEntities); err != nil {
		// Get current count for debugging
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d spec entities, got %d total (baseline seq: %d): %w",
			minExpectedEntities, len(entries), s.baselineSequence, err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))
	result.SetDetail("baseline_sequence", s.baselineSequence)

	// Extract spec entity payloads from BaseMessage-wrapped log entries
	entities := s.extractSpecEntitiesAfterSequence(entries, s.baselineSequence)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifySourceOfTruth verifies the source-of-truth spec entity was created correctly.
func (s *OpenSpecIngestScenario) stageVerifySourceOfTruth(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	if len(entities) == 0 {
		return fmt.Errorf("no spec entities captured")
	}

	// Find the source-of-truth spec entity (auth.md)
	var sotSpec map[string]any
	for _, entity := range entities {
		predicates := extractPredicates(entity)
		if specType, ok := predicates[specVocab.SpecSpecType].(string); ok && specType == "source-of-truth" {
			sotSpec = entity
			break
		}
	}

	if sotSpec == nil {
		return fmt.Errorf("source-of-truth spec entity not found")
	}

	predicates := extractPredicates(sotSpec)

	// Verify expected predicates
	if specType, ok := predicates[specVocab.SpecType].(string); !ok || specType != "specification" {
		return fmt.Errorf("expected spec.type=specification, got %v", predicates[specVocab.SpecType])
	}

	if title, ok := predicates[specVocab.SpecTitle].(string); !ok || title != "Authentication Specification" {
		return fmt.Errorf("expected spec.title='Authentication Specification', got %v", predicates[specVocab.SpecTitle])
	}

	if sourceType, ok := predicates[sourceVocab.SourceType].(string); !ok || sourceType != "openspec" {
		return fmt.Errorf("expected source.type=openspec, got %v", predicates[sourceVocab.SourceType])
	}

	result.SetDetail("source_of_truth_spec", sotSpec)
	result.SetDetail("sot_predicates", predicates)
	return nil
}

// stageVerifyRequirements verifies requirement entities were created and linked.
func (s *OpenSpecIngestScenario) stageVerifyRequirements(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Find requirement entities
	var requirements []map[string]any
	for _, entity := range entities {
		predicates := extractPredicates(entity)
		if specType, ok := predicates[specVocab.SpecType].(string); ok && specType == "requirement" {
			requirements = append(requirements, entity)
		}
	}

	// auth.md has 3 requirements: Token-Refresh, Session-Timeout, MFA-Optional
	if len(requirements) < 3 {
		return fmt.Errorf("expected at least 3 requirement entities, got %d", len(requirements))
	}

	// Check for Token-Refresh requirement
	var tokenRefreshReq map[string]any
	for _, req := range requirements {
		predicates := extractPredicates(req)
		if name, ok := predicates["spec.requirement.name"].(string); ok && name == "Token-Refresh" {
			tokenRefreshReq = req
			break
		}
	}

	if tokenRefreshReq == nil {
		return fmt.Errorf("Token-Refresh requirement not found")
	}

	predicates := extractPredicates(tokenRefreshReq)

	// Verify normatives were extracted
	if normatives, ok := predicates["spec.requirement.normative"].([]any); ok {
		if len(normatives) < 2 {
			return fmt.Errorf("expected at least 2 normatives, got %d", len(normatives))
		}
	} else {
		return fmt.Errorf("normatives not found or wrong type")
	}

	result.SetDetail("requirement_count", len(requirements))
	result.SetDetail("token_refresh_req", tokenRefreshReq)
	return nil
}

// stageVerifyScenarios verifies scenario entities were created and linked to requirements.
func (s *OpenSpecIngestScenario) stageVerifyScenarios(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Find scenario entities
	var scenarios []map[string]any
	for _, entity := range entities {
		predicates := extractPredicates(entity)
		if specType, ok := predicates[specVocab.SpecType].(string); ok && specType == "scenario" {
			scenarios = append(scenarios, entity)
		}
	}

	// auth.md has at least 4 scenarios
	if len(scenarios) < 4 {
		return fmt.Errorf("expected at least 4 scenario entities, got %d", len(scenarios))
	}

	// Check for "Valid Token Refresh" scenario
	var tokenRefreshScenario map[string]any
	for _, s := range scenarios {
		predicates := extractPredicates(s)
		if name, ok := predicates["spec.scenario.name"].(string); ok && name == "Valid Token Refresh" {
			tokenRefreshScenario = s
			break
		}
	}

	if tokenRefreshScenario == nil {
		return fmt.Errorf("'Valid Token Refresh' scenario not found")
	}

	predicates := extractPredicates(tokenRefreshScenario)

	// Verify Given/When/Then were extracted
	if given, ok := predicates["spec.scenario.given"].(string); !ok || given == "" {
		return fmt.Errorf("scenario missing Given clause")
	}
	if when, ok := predicates["spec.scenario.when"].(string); !ok || when == "" {
		return fmt.Errorf("scenario missing When clause")
	}
	if then, ok := predicates["spec.scenario.then"].(string); !ok || then == "" {
		return fmt.Errorf("scenario missing Then clause")
	}

	result.SetDetail("scenario_count", len(scenarios))
	result.SetDetail("token_refresh_scenario", tokenRefreshScenario)
	return nil
}

// stageVerifyDeltaSpec verifies the delta spec entity and its operations.
func (s *OpenSpecIngestScenario) stageVerifyDeltaSpec(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Find the delta spec entity
	var deltaSpec map[string]any
	for _, entity := range entities {
		predicates := extractPredicates(entity)
		if specType, ok := predicates[specVocab.SpecSpecType].(string); ok && specType == "delta" {
			deltaSpec = entity
			break
		}
	}

	if deltaSpec == nil {
		return fmt.Errorf("delta spec entity not found")
	}

	predicates := extractPredicates(deltaSpec)

	// Verify modifies relationship
	if modifies, ok := predicates[specVocab.Modifies].(string); !ok || modifies != "auth.spec" {
		return fmt.Errorf("expected spec.modifies='auth.spec', got %v", predicates[specVocab.Modifies])
	}

	// Find delta operations
	var deltaOps []map[string]any
	for _, entity := range entities {
		predicates := extractPredicates(entity)
		if specType, ok := predicates[specVocab.SpecType].(string); ok && specType == "delta-operation" {
			deltaOps = append(deltaOps, entity)
		}
	}

	// session-timeout.md has 3 delta operations: 1 added, 1 modified, 1 removed
	if len(deltaOps) < 3 {
		return fmt.Errorf("expected at least 3 delta operations, got %d", len(deltaOps))
	}

	// Count operation types
	opCounts := make(map[string]int)
	for _, op := range deltaOps {
		predicates := extractPredicates(op)
		if opType, ok := predicates["spec.delta.operation"].(string); ok {
			opCounts[opType]++
		}
	}

	if opCounts["added"] < 1 {
		return fmt.Errorf("expected at least 1 'added' operation, got %d", opCounts["added"])
	}
	if opCounts["modified"] < 1 {
		return fmt.Errorf("expected at least 1 'modified' operation, got %d", opCounts["modified"])
	}
	if opCounts["removed"] < 1 {
		return fmt.Errorf("expected at least 1 'removed' operation, got %d", opCounts["removed"])
	}

	result.SetDetail("delta_spec", deltaSpec)
	result.SetDetail("delta_operation_count", len(deltaOps))
	result.SetDetail("delta_op_types", opCounts)
	return nil
}

// waitForNewSpecEntities waits for spec entities to appear after baseline sequence.
func (s *OpenSpecIngestScenario) waitForNewSpecEntities(ctx context.Context, minCount int) error {
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

			entities := s.extractSpecEntitiesAfterSequence(entries, s.baselineSequence)
			// Count unique spec specification entities
			specCount := 0
			for _, e := range entities {
				predicates := extractPredicates(e)
				if t, ok := predicates[specVocab.SpecType].(string); ok && t == "specification" {
					specCount++
				}
			}
			if specCount >= minCount {
				return nil
			}
		}
	}
}

// extractSpecEntitiesAfterSequence extracts spec entity payloads from message log entries.
func (s *OpenSpecIngestScenario) extractSpecEntitiesAfterSequence(entries []client.LogEntry, minSequence int64) []map[string]any {
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

		// Check if this is an OpenSpec entity (has spec.type predicate)
		predicates := extractPredicates(payload)
		if _, ok := predicates[specVocab.SpecType]; ok {
			entities = append(entities, payload)
		}
	}
	return entities
}

// extractPredicates extracts predicates from an entity payload into a map.
func extractPredicates(entity map[string]any) map[string]any {
	predicates := make(map[string]any)
	triples, ok := entity["triples"].([]any)
	if !ok {
		return predicates
	}
	for _, t := range triples {
		triple, ok := t.(map[string]any)
		if !ok {
			continue
		}
		pred, _ := triple["predicate"].(string)
		obj := triple["object"]
		predicates[pred] = obj
	}
	return predicates
}
