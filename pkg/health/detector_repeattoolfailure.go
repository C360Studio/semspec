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
// streakKey identifies a same-(loop, tool, error-class) failure streak.
type repeatStreakKey struct {
	loopID  string
	tool    string
	errorCl string
}

// streakState tracks the running streak count for a repeatStreakKey.
type repeatStreakState struct {
	count    int
	firstSeq int64 // sequence of the first failure in the streak
	lastSeq  int64 // sequence of the most recent failure
}

// repeatToolResult is the parsed shape of a tool.result payload that
// the repeat-failure walker actually consumes. Defining it once keeps
// the inline-anonymous-struct churn out of the walk loop.
type repeatToolResult struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Error  string `json:"error"`
	LoopID string `json:"loop_id"`
}

// Run scans the bundle's tool.result entries and emits one diagnosis
// per (loop_id, tool_name, error_class) tuple whose failure streak hits
// repeatThreshold. The walk is chronological by message Sequence;
// streaks reset on a same-tool success or a different error class for
// the same (loop, tool) pair. Idempotent — re-running on the same
// bundle yields the same diagnoses in the same order.
func (RepeatToolFailure) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}

	// Pre-pass: build call_id → tool name from tool.execute dispatches
	// so we can label the failure by the tool that produced it. Same
	// shape as detector_graphtoolfailure for consistency.
	callIDToName := buildToolCallIDIndex(b.Messages)

	// Walk tool.result entries in chronological order, tracking the
	// running streak per (loop_id, tool_name, error_class) tuple.
	// Reset the streak on a same-tool success (same loop_id) or a
	// different error class. Emit one diagnosis per loop+tool when
	// the streak hits the threshold; subsequent failures on the same
	// streak DON'T re-emit — once is enough for the operator.
	streaks := make(map[repeatStreakKey]*repeatStreakState)
	emitted := make(map[repeatStreakKey]bool)

	type seqDiag struct {
		seq    int64
		loopID string
		diag   Diagnosis
	}
	var matches []seqDiag

	for _, idx := range chronoToolResultIndices(b.Messages) {
		m := b.Messages[idx]
		var env struct {
			Payload repeatToolResult `json:"payload"`
		}
		if err := json.Unmarshal(m.RawData, &env); err != nil {
			// A tool.result payload that won't decode is an OBSERVER-side
			// condition (e.g. beta.107 payload summarization truncates the
			// body), NOT an agent tool failure. Skip it silently rather than
			// emit a RepeatToolFailure with an empty evidence_id (issue #179),
			// which reads as a real wedge when it is just incomplete capture.
			continue
		}
		toolName := env.Payload.Name
		if toolName == "" {
			toolName = callIDToName[env.Payload.CallID]
		}
		if toolName == "" || env.Payload.LoopID == "" {
			// Can't attribute the failure to a loop+tool — skip rather than
			// guess (an unattributable error is not actionable as a repeat).
			continue
		}

		if env.Payload.Error == "" {
			resetStreaksForTool(streaks, env.Payload.LoopID, toolName)
			continue
		}

		// Benign redelivery shape (issue #179): a redelivered duplicate loop for
		// an already-COMPLETED node hits a GC'd worktree and the sandbox returns
		// "worktree not found". The real node finished via its original loop; the
		// duplicate correctly 404s. Ignore it entirely — neither count it toward
		// a streak nor reset one.
		if isBenignWorktree404(env.Payload.Error) {
			continue
		}

		key := repeatStreakKey{
			loopID:  env.Payload.LoopID,
			tool:    toolName,
			errorCl: classifyError(env.Payload.Error),
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
			st = &repeatStreakState{firstSeq: m.Sequence}
			streaks[key] = st
		}
		st.count++
		st.lastSeq = m.Sequence

		if st.count == repeatThreshold && !emitted[key] {
			emitted[key] = true
			matches = append(matches, seqDiag{
				seq:    st.lastSeq,
				loopID: key.loopID,
				diag:   buildRepeatFailureDiagnosis(key, st, env.Payload.Error),
			})
		}
	}

	// Gate on loop terminal outcome (issue #179): a loop that ultimately
	// RECOVERED (outcome=success — e.g. a gradle exit-1 that repeated then
	// passed at iteration 47-77) must not raise a critical wedge alert in a
	// post-hoc bundle. A loop still running (no terminal outcome) keeps its
	// critical so live --bail-on detection still fires on a genuine in-progress
	// wedge.
	outcomes := loopOutcomes(b)

	sort.Slice(matches, func(i, j int) bool { return matches[i].seq < matches[j].seq })
	out := make([]Diagnosis, 0, len(matches))
	for _, m := range matches {
		if outcomes[m.loopID] == loopOutcomeSuccess {
			continue
		}
		out = append(out, m.diag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loopOutcomeSuccess is the loop-entity outcome value indicating the loop
// recovered/completed successfully.
const loopOutcomeSuccess = "success"

// isBenignWorktree404 reports whether a tool error is the benign
// "worktree not found" 404 that a redelivered duplicate loop hits when its node
// already completed via the original loop and the worktree was GC'd (issue #179).
// Matched narrowly on the sentinel phrase so genuine worktree failures (e.g. a
// worktree that never existed for a live node) are not swallowed.
func isBenignWorktree404(errMsg string) bool {
	return strings.Contains(errMsg, "worktree not found")
}

// loopOutcomes decodes b.Loops into a loopID → terminal-outcome map
// ("success", "failure", …). The map is keyed by the loop entity's `id`, which
// equals the `loop_id` stamped on each tool.result payload, so a match's loopID
// joins directly. AGENT_LOOPS also carries terminal-state marker entries that
// carry only `loop_id` (no `id` field); those decode to an empty id and are
// skipped, and a known (non-empty) outcome is never overwritten by an empty one.
// Only "success" suppresses (a recovered loop); "truncated"/"failed"/unknown
// keep their critical so a genuine wedge still fires.
func loopOutcomes(b *Bundle) map[string]string {
	out := make(map[string]string, len(b.Loops))
	for _, e := range b.Loops {
		var le struct {
			ID      string `json:"id"`
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(e.Value, &le); err != nil || le.ID == "" {
			continue
		}
		if le.Outcome != "" || out[le.ID] == "" {
			out[le.ID] = le.Outcome
		}
	}
	return out
}

// buildToolCallIDIndex walks tool.execute dispatches and returns a
// call_id → tool_name lookup so tool.result entries that omit the name
// (older payload shape) can still be attributed.
func buildToolCallIDIndex(messages []Message) map[string]string {
	idx := make(map[string]string)
	for _, m := range messages {
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
			idx[env.Payload.ID] = env.Payload.Name
		}
	}
	return idx
}

// chronoToolResultIndices returns the indices into messages of every
// tool.result entry, sorted by Sequence ascending. b.Messages is not
// guaranteed to be in chronological order so we sort by sequence to
// walk deterministically.
func chronoToolResultIndices(messages []Message) []int {
	type indexed struct {
		idx int
		seq int64
	}
	order := make([]indexed, 0, len(messages))
	for i, m := range messages {
		if strings.HasPrefix(m.Subject, "tool.result") {
			order = append(order, indexed{idx: i, seq: m.Sequence})
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].seq < order[j].seq })
	out := make([]int, len(order))
	for i, o := range order {
		out[i] = o.idx
	}
	return out
}

// resetStreaksForTool drops every streak for this (loop, tool) pair
// regardless of error class, used when the same tool succeeds in the
// same loop — the model made progress so the wedge cleared.
func resetStreaksForTool(streaks map[repeatStreakKey]*repeatStreakState, loopID, tool string) {
	for k := range streaks {
		if k.loopID == loopID && k.tool == tool {
			delete(streaks, k)
		}
	}
}

// buildRepeatFailureDiagnosis assembles the Diagnosis for a streak that
// hit the repeat threshold. firstError is the verbatim error message
// from the just-arrived (lastSeq) failure; carrying it on the
// EvidenceLogLine pinned to firstSeq is a documented oversimplification
// — the streak's first error class matched, even if the wording drifted.
// For investigators the most-recent error wording is the more useful
// surface, and pinning it to firstSeq keeps the evidence chain tied to
// the streak's start.
func buildRepeatFailureDiagnosis(key repeatStreakKey, st *repeatStreakState, latestError string) Diagnosis {
	return Diagnosis{
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
				Value: latestError,
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
	}
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
