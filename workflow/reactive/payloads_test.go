package reactive

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Callback tests
// ---------------------------------------------------------------------------

func TestCallback_InjectCallback(t *testing.T) {
	var cb Callback
	fields := reactiveEngine.CallbackFields{
		TaskID:          "task-123",
		CallbackSubject: "workflow.callback.plan-review.exec-001",
		ExecutionID:     "exec-001",
	}

	cb.InjectCallback(fields)

	assert.Equal(t, "task-123", cb.TaskID)
	assert.Equal(t, "workflow.callback.plan-review.exec-001", cb.CallbackSubject)
	assert.Equal(t, "exec-001", cb.ExecutionID)
	assert.True(t, cb.HasCallback())
}

func TestCallback_HasCallback(t *testing.T) {
	tests := []struct {
		name     string
		cb       Callback
		expected bool
	}{
		{"empty", Callback{}, false},
		{"only task_id", Callback{TaskID: "t1"}, false},
		{"only callback_subject", Callback{CallbackSubject: "s1"}, false},
		{"both set", Callback{TaskID: "t1", CallbackSubject: "s1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cb.HasCallback())
		})
	}
}

func TestCallback_SetCallback(t *testing.T) {
	var cb Callback
	cb.SetCallback("task-456", "workflow.callback.test.exec-002")

	assert.Equal(t, "task-456", cb.TaskID)
	assert.Equal(t, "workflow.callback.test.exec-002", cb.CallbackSubject)
	assert.True(t, cb.HasCallback())
}

func TestCallback_JSONWireFormat(t *testing.T) {
	cb := Callback{
		TaskID:          "task-789",
		CallbackSubject: "workflow.callback.plan-review.exec-003",
		ExecutionID:     "exec-003",
	}

	data, err := json.Marshal(cb)
	require.NoError(t, err)

	// Verify wire format matches reactive.CallbackFields and workflow.CallbackFields
	var wireFormat map[string]string
	require.NoError(t, json.Unmarshal(data, &wireFormat))

	assert.Equal(t, "task-789", wireFormat["task_id"])
	assert.Equal(t, "workflow.callback.plan-review.exec-003", wireFormat["callback_subject"])
	assert.Equal(t, "exec-003", wireFormat["execution_id"])
}

// ---------------------------------------------------------------------------
// CallbackInjectable interface compliance
// ---------------------------------------------------------------------------

func TestPlannerRequest_ImplementsCallbackInjectable(t *testing.T) {
	req := &PlannerRequest{Slug: "test-plan"}

	// Verify it implements CallbackInjectable
	var injectable reactiveEngine.CallbackInjectable = req
	injectable.InjectCallback(reactiveEngine.CallbackFields{
		TaskID:          "task-001",
		CallbackSubject: "workflow.callback.plan-review.exec-001",
		ExecutionID:     "exec-001",
	})

	assert.True(t, req.HasCallback())
	assert.Equal(t, "task-001", req.TaskID)
}

func TestAllRequestTypes_ImplementCallbackInjectable(t *testing.T) {
	// Compile-time check: all request types implement CallbackInjectable
	types := []reactiveEngine.CallbackInjectable{
		&PlannerRequest{},
		&PlanReviewRequest{},
		&PhaseGeneratorRequest{},
		&PhaseReviewRequest{},
		&TaskGeneratorRequest{},
		&TaskReviewRequest{},
		&DeveloperRequest{},
		&ValidationRequest{},
		&TaskCodeReviewRequest{},
	}

	for _, typ := range types {
		typ.InjectCallback(reactiveEngine.CallbackFields{
			TaskID:          "test-task",
			CallbackSubject: "test.callback",
			ExecutionID:     "test-exec",
		})
	}
	assert.Len(t, types, 9)
}

// ---------------------------------------------------------------------------
// message.Payload interface compliance
// ---------------------------------------------------------------------------

func TestAllRequestTypes_ImplementPayload(t *testing.T) {
	// Compile-time check: all request types implement message.Payload
	types := []message.Payload{
		&PlannerRequest{Slug: "test"},
		&PlanReviewRequest{Slug: "test", RequestID: "r1"},
		&PhaseGeneratorRequest{Slug: "test"},
		&PhaseReviewRequest{Slug: "test", RequestID: "r1"},
		&TaskGeneratorRequest{Slug: "test"},
		&TaskReviewRequest{Slug: "test", RequestID: "r1"},
		&DeveloperRequest{Slug: "test"},
		&ValidationRequest{Slug: "test"},
		&TaskCodeReviewRequest{Slug: "test"},
	}

	for _, typ := range types {
		schema := typ.Schema()
		assert.NotEmpty(t, schema.Domain)
		assert.NotEmpty(t, schema.Category)
		assert.NotEmpty(t, schema.Version)
		assert.NoError(t, typ.Validate())
	}
}

