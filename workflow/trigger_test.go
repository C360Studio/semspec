package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
)

func TestWorkflowTriggerPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		payload WorkflowTriggerPayload
		wantErr string
	}{
		{
			name:    "missing workflow_id",
			payload: WorkflowTriggerPayload{Slug: "test"},
			wantErr: "workflow_id",
		},
		{
			name:    "missing slug",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow"},
			wantErr: "slug",
		},
		{
			name: "valid payload",
			payload: WorkflowTriggerPayload{
				WorkflowID: "test-workflow",
				Slug:       "test-feature",
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
		Slug:        "test-feature",
		Title:       "Test Feature",
		Description: "A test feature",
		Auto:        true,
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

	// Verify slug is in JSON
	if !strings.Contains(string(data), `"slug":"test-feature"`) {
		t.Errorf("JSON does not contain slug: %s", data)
	}

	// Unmarshal
	var decoded WorkflowTriggerPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.WorkflowID != payload.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", decoded.WorkflowID, payload.WorkflowID)
	}
	if decoded.Slug != payload.Slug {
		t.Errorf("Slug = %q, want %q", decoded.Slug, payload.Slug)
	}
	if decoded.Auto != payload.Auto {
		t.Errorf("Auto = %v, want %v", decoded.Auto, payload.Auto)
	}
	if decoded.Model != payload.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, payload.Model)
	}
}

func TestWorkflowTriggerPayload_UnmarshalNestedData(t *testing.T) {
	// Test that we can unmarshal the old nested format for backward compat
	oldFormat := `{
		"workflow_id": "plan-review-loop",
		"role": "planner",
		"request_id": "req-123",
		"data": {
			"slug": "add-feature",
			"title": "Add Feature",
			"description": "Add a new feature",
			"trace_id": "trace-456"
		}
	}`

	var payload TriggerPayload
	if err := json.Unmarshal([]byte(oldFormat), &payload); err != nil {
		t.Fatalf("failed to unmarshal old format: %v", err)
	}

	if payload.WorkflowID != "plan-review-loop" {
		t.Errorf("WorkflowID = %q, want %q", payload.WorkflowID, "plan-review-loop")
	}
	if payload.Slug != "add-feature" {
		t.Errorf("Slug = %q, want %q (extracted from nested data)", payload.Slug, "add-feature")
	}
	if payload.Title != "Add Feature" {
		t.Errorf("Title = %q, want %q (extracted from nested data)", payload.Title, "Add Feature")
	}
	if payload.TraceID != "trace-456" {
		t.Errorf("TraceID = %q, want %q (extracted from nested data)", payload.TraceID, "trace-456")
	}
}

// ---------------------------------------------------------------------------
// ParseNATSMessage — wire format tests
// ---------------------------------------------------------------------------

