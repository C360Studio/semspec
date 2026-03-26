package projectmanager

import (
	"time"

	"github.com/c360studio/semspec/workflow"
)

// GenerateStandardsRequest is the request body for POST /api/project/generate-standards.
type GenerateStandardsRequest struct {
	// Detection is the full workflow.DetectionResult from /detect.
	Detection workflow.DetectionResult `json:"detection"`

	// ExistingDocsContent maps relative file path to file content.
	// The UI reads these files and sends them so the LLM has project context.
	ExistingDocsContent map[string]string `json:"existing_docs_content"`
}

// GenerateStandardsResponse is the response body for POST /api/project/generate-standards.
type GenerateStandardsResponse struct {
	// Rules is the generated set of project standards.
	// Empty in the stub implementation — LLM integration is Phase 3.
	Rules []workflow.Rule `json:"rules"`

	// TokenEstimate is the approximate token count for all rules.
	TokenEstimate int `json:"token_estimate"`
}

// ApproveRequest is the request body for POST /api/project/approve.
type ApproveRequest struct {
	File string `json:"file"`
}

// ApproveResponse is the response from POST /api/project/approve.
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
	Rules []workflow.Rule `json:"rules"`
}
