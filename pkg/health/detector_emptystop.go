package health

import (
	"sort"
	"strconv"
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
//	len(message.tool_calls) == 0  // null and [] are equivalent
//	message.content == ""
//	there exists an earlier agent.response in the same loop with
//	  len(message.tool_calls) > 0
//
// Severity is critical: an empty stop after tool calls means the loop
// did not complete its work, and the bundle reader should treat it as
// a hard failure shape rather than a warning to investigate later.
type EmptyStopAfterToolCalls struct{}

// EmptyStopShape is the Diagnosis.Shape value for this detector.
const EmptyStopShape = "EmptyStopAfterToolCalls"

// Name implements Detector.
func (EmptyStopAfterToolCalls) Name() string { return EmptyStopShape }

// Run implements Detector. Pure; no I/O.
func (EmptyStopAfterToolCalls) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}
	byLoop, malformed := walkAgentResponses(b.Messages)

	type seqDiag struct {
		seq  int64
		diag Diagnosis
	}
	var matches []seqDiag
	for _, entries := range byLoop {
		// walkAgentResponses returned each loop's slice sorted
		// ascending by sequence — chronological order. That's
		// load-bearing for the "prior tool_call" predicate below.
		seenToolCall := false
		for _, r := range entries {
			if r.HasToolCalls() {
				seenToolCall = true
				continue
			}
			if !seenToolCall {
				continue
			}
			if !r.IsStop() || r.Content != "" {
				continue
			}
			matches = append(matches, seqDiag{
				seq: r.Sequence,
				diag: Diagnosis{
					Shape:    EmptyStopShape,
					Severity: SeverityCritical,
					Evidence: []EvidenceRef{
						{
							Kind:  EvidenceAgentResponse,
							ID:    strconv.FormatInt(r.Sequence, 10),
							Field: "finish_reason",
							Value: r.FinishReason,
						},
					},
					Remediation: "Model returned finish_reason=stop with no tool_calls and empty content after a prior tool_call in the same loop. Likely an empty turn that wedges the loop; on retry, inject the failure context per feedback_retries_must_inject_failure_context.md so the model knows the prior turn was rejected.",
					MemoryRef:   "feedback_retries_must_inject_failure_context.md",
				},
			})
		}
	}
	// Numeric sort by sequence so fixture-driven tests can pin order
	// without flake. Lex-sort would interleave 1103 between 110 and
	// 111 — silent across small fixtures, broken at scale.
	sort.Slice(matches, func(i, j int) bool { return matches[i].seq < matches[j].seq })

	out := make([]Diagnosis, 0, len(matches)+1)
	for _, m := range matches {
		out = append(out, m.diag)
	}
	if malformed > 0 {
		out = append(out, undeterminedFromMalformed(EmptyStopShape, malformed))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
