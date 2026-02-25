package workflowapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract tests validate that Go API response types serialize to JSON in a way
// that matches the OpenAPI specification consumed by the TypeScript frontend.
//
// The motivating bug: PlanWithStatus.ActiveLoops was tagged json:"active_loops,omitempty".
// When the slice was nil, Go omitted the field entirely from JSON, but TypeScript
// expected active_loops: ActiveLoop[] (required). This crashed the frontend.
//
// These tests must pass before any change to a struct's JSON tags or field layout.

// TestPlanWithStatusContract_NilActiveLoops is the exact regression test for the
// omitempty bug. active_loops MUST be present in JSON output even when the slice
// is nil — the field is required by the OpenAPI spec.
func TestPlanWithStatusContract_NilActiveLoops(t *testing.T) {
	p := &PlanWithStatus{
		Plan:  &workflow.Plan{ID: "test", Slug: "test", Title: "test", ProjectID: "default"},
		Stage: "drafting",
		// ActiveLoops is intentionally nil — this is the regression scenario
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// active_loops MUST be present — TypeScript marks this field required.
	// omitempty would cause a nil slice to be absent, crashing the frontend.
	_, exists := raw["active_loops"]
	assert.True(t, exists, "active_loops must always be present in JSON (must not use omitempty)")

	// The value may be null (nil slice marshals as null) or an empty array — either
	// is acceptable to a TypeScript consumer that handles both. The critical invariant
	// is that the key is not absent.
}

// TestPlanWithStatusContract_RequiredFields verifies that all fields marked as
// required in the OpenAPI specification are present in the JSON output.
func TestPlanWithStatusContract_RequiredFields(t *testing.T) {
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID:        "test-id",
			Slug:      "test-slug",
			Title:     "Test Plan",
			ProjectID: "default",
			Approved:  false,
			CreatedAt: time.Now(),
		},
		Stage: "drafting",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// These fields are marked required in the OpenAPI spec. Any field absent here
	// will cause TypeScript runtime errors when accessing the property.
	requiredFields := []string{
		"id", "slug", "title", "project_id",
		"approved", "created_at",
		"stage", "active_loops",
	}
	for _, field := range requiredFields {
		_, exists := raw[field]
		assert.True(t, exists, "required field %q must be present in JSON output", field)
	}
}

// TestActiveLoopStatusContract_FieldNames verifies that ActiveLoopStatus serializes
// with the correct snake_case field names expected by the TypeScript frontend.
func TestActiveLoopStatusContract_FieldNames(t *testing.T) {
	als := ActiveLoopStatus{
		LoopID: "loop-1",
		Role:   "planner",
		State:  "executing",
	}
	data, err := json.Marshal(als)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	expectedFields := []string{"loop_id", "role", "state"}
	for _, field := range expectedFields {
		_, exists := raw[field]
		assert.True(t, exists, "field %q must be present in ActiveLoopStatus JSON", field)
	}

	// Guard against accidental extra fields — the spec defines exactly these three.
	assert.Equal(t, len(expectedFields), len(raw),
		"ActiveLoopStatus must have exactly %d fields, got %d: %v",
		len(expectedFields), len(raw), raw)
}

// TestTaskContract_RequiredFields verifies that workflow.Task serializes all fields
// that the OpenAPI spec marks as required, including acceptance_criteria which must
// always be present (even as an empty array, never absent).
func TestTaskContract_RequiredFields(t *testing.T) {
	task := workflow.Task{
		ID:          "task.test.1",
		PlanID:      "plan-1",
		Sequence:    1,
		Description: "Test task",
		Status:      workflow.TaskStatusPending,
		CreatedAt:   time.Now(),
		AcceptanceCriteria: []workflow.AcceptanceCriterion{
			{Given: "given", When: "when", Then: "then"},
		},
	}
	data, err := json.Marshal(task)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	requiredFields := []string{
		"id", "plan_id", "sequence", "description",
		"status", "created_at", "acceptance_criteria",
	}
	for _, field := range requiredFields {
		_, exists := raw[field]
		assert.True(t, exists, "required field %q must be present in Task JSON", field)
	}
}

