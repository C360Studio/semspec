package workflow

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// ProjectEntityID returns the entity ID for a project.
// Format: semspec.local.project.{slug}
func ProjectEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.project.%s", slug)
}

// PlanEntityID returns the entity ID for a plan.
// Format: semspec.local.workflow.plan.plan.{slug}
func PlanEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.plan.plan.%s", slug)
}

// SpecEntityID returns the entity ID for a specification document.
func SpecEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.spec.spec.%s", slug)
}

// TasksEntityID returns the entity ID for a tasks document.
func TasksEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.tasks.tasks.%s", slug)
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
