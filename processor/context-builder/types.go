// Package contextbuilder provides context gathering for workflow tasks.
// It builds relevant context (SOPs, diffs, files, entities) based on task type
// and respects token budget constraints.
package contextbuilder

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// TaskType represents the type of context building task.
// Note: A corresponding TaskType is defined in the strategies package.
// This duplication is intentional to avoid import cycles. The builder
// converts between these types when calling strategies.
type TaskType string

const (
	// TaskTypeReview builds context for code review tasks.
	// Includes: SOPs (all-or-nothing), git diffs, related tests, conventions.
	TaskTypeReview TaskType = "review"

	// TaskTypeImplementation builds context for implementation tasks.
	// Includes: spec document, source files in scope, patterns, architecture docs.
	TaskTypeImplementation TaskType = "implementation"

	// TaskTypeExploration builds context for exploration tasks.
	// Includes: codebase summary, entities matching topic, related docs.
	TaskTypeExploration TaskType = "exploration"
)

// IsValid returns true if the task type is recognized.
func (t TaskType) IsValid() bool {
	switch t {
	case TaskTypeReview, TaskTypeImplementation, TaskTypeExploration:
		return true
	}
	return false
}

// ContextBuildRequest is the input message for context building.
// Published to context.build.<task_type> subjects.
type ContextBuildRequest struct {
	// RequestID uniquely identifies this context build request.
	RequestID string `json:"request_id"`

	// TaskType determines which strategy to use for context building.
	TaskType TaskType `json:"task_type"`

	// WorkflowID is the optional workflow this context is being built for.
	WorkflowID string `json:"workflow_id,omitempty"`

	// Files are the changed files (for review tasks).
	Files []string `json:"files,omitempty"`

	// GitRef is the commit or branch reference (for review tasks).
	GitRef string `json:"git_ref,omitempty"`

	// Topic is the search topic (for exploration tasks).
	Topic string `json:"topic,omitempty"`

	// SpecEntityID is the specification entity ID (for implementation tasks).
	SpecEntityID string `json:"spec_entity_id,omitempty"`

	// Capability is the model capability to use for budget calculation.
	// Examples: "reviewing", "coding", "planning".
	Capability string `json:"capability,omitempty"`

	// Model is an explicit model override for budget calculation.
	Model string `json:"model,omitempty"`

	// TokenBudget is an explicit budget override.
	TokenBudget int `json:"token_budget,omitempty"`
}

// Schema implements message.Payload.
func (r *ContextBuildRequest) Schema() message.Type {
	return message.Type{Domain: "context", Category: "request", Version: "v1"}
}

// Validate implements message.Payload.
// Validates the request has required fields for its task type.
func (r *ContextBuildRequest) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}

	if !r.TaskType.IsValid() {
		return fmt.Errorf("invalid task_type: %s", r.TaskType)
	}

	// Task-specific validation
	switch r.TaskType {
	case TaskTypeReview:
		if len(r.Files) == 0 && r.GitRef == "" {
			return fmt.Errorf("review task requires files or git_ref")
		}
	case TaskTypeImplementation:
		// Implementation can work with spec entity, files, or topic
	case TaskTypeExploration:
		// Exploration can work with just a topic or codebase summary
	}

	// Validate token budget if specified
	if r.TokenBudget < 0 {
		return fmt.Errorf("token_budget cannot be negative")
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ContextBuildRequest) MarshalJSON() ([]byte, error) {
	type Alias ContextBuildRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ContextBuildRequest) UnmarshalJSON(data []byte) error {
	type Alias ContextBuildRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ContextBuildResponse is the output message containing built context.
// Published to context.built.<request_id> subjects.
type ContextBuildResponse struct {
	// RequestID matches the request this response is for.
	RequestID string `json:"request_id"`

	// TaskType is the task type from the request.
	TaskType TaskType `json:"task_type"`

	// TokenCount is the total tokens in the built context.
	TokenCount int `json:"token_count"`

	// Entities are references to graph entities included in context.
	Entities []EntityRef `json:"entities,omitempty"`

	// Documents maps file paths to their content.
	Documents map[string]string `json:"documents,omitempty"`

	// Diffs contains the git diff content.
	Diffs string `json:"diffs,omitempty"`

	// Provenance tracks where context items came from.
	Provenance []ProvenanceEntry `json:"provenance,omitempty"`

	// SOPIDs lists the SOP entity IDs included (for review validation).
	SOPIDs []string `json:"sop_ids,omitempty"`

	// TokensUsed is the actual tokens used.
	TokensUsed int `json:"tokens_used"`

	// TokensBudget is the total budget available.
	TokensBudget int `json:"tokens_budget"`

	// Truncated indicates if content was truncated to fit budget.
	Truncated bool `json:"truncated"`

	// Error contains any error message if context building failed.
	Error string `json:"error,omitempty"`
}

// Schema implements message.Payload.
func (r *ContextBuildResponse) Schema() message.Type {
	return message.Type{Domain: "context", Category: "response", Version: "v1"}
}

// Validate implements message.Payload.
func (r *ContextBuildResponse) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ContextBuildResponse) MarshalJSON() ([]byte, error) {
	type Alias ContextBuildResponse
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ContextBuildResponse) UnmarshalJSON(data []byte) error {
	type Alias ContextBuildResponse
	return json.Unmarshal(data, (*Alias)(r))
}

