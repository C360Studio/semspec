# 08: Trajectory Comparison

## Overview

The trajectory comparison feature allows users to see how different models complete the same task. This is useful for:

*   Evaluating the performance of different models.
*   Debugging and auditing the behavior of the `semspec` system.
*   Proving that `semspec` is doing what it says it's doing.

## Data Model Changes

To enable trajectory comparison, we will introduce a `ComparisonID` to associate multiple runs of the same task.

### `BatchTriggerPayload`

The `workflow.BatchTriggerPayload` struct will be extended to include a `ComparisonID`.

```go
// BatchTriggerPayload triggers task-dispatcher to execute all tasks for a plan.
type BatchTriggerPayload struct {
	// RequestID uniquely identifies this request
	RequestID string `json:"request_id"`

	// Slug is the plan slug
	Slug string `json:"slug"`

	// BatchID uniquely identifies this execution batch
	BatchID string `json:"batch_id"`

	// WorkflowID is the parent workflow ID if applicable
	WorkflowID string `json:"workflow_id,omitempty"`

	// ComparisonID groups multiple runs of the same task for comparison.
	ComparisonID string `json:"comparison_id,omitempty"`
}
```

### `TaskExecutionPayload`

The `workflow.TaskExecutionPayload` struct will also be extended to include the `ComparisonID`.

```go
// TaskExecutionPayload carries all information needed to execute a task.
type TaskExecutionPayload struct {
    // ... existing fields ...

	// ComparisonID groups multiple runs of the same task for comparison.
	ComparisonID string `json:"comparison_id,omitempty"`
}
```

### `LLMCallRecord`

Finally, the `llm.LLMCallRecord` struct will be updated to store the `ComparisonID`.

```go
// LLMCallRecord represents a single LLM API call with full context for trajectory tracking.
type LLMCallRecord struct {
    // ... existing fields ...

	// ComparisonID groups multiple runs of the same task for comparison.
	ComparisonID string `json:"comparison_id,omitempty"`
}
```

## API Changes

The `trajectory-api` component will be extended with a new endpoint to retrieve all trajectories for a given `ComparisonID`.

### `GET /comparison/{comparison_id}`

This endpoint will return an array of `Trajectory` objects, where each object corresponds to a different run of the same task.

## UI/UX

A "Compare Runs" button will be added to the UI for tasks that have been run multiple times with different models. Clicking this button will open a side-by-side comparison view.

The view will have a column for each model run, and rows for each step in the trajectory. This will allow users to easily compare the prompts, responses, and other data between the runs.

## Workflow

1.  A user decides to run a task with multiple models for comparison.
2.  The UI generates a `ComparisonID`.
3.  For each model to be compared, the UI:
    a. Modifies the `model.Registry` configuration to set the desired model for the relevant capability.
    b. Triggers the task execution, passing the `ComparisonID` in the `BatchTriggerPayload`.
4.  The `semspec` system executes the task and stores the trajectory with the `ComparisonID`.
5.  When the runs are complete, the user clicks the "Compare Runs" button.
6.  The UI calls the `GET /comparison/{comparison_id}` endpoint to retrieve the trajectories.
7.  The UI displays the trajectories in a side-by-side view for comparison.
