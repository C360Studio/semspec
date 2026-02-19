package constitution

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "constitution",
		Category:    "check",
		Version:     "v1",
		Description: "Constitution check request payload",
		Factory:     func() any { return &CheckRequestPayload{} },
	})
	if err != nil {
		panic("failed to register CheckRequestPayload: " + err.Error())
	}

	err = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "constitution",
		Category:    "result",
		Version:     "v1",
		Description: "Constitution check result payload",
		Factory:     func() any { return &CheckResultPayload{} },
	})
	if err != nil {
		panic("failed to register CheckResultPayload: " + err.Error())
	}

	err = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "constitution",
		Category:    "entity",
		Version:     "v1",
		Description: "Constitution entity payload for graph ingestion",
		Factory:     func() any { return &EntityPayload{} },
	})
	if err != nil {
		panic("failed to register EntityPayload: " + err.Error())
	}
}

// CheckRequestType is the message type for constitution check requests.
var CheckRequestType = message.Type{Domain: "constitution", Category: "check", Version: "v1"}

// CheckResultType is the message type for constitution check results.
var CheckResultType = message.Type{Domain: "constitution", Category: "result", Version: "v1"}

// ConstitutionEntityType is the message type for constitution entity payloads.
var ConstitutionEntityType = message.Type{Domain: "constitution", Category: "entity", Version: "v1"}

// CheckRequestPayload represents a request to check content against the constitution.
type CheckRequestPayload struct {
	RequestID string            `json:"request_id"`
	Content   string            `json:"content"`
	Context   map[string]string `json:"context,omitempty"`
}

// Schema returns the message type for Payload interface.
func (p *CheckRequestPayload) Schema() message.Type { return CheckRequestType }

// Validate validates the payload for Payload interface.
func (p *CheckRequestPayload) Validate() error {
	if p.RequestID == "" {
		return errors.New("request_id is required")
	}
	if p.Content == "" {
		return errors.New("content is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *CheckRequestPayload) MarshalJSON() ([]byte, error) {
	type Alias CheckRequestPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *CheckRequestPayload) UnmarshalJSON(data []byte) error {
	type Alias CheckRequestPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// CheckResultPayload represents the result of a constitution check.
type CheckResultPayload struct {
	RequestID  string      `json:"request_id"`
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations,omitempty"`
	Warnings   []Violation `json:"warnings,omitempty"`
	CheckedAt  time.Time   `json:"checked_at"`
}

// Schema returns the message type for Payload interface.
func (p *CheckResultPayload) Schema() message.Type { return CheckResultType }

// Validate validates the payload for Payload interface.
func (p *CheckResultPayload) Validate() error {
	if p.RequestID == "" {
		return errors.New("request_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *CheckResultPayload) MarshalJSON() ([]byte, error) {
	type Alias CheckResultPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *CheckResultPayload) UnmarshalJSON(data []byte) error {
	type Alias CheckResultPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// EntityPayload implements message.Payload and graph.Graphable for constitution entity ingestion.
type EntityPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *EntityPayload) EntityID() string { return p.ID }

// Triples returns the entity triples for Graphable interface.
func (p *EntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *EntityPayload) Schema() message.Type { return ConstitutionEntityType }

// Validate validates the payload for Payload interface.
func (p *EntityPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *EntityPayload) MarshalJSON() ([]byte, error) {
	type Alias EntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *EntityPayload) UnmarshalJSON(data []byte) error {
	type Alias EntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}
