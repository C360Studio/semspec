// Package parseincident emits ADR-035 parse.incident triples to the
// SKG when a parse-checkpoint result indicates a quirk fired or the
// response was rejected. Triples use the vocabulary predicates from
// vocabulary/observability so operators can query incident rates by
// (role, model, prompt_version) — the partition keys called out in
// ADR-035 §3.
//
// Phase 2 of the named-quirks list landing per ADR-035 audit: callers
// migrate from ExtractJSON → ParseStrict and feed the ParseResult into
// Emit alongside their per-call context. Phase 1 (per-fire counters +
// structured logs) remains in place; this package adds the per-call
// SKG-queryable signal on top.
//
// Emit is best-effort. Triple-write failures are returned to the caller
// so they can log; callers should NOT fail their loop on them — the
// telemetry is observability, not gating.
package parseincident

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/c360studio/semspec/vocabulary/observability"
)

// MaxRawResponseBytes caps the raw-response excerpt stored on each
// incident triple. Wedged loops can emit 50 KiB+ chain-of-thought
// blocks; storing the full response on a triple is heavier than the
// audit value justifies. When the original response exceeds the cap,
// the truncated text is stored alongside an `llm.parse.raw_response_truncated`
// = true predicate so a reader knows they're looking at a clipped view.
const MaxRawResponseBytes = 4096

// IncidentContext carries the per-call context that distinguishes
// incidents across roles, models, prompts, and call IDs. CallID is
// the LLM loop ID (loop.ID) that produced the response. Empty fields
// are skipped at write time — no sentinel-value triples land in the
// graph.
type IncidentContext struct {
	CallID        string // loop.ID — required; emit returns an error when empty.
	Role          string // "planner" | "developer" | "code-reviewer" | ...
	Model         string // endpoint name from the model registry (loop.Model).
	PromptVersion string // prompt-pack revision; "" when the caller has no version metadata.
}

// IncidentEvent describes WHAT happened at the parse checkpoint. The
// caller chooses Outcome based on its own parse-result interpretation:
// strict means the parse returned typed output with no quirks fired;
// tolerated_quirk means at least one named quirk applied; rejected
// means the parse produced no usable output and a RETRY HINT was
// (or will be) injected.
type IncidentEvent struct {
	Checkpoint  string   // observability.CheckpointResponseParse | CheckpointToolCall
	Outcome     string   // observability.OutcomeStrict | OutcomeToleratedQuirk | OutcomeRejected
	Quirks      []string // Empty unless Outcome=tolerated_quirk; one entry per fired quirk.
	Reason      string   // Human-readable; for rejected, this is the RETRY HINT text.
	RawResponse string   // Unmodified LLM output. Truncated at MaxRawResponseBytes by Emit; truncation flagged via a separate predicate.
}

// tripleWriter is the minimal interface Emit needs from the graph
// write surface. graphutil.TripleWriter satisfies it implicitly. The
// interface is unexported so a second implementation outside this
// package would have to mirror it consciously rather than satisfy by
// accident.
type tripleWriter interface {
	WriteTriple(ctx context.Context, entityID, predicate string, object any) error
}