// sampleTrigger returns a TriggerPayload with fields populated so tests
// can assert nothing was dropped. Includes fields used across all workflows
// (plan-review-loop, task-review-loop, task-execution-loop).
func sampleTrigger() TriggerPayload {
	return TriggerPayload{
		WorkflowID:    "plan-review-loop",
		Role:          "planner",
		Model:         "qwen",
		Prompt:        "Add a goodbye endpoint",
		RequestID:     "req-123",
		TraceID:       "trace-abc",
		Slug:          "add-goodbye-endpoint",
		Title:         "Add goodbye endpoint",
		Description:   "Add a goodbye endpoint that returns a farewell message",
		ProjectID:     "proj-42",
		ScopePatterns: []string{"src/**/*.go"},
		// Data blob includes task-execution-loop fields not on the struct.
		Data: json.RawMessage(`{"task_id":"task.add-goodbye-endpoint.1"}`),
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
		Slug:       "s",
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
// Workflow definition validation (ADR-020)
// ---------------------------------------------------------------------------

// validateWorkflowTriggerPaths validates that every trigger.payload reference
// in a workflow definition resolves to a field in the merged trigger payload.
//
// References come in three forms:
//   - "from": "trigger.payload.*" in step inputs
//   - "template": "...${trigger.payload.*}..." in step inputs
//   - ${trigger.payload.*} in publish_agent ActionDef fields (condition.field, etc.)
func validateWorkflowTriggerPaths(t *testing.T, defPath string) {
	t.Helper()

	raw, err := os.ReadFile(defPath)
	if err != nil {
		t.Skipf("workflow definition not found at %s: %v", defPath, err)
	}

	// Extract all ${trigger.payload.*} interpolation variables (in templates, conditions, agent fields).
	interpRe := regexp.MustCompile(`\$\{trigger\.payload\.([^}]+)\}`)
	interpMatches := interpRe.FindAllStringSubmatch(string(raw), -1)

	// Extract all "from": "trigger.payload.*" input references.
	fromRe := regexp.MustCompile(`"from"\s*:\s*"trigger\.payload\.([^"]+)"`)
	fromMatches := fromRe.FindAllStringSubmatch(string(raw), -1)

	allPaths := make(map[string]bool)
	for _, m := range interpMatches {
		path := m[1]
		// Strip default-value syntax: "model:-qwen" → "model"
		if idx := strings.Index(path, ":-"); idx != -1 {
			path = path[:idx]
		}
		allPaths[path] = true
	}
	for _, m := range fromMatches {
		allPaths[m[1]] = true
	}

	if len(allPaths) == 0 {
		t.Fatal("no trigger.payload references found in workflow definition")
	}

	trigger := sampleTrigger()
	merged := simulateMergedPayload(t, &trigger)

	for path := range allPaths {
		if !resolveMapPath(merged, path) {
			t.Errorf("trigger.payload.%s does not resolve in merged payload; "+
				"available keys: %v", path, mapKeys(merged))
		}
	}
}

// validateWorkflowInputRefs validates that all "from" references in step
// inputs point to valid sources: trigger.payload.*, execution.*, or a
// declared step output. Template inputs are skipped (validated by semstreams).
func validateWorkflowInputRefs(t *testing.T, defPath string) {
	t.Helper()

	raw, err := os.ReadFile(defPath)
	if err != nil {
		t.Skipf("workflow definition not found at %s: %v", defPath, err)
	}

	var def struct {
		Steps []struct {
			Name    string                     `json:"name"`
			Inputs  map[string]json.RawMessage `json:"inputs,omitempty"`
			Outputs map[string]json.RawMessage `json:"outputs,omitempty"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("failed to parse workflow definition: %v", err)
	}

	// Collect all declared step outputs.
	declaredOutputs := make(map[string]bool)
	for _, step := range def.Steps {
		for outName := range step.Outputs {
			declaredOutputs[step.Name+"."+outName] = true
		}
	}

	for _, step := range def.Steps {
		for inputName, rawInput := range step.Inputs {
			var ref struct {
				From     string `json:"from"`
				Template string `json:"template"`
			}
			if err := json.Unmarshal(rawInput, &ref); err != nil {
				t.Errorf("step %q input %q: failed to parse: %v", step.Name, inputName, err)
				continue
			}

			// Template inputs are validated by semstreams at load time.
			if ref.Template != "" {
				if ref.From != "" {
					t.Errorf("step %q input %q: has both 'from' and 'template' (must be exactly one)",
						step.Name, inputName)
				}
				continue
			}

			if ref.From == "" {
				t.Errorf("step %q input %q: has neither 'from' nor 'template'", step.Name, inputName)
				continue
			}

			// Valid sources: trigger.payload.*, execution.*, or step.output
			if strings.HasPrefix(ref.From, "trigger.payload.") {
				continue
			}
			if strings.HasPrefix(ref.From, "execution.") {
				continue
			}

			// Must reference a declared step output (first two segments: step.output).
			parts := strings.SplitN(ref.From, ".", 3)
			if len(parts) < 2 {
				t.Errorf("step %q input %q: invalid from reference %q (need at least step.output)",
					step.Name, inputName, ref.From)
				continue
			}
			stepOutput := parts[0] + "." + parts[1]
			if !declaredOutputs[stepOutput] {
				t.Errorf("step %q input %q: from reference %q points to undeclared output %q; declared outputs: %v",
					step.Name, inputName, ref.From, stepOutput, mapKeys2(declaredOutputs))
			}
		}
	}
}

// --- Per-workflow test functions ---

func TestWorkflowDefinitionPaths_PlanReviewLoop(t *testing.T) {
	validateWorkflowTriggerPaths(t, filepath.Join("..", "configs", "workflows", "plan-review-loop.json"))
}

func TestWorkflowDefinitionInputsFromRefs_PlanReviewLoop(t *testing.T) {
	validateWorkflowInputRefs(t, filepath.Join("..", "configs", "workflows", "plan-review-loop.json"))
}

func TestWorkflowDefinitionPaths_TaskReviewLoop(t *testing.T) {
	validateWorkflowTriggerPaths(t, filepath.Join("..", "configs", "workflows", "task-review-loop.json"))
}

func TestWorkflowDefinitionInputsFromRefs_TaskReviewLoop(t *testing.T) {
	validateWorkflowInputRefs(t, filepath.Join("..", "configs", "workflows", "task-review-loop.json"))
}

func TestWorkflowDefinitionPaths_TaskExecutionLoop(t *testing.T) {
	validateWorkflowTriggerPaths(t, filepath.Join("..", "configs", "workflows", "task-execution-loop.json"))
}

func TestWorkflowDefinitionInputsFromRefs_TaskExecutionLoop(t *testing.T) {
	validateWorkflowInputRefs(t, filepath.Join("..", "configs", "workflows", "task-execution-loop.json"))
}

// TestTriggerPayload_TraceIDSurvivesFlattening verifies that trace_id in
// TriggerPayload survives the semstreams buildMergedPayload() flattening.
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

// TestTriggerPayload_AllFieldsFlatten ensures all semspec-specific fields
// are accessible at the top level of the merged payload.
func TestTriggerPayload_AllFieldsFlatten(t *testing.T) {
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

func TestMarshalTriggerData(t *testing.T) {
	data := MarshalTriggerData(
		"test-slug",
		"Test Title",
		"Test Description",
		"trace-123",
		"proj-456",
		[]string{"src/**/*.go"},
		true,
	)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["slug"] != "test-slug" {
		t.Errorf("slug = %q, want %q", parsed["slug"], "test-slug")
	}
	if parsed["title"] != "Test Title" {
		t.Errorf("title = %q, want %q", parsed["title"], "Test Title")
	}
	if parsed["trace_id"] != "trace-123" {
		t.Errorf("trace_id = %q, want %q", parsed["trace_id"], "trace-123")
	}
	if parsed["auto"] != true {
		t.Errorf("auto = %v, want true", parsed["auto"])
	}
}

func TestNewSemstreamsTrigger(t *testing.T) {
	trigger := NewSemstreamsTrigger(
		"plan-review-loop",
		"planner",
		"Test prompt",
		"req-123",
		"test-slug",
		"Test Title",
		"Test Description",
		"trace-456",
		"proj-789",
		[]string{"**/*.go"},
		false,
	)

	if trigger.WorkflowID != "plan-review-loop" {
		t.Errorf("WorkflowID = %q, want %q", trigger.WorkflowID, "plan-review-loop")
	}
	if trigger.Role != "planner" {
		t.Errorf("Role = %q, want %q", trigger.Role, "planner")
	}
	if trigger.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want %q", trigger.RequestID, "req-123")
	}

	// Verify Data blob contains correct fields
	var data map[string]any
	if err := json.Unmarshal(trigger.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal Data: %v", err)
	}
	if data["slug"] != "test-slug" {
		t.Errorf("Data.slug = %q, want %q", data["slug"], "test-slug")
	}
	if data["trace_id"] != "trace-456" {
		t.Errorf("Data.trace_id = %q, want %q", data["trace_id"], "trace-456")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// simulateMergedPayload replicates the semstreams workflow-processor
// buildMergedPayload() behavior: Data blob is parsed first (base layer),
// then struct fields are overlaid.
func simulateMergedPayload(t *testing.T, trigger *TriggerPayload) map[string]any {
	t.Helper()
	result := make(map[string]any)

	// Step 1: Parse Data blob (base layer) - this is where custom fields come from
	if len(trigger.Data) > 0 {
		if err := json.Unmarshal(trigger.Data, &result); err != nil {
			// If Data is not JSON, try marshaling the semspec fields directly
			result["_data"] = string(trigger.Data)
		}
	}

	// For flattened TriggerPayload, add the semspec fields directly
	if trigger.Slug != "" {
		result["slug"] = trigger.Slug
	}
	if trigger.Title != "" {
		result["title"] = trigger.Title
	}
	if trigger.Description != "" {
		result["description"] = trigger.Description
	}
	if trigger.ProjectID != "" {
		result["project_id"] = trigger.ProjectID
	}
	if trigger.TraceID != "" {
		result["trace_id"] = trigger.TraceID
	}
	if trigger.Auto {
		result["auto"] = trigger.Auto
	}
	if len(trigger.ScopePatterns) > 0 {
		result["scope_patterns"] = trigger.ScopePatterns
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

func mapKeys2(m map[string]bool) []string {
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
	if got.Slug != want.Slug {
		t.Errorf("Slug = %q, want %q", got.Slug, want.Slug)
	}
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
	if got.Description != want.Description {
		t.Errorf("Description = %q, want %q", got.Description, want.Description)
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
