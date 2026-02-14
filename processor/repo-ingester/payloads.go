package repoingester

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "repo",
		Category:    "entity",
		Version:     "v1",
		Description: "Repository source entity payload for graph ingestion",
		Factory:     func() any { return &RepoEntityPayload{} },
	})
	if err != nil {
		panic("failed to register RepoEntityPayload: " + err.Error())
	}
}

// RepoEntityType is the message type for repository entity payloads.
var RepoEntityType = message.Type{Domain: "repo", Category: "entity", Version: "v1"}

// RepoEntityPayload implements message.Payload and graph.Graphable for repository entity ingestion.
type RepoEntityPayload struct {
	EntityID_  string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
	RepoPath   string           `json:"repo_path,omitempty"` // For AST indexer
}

// EntityID returns the entity identifier for Graphable interface.
func (p *RepoEntityPayload) EntityID() string { return p.EntityID_ }

// Triples returns the entity triples for Graphable interface.
func (p *RepoEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *RepoEntityPayload) Schema() message.Type { return RepoEntityType }

// Validate validates the payload for Payload interface.
func (p *RepoEntityPayload) Validate() error {
	if p.EntityID_ == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *RepoEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias RepoEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *RepoEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias RepoEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}
