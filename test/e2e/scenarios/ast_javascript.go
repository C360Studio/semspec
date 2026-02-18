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

// ASTJavaScriptScenario tests JavaScript AST processor verification.
// It copies a JavaScript fixture project and verifies AST entities are extracted correctly.
type ASTJavaScriptScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewASTJavaScriptScenario creates a new JavaScript AST processor scenario.
func NewASTJavaScriptScenario(cfg *config.Config) *ASTJavaScriptScenario {
	return &ASTJavaScriptScenario{
		name:        "ast-javascript",
		description: "Tests JavaScript AST processor: verifies classes, functions, and ES6/CommonJS modules are extracted",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ASTJavaScriptScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ASTJavaScriptScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ASTJavaScriptScenario) Setup(ctx context.Context) error {
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

	// Copy JavaScript fixture to workspace
	fixturePath := s.config.JSFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy JavaScript fixture: %w", err)
	}

	return nil
}

// Execute runs the JavaScript AST scenario.
func (s *ASTJavaScriptScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-classes", s.stageVerifyClasses},
		{"verify-functions", s.stageVerifyFunctions},
		{"verify-modules", s.stageVerifyModules},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())

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
func (s *ASTJavaScriptScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageVerifyFixture verifies the JavaScript fixture was copied correctly.
func (s *ASTJavaScriptScenario) stageVerifyFixture(ctx context.Context, result *Result) error {
	// Check that expected files exist
	expectedFiles := []string{
		"package.json",
		"src/index.js",
		"src/auth/auth.service.js",
		"src/auth/auth.utils.js",
		"src/legacy/commonjs.js",
		"src/legacy/prototype.js",
		"src/patterns/async.js",
		"src/patterns/closures.js",
		"src/patterns/functional.js",
		"README.md",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify auth.service.js content
	content, err := s.fs.ReadFileRelative("src/auth/auth.service.js")
	if err != nil {
		return fmt.Errorf("read auth.service.js: %w", err)
	}
	if !strings.Contains(content, "class AuthService") {
		return fmt.Errorf("auth.service.js doesn't contain expected AuthService class")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures AST entity messages via the message-logger service.
func (s *ASTJavaScriptScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for AST indexing to produce entities via message-logger
	minExpectedEntities := 10

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Filter by exact subject
	if err := s.http.WaitForMessageSubject(waitCtx, "graph.ingest.entity", minExpectedEntities); err != nil {
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d entities, got %d: %w", minExpectedEntities, len(entries), err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))

	// Extract entity payloads
	entities := extractJSEntitiesFromLogEntries(entries)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyClasses verifies JavaScript class entities were extracted.
func (s *ASTJavaScriptScenario) stageVerifyClasses(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedClasses := map[string]bool{
		"AuthService":  false,
		"AuthResult":   false,
		"SimpleCache":  false,
		"EventEmitter": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for className := range expectedClasses {
			if strings.Contains(id, className) {
				expectedClasses[className] = true
			}
		}

		// Also check triples for class type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "class" {
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == "dc.terms.title" {
							for className := range expectedClasses {
								if obj2 == className {
									expectedClasses[className] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for className, found := range expectedClasses {
		if !found {
			missing = append(missing, className)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("class entities not found: %v", missing)
	}

	result.SetDetail("classes_verified", []string{"AuthService", "AuthResult", "SimpleCache", "EventEmitter"})
	return nil
}

// stageVerifyFunctions verifies JavaScript function entities were extracted.
func (s *ASTJavaScriptScenario) stageVerifyFunctions(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedFuncs := map[string]bool{
		"validateEmail": false,
		"hashPassword":  false,
		"retry":         false,
		"memoize":       false,
		"compose":       false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for funcName := range expectedFuncs {
			if strings.Contains(id, funcName) {
				expectedFuncs[funcName] = true
			}
		}

		// Check triples for function type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "function" {
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

	result.SetDetail("functions_verified", []string{"validateEmail", "hashPassword", "retry", "memoize", "compose"})
	return nil
}

// stageVerifyModules verifies JavaScript file entities were extracted with correct language.
func (s *ASTJavaScriptScenario) stageVerifyModules(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Check that file entities exist for key modules
	expectedModules := map[string]bool{
		"index.js":        false,
		"auth.service.js": false,
		"commonjs.js":     false,
	}

	for _, entity := range entities {
		if triples, ok := entity["triples"].([]any); ok {
			isFile := false
			fileName := ""
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "file" {
					isFile = true
				}
				if pred == "dc.terms.title" {
					fileName = obj
				}
			}
			if isFile {
				for modName := range expectedModules {
					if fileName == modName {
						expectedModules[modName] = true
					}
				}
			}
		}
	}

	var missing []string
	for modName, found := range expectedModules {
		if !found {
			missing = append(missing, modName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("module file entities not found: %v", missing)
	}

	result.SetDetail("modules_verified", []string{"index.js", "auth.service.js", "commonjs.js"})
	return nil
}

// extractJSEntitiesFromLogEntries parses BaseMessage-wrapped entity payloads from message-logger entries.
func extractJSEntitiesFromLogEntries(entries []client.LogEntry) []map[string]any {
	var entities []map[string]any
	for _, entry := range entries {
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
