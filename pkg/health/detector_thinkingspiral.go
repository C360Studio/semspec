package health

import (
	"sort"
	"strconv"
)

// ThinkingSpiral detects an agent.response that finished with stop,
// no tool_calls, and a high completion-token count — the canonical
// shape of a model burning generation budget on its reasoning channel
// without producing an action.
//
// Match conditions (all must hold):
//
//	finish_reason == "stop"
//	len(message.tool_calls) == 0
//	usage.completion_tokens > ThinkingSpiralCompletionTokenThreshold
//
// Distinct from EmptyStopAfterToolCalls: that shape requires a prior
// tool_call in the same loop. ThinkingSpiral fires regardless of
// loop history because the high-token-count predicate is what makes
// it a spiral (the model spent budget reasoning, not just bailed).
//
// Severity is warning: the loop didn't advance, but the spiral is
// usually recoverable on retry with a stronger system prompt or a
// switch to a model with explicit reasoning_content channel support.
type ThinkingSpiral struct{}

// ThinkingSpiralShape is the Diagnosis.Shape value for this detector.
const ThinkingSpiralShape = "ThinkingSpiral"

// ThinkingSpiralCompletionTokenThreshold is the lower bound for
// "burned generation budget on reasoning." Sourced from the
// local-easy-v107-rerun-0930 fixture's qwen3:14b retry shape:
// completion_tokens=915 per attempt. 500 leaves headroom while still
// excluding short empty stops that genuinely just bailed.
const ThinkingSpiralCompletionTokenThreshold = 500

// Name implements Detector.
func (ThinkingSpiral) Name() string { return ThinkingSpiralShape }

// Run implements Detector. Pure; no I/O.
func (ThinkingSpiral) Run(b *Bundle) []Diagnosis {
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
		for _, r := range entries {
			if !r.IsStop() || r.HasToolCalls() {
				continue
			}
			if r.CompletionToks <= ThinkingSpiralCompletionTokenThreshold {
				continue
			}
			matches = append(matches, seqDiag{
				seq: r.Sequence,
				diag: Diagnosis{
					Shape:    ThinkingSpiralShape,
					Severity: SeverityWarning,
					Evidence: []EvidenceRef{
						{
							Kind:  EvidenceAgentResponse,
							ID:    strconv.FormatInt(r.Sequence, 10),
							Field: "usage.completion_tokens",
							Value: r.CompletionToks,
						},
					},
					Remediation: "Model finished with stop + no tool_calls + high completion_tokens (" + strconv.Itoa(r.CompletionToks) + " > " + strconv.Itoa(ThinkingSpiralCompletionTokenThreshold) + " threshold). Generation budget went to reasoning, not to an action. Try a model with native reasoning_content channel support, or strengthen the persona's tool-use mandate.",
				},
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].seq < matches[j].seq })

	out := make([]Diagnosis, 0, len(matches)+1)
	for _, m := range matches {
		out = append(out, m.diag)
	}
	if malformed > 0 {
		out = append(out, undeterminedFromMalformed(ThinkingSpiralShape, malformed))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