// EntityRef is a reference to a graph entity in the context.
type EntityRef struct {
	// ID is the entity identifier.
	ID string `json:"id"`

	// Type is the entity type (e.g., "sop", "function", "type").
	Type string `json:"type,omitempty"`

	// Content is the hydrated entity content (optional).
	Content string `json:"content,omitempty"`

	// Tokens is the token count for this entity.
	Tokens int `json:"tokens,omitempty"`
}

// ProvenanceEntry tracks the source of a context item.
type ProvenanceEntry struct {
	// Source identifies where the item came from.
	// Examples: "sop:entity-id", "git:HEAD~1..HEAD", "file:/path/to/file".
	Source string `json:"source"`

	// Type categorizes the source.
	Type ProvenanceType `json:"type"`

	// Tokens is the token count for this item.
	Tokens int `json:"tokens"`

	// Truncated indicates if this item was truncated.
	Truncated bool `json:"truncated,omitempty"`

	// Priority is the allocation priority (lower = higher priority).
	Priority int `json:"priority,omitempty"`
}

// ProvenanceType categorizes provenance sources.
type ProvenanceType string

const (
	ProvenanceTypeSOP        ProvenanceType = "sop"
	ProvenanceTypeGitDiff    ProvenanceType = "git_diff"
	ProvenanceTypeFile       ProvenanceType = "file"
	ProvenanceTypeEntity     ProvenanceType = "entity"
	ProvenanceTypeGraph      ProvenanceType = "graph"
	ProvenanceTypeSummary    ProvenanceType = "summary"
	ProvenanceTypeSpec       ProvenanceType = "spec"
	ProvenanceTypeTest       ProvenanceType = "test"
	ProvenanceTypeConvention ProvenanceType = "convention"
)

// ContextItem represents a single item of context being built.
type ContextItem struct {
	// Name identifies this item.
	Name string

	// Content is the actual context content.
	Content string

	// Tokens is the token count.
	Tokens int

	// Priority for budget allocation (lower = higher priority).
	Priority int

	// Type is the provenance type.
	Type ProvenanceType

	// Required means the item must be included or fail.
	Required bool

	// EntityID is the associated entity ID if applicable.
	EntityID string
}

// SOPEntity represents a Standard Operating Procedure entity from the graph.
type SOPEntity struct {
	// ID is the entity identifier.
	ID string `json:"id"`

	// Title is the SOP title.
	Title string `json:"title"`

	// Content is the full SOP content.
	Content string `json:"content"`

	// AppliesTo is the path pattern this SOP applies to.
	AppliesTo string `json:"applies_to"`

	// Tokens is the token count for this SOP.
	Tokens int `json:"tokens"`
}

// GraphEntity represents a generic entity from the knowledge graph.
type GraphEntity struct {
	// ID is the entity identifier.
	ID string `json:"id"`

	// Triples are the predicate-object pairs.
	Triples []Triple `json:"triples,omitempty"`
}

// Triple is a predicate-object pair from an entity.
type Triple struct {
	Predicate string `json:"predicate"`
	Object    any    `json:"object"`
}