func TestAllResultTypes_ImplementPayload(t *testing.T) {
	// Compile-time check: all result types implement message.Payload
	types := []message.Payload{
		&PlannerResult{},
		&ReviewResult{},
		&PhaseGeneratorResult{},
		&TaskGeneratorResult{},
		&TaskReviewResult{},
		&ValidationResult{},
		&DeveloperResult{},
		&TaskCodeReviewResult{},
	}

	for _, typ := range types {
		schema := typ.Schema()
		assert.NotEmpty(t, schema.Domain)
		assert.NotEmpty(t, schema.Category)
		assert.NotEmpty(t, schema.Version)
		assert.NoError(t, typ.Validate())
	}
}

// ---------------------------------------------------------------------------
// Payload validation
// ---------------------------------------------------------------------------

func TestPlannerRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     PlannerRequest
		wantErr bool
	}{
		{"valid", PlannerRequest{Slug: "test-plan"}, false},
		{"missing slug", PlannerRequest{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPlanReviewRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     PlanReviewRequest
		wantErr bool
	}{
		{"valid", PlanReviewRequest{Slug: "test", RequestID: "r1"}, false},
		{"missing slug", PlanReviewRequest{RequestID: "r1"}, true},
		{"missing request_id", PlanReviewRequest{Slug: "test"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// JSON round-trip
// ---------------------------------------------------------------------------

func TestPlannerRequest_JSONRoundTrip(t *testing.T) {
	req := &PlannerRequest{
		Callback: Callback{
			TaskID:          "task-001",
			CallbackSubject: "workflow.callback.plan-review.exec-001",
			ExecutionID:     "exec-001",
		},
		RequestID:     "req-123",
		Slug:          "add-auth",
		Title:         "Add authentication",
		Description:   "Implement JWT-based auth",
		ProjectID:     "proj-001",
		TraceID:       "trace-abc",
		ScopePatterns: []string{"src/auth/**"},
		Revision:      true,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded PlannerRequest
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, req.TaskID, decoded.TaskID)
	assert.Equal(t, req.CallbackSubject, decoded.CallbackSubject)
	assert.Equal(t, req.ExecutionID, decoded.ExecutionID)
	assert.Equal(t, req.Slug, decoded.Slug)
	assert.Equal(t, req.Title, decoded.Title)
	assert.Equal(t, req.Description, decoded.Description)
	assert.Equal(t, req.ProjectID, decoded.ProjectID)
	assert.Equal(t, req.TraceID, decoded.TraceID)
	assert.Equal(t, req.ScopePatterns, decoded.ScopePatterns)
	assert.True(t, decoded.Revision)
}

func TestReviewResult_JSONRoundTrip(t *testing.T) {
	result := &ReviewResult{
		RequestID:         "req-456",
		Slug:              "add-auth",
		Verdict:           "approved",
		Summary:           "Plan looks good",
		Findings:          json.RawMessage(`[{"severity":"info"}]`),
		FormattedFindings: "### Findings\n- Good plan",
		Status:            "completed",
		LLMRequestIDs:     []string{"llm-1", "llm-2"},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded ReviewResult
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, result.Verdict, decoded.Verdict)
	assert.Equal(t, result.Summary, decoded.Summary)
	assert.Equal(t, result.FormattedFindings, decoded.FormattedFindings)
	assert.True(t, decoded.IsApproved())
	assert.Equal(t, result.LLMRequestIDs, decoded.LLMRequestIDs)
}

// ---------------------------------------------------------------------------
// ParseReactivePayload
// ---------------------------------------------------------------------------

func TestParseReactivePayload(t *testing.T) {
	// Simulate what the reactive engine publishes: BaseMessage wrapping a payload
	req := &PlannerRequest{
		Callback: Callback{
			TaskID:          "task-001",
			CallbackSubject: "workflow.callback.plan-review.exec-001",
		},
		Slug:  "test-plan",
		Title: "Test Plan",
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "reactive-workflow")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	// Parse using the reactive parser
	parsed, err := ParseReactivePayload[PlannerRequest](data)
	require.NoError(t, err)

	assert.Equal(t, "test-plan", parsed.Slug)
	assert.Equal(t, "Test Plan", parsed.Title)
	assert.Equal(t, "task-001", parsed.TaskID)
	assert.Equal(t, "workflow.callback.plan-review.exec-001", parsed.CallbackSubject)
}

func TestParseReactivePayload_EmptyPayload(t *testing.T) {
	data := []byte(`{"type":{"domain":"test","category":"test","version":"v1"},"payload":{}}`)

	parsed, err := ParseReactivePayload[PlannerRequest](data)
	require.NoError(t, err)
	assert.Empty(t, parsed.Slug) // Parsed but fields empty
}

func TestParseReactivePayload_InvalidJSON(t *testing.T) {
	_, err := ParseReactivePayload[PlannerRequest]([]byte("not json"))
	assert.Error(t, err)
}
