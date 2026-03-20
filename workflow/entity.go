package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// ProjectEntityID returns the entity ID for a project.
// Format: c360.semspec.workflow.project.project.{slug}
func ProjectEntityID(slug string) string {
	return fmt.Sprintf("c360.semspec.workflow.project.project.%s", slug)
}

// PlanEntityID returns the entity ID for a plan.
// Format: c360.semspec.workflow.plan.plan.{slug}
func PlanEntityID(slug string) string {
	return fmt.Sprintf("c360.semspec.workflow.plan.plan.%s", slug)
}

// SpecEntityID returns the entity ID for a specification document.
// Format: c360.semspec.workflow.plan.spec.{slug}
func SpecEntityID(slug string) string {
	return fmt.Sprintf("c360.semspec.workflow.plan.spec.%s", slug)
}

// TasksEntityID returns the entity ID for a tasks document.
// Format: c360.semspec.workflow.plan.tasks.{slug}
func TasksEntityID(slug string) string {
	return fmt.Sprintf("c360.semspec.workflow.plan.tasks.%s", slug)
}

// TaskEntityID returns the entity ID for a single task.
// Format: c360.semspec.workflow.task.task.{slug}-{seq}
func TaskEntityID(slug string, seq int) string {
	return fmt.Sprintf("c360.semspec.workflow.task.task.%s-%d", slug, seq)
}

// PhaseEntityID returns the entity ID for a single phase.
// Format: c360.semspec.workflow.phase.phase.{slug}-{seq}
func PhaseEntityID(slug string, seq int) string {
	return fmt.Sprintf("c360.semspec.workflow.phase.phase.%s-%d", slug, seq)
}

// ApprovalEntityID returns the entity ID for an approval decision.
// Format: c360.semspec.workflow.approval.approval.{id}
func ApprovalEntityID(id string) string {
	return fmt.Sprintf("c360.semspec.workflow.approval.approval.%s", id)
}

// PhasesEntityID returns the entity ID for a phases document.
// Format: c360.semspec.workflow.plan.phases.{slug}
func PhasesEntityID(slug string) string {
	return fmt.Sprintf("c360.semspec.workflow.plan.phases.%s", slug)
}

// ExtractSlugFromTaskID extracts the plan slug from a task entity ID.
// Task entity IDs have the format: c360.semspec.workflow.task.task.{slug}-{seq}
// Returns empty string if the format doesn't match or the slug is invalid.
func ExtractSlugFromTaskID(taskID string) string {
	const prefix = "c360.semspec.workflow.task.task."
	if !strings.HasPrefix(taskID, prefix) {
		return ""
	}
	remainder := strings.TrimPrefix(taskID, prefix)
	if remainder == "" {
		return ""
	}

	// Find the last hyphen followed by only digits (the sequence number).
	lastHyphen := strings.LastIndex(remainder, "-")
	if lastHyphen <= 0 {
		return ""
	}

	seqPart := remainder[lastHyphen+1:]
	if seqPart == "" {
		return ""
	}
	for _, r := range seqPart {
		if !unicode.IsDigit(r) {
			return ""
		}
	}

	slug := remainder[:lastHyphen]
	if err := ValidateSlug(slug); err != nil {
		return ""
	}
	return slug
}

// QuestionEntityID returns the entity ID for a question.
// Format: c360.semspec.workflow.question.question.{id}
func QuestionEntityID(id string) string {
	return fmt.Sprintf("c360.semspec.workflow.question.question.%s", id)
}

// RequirementEntityID returns the entity ID for a requirement.
// Format: c360.semspec.workflow.requirement.requirement.{id}
func RequirementEntityID(id string) string {
	return fmt.Sprintf("c360.semspec.workflow.requirement.requirement.%s", id)
}

// ScenarioEntityID returns the entity ID for a scenario.
// Format: c360.semspec.workflow.scenario.scenario.{id}
func ScenarioEntityID(id string) string {
	return fmt.Sprintf("c360.semspec.workflow.scenario.scenario.%s", id)
}

// ChangeProposalEntityID returns the entity ID for a change proposal.
// Format: c360.semspec.workflow.change-proposal.change-proposal.{id}
func ChangeProposalEntityID(id string) string {
	return fmt.Sprintf("c360.semspec.workflow.change-proposal.change-proposal.%s", id)
}

// DAGNodeEntityID returns the entity ID for a DAG execution node.
// Format: local.semspec.workflow.dag-node.node.{executionID}-{nodeID}
// Dots in executionID and nodeID are replaced with hyphens so the result has
// exactly 6 dot-separated parts.
func DAGNodeEntityID(executionID, nodeID string) string {
	instance := strings.ReplaceAll(executionID+"-"+nodeID, ".", "-")
	return fmt.Sprintf("local.semspec.workflow.dag-node.node.%s", instance)
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

// ChangeProposalEntityType is the message type for change proposal entity payloads.
var ChangeProposalEntityType = message.Type{
	Domain:   "change-proposal",
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
	{"plan", "Plan entity payload for graph ingestion", EntityType},
	{"phase", "Phase entity payload for graph ingestion", PhaseEntityType},
	{"approval", "Approval entity payload for graph ingestion", ApprovalEntityType},
	{"task", "Task entity payload for graph ingestion", TaskEntityType},
	{"question", "Question entity payload for graph ingestion", QuestionEntityType},
	{"requirement", "Requirement entity payload for graph ingestion", RequirementEntityType},
	{"scenario", "Scenario entity payload for graph ingestion", ScenarioEntityType},
	{"change-proposal", "ChangeProposal entity payload for graph ingestion", ChangeProposalEntityType},
	{"dag-node", "DAG execution node entity payload for graph ingestion", DAGNodeEntityType},
}

func init() {
	for _, et := range workflowEntityTypes {
		msgType := et.msgType
		_ = component.RegisterPayload(&component.PayloadRegistration{
			Domain:      et.domain,
			Category:    "entity",
			Version:     "v1",
			Description: et.description,
			Factory: func() any {
				p := &EntityPayload{}
				p.msgType = msgType
				return p
			},
		})
	}
}
