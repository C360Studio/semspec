package cancellation

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// Signal is published to agent.signal.cancel.<loopID> when a running workflow
// loop must be stopped. The recipient (typically the scenario-execution-loop or
// the dag-execution-loop) should observe this signal on its KV watch or NATS
// subscription and transition to a cancelled/failed terminal state.
//
// Published to: agent.signal.cancel.<loopID>
type Signal struct {
	// LoopID is the identifier of the workflow execution loop to cancel.
	// It is also encoded in the NATS subject for routing: agent.signal.cancel.<LoopID>.
	LoopID string `json:"loop_id"`

	// Reason is a human-readable explanation of why the loop is being cancelled.
	// Forwarded to the loop's error/failure event for observability.
	Reason string `json:"reason"`
}

// SignalType is the canonical message.Type for Signal.
var SignalType = message.Type{
	Domain:   "agent",
	Category: "cancellation-signal",
	Version:  "v1",
}

// Schema implements message.Payload.
func (s *Signal) Schema() message.Type {
	return SignalType
}

// Validate implements message.Payload.
func (s *Signal) Validate() error {
	if s.LoopID == "" {
		return fmt.Errorf("loop_id is required")
	}
	if s.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *Signal) MarshalJSON() ([]byte, error) {
	type Alias Signal
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Signal) UnmarshalJSON(data []byte) error {
	type Alias Signal
	return json.Unmarshal(data, (*Alias)(s))
}
