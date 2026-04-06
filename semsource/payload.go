// Package semsource registers semsource payload types so that semspec can
// deserialize messages published by a headless semsource instance on the
// shared NATS bus. Since semsource runs headless (using semspec's graph
// components), semspec owns the payload registration.
//
// Import this package with a blank import to trigger payload registration
// before any components start consuming messages.
package semsource

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// Compile-time interface compliance checks.
var (
	_ graph.Graphable = (*EntityPayload)(nil)
	_ message.Payload = (*EntityPayload)(nil)
)

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "semsource",
		Category:    "entity",
		Version:     "v1",
		Description: "Entity streamed from a semsource ingestion instance",
		Factory: func() any {
			return &EntityPayload{}
		},
		Example: map[string]any{
			"id":         "org.platform.domain.system.type.instance",
			"triples":    []any{},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		},
	}); err != nil {
		panic(fmt.Sprintf("semsource: failed to register entity payload: %v", err))
	}

	// Register semsource status heartbeat so message-logger can parse it.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "semsource",
		Category:    "status",
		Version:     "v1",
		Description: "Semsource instance status heartbeat",
		Factory: func() any {
			return &StatusPayload{}
		},
	}); err != nil {
		panic(fmt.Sprintf("semsource: failed to register status payload: %v", err))
	}

	// Register semsource manifest — published at startup listing configured sources.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "semsource",
		Category:    "manifest",
		Version:     "v1",
		Description: "Source manifest listing all configured ingestion sources",
		Factory:     func() any { return &ManifestPayload{} },
	}); err != nil {
		panic(fmt.Sprintf("semsource: failed to register manifest payload: %v", err))
	}

	// Register semsource predicates — predicate schema per source type.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "semsource",
		Category:    "predicates",
		Version:     "v1",
		Description: "Predicate schema advertising predicates emitted per source type",
		Factory:     func() any { return &PredicateSchemaPayload{} },
	}); err != nil {
		panic(fmt.Sprintf("semsource: failed to register predicates payload: %v", err))
	}
}

// EntityPayload carries a graph entity received from semsource.
// It implements graph.Graphable so graph-ingest can persist it directly,
// and message.Payload so the component framework can deserialize it from wire.
type EntityPayload struct {
	// ID is the six-part federated entity identifier.
	ID string `json:"id"`

	// TripleData are the semantic facts that make up this entity's current state.
	TripleData []message.Triple `json:"triples"`

	// UpdatedAt records when the entity was last modified in semsource.
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID satisfies graph.Graphable.
func (p *EntityPayload) EntityID() string {
	return p.ID
}

// Triples satisfies graph.Graphable.
func (p *EntityPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema satisfies message.Payload.
func (p *EntityPayload) Schema() message.Type {
	return message.Type{Domain: "semsource", Category: "entity", Version: "v1"}
}

// Validate satisfies message.Payload.
func (p *EntityPayload) Validate() error {
	if p.ID == "" {
		return errors.New("semsource entity payload: id is required")
	}
	return nil
}

// MarshalJSON satisfies json.Marshaler.
func (p *EntityPayload) MarshalJSON() ([]byte, error) {
	type Alias EntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON satisfies json.Unmarshaler.
func (p *EntityPayload) UnmarshalJSON(data []byte) error {
	type Alias EntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// StatusPayload is the semsource heartbeat message.
type StatusPayload struct {
	Status   string `json:"status"`
	Sources  int    `json:"sources"`
	Entities int    `json:"entities"`
	Uptime   string `json:"uptime,omitempty"`
}

// Schema returns the message type for semsource status heartbeats.
func (p *StatusPayload) Schema() message.Type {
	return message.Type{Domain: "semsource", Category: "status", Version: "v1"}
}

// Validate checks the payload for correctness.
func (p *StatusPayload) Validate() error { return nil }
