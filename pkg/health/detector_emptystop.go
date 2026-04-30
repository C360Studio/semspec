package health

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// EmptyStopAfterToolCalls detects an agent.response that finished
// with stop, no tool_calls, and empty content — AFTER at least one
// prior agent.response in the same loop emitted tool_calls. The shape
// usually means the model bailed mid-sequence: it had been calling
// tools, then produced a turn with nothing actionable, leaving the
// loop wedged.
//
// Match conditions (all must hold for a single agent.response):
//
//	finish_reason == "stop"
//	len(message.tool_calls) == 0
//	message.content == ""
//	there exists an earlier agent.response in the same loop with
//	  len(message.tool_calls) > 0
//
// Loop identity comes from the message subject:
// "agent.response.<loop_id>:req:<request_id>". Earlier-ness is by
// Bundle.Messages.Sequence (ascending = chronological).
//
// Severity is critical: an empty stop after tool calls means the loop
// did not complete its work, and the bundle reader should treat it as
// a hard failure shape rather than a warning to investigate later.
type EmptyStopAfterToolCalls struct{}

// EmptyStopShape is the Diagnosis.Shape value for this detector. Pinned
// as a constant so callers (UIs, tests, downstream tooling) can switch
// on it without retyping the literal.
const EmptyStopShape = "EmptyStopAfterToolCalls"

const subjectPrefixAgentResponse = "agent.response."

// Name implements Detector.
func (EmptyStopAfterToolCalls) Name() string { return EmptyStopShape }

// agentResponseShape is the minimal projection of the agent.response
// payload the detector reads. Kept local so the detector doesn't
// depend on semstreams' agentic types — the bundle layer's whole
// point is to be resilient to upstream schema evolution.
type agentResponseShape struct {
	Payload struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	} `json:"payload"`
}

// observation is one parsed agent.response with the predicates the
// detector needs. Local type, never escapes Run.
type observation struct {
	seq           int64
	hasToolCall   bool
	matchesShape  bool
	subject       string
	finishReason  string
	contentLength int
}

// Run implements Detector. Pure; no I/O.
func (EmptyStopAfterToolCalls) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}
	byLoop := groupResponsesByLoop(b.Messages)
	var out []Diagnosis
	for _, entries := range byLoop {
		// Bundle.Messages comes back newest-first from the message-logger;
		// detector logic needs chronological order so the "prior tool_call"
		// condition reflects actual time, not bundle order.
		sort.Slice(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })
		seenToolCall := false
		for _, e := range entries {
			if e.hasToolCall {
				seenToolCall = true
				continue
			}
			if !e.matchesShape || !seenToolCall {
				continue
			}
			out = append(out, Diagnosis{
				Shape:    EmptyStopShape,
				Severity: SeverityCritical,
				Evidence: []EvidenceRef{
					{
						Kind:  EvidenceAgentResponse,
						ID:    strconv.FormatInt(e.seq, 10),
						Field: "finish_reason",
						Value: e.finishReason,
					},
				},
				Remediation: "Model returned finish_reason=stop with no tool_calls and empty content after a prior tool_call in the same loop. Likely an empty turn that wedges the loop; on retry, inject the failure context per feedback_retries_must_inject_failure_context.md so the model knows the prior turn was rejected.",
				MemoryRef:   "feedback_retries_must_inject_failure_context.md",
			})
		}
	}
	// Stable order across runs — by sequence — so fixture-driven tests
	// can compare against a golden list without flake.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Evidence[0].ID < out[j].Evidence[0].ID
	})
	return out
}

// groupResponsesByLoop walks bundle messages, decodes each
// agent.response payload, and groups the parsed observations by
// loop_id. Messages that aren't agent.responses or whose RawData
// doesn't decode are skipped — the detector should not fail loudly
// on a malformed entry; that's a different shape.
func groupResponsesByLoop(messages []Message) map[string][]observation {
	out := make(map[string][]observation)
	for _, m := range messages {
		if !strings.HasPrefix(m.Subject, subjectPrefixAgentResponse) {
			continue
		}
		loopID := extractLoopIDFromSubject(m.Subject)
		if loopID == "" {
			continue
		}
		var shape agentResponseShape
		if err := json.Unmarshal(m.RawData, &shape); err != nil {
			continue
		}
		obs := observation{
			seq:           m.Sequence,
			subject:       m.Subject,
			finishReason:  shape.Payload.FinishReason,
			contentLength: len(shape.Payload.Message.Content),
			hasToolCall:   len(shape.Payload.Message.ToolCalls) > 0,
		}
		obs.matchesShape = shape.Payload.FinishReason == "stop" &&
			!obs.hasToolCall &&
			obs.contentLength == 0
		out[loopID] = append(out[loopID], obs)
	}
	return out
}

// extractLoopIDFromSubject returns the loop UUID portion of an
// agent.response subject. The subject form is
// "agent.response.<loop_id>:req:<request_id>"; the loop_id may itself
// contain hyphens but never colons (UUID v4 format), so splitting on
// the first colon is safe.
func extractLoopIDFromSubject(subject string) string {
	rest := strings.TrimPrefix(subject, subjectPrefixAgentResponse)
	if rest == subject {
		return ""
	}
	if idx := strings.Index(rest, ":"); idx > 0 {
		return rest[:idx]
	}
	return rest
}
