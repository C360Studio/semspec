package workfloworchestrator

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow/validation"
)

// testLogger returns a silent logger for tests
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestStepToDocumentType(t *testing.T) {
	c := &Component{}

	tests := []struct {
		step     string
		expected validation.DocumentType
	}{
		{"propose", validation.DocumentTypeProposal},
		{"proposal", validation.DocumentTypeProposal},
		{"design", validation.DocumentTypeDesign},
		{"spec", validation.DocumentTypeSpec},
		{"tasks", validation.DocumentTypeTasks},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			got := c.stepToDocumentType(tt.step)
			if got != tt.expected {
				t.Errorf("stepToDocumentType(%q) = %q, want %q", tt.step, got, tt.expected)
			}
		})
	}
}

func TestGetDocumentPath(t *testing.T) {
	c := &Component{repoPath: "/test/repo"}

	tests := []struct {
		slug     string
		step     string
		expected string
	}{
		{"add-auth", "propose", "/test/repo/.semspec/changes/add-auth/proposal.md"},
		{"add-auth", "design", "/test/repo/.semspec/changes/add-auth/design.md"},
		{"add-auth", "spec", "/test/repo/.semspec/changes/add-auth/spec.md"},
		{"add-auth", "tasks", "/test/repo/.semspec/changes/add-auth/tasks.md"},
		{"my-feature", "custom", "/test/repo/.semspec/changes/my-feature/custom.md"},
	}

	for _, tt := range tests {
		name := tt.slug + "/" + tt.step
		t.Run(name, func(t *testing.T) {
			got := c.getDocumentPath(tt.slug, tt.step)
			if got != tt.expected {
				t.Errorf("getDocumentPath(%q, %q) = %q, want %q", tt.slug, tt.step, got, tt.expected)
			}
		})
	}
}

func TestGetDocumentPathNoRepoPath(t *testing.T) {
	c := &Component{repoPath: ""}

	path := c.getDocumentPath("test", "propose")
	expected := ".semspec/changes/test/proposal.md"
	if path != expected {
		t.Errorf("getDocumentPath with empty repoPath = %q, want %q", path, expected)
	}
}

func TestValidateAndHandleRetry_NonComplete(t *testing.T) {
	c := &Component{
		validator:    validation.NewValidator(),
		retryManager: validation.NewRetryManager(validation.DefaultRetryConfig()),
		logger:       testLogger(),
		config: Config{
			Validation: &ValidationConfig{
				Enabled:    true,
				MaxRetries: 3,
			},
		},
	}

	state := &LoopState{
		Status: "failed", // Not "complete"
	}

	// Should return true (skip validation) for non-complete status
	result := c.validateAndHandleRetry(nil, state)
	if !result {
		t.Error("expected true for non-complete status")
	}
}

func TestValidateAndHandleRetry_NoWorkflowContext(t *testing.T) {
	c := &Component{
		validator:    validation.NewValidator(),
		retryManager: validation.NewRetryManager(validation.DefaultRetryConfig()),
		logger:       testLogger(),
		config: Config{
			Validation: &ValidationConfig{
				Enabled:    true,
				MaxRetries: 3,
			},
		},
	}

	state := &LoopState{
		Status: "complete",
		// No WorkflowSlug or WorkflowStep
	}

	// Should return true (skip validation) without workflow context
	result := c.validateAndHandleRetry(nil, state)
	if !result {
		t.Error("expected true without workflow context")
	}
}

