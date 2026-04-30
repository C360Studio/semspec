package health

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
)

// RapidShallowToolCalls detects an agentic loop that has issued many tool
// calls without ever invoking submit_work. The shape usually means the
// model is "exploring" — calling bash, graph_*, http_request, etc. — but
// never converging on a deliverable. Loops in this state typically run
// until the iteration budget exhausts and then fail.
//
// Match conditions (all must hold for a single loop):
//
//	count(agent.response messages with len(tool_calls) > 0) >= ToolCallThreshold
//	count(agent.response messages whose tool_calls include "submit_work") == 0
//
// Distinct from EmptyStopAfterToolCalls: that shape requires a stop+empty
// finish AFTER tool_calls. RapidShallowToolCalls fires while the loop is
// STILL ACTIVE (or recently failed) — the model never bailed cleanly,
// it just kept exploring.
//
// Severity is warning, not critical. A loop in this state hasn't failed
// yet — it's heading toward an iteration-budget exhaustion. Operators
// running with --bail-on warning get a chance to kill it before the
// budget runs out; --bail-on critical lets the loop play out (and then
// the budget-exhaustion error surfaces through other paths).
//
// Caught 2026-04-30 PM during qwen3.5-35b-a3b openrouter run 6: model
// alternated model_call → bash (2ms instant fail) → model_call → bash
// indefinitely, never reached submit_work. Trajectory snapshot showed
// 4 steps in 60s with all bash calls returning instantly — the
// "entity-IDs-as-bash-paths" cargo-cult pattern from
// feedback_prompts_reasons_not_rules.md.
type RapidShallowToolCalls struct{}

// RapidShallowToolCallsShape is the Diagnosis.Shape value for this detector.
const RapidShallowToolCallsShape = "RapidShallowToolCalls"

// rapidShallowToolCallThreshold is the count of tool-call-bearing
// agent.responses (in a single loop, no submit_work yet) that triggers
// the diagnosis. Tuned conservatively: an architect or developer on a
// non-trivial task may legitimately make 4-5 graph/bash calls before
// submitting. 6+ calls without submit_work signals the loop has
// stopped converging.
const rapidShallowToolCallThreshold = 6

// submitWorkToolName is the name of the load-bearing terminal tool
// every agentic loop is expected to call. Detector matches against
// it explicitly — a loop without this call hasn't produced a
// deliverable, regardless of how many other tools fired.
const submitWorkToolName = "submit_work"

// Name implements Detector.
func (RapidShallowToolCalls) Name() string { return RapidShallowToolCallsShape }

// rapidShallowToolCallShape is the parsed agent.response projection the
// detector consumes. We carry the tool_call NAMES (not just count) so
// we can distinguish "called submit_work, mid-flight" from "never
// called submit_work."
type rapidShallowResponse struct {
	Sequence  int64
	LoopID    string
	ToolNames []string
}

// Run implements Detector. Pure; no I/O.
func (RapidShallowToolCalls) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}
	byLoop, malformed := walkRapidShallow(b.Messages)

	type seqDiag struct {
		seq  int64
		diag Diagnosis
	}
	var matches []seqDiag
	for loopID, entries := range byLoop {
		// Sort chronologically so tests can pin the latest-seq evidence
		// stably and the diagnosis cites a deterministic boundary.
		sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
		var toolCallCount int
		var submittedWork bool
		var lastToolCallSeq int64
		for _, r := range entries {
			if len(r.ToolNames) == 0 {
				continue
			}
			toolCallCount++
			lastToolCallSeq = r.Sequence
			if slices.Contains(r.ToolNames, submitWorkToolName) {
				submittedWork = true
				break
			}
		}
		if submittedWork || toolCallCount < rapidShallowToolCallThreshold {
			continue
		}
		matches = append(matches, seqDiag{
			seq: lastToolCallSeq,
			diag: Diagnosis{
				Shape:    RapidShallowToolCallsShape,
				Severity: SeverityWarning,
				Evidence: []EvidenceRef{
					{
						Kind:  EvidenceLoopEntry,
						ID:    loopID,
						Field: "tool_call_count_without_submit_work",
						Value: toolCallCount,
					},
				},
				Remediation: "Loop has issued " + strconv.Itoa(toolCallCount) +
					" tool calls without invoking submit_work. The model is exploring but not converging on a deliverable. Common causes: cargo-cult bash arguments (e.g. passing entity IDs as filesystem paths — see feedback_prompts_reasons_not_rules.md), repeated graph_search calls without acting on results, or a persona prompt that doesn't sufficiently emphasize the submit_work terminal step. Consider --bail-on warning when running this model class so the loop dies before iteration budget exhausts.",
				MemoryRef: "feedback_prompts_reasons_not_rules.md",
			},
		})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].seq < matches[j].seq })

	out := make([]Diagnosis, 0, len(matches)+1)
	for _, m := range matches {
		out = append(out, m.diag)
	}
	if malformed > 0 {
		out = append(out, undeterminedFromMalformed(RapidShallowToolCallsShape, malformed))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// walkRapidShallow parses agent.response messages and projects each into
// a rapidShallowResponse with the tool-call NAMES. A separate pass from
// walkAgentResponses (which projects content + finish_reason) because
// this detector only cares about tool-call identity. Returns
// malformed-count for SeverityUndetermined surfacing.
func walkRapidShallow(messages []Message) (byLoop map[string][]rapidShallowResponse, malformedCount int) {
	byLoop = make(map[string][]rapidShallowResponse)
	for _, m := range messages {
		if !isAgentResponseSubject(m.Subject) {
			continue
		}
		loopID := extractLoopIDFromSubject(m.Subject)
		if loopID == "" {
			continue
		}
		var p rapidShallowResponsePayload
		if err := json.Unmarshal(m.RawData, &p); err != nil {
			malformedCount++
			continue
		}
		names := make([]string, 0, len(p.Payload.Message.ToolCalls))
		for _, tc := range p.Payload.Message.ToolCalls {
			if tc.Function.Name != "" {
				names = append(names, tc.Function.Name)
			}
		}
		byLoop[loopID] = append(byLoop[loopID], rapidShallowResponse{
			Sequence:  m.Sequence,
			LoopID:    loopID,
			ToolNames: names,
		})
	}
	return byLoop, malformedCount
}

// isAgentResponseSubject mirrors the subject-prefix check used by
// walkAgentResponses, kept local to avoid coupling on detector ordering.
func isAgentResponseSubject(subject string) bool {
	const prefix = "agent.response."
	return len(subject) > len(prefix) && subject[:len(prefix)] == prefix
}

// rapidShallowResponsePayload picks just the tool-call name field from
// each agent.response. Distinct from agentResponsePayload (which projects
// content + finish_reason) because we want NAMES, not RawMessage blobs.
type rapidShallowResponsePayload struct {
	Payload struct {
		Message struct {
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"payload"`
}
