package payloads

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// ---------------------------------------------------------------------------
// Graph topology refactor payloads (ADR-024)
// ---------------------------------------------------------------------------

// RequirementGeneratorRequest is the typed payload sent to the requirement-generator
// component. Dispatched after plan approval to generate Requirements for a plan.
type RequirementGeneratorRequest struct {
	ExecutionID           string            `json:"execution_id,omitempty"`
	Slug                  string            `json:"slug"`
	Title                 string            `json:"title"`
	Prompt                string            `json:"prompt,omitempty"`
	TraceID               string            `json:"trace_id,omitempty"`
	ReplaceRequirementIDs []string          `json:"replace_requirement_ids,omitempty"` // partial regen: IDs being replaced
	RejectionReasons      map[string]string `json:"rejection_reasons,omitempty"`       // per-ID reason from human review

	// Plan content fields — carried in the payload to avoid disk reads downstream.
	// When populated, requirement-generator uses these directly instead of loading plan.json.
	Goal    string          `json:"goal,omitempty"`
	Context string          `json:"context,omitempty"`
	Scope   *workflow.Scope `json:"scope,omitempty"` // pointer so omitempty works on struct types

	// ExistingRequirements carries approved requirements for partial regen context.
	// The generator preserves these and only regenerates the IDs in ReplaceRequirementIDs.
	ExistingRequirements []workflow.Requirement `json:"existing_requirements,omitempty"`
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

	// Plan content fields — carried in the payload to avoid disk reads downstream.
	// When populated, scenario-generator uses these directly instead of loading plan.json.
	PlanGoal    string `json:"plan_goal,omitempty"`
	PlanContext string `json:"plan_context,omitempty"`

	// Requirement content fields — carried so scenario-generator doesn't need graph reads.
	RequirementTitle       string `json:"requirement_title,omitempty"`
	RequirementDescription string `json:"requirement_description,omitempty"`
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

// ChangeProposalCascadeRequestType is the message type for cascade handler requests.
var ChangeProposalCascadeRequestType = message.Type{
	Domain:   "workflow",
	Category: "change-proposal-cascade-request",
	Version:  "v1",
}

// ChangeProposalAcceptedEvent is the payload published after a cascade completes
// successfully. It summarizes what was affected by the accepted ChangeProposal.
type ChangeProposalAcceptedEvent struct {
	ProposalID             string   `json:"proposal_id"`
	Slug                   string   `json:"slug"`
	TraceID                string   `json:"trace_id,omitempty"`
	AffectedRequirementIDs []string `json:"affected_requirement_ids"`
	AffectedScenarioIDs    []string `json:"affected_scenario_ids"`
}

// Schema implements message.Payload.
func (p *ChangeProposalAcceptedEvent) Schema() message.Type {
	return ChangeProposalAcceptedEventType
}

// Validate implements message.Payload.
func (p *ChangeProposalAcceptedEvent) Validate() error {
	if p.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ChangeProposalAcceptedEvent) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalAcceptedEvent
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ChangeProposalAcceptedEvent) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalAcceptedEvent
	return json.Unmarshal(data, (*Alias)(p))
}

// ChangeProposalAcceptedEventType is the message type for accepted events.
var ChangeProposalAcceptedEventType = message.Type{
	Domain:   "workflow",
	Category: "change-proposal-accepted",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Generation event payloads (single-writer fix)
// ---------------------------------------------------------------------------

// ScenariosForRequirementGeneratedType is the message type for per-requirement scenario events.
var ScenariosForRequirementGeneratedType = message.Type{
	Domain:   "workflow",
	Category: "scenarios-for-requirement-generated",
	Version:  "v1",
}

// ScenariosForRequirementGeneratedPayload wraps workflow.ScenariosForRequirementGeneratedEvent
// to satisfy message.Payload for publishing via message.NewBaseMessage.
type ScenariosForRequirementGeneratedPayload struct {
	workflow.ScenariosForRequirementGeneratedEvent
}

// Schema implements message.Payload.
func (p *ScenariosForRequirementGeneratedPayload) Schema() message.Type {
	return ScenariosForRequirementGeneratedType
}

// Validate implements message.Payload.
func (p *ScenariosForRequirementGeneratedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if p.RequirementID == "" {
		return fmt.Errorf("requirement_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ScenariosForRequirementGeneratedPayload) MarshalJSON() ([]byte, error) {
	type Alias workflow.ScenariosForRequirementGeneratedEvent
	return json.Marshal((*Alias)(&p.ScenariosForRequirementGeneratedEvent))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ScenariosForRequirementGeneratedPayload) UnmarshalJSON(data []byte) error {
	type Alias workflow.ScenariosForRequirementGeneratedEvent
	return json.Unmarshal(data, (*Alias)(&p.ScenariosForRequirementGeneratedEvent))
}

// GenerationFailedType is the message type for generation failure events.
var GenerationFailedType = message.Type{
	Domain:   "workflow",
	Category: "generation-failed",
	Version:  "v1",
}

// GenerationFailedPayload wraps workflow.GenerationFailedEvent to satisfy
// message.Payload for publishing via message.NewBaseMessage.
type GenerationFailedPayload struct {
	workflow.GenerationFailedEvent
}

// Schema implements message.Payload.
func (p *GenerationFailedPayload) Schema() message.Type { return GenerationFailedType }

// Validate implements message.Payload.
func (p *GenerationFailedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *GenerationFailedPayload) MarshalJSON() ([]byte, error) {
	type Alias workflow.GenerationFailedEvent
	return json.Marshal((*Alias)(&p.GenerationFailedEvent))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *GenerationFailedPayload) UnmarshalJSON(data []byte) error {
	type Alias workflow.GenerationFailedEvent
	return json.Unmarshal(data, (*Alias)(&p.GenerationFailedEvent))
}
