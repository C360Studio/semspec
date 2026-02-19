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

// ASTTypeScriptScenario tests TypeScript AST processor verification.
// It copies a TypeScript fixture project and verifies AST entities are extracted correctly.
type ASTTypeScriptScenario struct {
	name             string
	description      string
	config           *config.Config
	http             *client.HTTPClient
	fs               *client.FilesystemClient
	baselineSequence int64 // Sequence number at setup time, used to filter for new entities
}

// NewASTTypeScriptScenario creates a new TypeScript AST processor scenario.
func NewASTTypeScriptScenario(cfg *config.Config) *ASTTypeScriptScenario {
	return &ASTTypeScriptScenario{
		name:        "ast-typescript",
		description: "Tests TypeScript AST processor: verifies interfaces, classes, and function entities are extracted",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ASTTypeScriptScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ASTTypeScriptScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ASTTypeScriptScenario) Setup(ctx context.Context) error {
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

	// Capture baseline sequence before copying fixture
	seq, err := s.http.GetMaxSequence(ctx)
	if err != nil {
		return fmt.Errorf("get baseline sequence: %w", err)
	}
	s.baselineSequence = seq

	// Copy TypeScript fixture to workspace
	fixturePath := s.config.TSFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy TypeScript fixture: %w", err)
	}

	return nil
}

// Execute runs the TypeScript AST scenario.
func (s *ASTTypeScriptScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-interfaces", s.stageVerifyInterfaces},
		{"verify-class", s.stageVerifyClass},
		{"verify-methods", s.stageVerifyMethods},
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
func (s *ASTTypeScriptScenario) Teardown(_ context.Context) error {
	return nil
}

// stageVerifyFixture verifies the TypeScript fixture was copied correctly.
func (s *ASTTypeScriptScenario) stageVerifyFixture(_ context.Context, result *Result) error {
	// Check that expected files exist
	expectedFiles := []string{
		"package.json",
		"tsconfig.json",
		"src/index.ts",
		"src/auth/auth.types.ts",
		"src/auth/auth.service.ts",
		"src/auth/auth.test.ts",
		"src/types/user.ts",
		"README.md",
	}

	for _, file := range expectedFiles {
		if !s.fs.FileExistsRelative(file) {
			return fmt.Errorf("expected file %s not found in workspace", file)
		}
	}

	// Verify package.json content
	content, err := s.fs.ReadFileRelative("package.json")
	if err != nil {
		return fmt.Errorf("read package.json: %w", err)
	}
	if !strings.Contains(content, "testproject") {
		return fmt.Errorf("package.json doesn't contain expected project name")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures AST entity messages via the message-logger service.
func (s *ASTTypeScriptScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for AST indexing to produce TypeScript entities via message-logger
	// We expect: User, Token, AuthResult interfaces + AuthService class + UserProfile, UserPreferences interfaces
	// + file entities + function/const entities = at least 10+
	minExpectedEntities := 10

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Wait specifically for NEW TypeScript entities (after baseline sequence)
	if err := s.http.WaitForNewLanguageEntities(waitCtx, "typescript", minExpectedEntities, s.baselineSequence); err != nil {
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d typescript entities, got %d total (baseline seq: %d): %w", minExpectedEntities, len(entries), s.baselineSequence, err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))
	result.SetDetail("baseline_sequence", s.baselineSequence)

	// Extract TypeScript entity payloads from BaseMessage-wrapped log entries (filtered by sequence)
	entities := extractEntitiesForLanguageAfterSequence(entries, "typescript", s.baselineSequence)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyInterfaces verifies User, Token, and AuthResult interface entities were extracted.
func (s *ASTTypeScriptScenario) stageVerifyInterfaces(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedInterfaces := map[string]bool{
		"User":       false,
		"Token":      false,
		"AuthResult": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for ifaceName := range expectedInterfaces {
			if strings.Contains(id, ifaceName) {
				expectedInterfaces[ifaceName] = true
			}
		}

		// Also check triples for interface type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == codeAst.CodeType && obj == "interface" {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle {
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

	result.SetDetail("interfaces_verified", []string{"User", "Token", "AuthResult"})
	return nil
}

// stageVerifyClass verifies the AuthService class entity was extracted.
func (s *ASTTypeScriptScenario) stageVerifyClass(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Look for the AuthService class entity
	found := false
	for _, entity := range entities {
		id, _ := entity["id"].(string)
		if strings.Contains(id, "AuthService") {
			found = true
			result.SetDetail("auth_service_id", id)
			break
		}

		// Also check triples for class type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == codeAst.CodeType && obj == "class" {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle && obj2 == "AuthService" {
							found = true
							result.SetDetail("auth_service_id", id)
							break
						}
					}
				}
			}
		}
		if found {
			break
		}
	}

	if !found {
		return fmt.Errorf("AuthService class entity not found")
	}

	return nil
}

// stageVerifyMethods verifies authenticate, refreshToken, and generateToken method entities.
func (s *ASTTypeScriptScenario) stageVerifyMethods(_ context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedMethods := map[string]bool{
		"authenticate":  false,
		"refreshToken":  false,
		"generateToken": false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for methodName := range expectedMethods {
			if strings.Contains(id, methodName) {
				expectedMethods[methodName] = true
			}
		}

		// Also check triples for method type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == codeAst.CodeType && (obj == "method" || obj == "function") {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle {
							for methodName := range expectedMethods {
								if obj2 == methodName {
									expectedMethods[methodName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for methodName, found := range expectedMethods {
		if !found {
			missing = append(missing, methodName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("method entities not found: %v", missing)
	}

	result.SetDetail("methods_verified", []string{"authenticate", "refreshToken", "generateToken"})
	return nil
}
