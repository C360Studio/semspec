package recoveryhint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/c360studio/semspec/vocabulary/observability"
)

// RecoveryContext carries the per-call context that distinguishes
// recovery events across roles, models, tools, and call IDs. CallID
// is the LLM loop ID making the tool call. ToolName is the registered
// agentic-tool name (e.g. "graph_query") that triggered the recovery.
type RecoveryContext struct {
	CallID   string // loop.ID — required.
	Role     string // "planner" | "developer" | "lesson-decomposer" | ...
	Model    string // endpoint name from the registry.
	ToolName string // tool that triggered recovery (e.g. "graph_query") — required.
}

// RecoveryEvent describes WHAT happened at the recovery boundary. The
// caller chooses Outcome based on whether candidates were produced;
// "suggested" emits Candidate triples, "not_suggested" emits an
// incident with no candidate triples (signal that recovery was
// attempted and failed — important for tuning prefix heuristics).
type RecoveryEvent struct {
	Outcome       string   // observability.ToolRecoveryOutcomeSuggested | OutcomeNotSuggested
	OriginalQuery string   // the query/argument that failed the original lookup
	Candidates    []string // empty when Outcome=not_suggested
}

// tripleWriter is the minimal interface Emit needs from the graph
// write surface. graphutil.TripleWriter satisfies it implicitly.
type tripleWriter interface {
	WriteTriple(ctx context.Context, entityID, predicate string, object any) error
}

// Emit writes the tool.recovery.incident triple set for a recovery
// event. Returns the incident node ID on success, or "" when no
// incident was emitted (nil writer or empty outcome).
//
// Triple write order matters: the relation triple
// `(call_id) -[tool.recovery.incident]-> (incident_node)` is written
// first so a partial-write failure still leaves the relation findable
// from the call entity. Each attribute predicate follows; individual
// write failures bubble up after all writes are attempted.
//
// Incident node IDs are deterministic — the suffix is a SHA-256 of
// the OriginalQuery so retry replays of the same loop on the same
// failed query produce idempotent SKG state. This matters more here
// than for parse incidents because a single loop can trigger many
// recoveries on different IDs (today's wedge: 28 occurrences in one
// loop), so a checkpoint-style fixed suffix would collide.
//
// Separator is `.` (period) because NATS KV keys allow only
// [a-zA-Z0-9_-./=]. The original `:` choice shipped alongside
// parseincident.Emit and was caught on first real-LLM run when
// graph-ingest CAS writes failed with "invalid key". See
// parseincident.Emit godoc for the cross-cutting rationale.
func Emit(ctx context.Context, tw tripleWriter, rc RecoveryContext, re RecoveryEvent) (string, error) {
	if tw == nil {
		return "", nil
	}
	if re.Outcome == "" {
		return "", nil
	}
	if rc.CallID == "" {
		return "", fmt.Errorf("recoveryhint: CallID required")
	}
	if rc.ToolName == "" {
		return "", fmt.Errorf("recoveryhint: ToolName required")
	}

	hash := sha256.Sum256([]byte(re.OriginalQuery))
	incidentID := fmt.Sprintf("%s.tool-recovery.%s.%s", rc.CallID, rc.ToolName, hex.EncodeToString(hash[:8]))

	// Relation first.
	if err := tw.WriteTriple(ctx, rc.CallID, observability.ToolRecoveryIncident, incidentID); err != nil {
		return "", fmt.Errorf("write tool.recovery.incident relation: %w", err)
	}

	// Required attribute predicates.
	required := []predicateWrite{
		{observability.ToolRecoveryOutcome, re.Outcome},
		{observability.ToolRecoveryToolName, rc.ToolName},
	}
	for _, w := range required {
		if err := tw.WriteTriple(ctx, incidentID, w.predicate, w.object); err != nil {
			return incidentID, fmt.Errorf("write %s: %w", w.predicate, err)
		}
	}

	// Optional attributes — empty values skipped so the graph doesn't
	// accumulate sentinel triples that need a later cleanup pass.
	optional := []predicateWrite{
		{observability.ToolRecoveryOriginalQuery, re.OriginalQuery},
		{observability.ToolRecoveryRole, rc.Role},
		{observability.ToolRecoveryModel, rc.Model},
	}
	for _, w := range optional {
		if w.object == "" {
			continue
		}
		if err := tw.WriteTriple(ctx, incidentID, w.predicate, w.object); err != nil {
			return incidentID, fmt.Errorf("write %s: %w", w.predicate, err)
		}
	}

	// Multi-valued candidate predicates — one triple per suggested ID.
	for _, c := range re.Candidates {
		if c == "" {
			continue
		}
		if err := tw.WriteTriple(ctx, incidentID, observability.ToolRecoveryCandidate, c); err != nil {
			return incidentID, fmt.Errorf("write candidate %q: %w", c, err)
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
