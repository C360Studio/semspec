package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// ProjectEntityID returns the entity ID for a project.
// Format: {org}.{platform}.wf.project.project.{hash}
func ProjectEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.project.project.%s", EntityPrefix(), HashInstanceID(slug))
}

// ProjectConfigEntityID returns the entity ID for a project initialization config file.
// configType is one of: "project", "checklist", "standards".
// Format: {org}.{platform}.wf.project.config.{hash}
func ProjectConfigEntityID(configType string) string {
	return fmt.Sprintf("%s.wf.project.config.%s", EntityPrefix(), HashInstanceID(configType))
}

// PlanEntityID returns the entity ID for a plan.
// Format: {org}.{platform}.wf.plan.plan.{hash}
func PlanEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.plan.%s", EntityPrefix(), HashInstanceID(slug))
}

// SpecEntityID returns the entity ID for a specification document.
// Format: {org}.{platform}.wf.plan.spec.{hash}
func SpecEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.spec.%s", EntityPrefix(), HashInstanceID(slug))
}

// TasksEntityID returns the entity ID for a tasks document.
// Format: {org}.{platform}.wf.plan.tasks.{hash}
func TasksEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.tasks.%s", EntityPrefix(), HashInstanceID(slug))
}

// TaskEntityID returns the entity ID for a single task.
// Format: {org}.{platform}.wf.task.task.{hash}
func TaskEntityID(slug string, seq int) string {
	return fmt.Sprintf("%s.wf.task.task.%s", EntityPrefix(), HashInstanceID(slug, fmt.Sprintf("%d", seq)))
}

// PhaseEntityID returns the entity ID for a single phase.
// Format: {org}.{platform}.wf.phase.phase.{hash}
func PhaseEntityID(slug string, seq int) string {
	return fmt.Sprintf("%s.wf.phase.phase.%s", EntityPrefix(), HashInstanceID(slug, fmt.Sprintf("%d", seq)))
}

// ApprovalEntityID returns the entity ID for an approval decision.
// Format: {org}.{platform}.wf.plan.approval.{hash}
func ApprovalEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.approval.%s", EntityPrefix(), HashInstanceID(id))
}

// PhasesEntityID returns the entity ID for a phases document.
// Format: {org}.{platform}.wf.plan.phases.{hash}
func PhasesEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.phases.%s", EntityPrefix(), HashInstanceID(slug))
}

// QuestionEntityID returns the entity ID for a question.
// Format: {org}.{platform}.wf.plan.question.{hash}
func QuestionEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.question.%s", EntityPrefix(), HashInstanceID(id))
}

// RequirementEntityID returns the entity ID for a requirement.
// Format: {org}.{platform}.wf.plan.req.{hash}
func RequirementEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.req.%s", EntityPrefix(), HashInstanceID(id))
}

// CapabilityEntityID returns the entity ID for a capability owned by a plan.
// Format: {org}.{platform}.wf.plan.capability.{hash}
//
// The logical pair (planSlug, capabilityName) is hashed into the instance
// segment so two different plans can both declare a capability with the same
// name (e.g. "mavsdk-bootstrap") without entity-ID collision. The logical
// values surface as triples on the entity (semspec.capability.name,
// semspec.capability.plan) so reverse lookups stay queryable.
//
// Added 2026-05-30 for ADR-040 (OpenSpec vocabulary alignment + bidirectional
// compat). Capability is a first-class entity — it owns triples and can be
// referenced from Requirement entities via semspec.requirement.capability.
func CapabilityEntityID(planSlug, capabilityName string) string {
	return fmt.Sprintf("%s.wf.plan.capability.%s", EntityPrefix(), HashInstanceID(planSlug, capabilityName))
}

// ScenarioEntityID returns the entity ID for a scenario.
// Format: {org}.{platform}.wf.plan.scenario.{hash}
func ScenarioEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.scenario.%s", EntityPrefix(), HashInstanceID(id))
}

// PlanDecisionEntityID returns the entity ID for a change proposal.
// Format: {org}.{platform}.wf.plan.proposal.{hash}
func PlanDecisionEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.proposal.%s", EntityPrefix(), HashInstanceID(id))
}

// DAGNodeEntityID returns the entity ID for a DAG execution node.
// Format: {org}.{platform}.wf.dag.node.{hash}
func DAGNodeEntityID(executionID, nodeID string) string {
	return fmt.Sprintf("%s.wf.dag.node.%s", EntityPrefix(), HashInstanceID(executionID, nodeID))
}

// EntityType is the message type for plan entity payloads.
var EntityType = message.Type{
	Domain:   "plan",
	Category: "entity",
	Version:  "v1",
}

// PhaseEntityType is the message type for phase entity payloads.
var PhaseEntityType = message.Type{
	Domain:   "phase",
	Category: "entity",
	Version:  "v1",
}