// TestTaskContract_AcceptanceCriteriaNeverOmitted verifies that acceptance_criteria
// is always emitted, even when empty. A nil slice with omitempty would break
// TypeScript consumers that iterate the field unconditionally.
func TestTaskContract_AcceptanceCriteriaNeverOmitted(t *testing.T) {
	task := workflow.Task{
		ID:          "task.test.2",
		PlanID:      "plan-1",
		Sequence:    2,
		Description: "Task with no criteria",
		Status:      workflow.TaskStatusPending,
		CreatedAt:   time.Now(),
		// AcceptanceCriteria is intentionally nil
	}
	data, err := json.Marshal(task)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	_, exists := raw["acceptance_criteria"]
	assert.True(t, exists,
		"acceptance_criteria must be present in JSON even when nil (must not use omitempty)")
}

// TestCreatePlanResponseContract_Fields verifies that CreatePlanResponse serializes
// all fields that the TypeScript client destructures from the HTTP 201 response.
func TestCreatePlanResponseContract_Fields(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		resp := CreatePlanResponse{
			Slug:      "test-slug",
			RequestID: "req-1",
			TraceID:   "trace-1",
			Message:   "created",
		}
		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		requiredFields := []string{"slug", "request_id", "trace_id", "message"}
		for _, field := range requiredFields {
			_, exists := raw[field]
			assert.True(t, exists, "field %q must be present in CreatePlanResponse JSON", field)
		}
	})

	t.Run("empty strings are not omitted", func(t *testing.T) {
		// Even zero-value strings must appear — the spec marks all four as required.
		resp := CreatePlanResponse{}
		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		for _, field := range []string{"slug", "request_id", "trace_id", "message"} {
			_, exists := raw[field]
			assert.True(t, exists, "field %q must be present even when empty string", field)
		}
	})
}

// TestAsyncOperationResponseContract_Fields verifies that AsyncOperationResponse
// serializes all fields expected by the TypeScript client for async operations
// like task generation (HTTP 202 responses).
func TestAsyncOperationResponseContract_Fields(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		resp := AsyncOperationResponse{
			Slug:      "test-slug",
			RequestID: "req-1",
			TraceID:   "trace-1",
			Message:   "started",
		}
		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		requiredFields := []string{"slug", "request_id", "trace_id", "message"}
		for _, field := range requiredFields {
			_, exists := raw[field]
			assert.True(t, exists, "field %q must be present in AsyncOperationResponse JSON", field)
		}
	})

	t.Run("empty strings are not omitted", func(t *testing.T) {
		resp := AsyncOperationResponse{}
		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		for _, field := range []string{"slug", "request_id", "trace_id", "message"} {
			_, exists := raw[field]
			assert.True(t, exists, "field %q must be present even when empty string", field)
		}
	})
}

// TestPlanWithStatusContract_EmbeddedFieldsFlattened verifies that embedding
// *workflow.Plan produces a flat JSON object rather than a nested "Plan" key.
// TypeScript expects all plan fields at the top level of PlanWithStatus responses.
func TestPlanWithStatusContract_EmbeddedFieldsFlattened(t *testing.T) {
	now := time.Now()
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID:        "test",
			Slug:      "test-slug",
			Title:     "Test",
			ProjectID: "default",
			Approved:  true,
			CreatedAt: now,
			Goal:      "test goal",
			Context:   "test context",
		},
		Stage: "approved",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// The embedded *workflow.Plan must be flattened — TypeScript accesses plan.id,
	// not plan.Plan.id. A non-nil embedded pointer with no json tag is flattened
	// by encoding/json automatically; this test guards against that changing.
	_, hasPlanKey := raw["Plan"]
	assert.False(t, hasPlanKey,
		"embedded Plan must be flattened into the top-level object, not nested under a 'Plan' key")

	// Verify each Plan field appears at the top level.
	planFields := []string{"id", "slug", "title", "project_id", "approved", "created_at", "goal", "context"}
	for _, field := range planFields {
		_, exists := raw[field]
		assert.True(t, exists, "Plan field %q must appear at the top level of PlanWithStatus JSON", field)
	}
}

