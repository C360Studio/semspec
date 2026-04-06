package semsource

import (
	"encoding/json"
	"time"

	"github.com/c360studio/semstreams/message"
)

// ManifestPayload describes the configured sources for a semsource instance.
// Published to graph.ingest.manifest at startup.
type ManifestPayload struct {
	Namespace string           `json:"namespace"`
	Sources   []ManifestSource `json:"sources"`
	Timestamp time.Time        `json:"timestamp"`
}

// ManifestSource is a configured ingestion source entry.
type ManifestSource struct {
	Type         string   `json:"type"`
	Path         string   `json:"path,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	URL          string   `json:"url,omitempty"`
	URLs         []string `json:"urls,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Language     string   `json:"language,omitempty"`
	Watch        bool     `json:"watch,omitempty"`
	PollInterval string   `json:"poll_interval,omitempty"`
}

// Schema implements message.Payload.
func (p *ManifestPayload) Schema() message.Type {
	return message.Type{Domain: "semsource", Category: "manifest", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ManifestPayload) Validate() error { return nil }

// MarshalJSON implements json.Marshaler.
func (p *ManifestPayload) MarshalJSON() ([]byte, error) {
	type Alias ManifestPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ManifestPayload) UnmarshalJSON(data []byte) error {
	type Alias ManifestPayload
	return json.Unmarshal(data, (*Alias)(p))
}
