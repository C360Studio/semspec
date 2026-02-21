package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"

	// Blank imports trigger payload type registrations (init side-effects).
	// Without these, BaseMessage deserialization can't reconstruct typed payloads.
	_ "github.com/c360studio/semstreams/processor/workflow"         // workflow.trigger.v1
	_ "github.com/c360studio/semstreams/processor/workflow/actions" // workflow.async_task.v1
)

func TestWorkflowTriggerPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		payload WorkflowTriggerPayload
		wantErr string
	}{
		{
			name:    "missing workflow_id",
			payload: WorkflowTriggerPayload{Data: &WorkflowTriggerData{Slug: "test", Description: "desc"}},
			wantErr: "workflow_id",
		},
		{
			name:    "missing slug",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow", Data: &WorkflowTriggerData{Description: "desc"}},
			wantErr: "slug",
		},
		{
			name:    "missing data",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow"},
			wantErr: "slug",
		},
		{
			name:    "missing description",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow", Data: &WorkflowTriggerData{Slug: "test"}},
			wantErr: "description",
		},
		{
			name: "valid payload",
			payload: WorkflowTriggerPayload{
				WorkflowID: "test-workflow",
				Data: &WorkflowTriggerData{
					Slug:        "test-feature",
					Description: "Test feature description",
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				// Verify it's a ValidationError
				if _, ok := err.(*ValidationError); !ok {
					t.Errorf("expected *ValidationError, got %T", err)
				}
			}
		})
	}
}

func TestWorkflowTriggerPayload_JSON(t *testing.T) {
	payload := WorkflowTriggerPayload{
		WorkflowID:  "test-workflow",
		Role:        "writer",
		Model:       "qwen",
		Prompt:      "Generate a plan",
		UserID:      "user-123",
		ChannelType: "cli",
		ChannelID:   "session-456",
		RequestID:   "req-789",
		Data: &WorkflowTriggerData{
			Slug:        "test-feature",
			Title:       "Test Feature",
			Description: "A test feature",
			Auto:        true,
		},
	}

	// Marshal
	data, err := json.Marshal(&payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify workflow_id is in JSON
	if !strings.Contains(string(data), `"workflow_id":"test-workflow"`) {
		t.Errorf("JSON does not contain workflow_id: %s", data)
	}

	// Verify data.slug is in JSON
	if !strings.Contains(string(data), `"slug":"test-feature"`) {
		t.Errorf("JSON does not contain data.slug: %s", data)
	}

	// Unmarshal
	var decoded WorkflowTriggerPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.WorkflowID != payload.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", decoded.WorkflowID, payload.WorkflowID)
	}
	if decoded.Data == nil {
		t.Fatal("Data is nil after unmarshal")
	}
	if decoded.Data.Slug != payload.Data.Slug {
		t.Errorf("Data.Slug = %q, want %q", decoded.Data.Slug, payload.Data.Slug)
	}
	if decoded.Data.Auto != payload.Data.Auto {
		t.Errorf("Data.Auto = %v, want %v", decoded.Data.Auto, payload.Data.Auto)
	}
	if decoded.Model != payload.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, payload.Model)
	}
}

// ---------------------------------------------------------------------------
// ParseNATSMessage — wire format tests
// ---------------------------------------------------------------------------

// sampleTrigger returns a TriggerPayload with every field populated so tests
// can assert nothing was dropped.
func sampleTrigger() TriggerPayload {
	return TriggerPayload{
		WorkflowID: "plan-review-loop",
		Role:       "planner",
		Prompt:     "Add a goodbye endpoint",
		RequestID:  "req-123",
		TraceID:    "trace-abc",
		Data: &TriggerData{
			Slug:        "add-goodbye-endpoint",
			Title:       "Add goodbye endpoint",
			Description: "Add a goodbye endpoint that returns a farewell message",
			ProjectID:   "proj-42",
			TraceID:     "trace-abc",
		},
	}
}

func TestParseNATSMessage_AsyncTaskPayload(t *testing.T) {
	// Simulates workflow-processor publish_async:
	// BaseMessage { type: workflow.async_task.v1, payload: { task_id, callback_subject, data: <step payload> } }
	inner := sampleTrigger()
	innerBytes, err := json.Marshal(inner)
	if err != nil {
		t.Fatal(err)
	}

	envelope := asyncTaskEnvelope{
		TaskID:          "task-99",
		CallbackSubject: "workflow.step.result.exec_abc",
		Data:            innerBytes,
	}

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "async_task", Version: "v1"},
		&testPayload{data: mustMarshal(t, envelope)},
		"workflow-processor",
	)
	wire, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseNATSMessage[TriggerPayload](wire)
	if err != nil {
		t.Fatalf("ParseNATSMessage: %v", err)
	}

	assertTrigger(t, got, inner)

	if got.TaskID != "task-99" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "task-99")
	}
	if got.CallbackSubject != "workflow.step.result.exec_abc" {
		t.Errorf("CallbackSubject = %q, want %q", got.CallbackSubject, "workflow.step.result.exec_abc")
	}
}

