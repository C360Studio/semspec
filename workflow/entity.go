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
// Format: {org}.{platform}.wf.project.project.{slug}
func ProjectEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.project.project.%s", EntityPrefix(), slug)
}

// PlanEntityID returns the entity ID for a plan.
// Format: {org}.{platform}.wf.plan.plan.{slug}
func PlanEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.plan.%s", EntityPrefix(), slug)
}

// SpecEntityID returns the entity ID for a specification document.
// Format: {org}.{platform}.wf.plan.spec.{slug}
func SpecEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.spec.%s", EntityPrefix(), slug)
}

// TasksEntityID returns the entity ID for a tasks document.
// Format: {org}.{platform}.wf.plan.tasks.{slug}
func TasksEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.tasks.%s", EntityPrefix(), slug)
}

// TaskEntityID returns the entity ID for a single task.
// Format: {org}.{platform}.wf.task.task.{slug}-{seq}
func TaskEntityID(slug string, seq int) string {
	return fmt.Sprintf("%s.wf.task.task.%s-%d", EntityPrefix(), slug, seq)
}

// PhaseEntityID returns the entity ID for a single phase.
// Format: {org}.{platform}.wf.phase.phase.{slug}-{seq}
func PhaseEntityID(slug string, seq int) string {
	return fmt.Sprintf("%s.wf.phase.phase.%s-%d", EntityPrefix(), slug, seq)
}

// ApprovalEntityID returns the entity ID for an approval decision.
// Format: {org}.{platform}.wf.plan.approval.{id}
func ApprovalEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.approval.%s", EntityPrefix(), id)
}

// PhasesEntityID returns the entity ID for a phases document.
// Format: {org}.{platform}.wf.plan.phases.{slug}
func PhasesEntityID(slug string) string {
	return fmt.Sprintf("%s.wf.plan.phases.%s", EntityPrefix(), slug)
}

// taskIDSuffix is the fixed segment that follows the org.platform prefix in task entity IDs.
const taskIDSuffix = "wf.task.task."

// ExtractSlugFromTaskID extracts the plan slug from a task entity ID.
// Task entity IDs have the format: {org}.{platform}.wf.task.task.{slug}-{seq}
// Returns empty string if the format doesn't match or the slug is invalid.
func ExtractSlugFromTaskID(taskID string) string {
	fullPrefix := EntityPrefix() + "." + taskIDSuffix
	if !strings.HasPrefix(taskID, fullPrefix) {
		return ""
	}
	remainder := strings.TrimPrefix(taskID, fullPrefix)
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
// Format: {org}.{platform}.wf.plan.question.{id}
func QuestionEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.question.%s", EntityPrefix(), id)
}

// RequirementEntityID returns the entity ID for a requirement.
// Format: {org}.{platform}.wf.plan.req.{id}
func RequirementEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.req.%s", EntityPrefix(), id)
}

// ScenarioEntityID returns the entity ID for a scenario.
// Format: {org}.{platform}.wf.plan.scenario.{id}
func ScenarioEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.scenario.%s", EntityPrefix(), id)
}

// ChangeProposalEntityID returns the entity ID for a change proposal.
// Format: {org}.{platform}.wf.plan.proposal.{id}
func ChangeProposalEntityID(id string) string {
	return fmt.Sprintf("%s.wf.plan.proposal.%s", EntityPrefix(), id)
}

// DAGNodeEntityID returns the entity ID for a DAG execution node.
// Format: {org}.{platform}.wf.dag.node.{executionID}-{nodeID}
// Dots in executionID and nodeID are replaced with hyphens so the result has
// exactly 6 dot-separated parts.
func DAGNodeEntityID(executionID, nodeID string) string {
	instance := strings.ReplaceAll(executionID+"-"+nodeID, ".", "-")
	return fmt.Sprintf("%s.wf.dag.node.%s", EntityPrefix(), instance)
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
