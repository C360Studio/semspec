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

	// Exploration carries the analyst sub-phase's capability list (ADR-040
	// Move 2). When populated, the requirement-generator produces ONE
	// Requirement per capability with CapabilityName set. Nil for legacy
	// plans (no analyst sub-phase) — back-compat preserved.
	Exploration *workflow.Exploration `json:"exploration,omitempty"`
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

	// ArchitectureContext is a pre-formatted summary of actors and integration points
	// from the architecture document. Injected when dispatching from PLAN_STATES watcher.
	ArchitectureContext string `json:"architecture_context,omitempty"`

	// RequiredTiers names the test-pyramid tiers the scenario-generator MUST
	// cover for this requirement plus any catalog harness profile IDs each
	// tier must bind to (ADR-041 Move 3). Computed by the scenario-generator's
	// classifier from the requirement's capability surfaces + the
	// architecture's selected harness profiles. The dispatch layer renders
	// this into the user prompt as a "Required tiers" bullet list so the
	// agent emits ≥1 scenario per required tier with the right tag + binding.
	// Empty in retry payloads originating before ADR-041 lands (legacy
	// back-compat); the dispatcher falls through to the legacy single-tier
	// prompt body in that case.
	RequiredTiers []RequiredTier `json:"required_tiers,omitempty"`

	// Story fields — populated when dispatching per-Story (ADR-043 PR 4j).
	// Bob authors one batch of scenarios per Story instead of per Requirement;
	// scenarios in the batch are auto-attached to this Story server-side.
	//
	// When StoryID is empty, the dispatcher operates in legacy per-Requirement
	// mode (pre-Sarah plans / mock fixtures without Stories) — Bob still emits
	// scenarios for the whole Requirement and the server falls back to the
	// "first story owns the scenarios" lookup.
	StoryID            string   `json:"story_id,omitempty"`
	StoryTitle         string   `json:"story_title,omitempty"`
	StoryIntent        string   `json:"story_intent,omitempty"`
	StoryFilesOwned    []string `json:"story_files_owned,omitempty"`
	StoryComponentName string   `json:"story_component_name,omitempty"`
}

// RequiredTier is the wire shape carried in ScenarioGeneratorRequest for a
// single tier requirement. Tag names a tier (e.g. "@unit", "@integration");
// HarnessProfileIDs lists the catalog profile IDs scenarios at that tier
// MUST bind to (populated only for "@integration"). ADR-041 Move 3.
type RequiredTier struct {
	Tag               string   `json:"tag"`
	HarnessProfileIDs []string `json:"harness_profile_ids,omitempty"`
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

// PlanDecisionReviewRequest is the typed payload dispatched to the plan-decision
// reviewer (LLM or human gate) when a PlanDecision enters the under_review state.
type PlanDecisionReviewRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	ProposalID  string `json:"proposal_id"`
	PlanID      string `json:"plan_id"`
	Slug        string `json:"slug"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *PlanDecisionReviewRequest) Schema() message.Type {
	return PlanDecisionReviewRequestType
}

// Validate implements message.Payload.
func (r *PlanDecisionReviewRequest) Validate() error {
	if r.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *PlanDecisionReviewRequest) MarshalJSON() ([]byte, error) {
	type Alias PlanDecisionReviewRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlanDecisionReviewRequest) UnmarshalJSON(data []byte) error {
	type Alias PlanDecisionReviewRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// PlanDecisionReviewRequestType is the message type for change proposal review requests.
var PlanDecisionReviewRequestType = message.Type{
	Domain:   "workflow",
	Category: "plan-decision-review-request",
	Version:  "v1",
}

// PlanDecisionCascadeRequest is the typed payload dispatched to the cascade handler
// when a PlanDecision is accepted. The cascade handler loads the proposal, traverses
// Requirement → Scenario → Task edges, marks affected tasks dirty, and publishes
// a task.dirty event with all affected task IDs.
type PlanDecisionCascadeRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	ProposalID  string `json:"proposal_id"`
	Slug        string `json:"slug"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *PlanDecisionCascadeRequest) Schema() message.Type {
	return PlanDecisionCascadeRequestType
}

