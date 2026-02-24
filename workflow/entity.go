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

// PlanEntityPayload represents a plan entity for graph ingestion.
type PlanEntityPayload struct {
	ID         string           `json:"entity_id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
}

// EntityID returns the entity ID.
func (p *PlanEntityPayload) EntityID() string {
	return p.ID
}

// Triples returns the entity triples.
func (p *PlanEntityPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema returns the message type for this payload.
func (p *PlanEntityPayload) Schema() message.Type {
	return EntityType
}

// Validate validates the payload.
func (p *PlanEntityPayload) Validate() error {
	if p.ID == "" {
		return &ValidationError{Field: "entity_id", Message: "entity_id is required"}
	}
	if len(p.TripleData) == 0 {
		return &ValidationError{Field: "triples", Message: "at least one triple is required"}
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *PlanEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias PlanEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *PlanEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias PlanEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// PhaseEntityPayload represents a phase entity for graph ingestion.
type PhaseEntityPayload struct {
	ID         string           `json:"entity_id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
}

// EntityID returns the entity ID.
func (p *PhaseEntityPayload) EntityID() string {
	return p.ID
}

// Triples returns the entity triples.
func (p *PhaseEntityPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema returns the message type for this payload.
func (p *PhaseEntityPayload) Schema() message.Type {
	return PhaseEntityType
}

// Validate validates the payload.
func (p *PhaseEntityPayload) Validate() error {
	if p.ID == "" {
		return &ValidationError{Field: "entity_id", Message: "entity_id is required"}
	}
	if len(p.TripleData) == 0 {
		return &ValidationError{Field: "triples", Message: "at least one triple is required"}
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *PhaseEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias PhaseEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *PhaseEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias PhaseEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ApprovalEntityPayload represents an approval decision for graph ingestion.
type ApprovalEntityPayload struct {
	ID         string           `json:"entity_id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
}

// EntityID returns the entity ID.
func (p *ApprovalEntityPayload) EntityID() string {
	return p.ID
}

// Triples returns the entity triples.
func (p *ApprovalEntityPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema returns the message type for this payload.
func (p *ApprovalEntityPayload) Schema() message.Type {
	return ApprovalEntityType
}

// Validate validates the payload.
func (p *ApprovalEntityPayload) Validate() error {
	if p.ID == "" {
		return &ValidationError{Field: "entity_id", Message: "entity_id is required"}
	}
	if len(p.TripleData) == 0 {
		return &ValidationError{Field: "triples", Message: "at least one triple is required"}
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *ApprovalEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias ApprovalEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *ApprovalEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias ApprovalEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
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

func init() {
	// Register the plan entity payload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "plan",
		Category:    "entity",
		Version:     "v1",
		Description: "Plan entity payload for graph ingestion",
		Factory:     func() any { return &PlanEntityPayload{} },
	})

	// Register the phase entity payload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "phase",
		Category:    "entity",
		Version:     "v1",
		Description: "Phase entity payload for graph ingestion",
		Factory:     func() any { return &PhaseEntityPayload{} },
	})

	// Register the approval entity payload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "approval",
		Category:    "entity",
		Version:     "v1",
		Description: "Approval entity payload for graph ingestion",
		Factory:     func() any { return &ApprovalEntityPayload{} },
	})
}
