package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
)

// WorkflowTriggerType is the message type for workflow trigger payloads.
// Uses semstreams' canonical workflow.trigger.v1 type.
var WorkflowTriggerType = message.Type{
	Domain:   "workflow",
	Category: "trigger",
	Version:  "v1",
}

// TriggerPayload is semspec's view of workflow trigger data.
// It provides access to both standard semstreams fields and semspec-specific Data fields.
//
// For SENDING triggers, use semstreams' TriggerPayload directly with
// custom fields marshaled into the Data blob.
// For RECEIVING triggers, use reactive.ParseReactivePayload[T].
type TriggerPayload struct {
	// WorkflowID identifies which workflow definition to execute
	WorkflowID string `json:"workflow_id"`

	// Well-known agent fields (accessible via ${trigger.payload.*})
	Role   string `json:"role,omitempty"`
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt,omitempty"`

	// Well-known routing fields
	UserID      string `json:"user_id,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`

	// Request tracking
	RequestID string `json:"request_id,omitempty"`

	// Semspec-specific fields
	Slug          string   `json:"slug,omitempty"`
	Title         string   `json:"title,omitempty"`
	Description   string   `json:"description,omitempty"`
	Auto          bool     `json:"auto,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`

	// Task-execution-specific fields
	TaskID           string   `json:"task_id,omitempty"`
	TaskType         TaskType `json:"task_type,omitempty"` // Pipeline selection: implement, research, refactor, document
	ContextRequestID string   `json:"context_request_id,omitempty"`
	FileScope        []string `json:"file_scope,omitempty"`      // Files/globs this task may touch
	SystemPrompt     string   `json:"system_prompt,omitempty"`   // Provider-aware system prompt for the agentic loop
	ScenarioBranch   string   `json:"scenario_branch,omitempty"` // Target branch for worktree merges (e.g. "semspec/scenario-auth")

	// PlanDecision-specific fields
	ProposalID string `json:"proposal_id,omitempty"`

	// Data holds any additional custom fields as raw JSON (kept for extensibility)
	Data json.RawMessage `json:"data,omitempty"`
}

// Schema returns the message type for TriggerPayload.
func (p *TriggerPayload) Schema() message.Type {
	return WorkflowTriggerType
}

// Validate validates the TriggerPayload.
func (p *TriggerPayload) Validate() error {
	if p.WorkflowID == "" {
		return &ValidationError{Field: "workflow_id", Message: "workflow_id is required"}
	}
	if p.Slug == "" {
		return &ValidationError{Field: "slug", Message: "slug is required"}
	}
	return nil
}

// MarshalJSON marshals the TriggerPayload to JSON.
func (p *TriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias TriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the TriggerPayload from JSON.
// It handles both flattened and nested Data structures.
func (p *TriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias TriggerPayload
	if err := json.Unmarshal(data, (*Alias)(p)); err != nil {
		return err
	}

	// If semspec-specific fields are empty but Data is present,
	// try to extract them from the Data blob (backward compat with stored KV executions)
	if len(p.Data) > 0 {
		var nested struct {
			Slug             string   `json:"slug,omitempty"`
			Title            string   `json:"title,omitempty"`
			Description      string   `json:"description,omitempty"`
			Auto             bool     `json:"auto,omitempty"`
			ProjectID        string   `json:"project_id,omitempty"`
			ScopePatterns    []string `json:"scope_patterns,omitempty"`
			TraceID          string   `json:"trace_id,omitempty"`
			LoopID           string   `json:"loop_id,omitempty"`
			TaskID           string   `json:"task_id,omitempty"`
			ContextRequestID string   `json:"context_request_id,omitempty"`
			ProposalID       string   `json:"proposal_id,omitempty"`
			FileScope        []string `json:"file_scope,omitempty"`
			SystemPrompt     string   `json:"system_prompt,omitempty"`
			ScenarioBranch   string   `json:"scenario_branch,omitempty"`
		}
		if err := json.Unmarshal(p.Data, &nested); err == nil {
			if p.Slug == "" && nested.Slug != "" {
				p.Slug = nested.Slug
			}
			if p.Title == "" && nested.Title != "" {
				p.Title = nested.Title
			}
			if p.Description == "" && nested.Description != "" {
				p.Description = nested.Description
			}
			if !p.Auto && nested.Auto {
				p.Auto = nested.Auto
			}
			if p.ProjectID == "" && nested.ProjectID != "" {
				p.ProjectID = nested.ProjectID
			}
			if len(p.ScopePatterns) == 0 && len(nested.ScopePatterns) > 0 {
				p.ScopePatterns = nested.ScopePatterns
			}
			if p.TraceID == "" && nested.TraceID != "" {
				p.TraceID = nested.TraceID
			}
			if p.LoopID == "" && nested.LoopID != "" {
				p.LoopID = nested.LoopID
			}
			if p.TaskID == "" && nested.TaskID != "" {
				p.TaskID = nested.TaskID
			}
			if p.ContextRequestID == "" && nested.ContextRequestID != "" {
				p.ContextRequestID = nested.ContextRequestID
			}
			if p.ProposalID == "" && nested.ProposalID != "" {
				p.ProposalID = nested.ProposalID
			}
			if len(p.FileScope) == 0 && len(nested.FileScope) > 0 {
				p.FileScope = nested.FileScope
			}
			if p.SystemPrompt == "" && nested.SystemPrompt != "" {
				p.SystemPrompt = nested.SystemPrompt
			}
			if p.ScenarioBranch == "" && nested.ScenarioBranch != "" {
				p.ScenarioBranch = nested.ScenarioBranch
			}
		}
	}
	return nil
}

// WorkflowTriggerPayload is an alias for TriggerPayload for backward compatibility.
type WorkflowTriggerPayload = TriggerPayload //revive:disable-line

// NewSemstreamsTrigger creates a TriggerPayload with semspec fields populated
// as top-level fields. The Data blob is no longer populated — the reactive
// engine reads all fields from the top level.
func NewSemstreamsTrigger(workflowID, role, prompt, requestID, slug, title, description, traceID, projectID string, scopePatterns []string, auto bool) *TriggerPayload {
	return &TriggerPayload{
		WorkflowID:    workflowID,
		Role:          role,
		Prompt:        prompt,
		RequestID:     requestID,
		Slug:          slug,
		Title:         title,
		Description:   description,
		TraceID:       traceID,
		ProjectID:     projectID,
		ScopePatterns: scopePatterns,
		Auto:          auto,
	}
}
