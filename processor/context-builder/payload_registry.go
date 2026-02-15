package contextbuilder

import (
	"github.com/c360studio/semstreams/component"
)

func init() {
	// Register ContextBuildRequest payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "context",
		Category:    "request",
		Version:     "v1",
		Description: "Context build request for gathering task-relevant context",
		Factory: func() any {
			return &ContextBuildRequest{}
		},
		Example: map[string]any{
			"request_id":  "req-123",
			"task_type":   "implementation",
			"workflow_id": "workflow-456",
			"files":       []string{"main.go", "handler.go"},
			"capability":  "coding",
		},
	}); err != nil {
		panic("failed to register ContextBuildRequest payload: " + err.Error())
	}

	// Register ContextBuildResponse payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "context",
		Category:    "response",
		Version:     "v1",
		Description: "Context build response containing gathered context",
		Factory: func() any {
			return &ContextBuildResponse{}
		},
		Example: map[string]any{
			"request_id":    "req-123",
			"task_type":     "implementation",
			"token_count":   5000,
			"tokens_used":   4500,
			"tokens_budget": 8000,
			"truncated":     false,
		},
	}); err != nil {
		panic("failed to register ContextBuildResponse payload: " + err.Error())
	}
}