// TestPlanWithStatusContract_ActiveLoopsPopulated verifies that a populated
// ActiveLoops slice serializes correctly — field present, array non-empty.
func TestPlanWithStatusContract_ActiveLoopsPopulated(t *testing.T) {
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID: "test", Slug: "test", Title: "test", ProjectID: "default",
		},
		Stage: "drafting",
		ActiveLoops: []ActiveLoopStatus{
			{LoopID: "loop-abc", Role: "planner", State: "executing"},
		},
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	loops, exists := raw["active_loops"]
	assert.True(t, exists, "active_loops must be present")

	loopSlice, ok := loops.([]any)
	require.True(t, ok, "active_loops must be a JSON array, got %T", loops)
	require.Len(t, loopSlice, 1, "active_loops must contain 1 element")

	loopObj, ok := loopSlice[0].(map[string]any)
	require.True(t, ok, "active_loops[0] must be a JSON object")
	assert.Equal(t, "loop-abc", loopObj["loop_id"])
	assert.Equal(t, "planner", loopObj["role"])
	assert.Equal(t, "executing", loopObj["state"])
}

// TestPlanWithStatusContract_ReviewFieldsPresent verifies that review metadata
// (findings, formatted_findings, iteration) serializes into the JSON output
// when populated, so the frontend can display escalation context.
func TestPlanWithStatusContract_ReviewFieldsPresent(t *testing.T) {
	findings := json.RawMessage(`[{"severity":"error","sop_id":"test-sop"}]`)
	now := time.Now()
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID:                      "test",
			Slug:                    "test",
			Title:                   "test",
			ProjectID:               "default",
			CreatedAt:               now,
			ReviewVerdict:           "escalated",
			ReviewSummary:           "Max revisions exceeded after 3 attempts",
			ReviewedAt:              &now,
			ReviewFindings:          findings,
			ReviewFormattedFindings: "- [ERROR] test-sop: violation found",
			ReviewIteration:         3,
		},
		Stage: "rejected",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// Verify review fields are present when populated
	_, hasFindings := raw["review_findings"]
	assert.True(t, hasFindings, "review_findings must be present when populated")

	_, hasFormatted := raw["review_formatted_findings"]
	assert.True(t, hasFormatted, "review_formatted_findings must be present when populated")

	iteration, hasIteration := raw["review_iteration"]
	assert.True(t, hasIteration, "review_iteration must be present when populated")
	assert.Equal(t, float64(3), iteration)

	// Verify review_findings is a parseable JSON array
	findingsRaw, ok := raw["review_findings"].([]any)
	require.True(t, ok, "review_findings must be a JSON array, got %T", raw["review_findings"])
	require.Len(t, findingsRaw, 1)
}

// TestPlanWithStatusContract_ReviewFieldsOmittedWhenEmpty verifies that review
// metadata fields are absent from JSON when not populated (omitempty behavior).
// These are optional fields — the frontend handles their absence.
func TestPlanWithStatusContract_ReviewFieldsOmittedWhenEmpty(t *testing.T) {
	p := &PlanWithStatus{
		Plan:  &workflow.Plan{ID: "test", Slug: "test", Title: "test", ProjectID: "default"},
		Stage: "drafting",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// These fields use omitempty — they must not appear when zero-valued
	_, hasFindings := raw["review_findings"]
	assert.False(t, hasFindings, "review_findings must not be present when nil")

	_, hasFormatted := raw["review_formatted_findings"]
	assert.False(t, hasFormatted, "review_formatted_findings must not be present when empty")

	_, hasIteration := raw["review_iteration"]
	assert.False(t, hasIteration, "review_iteration must not be present when zero")
}

// TestTaskContract_EscalationFieldsPresent verifies that task escalation metadata
// (reason, feedback, iteration, timestamp) serializes correctly when populated.
func TestTaskContract_EscalationFieldsPresent(t *testing.T) {
	now := time.Now()
	task := workflow.Task{
		ID:                  "task.test.1",
		PlanID:              "plan-1",
		Sequence:            1,
		Description:         "Escalated task",
		Status:              workflow.TaskStatusFailed,
		CreatedAt:           now,
		CompletedAt:         &now,
		AcceptanceCriteria:  []workflow.AcceptanceCriterion{},
		EscalationReason:    "Max retries exceeded after 3 attempts",
		EscalationFeedback:  "Code does not compile after fix attempt",
		EscalationIteration: 3,
		EscalatedAt:         &now,
		LastError:           "Developer agent failed: LLM timeout",
		LastErrorAt:         &now,
	}
	data, err := json.Marshal(task)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "Max retries exceeded after 3 attempts", raw["escalation_reason"])
	assert.Equal(t, "Code does not compile after fix attempt", raw["escalation_feedback"])
	assert.Equal(t, float64(3), raw["escalation_iteration"])
	_, hasEscalatedAt := raw["escalated_at"]
	assert.True(t, hasEscalatedAt, "escalated_at must be present when set")
	assert.Equal(t, "Developer agent failed: LLM timeout", raw["last_error"])
	_, hasLastErrorAt := raw["last_error_at"]
	assert.True(t, hasLastErrorAt, "last_error_at must be present when set")
}

