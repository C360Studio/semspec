package health

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// RepeatToolFailure detects an agent loop that calls the same tool with the
// same error response three or more times in a row without resolution. The
// shape signals "the model isn't reading the validator/tool error" — a
// classic instruction-following failure that burns iterations and (on
// loops with hard caps) wedges the entire workflow when budget exhausts.
//
// The 2026-05-03 v4 regression caught this on the reviewer role:
// qwen3-coder-next correctly chose verdict="rejected" but consistently
// omitted rejection_type from submit_work args. The validator returned
// "rejection_type is required when verdict is rejected — must be one of:
// fixable, restructure" 35+ times across 5 reviewer loops, each retry
// re-submitting the exact same shape. The agent's tool-result error
// surface received the message every time; the model ignored it every
// time. Five loops hit either the 50-iter cap or context-deadline
// exhaustion.
//
// Detector behavior is deliberately conservative — three consecutive
// same-shape failures within one loop is the trigger. One repeat is
// noise, two is bad luck, three is a wedge. We use prefix-matching on
// the error string so the detector groups by failure CLASS rather than
// exact message; small variations in error wording (line numbers,
// truncation) shouldn't reset the counter.
//
// Severity is critical because the loop is producing zero forward progress
// and will hit its iter/deadline cap if not interrupted. The remediation
// points at validator hardening (auto-fill defaults) or persona-fix
// (example-anchoring bias) rather than retry-with-feedback, since the
// agent already has the feedback and is ignoring it.
type RepeatToolFailure struct{}

// RepeatToolFailureShape is the Diagnosis.Shape value for this detector.
const RepeatToolFailureShape = "RepeatToolFailure"

// repeatThreshold is how many consecutive same-class failures within a
// single loop trip the detector. Three balances false-positive risk
// (occasional duplicate errors during normal exploration) with
// false-negative risk (silent budget burn on a stuck loop).
const repeatThreshold = 3

// Name implements Detector.
func (RepeatToolFailure) Name() string { return RepeatToolFailureShape }