func TestParseNATSMessage_CoreJSON(t *testing.T) {
	// Simulates workflow-processor publish/call:
	// BaseMessage { type: core.json.v1, payload: { data: <step payload> } }
	inner := sampleTrigger()
	innerBytes, err := json.Marshal(inner)
	if err != nil {
		t.Fatal(err)
	}

	envelope := genericJSONEnvelope{Data: innerBytes}

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "core", Category: "json", Version: "v1"},
		&testPayload{data: mustMarshal(t, envelope)},
		"workflow-processor",
	)
	wire, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseNATSMessage[TriggerPayload](wire)
	if err != nil {
		t.Fatalf("ParseNATSMessage: %v", err)
	}

	assertTrigger(t, got, inner)
}

func TestParseNATSMessage_DirectBaseMessage(t *testing.T) {
	// Standard component-to-component: BaseMessage wrapping TriggerPayload.
	inner := sampleTrigger()

	baseMsg := message.NewBaseMessage(WorkflowTriggerType, &inner, "workflow-api")
	wire, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseNATSMessage[TriggerPayload](wire)
	if err != nil {
		t.Fatalf("ParseNATSMessage: %v", err)
	}

	assertTrigger(t, got, inner)
}

func TestParseNATSMessage_RawJSON(t *testing.T) {
	// Legacy fallback: plain JSON (no BaseMessage wrapper).
	inner := sampleTrigger()
	wire, err := json.Marshal(inner)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseNATSMessage[TriggerPayload](wire)
	if err != nil {
		t.Fatalf("ParseNATSMessage: %v", err)
	}

	assertTrigger(t, got, inner)
}

func TestParseNATSMessage_AsyncTask_CallbackInjection(t *testing.T) {
	inner := TriggerPayload{
		WorkflowID: "test-wf",
		Data:       &TriggerData{Slug: "s", Description: "d"},
	}
	innerBytes, _ := json.Marshal(inner)

	envelope := asyncTaskEnvelope{
		TaskID:          "injected-task",
		CallbackSubject: "injected-callback",
		Data:            innerBytes,
	}

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "async_task", Version: "v1"},
		&testPayload{data: mustMarshal(t, envelope)},
		"workflow-processor",
	)
	wire, _ := json.Marshal(baseMsg)

	got, err := ParseNATSMessage[TriggerPayload](wire)
	if err != nil {
		t.Fatalf("ParseNATSMessage: %v", err)
	}

	if !got.HasCallback() {
		t.Fatal("HasCallback() = false after injection")
	}
	if got.TaskID != "injected-task" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "injected-task")
	}
	if got.CallbackSubject != "injected-callback" {
		t.Errorf("CallbackSubject = %q, want %q", got.CallbackSubject, "injected-callback")
	}
}

// ---------------------------------------------------------------------------
// Workflow definition interpolation path validation
// ---------------------------------------------------------------------------

// TestWorkflowDefinitionPaths_PlanReviewLoop validates that every
// ${trigger.payload.*} interpolation path in plan-review-loop.json resolves
// to a field that exists in the merged payload produced by the semstreams
// workflow-processor's buildMergedPayload().
//
// The workflow-processor flattens TriggerData fields to the top level
// (e.g., Data.slug → trigger.payload.slug). A path like
// trigger.payload.data.slug will fail silently, returning the literal
// template string — exactly the bug this test prevents.
func TestWorkflowDefinitionPaths_PlanReviewLoop(t *testing.T) {
	defPath := filepath.Join("..", "configs", "workflows", "plan-review-loop.json")
	raw, err := os.ReadFile(defPath)
	if err != nil {
		t.Skipf("workflow definition not found at %s: %v", defPath, err)
	}

	// Reject unsupported default-value syntax (e.g., ${trigger.payload.trace_id:-}).
	// Semstreams interpolation treats ":-" as part of the key, causing silent failures.
	unsupported := regexp.MustCompile(`\$\{[^}]+:-[^}]*\}`)
	if badMatches := unsupported.FindAllString(string(raw), -1); len(badMatches) > 0 {
		t.Errorf("workflow definition contains unsupported default-value syntax "+
			"(semstreams treats ':-' as part of the key): %v", badMatches)
	}

	// Extract all ${trigger.payload.*} interpolation variables.
	re := regexp.MustCompile(`\$\{trigger\.payload\.([^}]+)\}`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("no ${trigger.payload.*} variables found in workflow definition")
	}

	// Build the merged payload map — simulates what semstreams
	// buildMergedPayload() produces from a TriggerPayload.
	trigger := sampleTrigger()
	merged := simulateMergedPayload(t, &trigger)

	for _, m := range matches {
		path := m[1] // e.g., "slug", "title", "data.slug"
		if !resolveMapPath(merged, path) {
			t.Errorf("${trigger.payload.%s} does not resolve in merged payload; "+
				"available keys: %v", path, mapKeys(merged))
		}
	}
}

