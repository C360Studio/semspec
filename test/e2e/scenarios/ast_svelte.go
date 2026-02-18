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

// ASTSvelteScenario tests Svelte AST processor verification.
// It copies a Svelte fixture project and verifies AST entities are extracted correctly,
// including Svelte 5 runes ($props, $state, $derived, $effect).
type ASTSvelteScenario struct {
	name             string
	description      string
	config           *config.Config
	http             *client.HTTPClient
	fs               *client.FilesystemClient
	baselineSequence int64 // Sequence number at setup time, used to filter for new entities
}

// NewASTSvelteScenario creates a new Svelte AST processor scenario.
func NewASTSvelteScenario(cfg *config.Config) *ASTSvelteScenario {
	return &ASTSvelteScenario{
		name:        "ast-svelte",
		description: "Tests Svelte AST processor: verifies component entities with runes ($props, $state, $derived) are extracted",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ASTSvelteScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ASTSvelteScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ASTSvelteScenario) Setup(ctx context.Context) error {
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

	// Copy Svelte fixture to workspace
	fixturePath := s.config.SvelteFixturePath()
	if err := s.fs.CopyFixture(fixturePath); err != nil {
		return fmt.Errorf("copy Svelte fixture: %w", err)
	}

	return nil
}

// Execute runs the Svelte AST scenario.
func (s *ASTSvelteScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-fixture", s.stageVerifyFixture},
		{"capture-entities", s.stageCaptureEntities},
		{"verify-components", s.stageVerifyComponents},
		{"verify-runes", s.stageVerifyRunes},
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
func (s *ASTSvelteScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageVerifyFixture verifies the Svelte fixture was copied correctly.
func (s *ASTSvelteScenario) stageVerifyFixture(ctx context.Context, result *Result) error {
	// Check that expected files exist
	expectedFiles := []string{
		"package.json",
		"src/lib/components/Button.svelte",
		"src/lib/components/Card.svelte",
		"src/lib/components/Icon.svelte",
		"src/routes/+page.svelte",
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
	if !strings.Contains(content, "svelte") {
		return fmt.Errorf("package.json doesn't contain svelte dependency")
	}

	result.SetDetail("fixture_files", expectedFiles)
	return nil
}

// stageCaptureEntities captures AST entity messages via the message-logger service.
func (s *ASTSvelteScenario) stageCaptureEntities(ctx context.Context, result *Result) error {
	// Wait for AST indexing to produce Svelte entities via message-logger
	// We expect: 4 component entities (Button, Card, Icon, +page) + file entities + function entities
	minExpectedEntities := 4

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Wait specifically for NEW Svelte entities (after baseline sequence)
	if err := s.http.WaitForNewLanguageEntities(waitCtx, "svelte", minExpectedEntities, s.baselineSequence); err != nil {
		entries, _ := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
		return fmt.Errorf("expected at least %d svelte entities, got %d total (baseline seq: %d): %w", minExpectedEntities, len(entries), s.baselineSequence, err)
	}

	// Fetch all captured entity messages
	entries, err := s.http.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	result.SetDetail("entity_count", len(entries))
	result.SetDetail("baseline_sequence", s.baselineSequence)

	// Extract Svelte entity payloads by framework (not language, since Svelte is a framework)
	// Language will be typescript/javascript, framework will be "svelte"
	entities := extractEntitiesForFrameworkAfterSequence(entries, "svelte", s.baselineSequence)
	result.SetDetail("entities", entities)

	return nil
}

// stageVerifyComponents verifies component entities were extracted correctly.
func (s *ASTSvelteScenario) stageVerifyComponents(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedComponents := map[string]bool{
		"Button": false,
		"Card":   false,
		"Icon":   false,
		"+page":  false,
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for compName := range expectedComponents {
			if strings.Contains(id, compName) {
				expectedComponents[compName] = true
			}
		}

		// Also check triples for component type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == codeAst.CodeType && obj == "component" {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle {
							for compName := range expectedComponents {
								if obj2 == compName {
									expectedComponents[compName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for compName, found := range expectedComponents {
		if !found {
			missing = append(missing, compName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("component entities not found: %v", missing)
	}

	result.SetDetail("components_verified", []string{"Button", "Card", "Icon", "+page"})
	return nil
}

// stageVerifyRunes verifies that Svelte 5 runes information was extracted.
func (s *ASTSvelteScenario) stageVerifyRunes(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	// Look for the Card component and verify it has runes info in doc comment
	var cardDocComment string
	for _, entity := range entities {
		// Check triples for Card component with doc comment containing runes
		if triples, ok := entity["triples"].([]any); ok {
			isCard := false
			docComment := ""

			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)

				if pred == codeAst.DcTitle && obj == "Card" {
					isCard = true
				}
				if pred == codeAst.CodeDocComment {
					docComment = obj
				}
			}

			if isCard && docComment != "" {
				cardDocComment = docComment
				break
			}
		}
	}

	if cardDocComment == "" {
		return fmt.Errorf("Card component doc comment not found or empty")
	}

	// Verify runes info is present
	expectedPatterns := []string{
		"Props:",  // $props() extraction
		"State:",  // $state() extraction
		"Derived", // $derived() extraction
		"Effect",  // $effect() extraction
	}

	var missingPatterns []string
	for _, pattern := range expectedPatterns {
		if !strings.Contains(cardDocComment, pattern) {
			missingPatterns = append(missingPatterns, pattern)
		}
	}

	if len(missingPatterns) > 0 {
		return fmt.Errorf("missing runes info in Card doc comment: %v (got: %s)", missingPatterns, cardDocComment)
	}

	result.SetDetail("runes_verified", expectedPatterns)
	result.SetDetail("card_doc_comment", cardDocComment)
	return nil
}

// stageVerifyFunctions verifies function entities were extracted from script blocks.
func (s *ASTSvelteScenario) stageVerifyFunctions(ctx context.Context, result *Result) error {
	entitiesVal, ok := result.GetDetail("entities")
	if !ok {
		return fmt.Errorf("no entities found in result")
	}

	entities, ok := entitiesVal.([]map[string]any)
	if !ok {
		return fmt.Errorf("entities not in expected format")
	}

	expectedFunctions := map[string]bool{
		"handleClick": false, // Card.svelte
		"increment":   false, // +page.svelte
		"addItem":     false, // +page.svelte
	}

	for _, entity := range entities {
		id, _ := entity["id"].(string)

		for funcName := range expectedFunctions {
			if strings.Contains(id, funcName) {
				expectedFunctions[funcName] = true
			}
		}

		// Also check triples for function type
		if triples, ok := entity["triples"].([]any); ok {
			for _, t := range triples {
				triple, _ := t.(map[string]any)
				pred, _ := triple["predicate"].(string)
				obj, _ := triple["object"].(string)
				if pred == codeAst.CodeType && obj == "function" {
					// Check if name matches (using dc.terms.title)
					for _, t2 := range triples {
						triple2, _ := t2.(map[string]any)
						pred2, _ := triple2["predicate"].(string)
						obj2, _ := triple2["object"].(string)
						if pred2 == codeAst.DcTitle {
							for funcName := range expectedFunctions {
								if obj2 == funcName {
									expectedFunctions[funcName] = true
								}
							}
						}
					}
				}
			}
		}
	}

	var missing []string
	for funcName, found := range expectedFunctions {
		if !found {
			missing = append(missing, funcName)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("function entities not found: %v", missing)
	}

	result.SetDetail("functions_verified", []string{"handleClick", "increment", "addItem"})
	return nil
}
