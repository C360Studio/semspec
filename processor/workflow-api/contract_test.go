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
