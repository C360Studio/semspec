package health

import (
	"encoding/json"
	"sort"
	"strings"
)

// agentResponse is the package-private projection of an agent.response
// payload that detectors share. New detectors that need a different
// field add it here rather than re-declaring a near-identical type —
// drift between detectors' projections is the kind of bug that's
// invisible until two detectors disagree on whether a response had
// content.
//
// Stays decoupled from semstreams' agentic types per bundle.go's
// "resilient to upstream schema evolution" rule.
type agentResponse struct {
	// Source coordinates
	Sequence int64
	Subject  string
	LoopID   string

	// Payload projection
	FinishReason   string
	Content        string
	ToolCalls      []json.RawMessage
	PromptTokens   int
	CompletionToks int
}

// HasToolCalls reports whether the response made any tool calls.
// Treats `null` and `[]` equivalently — both decode to a nil/zero-len
// slice and are semantically "no tool calls."
func (r agentResponse) HasToolCalls() bool { return len(r.ToolCalls) > 0 }

// IsStop reports whether the model finished the turn with stop.
func (r agentResponse) IsStop() bool { return r.FinishReason == "stop" }

// agentResponsePayload is the on-the-wire JSON shape we decode into
// an agentResponse. Mirrors only the fields the v1 detector set
// reads; additive within v1.
type agentResponsePayload struct {
	Payload struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	} `json:"payload"`
}

// agentResponseSubjectPrefix is the NATS subject prefix every
// agent.response carries. Subject form: agent.response.<loop_id>:req:<request_id>.
const agentResponseSubjectPrefix = "agent.response."

// walkAgentResponses parses every agent.response in messages, groups
// the parsed projections by loop ID, and sorts each group ascending
// by sequence (chronological).
//
// Bundle.Messages from FetchMessages is newest-first; the sort here
// is what every detector needs for "did X happen BEFORE Y" predicates.
//
// Returns the grouped responses plus a count of messages that looked
// like agent.responses but failed to decode. Detectors that want to
// surface "we couldn't decide because input was corrupt" can emit a
// SeverityUndetermined diagnosis when malformedCount > 0.
func walkAgentResponses(messages []Message) (byLoop map[string][]agentResponse, malformedCount int) {
	byLoop = make(map[string][]agentResponse)
	for _, m := range messages {
		if !strings.HasPrefix(m.Subject, agentResponseSubjectPrefix) {
			continue
		}
		loopID := extractLoopIDFromSubject(m.Subject)
		if loopID == "" {
			continue
		}
		var p agentResponsePayload
		if err := json.Unmarshal(m.RawData, &p); err != nil {
			malformedCount++
			continue
		}
		byLoop[loopID] = append(byLoop[loopID], agentResponse{
			Sequence:       m.Sequence,
			Subject:        m.Subject,
			LoopID:         loopID,
			FinishReason:   p.Payload.FinishReason,
			Content:        p.Payload.Message.Content,
			ToolCalls:      p.Payload.Message.ToolCalls,
			PromptTokens:   p.Payload.Usage.PromptTokens,
			CompletionToks: p.Payload.Usage.CompletionTokens,
		})
	}
	for id := range byLoop {
		entries := byLoop[id]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
	}
	return byLoop, malformedCount
}

// extractLoopIDFromSubject returns the loop UUID portion of an
// agent.response subject. Subject form is
// "agent.response.<loop_id>:req:<request_id>"; loop_id is a UUID v4
// (no colons), so first-colon split is safe.
func extractLoopIDFromSubject(subject string) string {
	rest := strings.TrimPrefix(subject, agentResponseSubjectPrefix)
	if rest == subject {
		return ""
	}
	if idx := strings.Index(rest, ":"); idx > 0 {
		return rest[:idx]
	}
	return rest
}

// undeterminedFromMalformed builds a SeverityUndetermined diagnosis
// recording N malformed agent.responses for the given detector. Used
// by every detector that walks agent.responses; surfacing the count
// on the bundle keeps "we found nothing" distinguishable from "we
// couldn't decide because input was corrupt."
func undeterminedFromMalformed(shape string, n int) Diagnosis {
	return Diagnosis{
		Shape:    shape,
		Severity: SeverityUndetermined,
		Evidence: []EvidenceRef{
			{Kind: EvidenceLogLine, Field: "malformed_agent_responses", Value: n},
		},
		Remediation: "One or more agent.response RawData payloads failed to decode. Re-run the bundle capture or inspect the raw message-logger entries for malformed JSON.",
	}
}
