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

// ASTGoScenario tests Go AST processor verification.
// It copies a Go fixture project and verifies AST entities are extracted correctly.
type ASTGoScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewASTGoScenario creates a new Go AST processor scenario.
func NewASTGoScenario(cfg *config.Config) *ASTGoScenario {
	return &ASTGoScenario{
		name:        "ast-go",
		description: "Tests Go AST processor: verifies types, functions, and package entities are extracted",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ASTGoScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ASTGoScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ASTGoScenario) Setup(ctx context.Context) error {
	// Create HTTP client for message-logger queries
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for HTTP service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	// Create filesystem client
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)

	// Clean workspace completely (not just .semspec)
	if err := s.fs.CleanWorkspaceAll(); err != nil {
		return fmt.Errorf("clean workspace: %w", err)
	}

	// Setup .semspec directory
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Copy Go fixture to workspace
	fixturePath := s.config.GoFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy Go fixture: %w", err)
	}

	return nil
}

// Execute runs the Go AST scenario.
func (s *ASTGoScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-package", s.stageVerifyPackage},
		{"verify-types", s.stageVerifyTypes},
		{"verify-functions", s.stageVerifyFunctions},
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
func (s *ASTGoScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageVerifyFixture verifies the Go fixture was copied correctly.
func (s *ASTGoScenario) stageVerifyFixture(ctx context.Context, result *Result) error {
	// Check that expected files exist
	expectedFiles := []string{
		"go.mod",
		"main.go",
		"internal/auth/auth.go",
		"internal/auth/auth_test.go",
		"README.md",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify go.mod content
	content, err := s.fs.ReadFileRelative("go.mod")
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	if !strings.Contains(content, "example.com/testproject") {
		return fmt.Errorf("go.mod doesn't contain expected module name")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures AST entity messages via the message-logger service.
func (s *ASTGoScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for AST indexing to produce entities via message-logger
	// We expect at least: package auth, User struct, Token struct, Authenticate func, RefreshToken func
	minExpectedEntities := 5

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Filter by exact subject - message-logger doesn't support NATS wildcards in HTTP API
	if err := s.http.WaitForMessageSubject(waitCtx, "graph.ingest.entity", minExpectedEntities); err != nil {
		// Get current count for debugging
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d entities, got %d: %w", minExpectedEntities, len(entries), err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))

	// Extract entity payloads from BaseMessage-wrapped log entries
	entities := extractEntitiesFromLogEntries(entries)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyPackage verifies the auth file entity was extracted.
// Note: The AST indexer produces "file" entities, not "package" entities.
func (s *ASTGoScenario) stageVerifyPackage(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Look for the auth file entity (auth.go or auth-go in ID)
	found := false
	for _, entity := range entities {
		id, _ := entity["id"].(string)
		if strings.Contains(id, "auth") && strings.Contains(id, "file") {
			found = true
			result.SetDetail("auth_file_id", id)
			break
		}

		// Also check triples for file type with auth in path
		if triples, ok := entity["triples"].([]any); ok {
			isFile := false
			hasAuthPath := false
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "file" {
					isFile = true
				}
				if pred == "code.artifact.path" && strings.Contains(obj, "auth") {
					hasAuthPath = true
				}
			}
			if isFile && hasAuthPath {
				found = true
				result.SetDetail("auth_file_id", id)
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return fmt.Errorf("auth file entity not found")
	}

	return nil
}

// stageVerifyTypes verifies User and Token struct entities were extracted.
func (s *ASTGoScenario) stageVerifyTypes(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedTypes := map[string]bool{
		"User":  false,
		"Token": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for typeName := range expectedTypes {
			if strings.Contains(id, typeName) || strings.Contains(id, strings.ToLower(typeName)) {
				expectedTypes[typeName] = true
			}
		}

		// Also check triples for struct type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "struct" {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == "dc.terms.title" {
							for typeName := range expectedTypes {
								if obj2 == typeName {
									expectedTypes[typeName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for typeName, found := range expectedTypes {
		if !found {
			missing = append(missing, typeName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("type entities not found: %v", missing)
	}

	result.SetDetail("types_verified", []string{"User", "Token"})
	return nil
}

// stageVerifyFunctions verifies Authenticate and RefreshToken function entities.
func (s *ASTGoScenario) stageVerifyFunctions(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedFuncs := map[string]bool{
		"Authenticate": false,
		"RefreshToken": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for funcName := range expectedFuncs {
			if strings.Contains(id, funcName) {
				expectedFuncs[funcName] = true
			}
		}

		// Also check triples for function type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && (obj == "function" || obj == "func") {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == "dc.terms.title" {
							for funcName := range expectedFuncs {
								if obj2 == funcName {
									expectedFuncs[funcName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for funcName, found := range expectedFuncs {
		if !found {
			missing = append(missing, funcName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("function entities not found: %v", missing)
	}

	result.SetDetail("functions_verified", []string{"Authenticate", "RefreshToken"})
	return nil
}

// extractEntitiesFromLogEntries parses BaseMessage-wrapped entity payloads from message-logger entries.
// Only includes entries with message_type "ast.entity.v1".
// RawData format: {"id":"uuid","type":{...},"payload":{"id":"entity-id","triples":[...]},"meta":{...}}
func extractEntitiesFromLogEntries(entries []client.LogEntry) []map[string]any {
	var entities []map[string]any
	for _, entry := range entries {
		// Filter to only entity messages (not RDF exports, etc.)
		if entry.MessageType != "ast.entity.v1" {
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
