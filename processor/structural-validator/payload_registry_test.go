package structuralvalidator

import (
	"encoding/json"
	"testing"
)

// TestValidationTrigger_CallbackFields verifies that the embedded
// CallbackFields are properly marshalled/unmarshalled via JSON and that
// HasCallback returns the correct value.
func TestValidationTrigger_CallbackFields(t *testing.T) {
	trigger := &ValidationTrigger{
		Slug:          "test-slug",
		FilesModified: []string{"main.go"},
		WorkflowID:    "task-execution-loop",
	}

	// No callback set → HasCallback should be false.
	if trigger.HasCallback() {
		t.Error("expected HasCallback()=false when no callback fields set")
	}

	// Set callback fields.
	trigger.CallbackSubject = "workflow.step-callback.exec-1.task-1"
	trigger.TaskID = "task-1"
	trigger.ExecutionID = "exec-1"

	if !trigger.HasCallback() {
		t.Error("expected HasCallback()=true when callback fields set")
	}

	// Round-trip through JSON.
	data, err := json.Marshal(trigger)
	if err != nil {
		t.Fatalf("marshal trigger: %v", err)
	}

	var decoded ValidationTrigger
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal trigger: %v", err)
	}

	if decoded.Slug != "test-slug" {
		t.Errorf("expected Slug=test-slug, got %q", decoded.Slug)
	}
	if decoded.CallbackSubject != "workflow.step-callback.exec-1.task-1" {
		t.Errorf("expected CallbackSubject preserved, got %q", decoded.CallbackSubject)
	}
	if decoded.TaskID != "task-1" {
		t.Errorf("expected TaskID preserved, got %q", decoded.TaskID)
	}
	if !decoded.HasCallback() {
		t.Error("expected HasCallback()=true after JSON round-trip")
	}
}

// TestValidationTrigger_SetCallback verifies the CallbackReceiver interface
// implementation (used by workflow.ParseNATSMessage to inject callback fields).
func TestValidationTrigger_SetCallback(t *testing.T) {
	trigger := &ValidationTrigger{
		Slug: "test-slug",
	}

	trigger.SetCallback("task-42", "workflow.step-callback.exec-99.task-42")

	if !trigger.HasCallback() {
		t.Error("expected HasCallback()=true after SetCallback")
	}
	if trigger.TaskID != "task-42" {
		t.Errorf("expected TaskID=task-42, got %q", trigger.TaskID)
	}
	if trigger.CallbackSubject != "workflow.step-callback.exec-99.task-42" {
		t.Errorf("expected CallbackSubject set, got %q", trigger.CallbackSubject)
	}
}

// TestValidationTrigger_Validate verifies the validation logic.
func TestValidationTrigger_Validate(t *testing.T) {
	// Empty slug → error.
	trigger := &ValidationTrigger{}
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
