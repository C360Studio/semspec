package recoveryhint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/c360studio/semstreams/message"

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

// RecoveryIncidentEntityType is the message.Type for tool-recovery-incident
// entities. Domain "tool-recovery-incident" names the entity class (the incident
// node IDs are `<call_id>.tool-recovery.<tool>.<hash>`). Kept local to avoid
// touching workflow/entity.go while nearby slices are editing it (issue #154); a
// future pass may register it in workflowEntityTypes for parity with LessonEntityType.
var RecoveryIncidentEntityType = message.Type{
	Domain:   "tool-recovery-incident",
	Category: "entity",
	Version:  "v1",
}

// tripleWriter is the minimal interface Emit needs from the graph
// write surface. graphutil.TripleWriter satisfies it implicitly.
type tripleWriter interface {
	WriteTriple(ctx context.Context, entityID, predicate string, object any) error
	UpsertEntity(ctx context.Context, entityType message.Type, entityID string, triples []message.Triple) error
}

// Emit writes the tool.recovery.incident entity for a recovery event.
// Returns the incident node ID on success, or "" when no incident was
// emitted (nil writer or empty outcome).
//
// The incident node is created via UpsertEntity (routes to
// create_with_triples on first write, update_with_triples on retry),
// which is metadata-bearing and survives the semstreams triple.add
// must-exist change (issue #154, slice #2). call_id is carried as a
// node attribute so the linkage to the agentic loop is preserved even
// though the loop entity has no ENTITY_STATES graph node.
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

	triples := buildIncidentTriples(incidentID, rc, re)
	if err := tw.UpsertEntity(ctx, RecoveryIncidentEntityType, incidentID, triples); err != nil {
		return "", fmt.Errorf("recoveryhint: upsert entity: %w", err)
	}

	return incidentID, nil
}

// buildIncidentTriples constructs the full []message.Triple for a
// tool-recovery-incident entity. It is a pure function so it can be tested
// independently of NATS.
//
// Required scalars (Outcome, ToolName, CallID) are always emitted. Optional
// fields (OriginalQuery, Role, Model) are emitted only when non-empty.
// Multi-valued Candidates emit one triple per candidate — never JSON-encoded —
// per feedback_no_json_in_triples.
func buildIncidentTriples(incidentID string, rc RecoveryContext, re RecoveryEvent) []message.Triple {
	triples := []message.Triple{
		{Subject: incidentID, Predicate: observability.ToolRecoveryOutcome, Object: re.Outcome},
		{Subject: incidentID, Predicate: observability.ToolRecoveryToolName, Object: rc.ToolName},
		// call_id is carried as an attribute so the linkage to the agentic loop
		// is preserved even though the loop has no ENTITY_STATES graph node.
		{Subject: incidentID, Predicate: observability.IncidentCallID, Object: rc.CallID},
	}

	// Optional attribute predicates — omit empty values so the graph does
	// not accumulate sentinel-value triples that need a later cleanup pass.
	if re.OriginalQuery != "" {
		triples = append(triples, message.Triple{Subject: incidentID, Predicate: observability.ToolRecoveryOriginalQuery, Object: re.OriginalQuery})
	}
	if rc.Role != "" {
		triples = append(triples, message.Triple{Subject: incidentID, Predicate: observability.ToolRecoveryRole, Object: rc.Role})
	}
	if rc.Model != "" {
		triples = append(triples, message.Triple{Subject: incidentID, Predicate: observability.ToolRecoveryModel, Object: rc.Model})
	}

	// Multi-valued candidate predicates — one triple per suggested candidate ID.
	for _, c := range re.Candidates {
		if c == "" {
			continue
		}
		triples = append(triples, message.Triple{Subject: incidentID, Predicate: observability.ToolRecoveryCandidate, Object: c})
	}

	return triples
}
