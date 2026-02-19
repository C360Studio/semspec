package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	codeAst "github.com/c360studio/semspec/processor/ast"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ASTJavaScriptScenario tests JavaScript AST processor verification.
// It copies a JavaScript fixture project and verifies AST entities are extracted correctly.
type ASTJavaScriptScenario struct {
	name             string
	description      string
	config           *config.Config
	http             *client.HTTPClient
	fs               *client.FilesystemClient
	baselineSequence int64 // Sequence number at setup time, used to filter for new entities
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

	// Capture baseline sequence before copying fixture
	seq, err := s.http.GetMaxSequence(ctx)
	if err != nil {
		return fmt.Errorf("get baseline sequence: %w", err)
	}
	s.baselineSequence = seq

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
func (s *ASTJavaScriptScenario) Teardown(_ context.Context) error {
	return nil
}

// stageVerifyFixture verifies the JavaScript fixture was copied correctly.
func (s *ASTJavaScriptScenario) stageVerifyFixture(_ context.Context, result *Result) error {
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
	// Wait for AST indexing to produce JavaScript entities via message-logger
	// The JavaScript fixture has 9 files producing ~157 unique entities.
	// Classes (4) are among the last to be published, so we need to wait for
	// a large portion of entities to ensure classes are included.
	minExpectedEntities := 100

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Wait specifically for NEW JavaScript entities (after baseline sequence)
	if err := s.http.WaitForNewLanguageEntities(waitCtx, "javascript", minExpectedEntities, s.baselineSequence); err != nil {
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d javascript entities, got %d total (baseline seq: %d): %w", minExpectedEntities, len(entries), s.baselineSequence, err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))
	result.SetDetail("baseline_sequence", s.baselineSequence)

	// Extract JavaScript entity payloads from BaseMessage-wrapped log entries (filtered by sequence)
	entities := extractEntitiesForLanguageAfterSequence(entries, "javascript", s.baselineSequence)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyClasses verifies JavaScript class entities were extracted.
func (s *ASTJavaScriptScenario) stageVerifyClasses(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedClasses := map[string]bool{
		"AuthService":        false,
		"AuthResult":         false,
		"SimpleCache":        false,
		"AccountLockedError": false, // Note: EventEmitter uses prototype pattern, not ES6 class
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
				if pred == codeAst.CodeType && obj == "class" {
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle {
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

	result.SetDetail("classes_verified", []string{"AuthService", "AuthResult", "SimpleCache", "AccountLockedError"})
	return nil
}

// stageVerifyFunctions verifies JavaScript function entities were extracted.
func (s *ASTJavaScriptScenario) stageVerifyFunctions(_ context.Context, result *Result) error {
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
				if pred == codeAst.CodeType && obj == "function" {
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle {
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
func (s *ASTJavaScriptScenario) stageVerifyModules(_ context.Context, result *Result) error {
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
				if pred == codeAst.CodeType && obj == "file" {
					isFile = true
				}
				if pred == codeAst.DcTitle {
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

// extractJSEntitiesFromLogEntries parses BaseMessage-wrapped entity payloads filtered to JavaScript language.
func extractJSEntitiesFromLogEntries(entries []client.LogEntry) []map[string]any {
	return extractEntitiesForLanguage(entries, "javascript")
}