func TestValidateAndHandleRetry_ValidDocument(t *testing.T) {
	dir := t.TempDir()
	c := &Component{
		validator:    validation.NewValidator(),
		retryManager: validation.NewRetryManager(validation.DefaultRetryConfig()),
		logger:       testLogger(),
		repoPath:     dir,
		config: Config{
			Validation: &ValidationConfig{
				Enabled:    true,
				MaxRetries: 3,
			},
		},
		registry: model.NewDefaultRegistry(),
	}

	// Create a valid proposal document
	changeDir := filepath.Join(dir, ".semspec", "changes", "test-slug")
	if err := os.MkdirAll(changeDir, 0755); err != nil {
		t.Fatalf("failed to create change dir: %v", err)
	}

	validProposal := `# Add Authentication

## Why

We need user authentication to protect sensitive endpoints and provide personalized experiences.
This change will enable users to log in securely and maintain session state across requests.

## What Changes

- Add new auth package with JWT token handling
- Create middleware for protected routes
- Add user session management
- Update API endpoints to check authentication

## Impact

The impact will be substantial across multiple components including the API layer.
`
	if err := os.WriteFile(filepath.Join(changeDir, "proposal.md"), []byte(validProposal), 0644); err != nil {
		t.Fatalf("failed to write proposal: %v", err)
	}

	state := &LoopState{
		Status:       "complete",
		WorkflowSlug: "test-slug",
		WorkflowStep: "propose",
	}

	// Should return true for valid document
	result := c.validateAndHandleRetry(nil, state)
	if !result {
		t.Error("expected true for valid document")
	}
}

func TestValidateDocument_Invalid(t *testing.T) {
	// This test verifies the document validation logic without needing NATS
	dir := t.TempDir()

	// Create an invalid proposal document (missing sections)
	changeDir := filepath.Join(dir, ".semspec", "changes", "test-slug")
	if err := os.MkdirAll(changeDir, 0755); err != nil {
		t.Fatalf("failed to create change dir: %v", err)
	}

	invalidProposal := `# Add Authentication

## Why

Short.
`
	if err := os.WriteFile(filepath.Join(changeDir, "proposal.md"), []byte(invalidProposal), 0644); err != nil {
		t.Fatalf("failed to write proposal: %v", err)
	}

	// Read and validate the document directly
	content, err := os.ReadFile(filepath.Join(changeDir, "proposal.md"))
	if err != nil {
		t.Fatalf("failed to read document: %v", err)
	}

	validator := validation.NewValidator()
	result := validator.Validate(string(content), validation.DocumentTypeProposal)

	if result.Valid {
		t.Error("expected invalid for document with short sections")
	}

	if len(result.MissingSections) == 0 {
		t.Error("expected missing sections to be reported")
	}

	// Verify feedback can be generated
	feedback := result.FormatFeedback()
	if feedback == "" {
		t.Error("expected non-empty feedback")
	}
}

func TestRetryManager_Integration(t *testing.T) {
	// Test the retry manager logic separately
	rm := validation.NewRetryManager(validation.RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       1,
		BackoffMultiplier: 2.0,
	})

	slug := "test-slug"
	step := "propose"

	// First attempt
	rm.RecordAttempt(slug, step)
	if !rm.CanRetry(slug, step) {
		t.Error("expected can retry after first attempt")
	}

	// Second attempt
	rm.RecordAttempt(slug, step)
	if !rm.CanRetry(slug, step) {
		t.Error("expected can retry after second attempt")
	}

	// Third attempt - should not allow more retries
	rm.RecordAttempt(slug, step)
	if rm.CanRetry(slug, step) {
		t.Error("expected cannot retry after third attempt")
	}

	// Test ShouldRetry decision
	invalidResult := &validation.ValidationResult{
		Valid:           false,
		MissingSections: []string{"Why"},
	}

	decision := rm.ShouldRetry(slug, step, invalidResult)
	if decision.ShouldRetry {
		t.Error("expected no retry after max attempts")
	}
	if !decision.IsFinalFailure {
		t.Error("expected final failure after max attempts")
	}
}

func TestValidateAndHandleRetry_MissingDocument(t *testing.T) {
	dir := t.TempDir()
	c := &Component{
		validator:    validation.NewValidator(),
		retryManager: validation.NewRetryManager(validation.DefaultRetryConfig()),
		logger:       testLogger(),
		repoPath:     dir,
		config: Config{
			Validation: &ValidationConfig{
				Enabled:    true,
				MaxRetries: 3,
			},
		},
	}

	// Don't create the document - it's missing

	state := &LoopState{
		Status:       "complete",
		WorkflowSlug: "missing-slug",
		WorkflowStep: "propose",
	}

	// Should return true when document can't be read (allow proceeding)
	result := c.validateAndHandleRetry(nil, state)
	if !result {
		t.Error("expected true when document is missing (can't validate)")
	}
}
