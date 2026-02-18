package workflow

import (
	"encoding/json"
	"fmt"
	"time"

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

// PlanEntityPayload represents a plan entity for graph ingestion.
type PlanEntityPayload struct {
	EntityID_  string           `json:"entity_id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
}

// EntityID returns the entity ID.
func (p *PlanEntityPayload) EntityID() string {
	return p.EntityID_
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
	if p.EntityID_ == "" {
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

// EntityType is the message type for entity payloads.
var EntityType = message.Type{
	Domain:   "plan",
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
}