// TestTaskContract_EscalationFieldsOmittedWhenEmpty verifies that escalation
// and error fields are absent when not populated (omitempty behavior).
func TestTaskContract_EscalationFieldsOmittedWhenEmpty(t *testing.T) {
	task := workflow.Task{
		ID:                 "task.test.2",
		PlanID:             "plan-1",
		Sequence:           1,
		Description:        "Normal task",
		Status:             workflow.TaskStatusPending,
		CreatedAt:          time.Now(),
		AcceptanceCriteria: []workflow.AcceptanceCriterion{},
	}
	data, err := json.Marshal(task)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	omittedFields := []string{
		"escalation_reason", "escalation_feedback", "escalation_iteration",
		"escalated_at", "last_error", "last_error_at",
	}
	for _, field := range omittedFields {
		_, exists := raw[field]
		assert.False(t, exists, "field %q must not be present when empty (omitempty)", field)
	}
}

// TestPlanContract_TaskReviewFieldsPresent verifies that task review metadata
// (verdict, summary, findings, formatted_findings, iteration) serializes into
// the JSON output when populated, so the frontend can display task review state
// separately from plan review state.
func TestPlanContract_TaskReviewFieldsPresent(t *testing.T) {
	findings := json.RawMessage(`[{"severity":"error","sop_id":"api-testing-sop"}]`)
	now := time.Now()
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID:                          "test",
			Slug:                        "test",
			Title:                       "test",
			ProjectID:                   "default",
			CreatedAt:                   now,
			TaskReviewVerdict:           "needs_changes",
			TaskReviewSummary:           "Tasks lack sufficient error coverage",
			TaskReviewedAt:              &now,
			TaskReviewFindings:          findings,
			TaskReviewFormattedFindings: "- [ERROR] api-testing-sop: missing error case coverage",
			TaskReviewIteration:         2,
		},
		Stage: "implementing",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// Verify task review fields are present when populated
	assert.Equal(t, "needs_changes", raw["task_review_verdict"])
	assert.Equal(t, "Tasks lack sufficient error coverage", raw["task_review_summary"])

	_, hasReviewedAt := raw["task_reviewed_at"]
	assert.True(t, hasReviewedAt, "task_reviewed_at must be present when set")

	_, hasFindings := raw["task_review_findings"]
	assert.True(t, hasFindings, "task_review_findings must be present when populated")

	_, hasFormatted := raw["task_review_formatted_findings"]
	assert.True(t, hasFormatted, "task_review_formatted_findings must be present when populated")

	iteration, hasIteration := raw["task_review_iteration"]
	assert.True(t, hasIteration, "task_review_iteration must be present when populated")
	assert.Equal(t, float64(2), iteration)

	// Verify task_review_findings is a parseable JSON array
	findingsRaw, ok := raw["task_review_findings"].([]any)
	require.True(t, ok, "task_review_findings must be a JSON array, got %T", raw["task_review_findings"])
	require.Len(t, findingsRaw, 1)
}