// TestTriggerPayload_TraceIDSurvivesFlattening verifies that trace_id in
// TriggerData survives the semstreams buildMergedPayload() flattening.
func TestTriggerPayload_TraceIDSurvivesFlattening(t *testing.T) {
	trigger := sampleTrigger()
	merged := simulateMergedPayload(t, &trigger)

	traceID, ok := merged["trace_id"]
	if !ok {
		t.Fatal("trace_id not in merged payload — lost during workflow interpolation")
	}
	if traceID != "trace-abc" {
		t.Errorf("trace_id = %q, want %q", traceID, "trace-abc")
	}
}

// TestTriggerPayload_AllDataFieldsFlatten ensures every TriggerData field
// is accessible at the top level of the merged payload.
func TestTriggerPayload_AllDataFieldsFlatten(t *testing.T) {
	trigger := sampleTrigger()
	merged := simulateMergedPayload(t, &trigger)

	requiredFields := map[string]string{
		"slug":        "add-goodbye-endpoint",
		"title":       "Add goodbye endpoint",
		"description": "Add a goodbye endpoint that returns a farewell message",
		"project_id":  "proj-42",
		"trace_id":    "trace-abc",
	}

	for field, want := range requiredFields {
		got, ok := merged[field]
		if !ok {
			t.Errorf("field %q missing from merged payload", field)
			continue
		}
		if fmt, ok := got.(string); ok && fmt != want {
			t.Errorf("merged[%q] = %q, want %q", field, fmt, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// simulateMergedPayload replicates the semstreams workflow-processor
// buildMergedPayload() behavior: Data blob is parsed first (base layer),
// then struct fields are overlaid. Fields NOT in the semstreams
// TriggerPayload struct (TraceID, LoopID) are dropped — they must be
// duplicated in TriggerData to survive.
func simulateMergedPayload(t *testing.T, trigger *TriggerPayload) map[string]any {
	t.Helper()
	result := make(map[string]any)

	// Step 1: Parse Data blob (base layer)
	if trigger.Data != nil {
		dataBytes, err := json.Marshal(trigger.Data)
		if err != nil {
			t.Fatalf("marshal TriggerData: %v", err)
		}
		if err := json.Unmarshal(dataBytes, &result); err != nil {
			result["_data"] = string(dataBytes)
		}
	}

	// Step 2: Overlay ONLY the fields that semstreams TriggerPayload knows.
	// This is the authoritative list from semstreams execution.go.
	if trigger.WorkflowID != "" {
		result["workflow_id"] = trigger.WorkflowID
	}
	if trigger.Role != "" {
		result["role"] = trigger.Role
	}
	if trigger.Model != "" {
		result["model"] = trigger.Model
	}
	if trigger.Prompt != "" {
		result["prompt"] = trigger.Prompt
	}
	if trigger.UserID != "" {
		result["user_id"] = trigger.UserID
	}
	if trigger.ChannelType != "" {
		result["channel_type"] = trigger.ChannelType
	}
	if trigger.ChannelID != "" {
		result["channel_id"] = trigger.ChannelID
	}
	if trigger.RequestID != "" {
		result["request_id"] = trigger.RequestID
	}

	return result
}

// resolveMapPath traverses a map using a dot-separated path.
func resolveMapPath(m map[string]any, path string) bool {
	parts := strings.Split(path, ".")
	current := any(m)

	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = cm[part]
		if !ok {
			return false
		}
	}
	return true
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func assertTrigger(t *testing.T, got *TriggerPayload, want TriggerPayload) {
	t.Helper()
	if got.WorkflowID != want.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", got.WorkflowID, want.WorkflowID)
	}
	if got.Role != want.Role {
		t.Errorf("Role = %q, want %q", got.Role, want.Role)
	}
	if got.Prompt != want.Prompt {
		t.Errorf("Prompt = %q, want %q", got.Prompt, want.Prompt)
	}
	if got.RequestID != want.RequestID {
		t.Errorf("RequestID = %q, want %q", got.RequestID, want.RequestID)
	}
	if got.TraceID != want.TraceID {
		t.Errorf("TraceID = %q, want %q", got.TraceID, want.TraceID)
	}
	if got.Data == nil {
		t.Fatal("Data is nil")
	}
	if got.Data.Slug != want.Data.Slug {
		t.Errorf("Data.Slug = %q, want %q", got.Data.Slug, want.Data.Slug)
	}
	if got.Data.Title != want.Data.Title {
		t.Errorf("Data.Title = %q, want %q", got.Data.Title, want.Data.Title)
	}
	if got.Data.Description != want.Data.Description {
		t.Errorf("Data.Description = %q, want %q", got.Data.Description, want.Data.Description)
	}
}

// testPayload wraps arbitrary pre-marshalled JSON in a message.Payload for
// constructing BaseMessages in tests.
type testPayload struct {
	data []byte
}

func (p *testPayload) Schema() message.Type { return message.Type{} }
func (p *testPayload) Validate() error      { return nil }

func (p *testPayload) MarshalJSON() ([]byte, error) {
	return p.data, nil
}

func (p *testPayload) UnmarshalJSON(data []byte) error {
	p.data = data
	return nil
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
