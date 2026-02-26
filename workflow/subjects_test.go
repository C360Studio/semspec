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
