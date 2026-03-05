package reactive

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// ---------------------------------------------------------------------------
// Graph topology refactor payloads (ADR-024)
// ---------------------------------------------------------------------------

// RequirementGeneratorRequest is the typed payload sent to the requirement-generator
// component. Dispatched after plan approval to generate Requirements for a plan.
type RequirementGeneratorRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Prompt      string `json:"prompt,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *RequirementGeneratorRequest) Schema() message.Type {
	return RequirementGeneratorRequestType
}

// Validate implements message.Payload.
func (r *RequirementGeneratorRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *RequirementGeneratorRequest) MarshalJSON() ([]byte, error) {
	type Alias RequirementGeneratorRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *RequirementGeneratorRequest) UnmarshalJSON(data []byte) error {
	type Alias RequirementGeneratorRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// RequirementGeneratorRequestType is the message type for requirement generator requests.
var RequirementGeneratorRequestType = message.Type{
	Domain:   "workflow",
	Category: "requirement-generator-request",
	Version:  "v1",
}

// ScenarioGeneratorRequest is the typed payload sent to the scenario-generator
// component. Dispatched after requirements are generated to produce Scenarios
// for a specific Requirement.
type ScenarioGeneratorRequest struct {
	ExecutionID   string `json:"execution_id,omitempty"`
	Slug          string `json:"slug"`
	RequirementID string `json:"requirement_id"`
	TraceID       string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *ScenarioGeneratorRequest) Schema() message.Type {
	return ScenarioGeneratorRequestType
}

// Validate implements message.Payload.
func (r *ScenarioGeneratorRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.RequirementID == "" {
		return fmt.Errorf("requirement_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ScenarioGeneratorRequest) MarshalJSON() ([]byte, error) {
	type Alias ScenarioGeneratorRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ScenarioGeneratorRequest) UnmarshalJSON(data []byte) error {
	type Alias ScenarioGeneratorRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ScenarioGeneratorRequestType is the message type for scenario generator requests.
var ScenarioGeneratorRequestType = message.Type{
	Domain:   "workflow",
	Category: "scenario-generator-request",
	Version:  "v1",
}

// ChangeProposalReviewRequest is the typed payload dispatched to the change-proposal
// reviewer (LLM or human gate) when a ChangeProposal enters the under_review state.
type ChangeProposalReviewRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	ProposalID  string `json:"proposal_id"`
	PlanID      string `json:"plan_id"`
	Slug        string `json:"slug"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *ChangeProposalReviewRequest) Schema() message.Type {
	return ChangeProposalReviewRequestType
}

// Validate implements message.Payload.
func (r *ChangeProposalReviewRequest) Validate() error {
	if r.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ChangeProposalReviewRequest) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalReviewRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ChangeProposalReviewRequest) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalReviewRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ChangeProposalReviewRequestType is the message type for change proposal review requests.
var ChangeProposalReviewRequestType = message.Type{
	Domain:   "workflow",
	Category: "change-proposal-review-request",
	Version:  "v1",
}

// ChangeProposalCascadeRequest is the typed payload dispatched to the cascade handler
// when a ChangeProposal is accepted. The cascade handler loads the proposal, traverses
// Requirement → Scenario → Task edges, marks affected tasks dirty, and publishes
// a task.dirty event with all affected task IDs.
type ChangeProposalCascadeRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	ProposalID  string `json:"proposal_id"`
	Slug        string `json:"slug"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *ChangeProposalCascadeRequest) Schema() message.Type {
	return ChangeProposalCascadeRequestType
}

// Validate implements message.Payload.
func (r *ChangeProposalCascadeRequest) Validate() error {
	if r.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// ChangeProposalCascadeRequestType is the message type for cascade handler requests.
var ChangeProposalCascadeRequestType = message.Type{
	Domain:   "workflow",
	Category: "change-proposal-cascade-request",
	Version:  "v1",
}

// MarshalJSON implements json.Marshaler.
func (r *ChangeProposalCascadeRequest) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalCascadeRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ChangeProposalCascadeRequest) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalCascadeRequest
	return json.Unmarshal(data, (*Alias)(r))
}
