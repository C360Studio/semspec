package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/natsclient"
)

// CallbackFields provides workflow-processor callback support for any
// trigger or result payload. When a workflow-processor dispatches work
// via publish_async, it injects these fields so the receiving component
// can publish an AsyncStepResult back to the callback subject.
//
// Embed this in any payload type that may be dispatched by the
// workflow-processor:
//
//	type MyTrigger struct {
//	    workflow.CallbackFields
//	    // ... component-specific fields
//	}
//
// Components check HasCallback() to determine whether to publish
// an AsyncStepResult or use legacy result publishing.
type CallbackFields struct {
	// CallbackSubject is where to publish AsyncStepResult when done.
	// Set by the workflow-processor's publish_async action.
	CallbackSubject string `json:"callback_subject,omitempty"`

	// TaskID correlates this request with the pending workflow step.
	// Used to match the AsyncStepResult with the parked execution.
	TaskID string `json:"task_id,omitempty"`

	// ExecutionID identifies the workflow execution this belongs to.
	// Optional — used for direct correlation when TaskID lookup fails.
	ExecutionID string `json:"execution_id,omitempty"`
}

// HasCallback returns true if the workflow-processor injected callback
// fields, meaning the component should publish an AsyncStepResult
// instead of (or in addition to) its legacy result message.
func (c *CallbackFields) HasCallback() bool {
	return c.CallbackSubject != "" && c.TaskID != ""
}

// AsyncStepResult mirrors the semstreams workflow.AsyncStepResult type.
// Defined here to avoid semspec components importing semstreams internal packages.
type AsyncStepResult struct {
	TaskID      string          `json:"task_id"`
	ExecutionID string          `json:"execution_id,omitempty"`
	Status      string          `json:"status"`
	Output      json.RawMessage `json:"output,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// Async step result status constants.
const (
	AsyncStatusSuccess = "success"
	AsyncStatusFailed  = "failed"
)

// PublishCallbackSuccess publishes a successful AsyncStepResult to the
// callback subject via JetStream. The output should be the component's
// structured result that the workflow can access via ${steps.<name>.output.*}.
func (c *CallbackFields) PublishCallbackSuccess(ctx context.Context, nc *natsclient.Client, output any) error {
	return c.publishCallback(ctx, nc, AsyncStatusSuccess, output, "")
}

// PublishCallbackFailure publishes a failed AsyncStepResult to the
// callback subject via JetStream.
func (c *CallbackFields) PublishCallbackFailure(ctx context.Context, nc *natsclient.Client, errMsg string) error {
	return c.publishCallback(ctx, nc, AsyncStatusFailed, nil, errMsg)
}

func (c *CallbackFields) publishCallback(ctx context.Context, nc *natsclient.Client, status string, output any, errMsg string) error {
	if !c.HasCallback() {
		return fmt.Errorf("no callback configured")
	}

	var outputJSON json.RawMessage
	if output != nil {
		var err error
		outputJSON, err = json.Marshal(output)
		if err != nil {
			return fmt.Errorf("marshal callback output: %w", err)
		}
	}

	result := &AsyncStepResult{
		TaskID:      c.TaskID,
		ExecutionID: c.ExecutionID,
		Status:      status,
		Output:      outputJSON,
		Error:       errMsg,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal callback result: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream for callback: %w", err)
	}

	if _, err := js.Publish(ctx, c.CallbackSubject, data); err != nil {
		return fmt.Errorf("publish callback to %s: %w", c.CallbackSubject, err)
	}

	return nil
}
