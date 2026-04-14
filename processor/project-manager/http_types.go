package projectmanager

import (
	"time"

	"github.com/c360studio/semspec/workflow"
)

// GenerateStandardsRequest is the request body for POST /project-manager/generate-standards.
type GenerateStandardsRequest struct {
	// Detection is the full workflow.DetectionResult from /detect.
	Detection workflow.DetectionResult `json:"detection"`

	// ExistingDocsContent maps relative file path to file content.
	// The UI reads these files and sends them so the LLM has project context.
	ExistingDocsContent map[string]string `json:"existing_docs_content"`
}

// GenerateStandardsResponse is the response body for POST /project-manager/generate-standards.
type GenerateStandardsResponse struct {
	// Items is the generated set of project standards.
	// Empty in the stub implementation — LLM integration is Phase 3.
	Items []workflow.Standard `json:"items"`

	// TokenEstimate is the approximate token count for all items.
	TokenEstimate int `json:"token_estimate"`
}

// ApproveRequest is the request body for POST /project-manager/approve.
type ApproveRequest struct {
	File string `json:"file"`
}

// ApproveResponse is the response from POST /project-manager/approve.
type ApproveResponse struct {
	File        string    `json:"file"`
	ApprovedAt  time.Time `json:"approved_at"`
	AllApproved bool      `json:"all_approved"`
}

// ConfigUpdateRequest contains the fields that can be updated via PATCH.
type ConfigUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Org         *string `json:"org,omitempty"`
	Platform    *string `json:"platform,omitempty"`
}

// ChecklistUpdateRequest contains the fields for updating the checklist.
type ChecklistUpdateRequest struct {
	Checks []workflow.Check `json:"checks"`
}

// StandardsUpdateRequest contains the fields for updating standards.
type StandardsUpdateRequest struct {
	Items []workflow.Standard `json:"items"`
}

// TestCheckRequest is the request body for POST /project-manager/test-check.
type TestCheckRequest struct {
	// Command is the shell command to run in the sandbox (required).
	Command string `json:"command"`
	// Timeout is an optional Go duration string (e.g. "30s"). Capped at 120s.
	Timeout string `json:"timeout,omitempty"`
}

// TestCheckResponse is the response from POST /project-manager/test-check.
type TestCheckResponse struct {
	// Passed is true when the command exited with code 0.
	Passed   bool   `json:"passed"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	// Duration is a human-readable elapsed time string (e.g. "1234ms").
	Duration string `json:"duration"`
}
