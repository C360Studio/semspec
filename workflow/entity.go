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

// ProposalEntityID returns the entity ID for a proposal.
// Format: semspec.local.workflow.proposal.proposal.{slug}
func ProposalEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.proposal.proposal.%s", slug)
}

// DesignEntityID returns the entity ID for a design document.
func DesignEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.design.design.%s", slug)
}

// SpecEntityID returns the entity ID for a specification document.
func SpecEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.spec.spec.%s", slug)
}

// TasksEntityID returns the entity ID for a tasks document.
func TasksEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.tasks.tasks.%s", slug)
}

// ProposalEntityPayload represents a proposal entity for graph ingestion.
type ProposalEntityPayload struct {
	EntityID_  string           `json:"entity_id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at,omitempty"`
}

// EntityID returns the entity ID.
func (p *ProposalEntityPayload) EntityID() string {
	return p.EntityID_
}

// Triples returns the entity triples.
func (p *ProposalEntityPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema returns the message type for this payload.
func (p *ProposalEntityPayload) Schema() message.Type {
	return EntityType
}

// Validate validates the payload.
func (p *ProposalEntityPayload) Validate() error {
	if p.EntityID_ == "" {
		return &ValidationError{Field: "entity_id", Message: "entity_id is required"}
	}
	if len(p.TripleData) == 0 {
		return &ValidationError{Field: "triples", Message: "at least one triple is required"}
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *ProposalEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias ProposalEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *ProposalEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias ProposalEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// EntityType is the message type for entity payloads.
var EntityType = message.Type{
	Domain:   "proposal",
	Category: "entity",
	Version:  "v1",
}

func init() {
	// Register the proposal entity payload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "proposal",
		Category:    "entity",
		Version:     "v1",
		Description: "Proposal entity payload for graph ingestion",
		Factory:     func() any { return &ProposalEntityPayload{} },
	})
}