// Validate implements message.Payload.
func (r *PlanDecisionCascadeRequest) Validate() error {
	if r.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *PlanDecisionCascadeRequest) MarshalJSON() ([]byte, error) {
	type Alias PlanDecisionCascadeRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlanDecisionCascadeRequest) UnmarshalJSON(data []byte) error {
	type Alias PlanDecisionCascadeRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// PlanDecisionCascadeRequestType is the message type for cascade handler requests.
var PlanDecisionCascadeRequestType = message.Type{
	Domain:   "workflow",
	Category: "plan-decision-cascade-request",
	Version:  "v1",
}

// PlanDecisionAcceptedEvent is the payload published after a cascade completes
// successfully. It summarizes what was affected by the accepted PlanDecision.
type PlanDecisionAcceptedEvent struct {
	ProposalID string `json:"proposal_id"`
	Slug       string `json:"slug"`
	TraceID    string `json:"trace_id,omitempty"`
	// Kind echoes the accepted proposal's kind so consumers can branch without
	// re-loading the plan. Empty for pre-existing/legacy producers (treated as
	// requirement_change by consumers). The requirement-executor keys off
	// architecture_revise to abandon — rather than resume — in-flight execs.
	Kind                   workflow.PlanDecisionKind `json:"kind,omitempty"`
	AffectedRequirementIDs []string                  `json:"affected_requirement_ids"`
	AffectedScenarioIDs    []string                  `json:"affected_scenario_ids"`
}

// Schema implements message.Payload.
func (p *PlanDecisionAcceptedEvent) Schema() message.Type {
	return PlanDecisionAcceptedEventType
}

// Validate implements message.Payload.
func (p *PlanDecisionAcceptedEvent) Validate() error {
	if p.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *PlanDecisionAcceptedEvent) MarshalJSON() ([]byte, error) {
	type Alias PlanDecisionAcceptedEvent
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *PlanDecisionAcceptedEvent) UnmarshalJSON(data []byte) error {
	type Alias PlanDecisionAcceptedEvent
	return json.Unmarshal(data, (*Alias)(p))
}

// PlanDecisionAcceptedEventType is the message type for accepted events.
var PlanDecisionAcceptedEventType = message.Type{
	Domain:   "workflow",
	Category: "plan-decision-accepted",
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

// StoriesGeneratedType is the message type for story-preparer's output
// (ADR-043 Move 3). Sarah publishes one event per plan; plan-manager (the
// single writer) consumes it, persists Plan.Stories + per-Task triples,
// and transitions the plan from preparing_stories to ready_for_execution.
var StoriesGeneratedType = message.Type{
	Domain:   "workflow",
	Category: "stories-generated",
	Version:  "v1",
}

// StoriesGeneratedPayload wraps workflow.StoriesGeneratedEvent to satisfy
// message.Payload for publishing via message.NewBaseMessage.
type StoriesGeneratedPayload struct {
	workflow.StoriesGeneratedEvent
}

// Schema implements message.Payload.
func (p *StoriesGeneratedPayload) Schema() message.Type {
	return StoriesGeneratedType
}

// Validate implements message.Payload. Slug is required (every event names
// the owning plan). Empty Stories is allowed at the message layer — Sarah
// may legitimately emit a zero-story payload as a "no stories to prepare"
// signal that plan-manager treats as a pass-through. Per-Story structural
// invariants live in workflow.ValidateStories, called by the story-preparer
// before publish and by plan-manager R3 before transition.
func (p *StoriesGeneratedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *StoriesGeneratedPayload) MarshalJSON() ([]byte, error) {
	type Alias workflow.StoriesGeneratedEvent
	return json.Marshal((*Alias)(&p.StoriesGeneratedEvent))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *StoriesGeneratedPayload) UnmarshalJSON(data []byte) error {
	type Alias workflow.StoriesGeneratedEvent
	return json.Unmarshal(data, (*Alias)(&p.StoriesGeneratedEvent))
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
