package structuralvalidator

import (
	"testing"

	"github.com/c360studio/semspec/workflow/reactive"
)

// TestValidationRequest_Validate verifies the validation logic.
func TestValidationRequest_Validate(t *testing.T) {
	// Empty slug → error.
	trigger := &reactive.ValidationRequest{}
	if err := trigger.Validate(); err == nil {
		t.Error("expected error for empty slug")
	}

	// Non-empty slug → ok.
	trigger.Slug = "valid"
	if err := trigger.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidationResult_Schema verifies the result schema matches registration.
func TestValidationResult_Schema(t *testing.T) {
	result := &ValidationResult{
		Slug:      "test",
		Passed:    true,
		ChecksRun: 2,
	}

	schema := result.Schema()
	if schema.Domain != "workflow" {
		t.Errorf("expected Domain=workflow, got %q", schema.Domain)
	}
	if schema.Category != "structural-validation-result" {
		t.Errorf("expected Category=structural-validation-result, got %q", schema.Category)
	}
	if schema.Version != "v1" {
		t.Errorf("expected Version=v1, got %q", schema.Version)
	}
}
