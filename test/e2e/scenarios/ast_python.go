package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ASTPythonScenario tests Python AST processor verification.
// It copies a Python fixture project and verifies AST entities are extracted correctly.
type ASTPythonScenario struct {
	name             string
	description      string
	config           *config.Config
	http             *client.HTTPClient
	fs               *client.FilesystemClient
	baselineSequence int64 // Sequence number at setup time, used to filter for new entities
}

// NewASTPythonScenario creates a new Python AST processor scenario.
func NewASTPythonScenario(cfg *config.Config) *ASTPythonScenario {
	return &ASTPythonScenario{
		name:        "ast-python",
		description: "Tests Python AST processor: verifies classes, functions, dataclasses, and decorators are extracted",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ASTPythonScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ASTPythonScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ASTPythonScenario) Setup(ctx context.Context) error {
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

	// Copy Python fixture to workspace
	fixturePath := s.config.PythonFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy Python fixture: %w", err)
	}

	return nil
}

// Execute runs the Python AST scenario.
func (s *ASTPythonScenario) Execute(ctx context.Context) (*Result, error) {
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
		{"verify-dataclasses", s.stageVerifyDataclasses},
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
func (s *ASTPythonScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageVerifyFixture verifies the Python fixture was copied correctly.
func (s *ASTPythonScenario) stageVerifyFixture(ctx context.Context, result *Result) error {
	// Check that expected files exist
	expectedFiles := []string{
		"src/__init__.py",
		"src/auth/__init__.py",
		"src/auth/models.py",
		"src/auth/service.py",
		"src/auth/utils.py",
		"src/models/base.py",
		"src/models/mixins.py",
		"src/generics/repository.py",
		"src/patterns/decorators.py",
		"src/patterns/async_patterns.py",
		"src/patterns/protocols.py",
		"README.md",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify service.py content
	content, err := s.fs.ReadFileRelative("src/auth/service.py")
	if err != nil {
		return fmt.Errorf("read service.py: %w", err)
	}
	if !strings.Contains(content, "class AuthService") {
		return fmt.Errorf("service.py doesn't contain expected AuthService class")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures AST entity messages via the message-logger service.
func (s *ASTPythonScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for AST indexing to produce Python entities via message-logger
	// We expect at least: AuthService class, User dataclass, authenticate func, etc.
	minExpectedEntities := 10

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Wait specifically for NEW Python entities (after baseline sequence)
	if err := s.http.WaitForNewLanguageEntities(waitCtx, "python", minExpectedEntities, s.baselineSequence); err != nil {
		// Get current count for debugging
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d python entities, got %d total (baseline seq: %d): %w", minExpectedEntities, len(entries), s.baselineSequence, err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))
	result.SetDetail("baseline_sequence", s.baselineSequence)

	// Extract Python entity payloads from BaseMessage-wrapped log entries (filtered by sequence)
	entities := extractEntitiesForLanguageAfterSequence(entries, "python", s.baselineSequence)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyClasses verifies Python class entities were extracted.
func (s *ASTPythonScenario) stageVerifyClasses(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedClasses := map[string]bool{
		"AuthService": false,
		"AsyncWorker": false,
		"TaskQueue":   false,
		"BaseEntity":  false,
		"Repository":  false,
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
					// Check if name matches (using dc.terms.title)
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

	result.SetDetail("classes_verified", []string{"AuthService", "AsyncWorker", "TaskQueue", "BaseEntity", "Repository"})
	return nil
}

// stageVerifyFunctions verifies Python function entities were extracted.
func (s *ASTPythonScenario) stageVerifyFunctions(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedFuncs := map[string]bool{
		"authenticate":   false,
		"validate_email": false,
		"timer":          false,
		"retry":          false,
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
				if pred == "code.artifact.type" && (obj == "function" || obj == "method") {
					// Check if name matches
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

	result.SetDetail("functions_verified", []string{"authenticate", "validate_email", "timer", "retry"})
	return nil
}

// stageVerifyDataclasses verifies Python dataclass entities were extracted as structs.
func (s *ASTPythonScenario) stageVerifyDataclasses(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Dataclasses should be extracted as struct type
	expectedDataclasses := map[string]bool{
		"User":       false,
		"Token":      false,
		"AuthResult": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for dcName := range expectedDataclasses {
			if strings.Contains(id, dcName) {
				expectedDataclasses[dcName] = true
			}
		}

		// Check triples for struct type (dataclasses map to struct)
		if triples, ok := entity["triples"].([]any); ok {
			isStructOrClass := false
			entityName := ""
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && (obj == "struct" || obj == "class") {
					isStructOrClass = true
				}
				if pred == "dc.terms.title" {
					entityName = obj
				}
			}
			if isStructOrClass && entityName != "" {
				for dcName := range expectedDataclasses {
					if entityName == dcName {
						expectedDataclasses[dcName] = true
					}
				}
			}
		}
	}

	var missing []string
	for dcName, found := range expectedDataclasses {
		if !found {
			missing = append(missing, dcName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("dataclass entities not found: %v", missing)
	}

	result.SetDetail("dataclasses_verified", []string{"User", "Token", "AuthResult"})
	return nil
}

// extractPythonEntitiesFromLogEntries parses BaseMessage-wrapped entity payloads filtered to Python language.
func extractPythonEntitiesFromLogEntries(entries []client.LogEntry) []map[string]any {
	return extractEntitiesForLanguage(entries, "python")
}