// Emit writes the parse.incident triple set for a checkpoint result.
// Returns the incident node ID on success, or "" when no incident was
// emitted (strict outcome or nil writer).
//
// Triple write order matters: the relation triple
// `(call_id) -[parse.incident]-> (incident_node)` is written first so
// a partial-write failure still leaves the relation findable. Each
// attribute predicate (checkpoint, outcome, role, etc.) follows;
// individual write failures bubble up after all writes are attempted.
//
// Strict outcomes are no-ops — the audit's "tolerance is the
// deviation worth recording" framing means strict parses don't need
// per-call telemetry beyond the existing happy-path Prom counters.
//
// Incident node IDs are deterministic — `<call_id>:parse:<checkpoint>`
// — so retry replays of the same loop produce idempotent SKG
// state rather than orphan-incident accumulation.
func Emit(ctx context.Context, tw tripleWriter, ic IncidentContext, ev IncidentEvent) (string, error) {
	if tw == nil {
		return "", nil
	}
	if ev.Outcome == observability.OutcomeStrict || ev.Outcome == "" {
		return "", nil
	}
	if ic.CallID == "" {
		return "", fmt.Errorf("parseincident: CallID required")
	}
	if ev.Checkpoint == "" {
		return "", fmt.Errorf("parseincident: Checkpoint required")
	}

	incidentID := fmt.Sprintf("%s:parse:%s", ic.CallID, ev.Checkpoint)

	// Relation first so a partial write still leaves the incident
	// findable from the call entity.
	if err := tw.WriteTriple(ctx, ic.CallID, observability.Incident, incidentID); err != nil {
		return "", fmt.Errorf("write parse.incident relation: %w", err)
	}

	// Required attribute predicates (always populated).
	required := []predicateWrite{
		{observability.Checkpoint, ev.Checkpoint},
		{observability.Outcome, ev.Outcome},
	}
	for _, w := range required {
		if err := tw.WriteTriple(ctx, incidentID, w.predicate, w.object); err != nil {
			return incidentID, fmt.Errorf("write %s: %w", w.predicate, err)
		}
	}

	// Optional attribute predicates — skipped on empty values so the
	// graph doesn't accumulate sentinel-value triples that need a
	// later cleanup pass.
	optional := []predicateWrite{
		{observability.Reason, ev.Reason},
		{observability.Role, ic.Role},
		{observability.Model, ic.Model},
		{observability.PromptVersion, ic.PromptVersion},
	}
	for _, w := range optional {
		if w.object == "" {
			continue
		}
		if err := tw.WriteTriple(ctx, incidentID, w.predicate, w.object); err != nil {
			return incidentID, fmt.Errorf("write %s: %w", w.predicate, err)
		}
	}

	// Raw response — always retained on rejected/tolerated_quirk per
	// ADR-035 §3, but capped at MaxRawResponseBytes with a separate
	// truncation flag so readers know whether they're seeing the full
	// or clipped text.
	if ev.RawResponse != "" {
		body, truncated := truncateUTF8Safe(ev.RawResponse, MaxRawResponseBytes)
		if err := tw.WriteTriple(ctx, incidentID, observability.RawResponse, body); err != nil {
			return incidentID, fmt.Errorf("write raw_response: %w", err)
		}
		if truncated {
			if err := tw.WriteTriple(ctx, incidentID, observability.RawResponseTruncated, true); err != nil {
				return incidentID, fmt.Errorf("write raw_response_truncated: %w", err)
			}
		}
	}

	// Multi-valued quirk predicates — one triple per fired quirk so
	// the IncidentRateExceeded detector (ADR-035 step 5) can slice by
	// individual quirk ID without parsing concatenated values.
	for _, q := range ev.Quirks {
		if q == "" {
			continue
		}
		if err := tw.WriteTriple(ctx, incidentID, observability.Quirk, q); err != nil {
			return incidentID, fmt.Errorf("write quirk %q: %w", q, err)
		}
	}

	return incidentID, nil
}

// predicateWrite pairs a predicate constant with its object value for
// table-driven writes inside Emit.
type predicateWrite struct {
	predicate string
	object    any
}

// EmitForResult is a convenience wrapper that derives the IncidentEvent
// from a (quirks, parseErr) pair — the most common shape callers face
// at a parse-checkpoint boundary. Three branches:
//
//   - parseErr != nil          → OutcomeRejected, Reason = parseErr.Error()
//   - len(quirks) > 0          → OutcomeToleratedQuirk, Quirks = quirks
//   - otherwise                → OutcomeStrict (Emit no-op)
//
// rawResponse is always carried through unchanged — the truncation cap
// is applied inside Emit. checkpoint is one of
// observability.CheckpointResponseParse | CheckpointToolCall.
//
// Returns the same (incidentID, error) tuple as Emit. nil tw is a
// no-op.
func EmitForResult(ctx context.Context, tw tripleWriter, ic IncidentContext, checkpoint string, quirks []string, rawResponse string, parseErr error) (string, error) {
	ev := IncidentEvent{
		Checkpoint:  checkpoint,
		RawResponse: rawResponse,
	}
	switch {
	case parseErr != nil:
		ev.Outcome = observability.OutcomeRejected
		ev.Reason = parseErr.Error()
	case len(quirks) > 0:
		ev.Outcome = observability.OutcomeToleratedQuirk
		ev.Quirks = quirks
	default:
		ev.Outcome = observability.OutcomeStrict
	}
	return Emit(ctx, tw, ic, ev)
}

// truncateUTF8Safe shortens s to at most maxBytes bytes without
// splitting a multibyte rune. Returns the (possibly truncated) string
// and a truncated flag indicating whether the input exceeded maxBytes.
//
// Graph stores can reject mid-rune bytes as invalid UTF-8; walking
// back to a rune boundary produces a clipped-but-valid string.
func truncateUTF8Safe(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	// Walk back from maxBytes until we land on a valid rune boundary.
	i := maxBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i], true
}