// ApprovalEntityType is the message type for approval entity payloads.
var ApprovalEntityType = message.Type{
	Domain:   "approval",
	Category: "entity",
	Version:  "v1",
}

// TaskEntityType is the message type for task entity payloads.
var TaskEntityType = message.Type{
	Domain:   "task",
	Category: "entity",
	Version:  "v1",
}

// QuestionEntityType is the message type for question entity payloads.
var QuestionEntityType = message.Type{
	Domain:   "question",
	Category: "entity",
	Version:  "v1",
}

// PlanEntityType is the message type for plan entity payloads.
var PlanEntityType = message.Type{
	Domain:   "plan",
	Category: "entity",
	Version:  "v1",
}

// CapabilityEntityType is the message type for capability entity payloads.
var CapabilityEntityType = message.Type{
	Domain:   "capability",
	Category: "entity",
	Version:  "v1",
}

// RequirementEntityType is the message type for requirement entity payloads.
var RequirementEntityType = message.Type{
	Domain:   "requirement",
	Category: "entity",
	Version:  "v1",
}

// ScenarioEntityType is the message type for scenario entity payloads.
var ScenarioEntityType = message.Type{
	Domain:   "scenario",
	Category: "entity",
	Version:  "v1",
}

// PlanDecisionEntityType is the message type for change proposal entity payloads.
var PlanDecisionEntityType = message.Type{
	Domain:   "plan-decision",
	Category: "entity",
	Version:  "v1",
}

// DAGNodeEntityType is the message type for DAG execution node entity payloads.
var DAGNodeEntityType = message.Type{
	Domain:   "dag-node",
	Category: "entity",
	Version:  "v1",
}

// EntityPayload is the unified entity payload for all workflow graph entities
// (plans, phases, approvals, tasks, questions). The message type is set at construction
// via NewEntityPayload and returned by Schema().
type EntityPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
	msgType    message.Type
}

// NewEntityPayload creates a EntityPayload with the given message type.
func NewEntityPayload(msgType message.Type, id string, triples []message.Triple) *EntityPayload {
	return &EntityPayload{
		ID:         id,
		TripleData: triples,
		UpdatedAt:  time.Now(),
		msgType:    msgType,
	}
}

// EntityID returns the entity ID.
func (p *EntityPayload) EntityID() string {
	return p.ID
}

// Triples returns the entity triples.
func (p *EntityPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema returns the message type for this payload.
func (p *EntityPayload) Schema() message.Type {
	return p.msgType
}

// Validate validates the payload.
func (p *EntityPayload) Validate() error {
	if p.ID == "" {
		return &ValidationError{Field: "id", Message: "id is required"}
	}
	if len(p.TripleData) == 0 {
		return &ValidationError{Field: "triples", Message: "at least one triple is required"}
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *EntityPayload) MarshalJSON() ([]byte, error) {
	type Alias EntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *EntityPayload) UnmarshalJSON(data []byte) error {
	type Alias EntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// workflowEntityTypes lists all workflow entity message types for consolidated registration.
var workflowEntityTypes = []struct {
	domain      string
	description string
	msgType     message.Type
}{
	// NOTE: The legacy "plan" domain entry covers PlanEntityType as well — both share
	// Domain:"plan"/Category:"entity"/Version:"v1" and the registry key is the tuple.
	// PlanEntityType is defined as a named alias for clarity at call sites (Phase 3a
	// batch writers) but is not re-registered here to avoid a duplicate-key error.
	{"plan", "Plan entity payload for graph ingestion", EntityType},
	{"phase", "Phase entity payload for graph ingestion", PhaseEntityType},
	{"approval", "Approval entity payload for graph ingestion", ApprovalEntityType},
	{"task", "Task entity payload for graph ingestion", TaskEntityType},
	{"question", "Question entity payload for graph ingestion", QuestionEntityType},
	{"requirement", "Requirement entity payload for graph ingestion", RequirementEntityType},
	{"scenario", "Scenario entity payload for graph ingestion", ScenarioEntityType},
	{"plan-decision", "PlanDecision entity payload for graph ingestion", PlanDecisionEntityType},
	{"dag-node", "DAG execution node entity payload for graph ingestion", DAGNodeEntityType},
	{"capability", "Capability entity payload for graph ingestion", CapabilityEntityType},
}

// RegisterPayloads registers every payload type owned by the workflow package
// (entity payloads here, plus the task and answer payloads defined alongside
// in task.go and question.go). Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	var errs []error
	for _, et := range workflowEntityTypes {
		msgType := et.msgType
		errs = append(errs, reg.Register(&payloadregistry.Registration{
			Domain:      et.domain,
			Category:    "entity",
			Version:     "v1",
			Description: et.description,
			Factory: func() any {
				p := &EntityPayload{}
				p.msgType = msgType
				return p
			},
		}))
	}
	errs = append(errs, registerTaskPayload(reg), registerAnswerPayload(reg), registerResearchPayloads(reg))
	return errors.Join(errs...)
}