// Run implements Detector. Pure; no I/O.
func (RepeatToolFailure) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}

	// Pre-pass: build call_id → tool name from tool.execute dispatches
	// so we can label the failure by the tool that produced it. Same
	// shape as detector_graphtoolfailure for consistency.
	callIDToName := make(map[string]string)
	for _, m := range b.Messages {
		if !strings.HasPrefix(m.Subject, "tool.execute.") {
			continue
		}
		var env struct {
			Payload struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(m.RawData, &env); err != nil {
			continue
		}
		if env.Payload.ID != "" && env.Payload.Name != "" {
			callIDToName[env.Payload.ID] = env.Payload.Name
		}
	}

	// Walk tool.result entries in chronological order, tracking the
	// running streak per (loop_id, tool_name, error_class) tuple.
	// Reset the streak on a same-tool success (same loop_id) or a
	// different error class. Emit one diagnosis per loop+tool when
	// the streak hits the threshold; subsequent failures on the same
	// streak DON'T re-emit — once is enough for the operator.

	type streakKey struct {
		loopID  string
		tool    string
		errorCl string
	}
	type streakState struct {
		count    int
		firstSeq int64 // sequence of the first failure in the streak
		lastSeq  int64 // sequence of the most recent failure
	}
	streaks := make(map[streakKey]*streakState)
	emitted := make(map[streakKey]bool)
	malformed := 0

	type seqDiag struct {
		seq  int64
		diag Diagnosis
	}
	var matches []seqDiag

	// b.Messages is not guaranteed to be in chronological order, so
	// build an index sorted by sequence to walk deterministically.
	type indexed struct {
		idx int
		seq int64
	}
	order := make([]indexed, 0, len(b.Messages))
	for i, m := range b.Messages {
		if strings.HasPrefix(m.Subject, "tool.result") {
			order = append(order, indexed{idx: i, seq: m.Sequence})
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].seq < order[j].seq })

	for _, o := range order {
		m := b.Messages[o.idx]
		var env struct {
			Payload struct {
				CallID string `json:"call_id"`
				Name   string `json:"name"`
				Error  string `json:"error"`
				LoopID string `json:"loop_id"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(m.RawData, &env); err != nil {
			malformed++
			continue
		}
		toolName := env.Payload.Name
		if toolName == "" {
			toolName = callIDToName[env.Payload.CallID]
		}
		if toolName == "" || env.Payload.LoopID == "" {
			// Can't attribute the failure to a loop+tool — skip rather
			// than guess. A bundle missing this metadata gets reported
			// as undetermined at the end.
			if env.Payload.Error != "" {
				malformed++
			}
			continue
		}

		if env.Payload.Error == "" {
			// Success on this tool resets every streak in this loop
			// for this tool, regardless of error class. The model made
			// progress; whatever was wedging it is no longer wedging.
			for k := range streaks {
				if k.loopID == env.Payload.LoopID && k.tool == toolName {
					delete(streaks, k)
				}
			}
			continue
		}

		errorCl := classifyError(env.Payload.Error)
		key := streakKey{
			loopID:  env.Payload.LoopID,
			tool:    toolName,
			errorCl: errorCl,
		}

		// A different error class on the same (loop, tool) means the
		// model's behavior changed — reset competing streaks.
		for k := range streaks {
			if k.loopID == key.loopID && k.tool == key.tool && k.errorCl != key.errorCl {
				delete(streaks, k)
			}
		}

		st, ok := streaks[key]
		if !ok {
			st = &streakState{firstSeq: m.Sequence}
			streaks[key] = st
		}
		st.count++
		st.lastSeq = m.Sequence

		if st.count == repeatThreshold && !emitted[key] {
			emitted[key] = true
			matches = append(matches, seqDiag{
				seq: st.lastSeq,
				diag: Diagnosis{
					Shape:    RepeatToolFailureShape,
					Severity: SeverityCritical,
					Evidence: []EvidenceRef{
						{
							Kind:  EvidenceLoopEntry,
							ID:    key.loopID,
							Field: "tool",
							Value: key.tool,
						},
						{
							Kind:  EvidenceLogLine,
							ID:    strconv.FormatInt(st.firstSeq, 10),
							Field: "first_failure",
							Value: env.Payload.Error,
						},
						{
							Kind:  EvidenceLogLine,
							ID:    strconv.FormatInt(st.lastSeq, 10),
							Field: "repeat_count",
							Value: strconv.Itoa(repeatThreshold),
						},
					},
					Remediation: "Loop is calling tool " + key.tool + " with the same error class (" + key.errorCl + ") " + strconv.Itoa(repeatThreshold) + "+ times in a row — the model is not reading the tool's error response. Classic instruction-following failure under example-anchoring bias. Fix shapes, in order: (a) auto-fill / soften the validator if the missing field has an obvious safe default; (b) hoist the missing field into the tool's persona JSON example as a populated first-class key (prose rules lose to JSON examples for mid-tier models); (c) inject the validator error with a 'RETRY HINT:' prefix so it stands out from happy-path tool results. Continuing the run wastes tokens and budget — terminate or remediate.",
					MemoryRef:   "feedback_failure_mode_taxonomy.md",
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
		out = append(out, undeterminedFromMalformed(RepeatToolFailureShape, malformed))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// classifyError reduces an error message to a stable class so streak
// detection groups by FAILURE TYPE rather than exact wording. Validator
// errors typically lead with a stable prefix ("validation failed: ..."
// or "rejection_type is required when ...") followed by variable
// content (line numbers, types, names). We take the first 60 characters
// after a known prefix as the class — long enough to distinguish error
// shapes, short enough to absorb the variable content.
//
// Tuning: if a real wedge fails to trip the detector because variable
// content is in the first 60 chars, the cure is to extend known prefix
// extraction here, not to lower the threshold.
func classifyError(msg string) string {
	msg = strings.TrimSpace(msg)
	const maxClass = 60
	// Strip a common "validation failed: " prefix to expose the real
	// failure class. Other transports may add their own prefixes;
	// extend here as failure modes accumulate.
	for _, prefix := range []string{
		"validation failed: ",
	} {
		if strings.HasPrefix(msg, prefix) {
			msg = msg[len(prefix):]
			break
		}
	}
	if len(msg) > maxClass {
		msg = msg[:maxClass]
	}
	return msg
}