// TestPlanContract_TaskReviewFieldsOmittedWhenEmpty verifies that task review
// metadata fields are absent from JSON when not populated (omitempty behavior).
func TestPlanContract_TaskReviewFieldsOmittedWhenEmpty(t *testing.T) {
	p := &PlanWithStatus{
		Plan:  &workflow.Plan{ID: "test", Slug: "test", Title: "test", ProjectID: "default"},
		Stage: "drafting",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// These fields use omitempty — they must not appear when zero-valued
	omittedFields := []string{
		"task_review_verdict", "task_review_summary", "task_reviewed_at",
		"task_review_findings", "task_review_formatted_findings", "task_review_iteration",
	}
	for _, field := range omittedFields {
		_, exists := raw[field]
		assert.False(t, exists, "field %q must not be present when empty (omitempty)", field)
	}
}

// TestPlanContract_TaskReviewAndPlanReviewCoexist verifies that task review
// fields and plan review fields can both be populated simultaneously without
// interference — this is the key invariant for distinguishing the two review phases.
func TestPlanContract_TaskReviewAndPlanReviewCoexist(t *testing.T) {
	now := time.Now()
	planFindings := json.RawMessage(`[{"severity":"info","sop_id":"plan-sop"}]`)
	taskFindings := json.RawMessage(`[{"severity":"error","sop_id":"task-sop"}]`)
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID:                          "test",
			Slug:                        "test",
			Title:                       "test",
			ProjectID:                   "default",
			CreatedAt:                   now,
			ReviewVerdict:               "approved",
			ReviewSummary:               "Plan looks good",
			ReviewedAt:                  &now,
			ReviewFindings:              planFindings,
			ReviewFormattedFindings:     "- [INFO] plan-sop: compliant",
			ReviewIteration:             1,
			TaskReviewVerdict:           "escalated",
			TaskReviewSummary:           "Max revisions exceeded",
			TaskReviewedAt:              &now,
			TaskReviewFindings:          taskFindings,
			TaskReviewFormattedFindings: "- [ERROR] task-sop: violation",
			TaskReviewIteration:         3,
		},
		Stage: "rejected",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// Plan review fields should be "approved" (from plan-review-loop)
	assert.Equal(t, "approved", raw["review_verdict"])
	assert.Equal(t, "Plan looks good", raw["review_summary"])
	assert.Equal(t, float64(1), raw["review_iteration"])

	// Task review fields should be "escalated" (from task-review-loop)
	assert.Equal(t, "escalated", raw["task_review_verdict"])
	assert.Equal(t, "Max revisions exceeded", raw["task_review_summary"])
	assert.Equal(t, float64(3), raw["task_review_iteration"])
}

// TestPlanContract_ErrorFieldsPresent verifies that plan error annotation fields
// serialize correctly when populated.
func TestPlanContract_ErrorFieldsPresent(t *testing.T) {
	now := time.Now()
	p := &PlanWithStatus{
		Plan: &workflow.Plan{
			ID:          "test",
			Slug:        "test",
			Title:       "test",
			ProjectID:   "default",
			CreatedAt:   now,
			LastError:   "Developer agent failed: connection refused",
			LastErrorAt: &now,
		},
		Stage: "implementing",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "Developer agent failed: connection refused", raw["last_error"])
	_, hasLastErrorAt := raw["last_error_at"]
	assert.True(t, hasLastErrorAt, "last_error_at must be present when set")
}

// TestPlanContract_ErrorFieldsOmittedWhenEmpty verifies that error annotation
// fields are absent when not populated (omitempty behavior).
func TestPlanContract_ErrorFieldsOmittedWhenEmpty(t *testing.T) {
	p := &PlanWithStatus{
		Plan:  &workflow.Plan{ID: "test", Slug: "test", Title: "test", ProjectID: "default"},
		Stage: "drafting",
	}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	_, hasError := raw["last_error"]
	assert.False(t, hasError, "last_error must not be present when empty")

	_, hasErrorAt := raw["last_error_at"]
	assert.False(t, hasErrorAt, "last_error_at must not be present when nil")
}
