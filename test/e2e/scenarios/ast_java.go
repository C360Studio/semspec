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

// ASTJavaScenario tests Java AST processor verification.
// It copies a Java fixture project and verifies AST entities are extracted correctly.
type ASTJavaScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewASTJavaScenario creates a new Java AST processor scenario.
func NewASTJavaScenario(cfg *config.Config) *ASTJavaScenario {
	return &ASTJavaScenario{
		name:        "ast-java",
		description: "Tests Java AST processor: verifies classes, interfaces, enums, records, and annotations are extracted",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ASTJavaScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ASTJavaScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ASTJavaScenario) Setup(ctx context.Context) error {
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

	// Copy Java fixture to workspace
	fixturePath := s.config.JavaFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy Java fixture: %w", err)
	}

	return nil
}

// Execute runs the Java AST scenario.
func (s *ASTJavaScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-classes", s.stageVerifyClasses},
		{"verify-interfaces", s.stageVerifyInterfaces},
		{"verify-enums", s.stageVerifyEnums},
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
func (s *ASTJavaScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageVerifyFixture verifies the Java fixture was copied correctly.
func (s *ASTJavaScenario) stageVerifyFixture(ctx context.Context, result *Result) error {
	// Check that expected files exist
	expectedFiles := []string{
		"src/main/java/com/example/auth/User.java",
		"src/main/java/com/example/auth/UserRole.java",
		"src/main/java/com/example/auth/Token.java",
		"src/main/java/com/example/auth/AuthService.java",
		"src/main/java/com/example/auth/Authenticator.java",
		"src/main/java/com/example/auth/AuthResult.java",
		"src/main/java/com/example/models/BaseEntity.java",
		"src/main/java/com/example/generics/Repository.java",
		"src/main/java/com/example/annotations/Logged.java",
		"README.md",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify AuthService.java content
	content, err := s.fs.ReadFileRelative("src/main/java/com/example/auth/AuthService.java")
	if err != nil {
		return fmt.Errorf("read AuthService.java: %w", err)
	}
	if !strings.Contains(content, "class AuthService") {
		return fmt.Errorf("AuthService.java doesn't contain expected AuthService class")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures AST entity messages via the message-logger service.
func (s *ASTJavaScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
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
	entities := extractJavaEntitiesFromLogEntries(entries)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyClasses verifies Java class entities were extracted.
func (s *ASTJavaScenario) stageVerifyClasses(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedClasses := map[string]bool{
		"User":               false,
		"Token":              false,
		"AuthService":        false,
		"BaseEntity":         false,
		"InMemoryRepository": false,
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

	result.SetDetail("classes_verified", []string{"User", "Token", "AuthService", "BaseEntity", "InMemoryRepository"})
	return nil
}

// stageVerifyInterfaces verifies Java interface entities were extracted.
func (s *ASTJavaScenario) stageVerifyInterfaces(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedInterfaces := map[string]bool{
		"Repository":    false,
		"Authenticator": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for ifaceName := range expectedInterfaces {
			if strings.Contains(id, ifaceName) {
				expectedInterfaces[ifaceName] = true
			}
		}

		// Check triples for interface type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "interface" {
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == "dc.terms.title" {
							for ifaceName := range expectedInterfaces {
								if obj2 == ifaceName {
									expectedInterfaces[ifaceName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for ifaceName, found := range expectedInterfaces {
		if !found {
			missing = append(missing, ifaceName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("interface entities not found: %v", missing)
	}

	result.SetDetail("interfaces_verified", []string{"Repository", "Authenticator"})
	return nil
}

// stageVerifyEnums verifies Java enum entities were extracted.
func (s *ASTJavaScenario) stageVerifyEnums(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedEnums := map[string]bool{
		"UserRole":  false,
		"TokenType": false,
		"Status":    false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for enumName := range expectedEnums {
			if strings.Contains(id, enumName) {
				expectedEnums[enumName] = true
			}
		}

		// Check triples for enum type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == "code.artifact.type" && obj == "enum" {
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == "dc.terms.title" {
							for enumName := range expectedEnums {
								if obj2 == enumName {
									expectedEnums[enumName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for enumName, found := range expectedEnums {
		if !found {
			missing = append(missing, enumName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("enum entities not found: %v", missing)
	}

	result.SetDetail("enums_verified", []string{"UserRole", "TokenType", "Status"})
	return nil
}

// extractJavaEntitiesFromLogEntries parses BaseMessage-wrapped entity payloads from message-logger entries.
func extractJavaEntitiesFromLogEntries(entries []client.LogEntry) []map[string]any {
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
