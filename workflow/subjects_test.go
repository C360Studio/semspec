package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanApprovedEvent_RoundTrip(t *testing.T) {
	event := PlanApprovedEvent{
		Slug:    "auth-refresh",
		Verdict: "approved",
		Summary: "All checks pass",
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded PlanApprovedEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event, decoded)
}

func TestPlanRevisionNeededEvent_RoundTrip(t *testing.T) {
	findings := json.RawMessage(`[{"issue":"missing tests","severity":"high"}]`)
	event := PlanRevisionNeededEvent{
		Slug:      "auth-refresh",
		Iteration: 2,
		Verdict:   "needs_revision",
		Findings:  findings,
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded PlanRevisionNeededEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event.Slug, decoded.Slug)
	assert.Equal(t, event.Iteration, decoded.Iteration)
	assert.Equal(t, event.Verdict, decoded.Verdict)
	assert.JSONEq(t, string(event.Findings), string(decoded.Findings))
}

func TestTasksApprovedEvent_RoundTrip(t *testing.T) {
	event := TasksApprovedEvent{
		Slug:      "auth-refresh",
		Verdict:   "approved",
		Summary:   "8 tasks generated",
		TaskCount: 8,
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded TasksApprovedEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event, decoded)
}

func TestTaskExecutionCompleteEvent_RoundTrip(t *testing.T) {
	event := TaskExecutionCompleteEvent{
		TaskID:     "task-001",
		Iterations: 2,
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded TaskExecutionCompleteEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event, decoded)
}

// TestParseNATSMessage_GenericJSONPayload verifies that ParseNATSMessage correctly
// unwraps GenericJSONPayload (core.json.v1) envelopes used by workflow publish actions.
func TestParseNATSMessage_PlanApprovedEvent(t *testing.T) {
	// Simulate the wire format produced by the workflow publish action:
	// BaseMessage { type: core.json.v1, payload: { data: { ...event fields... } } }
	wire := `{
		"id": "msg-001",
		"type": {"domain": "core", "category": "json", "version": "v1"},
		"payload": {
			"data": {
				"slug": "auth-refresh",
				"verdict": "approved",
				"summary": "All checks pass"
			}
		},
		"meta": {"created_at": 1708700000, "source": "workflow"}
	}`

	event, err := ParseNATSMessage[PlanApprovedEvent]([]byte(wire))
	require.NoError(t, err)

	assert.Equal(t, "auth-refresh", event.Slug)
	assert.Equal(t, "approved", event.Verdict)
	assert.Equal(t, "All checks pass", event.Summary)
}

func TestParseNATSMessage_PlanRevisionNeededEvent(t *testing.T) {
	wire := `{
		"id": "msg-002",
		"type": {"domain": "core", "category": "json", "version": "v1"},
		"payload": {
			"data": {
				"slug": "auth-refresh",
				"iteration": 1,
				"verdict": "needs_revision",
				"findings": [{"issue": "missing scope"}]
			}
		},
		"meta": {"created_at": 1708700000, "source": "workflow"}
	}`

	event, err := ParseNATSMessage[PlanRevisionNeededEvent]([]byte(wire))
	require.NoError(t, err)

	assert.Equal(t, "auth-refresh", event.Slug)
	assert.Equal(t, 1, event.Iteration)
	assert.Equal(t, "needs_revision", event.Verdict)
	assert.JSONEq(t, `[{"issue": "missing scope"}]`, string(event.Findings))
}

func TestParseNATSMessage_TasksApprovedEvent(t *testing.T) {
	wire := `{
		"id": "msg-003",
		"type": {"domain": "core", "category": "json", "version": "v1"},
		"payload": {
			"data": {
				"slug": "todo-app",
				"verdict": "approved",
				"summary": "All tasks valid",
				"task_count": 5
			}
		},
		"meta": {"created_at": 1708700000, "source": "workflow"}
	}`

	event, err := ParseNATSMessage[TasksApprovedEvent]([]byte(wire))
	require.NoError(t, err)

	assert.Equal(t, "todo-app", event.Slug)
	assert.Equal(t, "approved", event.Verdict)
	assert.Equal(t, 5, event.TaskCount)
}

func TestTypedSubjectPatterns(t *testing.T) {
	// Verify subject patterns are correctly set
	assert.Equal(t, "workflow.events.plan.approved", PlanApproved.Pattern)
	assert.Equal(t, "workflow.events.plan.revision_needed", PlanRevisionNeeded.Pattern)
	assert.Equal(t, "workflow.events.plan.review_complete", PlanReviewLoopComplete.Pattern)
	assert.Equal(t, "workflow.events.tasks.approved", TasksApproved.Pattern)
	assert.Equal(t, "workflow.events.tasks.revision_needed", TasksRevisionNeeded.Pattern)
	assert.Equal(t, "workflow.events.tasks.review_complete", TaskReviewLoopComplete.Pattern)
	assert.Equal(t, "workflow.events.task.validation_passed", StructuralValidationPassed.Pattern)
	assert.Equal(t, "workflow.events.task.rejection_categorized", RejectionCategorized.Pattern)
	assert.Equal(t, "workflow.events.task.execution_complete", TaskExecutionComplete.Pattern)
}
