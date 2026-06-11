// Package graphutil provides shared graph write helpers used across orchestrator
// components. Centralising writeTriple and portSubject here removes the
// verbatim copy that previously existed in review-orchestrator,
// execution-orchestrator, scenario-executor, plan-coordinator, and
// plan-decision-handler.
package graphutil

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// TripleWriter provides graph triple write capabilities via NATS request/reply.
// It wraps a NATS client and logger, eliminating per-component boilerplate for
// the writeTriple pattern.
//
// Usage:
//
//	tw := &graphutil.TripleWriter{
//	    NATSClient:    deps.NATSClient,
//	    Logger:        logger,
//	    ComponentName: "my-component",
//	}
//	if err := tw.WriteTriple(ctx, entityID, wf.Phase, "generating"); err != nil {
//	    // handle error
//	}
type TripleWriter struct {
	NATSClient    *natsclient.Client
	Logger        *slog.Logger
	ComponentName string
}

// WriteTriple sends an AddTripleRequest to graph-ingest via NATS request/reply.
// graph-ingest handles CAS writes to ENTITY_STATES KV and returns a KVRevision.
//
// Pass numeric values (int, int64, float64) directly — do not format them as
// strings. The graph store accepts any JSON-serialisable object value.
//
// Returns an error on failure; callers should error-check critical triples
// (e.g., workflow.phase) and can safely ignore non-critical ones with _.
func (tw *TripleWriter) WriteTriple(ctx context.Context, entityID, predicate string, object any) error {
	req := graph.AddTripleRequest{
		Triple: message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     tw.ComponentName,
			Timestamp:  time.Now(),
			Confidence: 1.0,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		tw.Logger.Warn("Failed to marshal triple request", "predicate", predicate, "error", err)
		return fmt.Errorf("marshal triple request: %w", err)
	}

	if tw.NATSClient == nil {
		return nil
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.mutation.triple.add", data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		tw.Logger.Warn("Triple write request failed",
			"predicate", predicate, "entity_id", entityID, "error", err)
		return fmt.Errorf("triple write request: %w", err)
	}

	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		tw.Logger.Warn("Failed to unmarshal triple response", "predicate", predicate, "error", err)
		return fmt.Errorf("unmarshal triple response: %w", err)
	}

	if !resp.Success {
		tw.Logger.Warn("Triple write rejected by graph-ingest",
			"predicate", predicate, "entity_id", entityID, "error", resp.Error)
		return fmt.Errorf("triple write rejected: %s", resp.Error)
	}

	return nil
}

// RemoveTriple removes ALL triples for (entityID, predicate) via
// graph.mutation.triple.remove. Idempotent: removing a predicate that does not
// exist (or an absent entity) is a no-op success.
func (tw *TripleWriter) RemoveTriple(ctx context.Context, entityID, predicate string) error {
	req := graph.RemoveTripleRequest{Subject: entityID, Predicate: predicate}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal remove request: %w", err)
	}
	if tw.NATSClient == nil {
		return nil
	}
	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.mutation.triple.remove", data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		tw.Logger.Warn("Triple remove request failed",
			"predicate", predicate, "entity_id", entityID, "error", err)
		return fmt.Errorf("triple remove request: %w", err)
	}
	var resp graph.RemoveTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal remove response: %w", err)
	}
	if !resp.Success {
		tw.Logger.Warn("Triple remove rejected by graph-ingest",
			"predicate", predicate, "entity_id", entityID, "error", resp.Error)
		return fmt.Errorf("triple remove rejected: %s", resp.Error)
	}
	return nil
}

// UpdateTriple upserts a SINGLE-VALUED predicate: it removes any existing
// triples for (entityID, predicate), then writes the new value, so the entity
// holds exactly one value per predicate rather than accumulating every
// historical value.
//
// WHY this exists: graph-ingest's AddTriple is APPEND-ONLY
// (entity.Triples = append(...), never replace-by-(subject,predicate)). Writing
// a scalar with WriteTriple on every mutation therefore grows the entity
// unboundedly. A retry-heavy plan-prep run bloated the plan entity past the
// 1 MiB KV value cap (2026-06-07), after which every write — including the
// recovery PlanDecision — was rejected and the plan went terminal. UpdateTriple
// bounds the entity to its field set.
//
// Use UpdateTriple for scalar fields that change over an entity's lifetime
// (status, title, timestamps, last_error, review fields). Keep WriteTriple
// (append) for genuinely multi-valued predicates — list members and edges
// (scope entries, trace IDs, affected requirement IDs, capability links).
func (tw *TripleWriter) UpdateTriple(ctx context.Context, entityID, predicate string, object any) error {
	// Best-effort remove: a failed remove only risks a lingering stale value, it
	// must not block recording the latest. The common path removes cleanly and
	// keeps the entity bounded.
	if err := tw.RemoveTriple(ctx, entityID, predicate); err != nil {
		tw.Logger.Warn("UpdateTriple remove step failed (continuing with add)",
			"predicate", predicate, "entity_id", entityID, "error", err)
	}
	return tw.WriteTriple(ctx, entityID, predicate, object)
}

// ReplaceTripleList replaces ALL values of a multi-valued predicate with the
// given set: it removes every existing (entityID, predicate) triple, then
// appends one per value. Use for list/edge predicates (scope entries, trace
// IDs, open questions) so re-writing the list on each mutation does not append
// duplicates and grow the entity without bound. Pass nil/empty to clear it.
func (tw *TripleWriter) ReplaceTripleList(ctx context.Context, entityID, predicate string, objects []string) error {
	if err := tw.RemoveTriple(ctx, entityID, predicate); err != nil {
		tw.Logger.Warn("ReplaceTripleList remove step failed (continuing)",
			"predicate", predicate, "entity_id", entityID, "error", err)
	}
	var firstErr error
	for _, o := range objects {
		if err := tw.WriteTriple(ctx, entityID, predicate, o); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReadEntity fetches an entity's triples from ENTITY_STATES via graph-ingest
// NATS request/reply. Returns a map of predicate → object (as string).
// Non-string objects are JSON-encoded.
func (tw *TripleWriter) ReadEntity(ctx context.Context, entityID string) (map[string]string, error) {
	if tw.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not configured")
	}

	reqData, err := json.Marshal(map[string]string{"id": entityID})
	if err != nil {
		return nil, fmt.Errorf("marshal entity query: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.ingest.query.entity", reqData, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("query entity %s: %w", entityID, err)
	}

	var entity graph.EntityState
	if err := json.Unmarshal(respData, &entity); err != nil {
		return nil, fmt.Errorf("unmarshal entity %s: %w", entityID, err)
	}

	result := make(map[string]string, len(entity.Triples))
	for _, t := range entity.Triples {
		switch v := t.Object.(type) {
		case string:
			result[t.Predicate] = v
		case float64:
			if v == float64(int64(v)) {
				result[t.Predicate] = fmt.Sprintf("%d", int64(v))
			} else {
				result[t.Predicate] = fmt.Sprintf("%g", v)
			}
		case bool:
			result[t.Predicate] = fmt.Sprintf("%t", v)
		default:
			data, _ := json.Marshal(v)
			result[t.Predicate] = string(data)
		}
	}

	return result, nil
}

// ReadEntitiesByPrefix fetches all entities matching an ID prefix from
// ENTITY_STATES via graph-ingest. Returns a map of entityID → predicate map.
// Each predicate maps to the last observed value; for multi-valued predicates
// use ReadEntitiesByPrefixMulti.
func (tw *TripleWriter) ReadEntitiesByPrefix(ctx context.Context, prefix string, limit int) (map[string]map[string]string, error) {
	if tw.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not configured")
	}

	if limit <= 0 {
		limit = 100
	}

	reqData, err := json.Marshal(map[string]any{"prefix": prefix, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("marshal prefix query: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.ingest.query.prefix", reqData, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("query prefix %s: %w", prefix, err)
	}

	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal prefix response: %w", err)
	}

	result := make(map[string]map[string]string, len(resp.Entities))
	for _, entity := range resp.Entities {
		triples := make(map[string]string, len(entity.Triples))
		for _, t := range entity.Triples {
			switch v := t.Object.(type) {
			case string:
				triples[t.Predicate] = v
			case float64:
				if v == float64(int64(v)) {
					triples[t.Predicate] = fmt.Sprintf("%d", int64(v))
				} else {
					triples[t.Predicate] = fmt.Sprintf("%g", v)
				}
			case bool:
				triples[t.Predicate] = fmt.Sprintf("%t", v)
			default:
				data, _ := json.Marshal(v)
				triples[t.Predicate] = string(data)
			}
		}
		result[entity.ID] = triples
	}

	return result, nil
}

// subjectEntityUpdateWithTriples is the NATS request/reply subject for the
// atomic update-entity-with-triples handler in graph-ingest.
// Source: semstreams processor/graph-ingest/mutations.go SubjectEntityUpdateWithTriples.
const subjectEntityUpdateWithTriples = "graph.mutation.entity.update_with_triples"

// subjectEntityCreateWithTriples is the NATS request/reply subject for the
// atomic create-entity-with-triples handler in graph-ingest.
// Source: semstreams processor/graph-ingest/mutations.go SubjectEntityCreateWithTriples.
const subjectEntityCreateWithTriples = "graph.mutation.entity.create_with_triples"

// upsertOutcome is a typed result from the internal send helpers, used to
// drive the create/retry loop without mixing response parsing with flow logic.
// Extracted as a type so the routing in upsertEntityVia is unit-testable
// without a live NATS server.
type upsertOutcome int

const (
	upsertDone        upsertOutcome = iota // Success (or Degraded-committed) — caller is done.
	upsertNeedCreate                       // update returned entity_not_found — fall back to create.
	upsertRetryUpdate                      // create returned entity_already_exists — retry update.
)

// String returns the name of the outcome for human-readable error messages.
func (o upsertOutcome) String() string {
	switch o {
	case upsertDone:
		return "done"
	case upsertNeedCreate:
		return "need_create"
	case upsertRetryUpdate:
		return "retry_update"
	default:
		return fmt.Sprintf("upsertOutcome(%d)", int(o))
	}
}

// upsertSender is a seam for unit-testing the routing logic in upsertEntityVia
// without a live NATS connection. The production path wires in
// sendUpdateWithTriples / sendCreateWithTriples; tests inject stubs.
type upsertSender struct {
	update func(ctx context.Context, req graph.UpdateEntityWithTriplesRequest) (upsertOutcome, error)
	create func(ctx context.Context, req graph.CreateEntityWithTriplesRequest) (upsertOutcome, error)
}

// UpsertEntity writes a full set of triples for an entity using the
// replace-own-predicates, preserve-foreign semantics.
//
// WHY this exists: semstreams beta.90 (PR #180) changed graph.ingest.entity's
// handler from CreateEntity (full-replace Put) to MergeEntity, which does a
// raw append: existing.Triples = append(existing.Triples, entity.Triples...).
// Components that republish the same mutable entity on every phase or status
// change therefore accumulate unbounded duplicate triples, and stale single-valued
// predicates (e.g. status) coexist with new ones — the rule engine reads
// first-match while lifecycle reads last-match, so the divergence is a silent
// correctness bug.
//
// The correct primitive at beta.103 is graph.mutation.entity.update_with_triples,
// whose handler runs inside a single UpdateWithRetry CAS. The ACTUAL mechanism
// (mutations.go ExpectedRevision=0 path): for each retry the handler reads the
// current entity, drops any triple whose predicate appears in RemoveTriples,
// then raw-appends AddTriples. Callers must send a COMPLETE, CURRENT value set
// with ONE triple per scalar predicate — duplicate predicates in a single
// publish would BOTH be appended. RemoveTriples is derived as the distinct set
// of predicates present in triples, so the net effect is that each republish
// replaces the caller's own predicates while leaving predicates written by
// other writers (hierarchy inference, rules) intact.
//
// NOTE: optional predicates omitted when their field is empty are absent from
// RemoveTriples. A field that transitions from set → empty will leave a stale
// value in the graph. Today all such fields are monotonically set-once, so
// this is benign; revisit if any field can be cleared after being set.
//
// Create fallback: update_with_triples returns ErrorCodeEntityNotFound when the
// entity is absent. UpsertEntity falls back to create_with_triples. If that
// returns ErrorCodeEntityExists (concurrent writer), UpsertEntity retries the
// update once. All other non-Success codes are returned as errors.
//
// Best-effort: returns nil when NATSClient is nil (matches WriteTriple guard).
// NATS calls use a 2 s timeout with zero retries — these are observability
// mirrors of authoritative KV state. A dropped write is self-healing: the
// next whole-entity republish overwrites it. This bound prevents UpsertEntity
// from ever blocking a phase transition for longer than 2 s, even when called
// inside a held mutex (e.g. requirement-executor's exec.mu critical section).
func (tw *TripleWriter) UpsertEntity(ctx context.Context, entityType message.Type, entityID string, triples []message.Triple) error {
	if tw.NATSClient == nil {
		return nil
	}
	s := upsertSender{
		update: tw.sendUpdateWithTriples,
		create: tw.sendCreateWithTriples,
	}
	return upsertEntityVia(ctx, s, entityType, entityID, triples)
}

// upsertEntityVia contains the routing logic for UpsertEntity, separated from
// the NATS transport so it can be exercised by unit tests that inject stubs.
func upsertEntityVia(ctx context.Context, s upsertSender, entityType message.Type, entityID string, triples []message.Triple) error {
	// Derive the distinct predicate set from the caller's triples.
	// This is the "replace own, preserve foreign" contract: we only
	// remove predicates we are about to write, leaving everything else.
	removePredicates := distinctPredicates(triples)

	entity := &graph.EntityState{
		ID:          entityID,
		MessageType: entityType,
	}

	updateReq := graph.UpdateEntityWithTriplesRequest{
		Entity:        entity,
		AddTriples:    triples,
		RemoveTriples: removePredicates,
		// ExpectedRevision zero → internal UpdateWithRetry merge path (no caller-side CAS).
	}

	outcome, err := s.update(ctx, updateReq)
	if err != nil {
		return err
	}

	switch outcome {
	case upsertDone:
		return nil
	case upsertNeedCreate:
		// Entity absent — try create first.
		createReq := graph.CreateEntityWithTriplesRequest{
			Entity:  entity,
			Triples: triples,
		}
		createOutcome, err := s.create(ctx, createReq)
		if err != nil {
			return err
		}
		switch createOutcome {
		case upsertDone:
			return nil
		case upsertRetryUpdate:
			// Concurrent writer created the entity between our update and create attempts.
			// Retry the update once; if it fails again, surface the error.
			retryOutcome, err := s.update(ctx, updateReq)
			if err != nil {
				return err
			}
			if retryOutcome != upsertDone {
				return fmt.Errorf("upsert entity %s: unexpected outcome after create-exists retry: %s", entityID, retryOutcome)
			}
			return nil
		default:
			return fmt.Errorf("upsert entity %s: unexpected create outcome: %s", entityID, createOutcome)
		}
	default:
		return fmt.Errorf("upsert entity %s: unexpected update outcome: %s", entityID, outcome)
	}
}

// decideUpdateOutcome maps an UpdateEntityWithTriplesResponse to a upsertOutcome.
// Extracted for unit-testability: no NATS required.
func decideUpdateOutcome(resp graph.UpdateEntityWithTriplesResponse) (upsertOutcome, error) {
	if resp.Success {
		return upsertDone, nil
	}
	// Degraded=true means the write committed but read-back failed; treat as success.
	// Per MutationResponse docs: do NOT retry on Degraded — the write is durable.
	if resp.Degraded {
		return upsertDone, nil
	}
	switch resp.ErrorCode {
	case graph.ErrorCodeEntityNotFound:
		return upsertNeedCreate, nil
	default:
		return 0, fmt.Errorf("update_with_triples rejected (code=%s): %s", resp.ErrorCode, resp.Error)
	}
}

// decideCreateOutcome maps a CreateEntityWithTriplesResponse to a upsertOutcome.
// Extracted for unit-testability: no NATS required.
func decideCreateOutcome(resp graph.CreateEntityWithTriplesResponse) (upsertOutcome, error) {
	if resp.Success {
		return upsertDone, nil
	}
	if resp.Degraded {
		return upsertDone, nil
	}
	switch resp.ErrorCode {
	case graph.ErrorCodeEntityExists:
		return upsertRetryUpdate, nil
	default:
		return 0, fmt.Errorf("create_with_triples rejected (code=%s): %s", resp.ErrorCode, resp.Error)
	}
}

// upsertRetryConfig is the retry configuration for UpsertEntity NATS calls.
// Zero retries + 2 s timeout: these publishes are best-effort observability
// mirrors of authoritative KV state; a dropped write self-heals on the next
// whole-entity republish. Zero retries ensures UpsertEntity never blocks a
// phase transition for more than 2 s even when called inside a held mutex
// (e.g. requirement-executor's exec.mu DAG-node loop).
var upsertRetryConfig = natsclient.RetryConfig{MaxRetries: 0}

// sendUpdateWithTriples marshals the request, sends via NATS request/reply,
// and parses the response into a upsertOutcome.
func (tw *TripleWriter) sendUpdateWithTriples(ctx context.Context, req graph.UpdateEntityWithTriplesRequest) (upsertOutcome, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal update_with_triples request: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, subjectEntityUpdateWithTriples, data, 2*time.Second, upsertRetryConfig)
	if err != nil {
		tw.Logger.Warn("update_with_triples request failed",
			"entity_id", req.Entity.ID, "error", err)
		return 0, fmt.Errorf("update_with_triples request: %w", err)
	}

	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal update_with_triples response: %w", err)
	}

	return decideUpdateOutcome(resp)
}

// sendCreateWithTriples marshals the request, sends via NATS request/reply,
// and parses the response into a upsertOutcome.
func (tw *TripleWriter) sendCreateWithTriples(ctx context.Context, req graph.CreateEntityWithTriplesRequest) (upsertOutcome, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal create_with_triples request: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, subjectEntityCreateWithTriples, data, 2*time.Second, upsertRetryConfig)
	if err != nil {
		tw.Logger.Warn("create_with_triples request failed",
			"entity_id", req.Entity.ID, "error", err)
		return 0, fmt.Errorf("create_with_triples request: %w", err)
	}

	var resp graph.CreateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal create_with_triples response: %w", err)
	}

	return decideCreateOutcome(resp)
}

// distinctPredicates returns the sorted, deduplicated set of predicate strings
// found in the given triples. Used to build RemoveTriples for UpsertEntity.
func distinctPredicates(triples []message.Triple) []string {
	seen := make(map[string]struct{}, len(triples))
	for _, t := range triples {
		seen[t.Predicate] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// ReadEntitiesByPrefixMulti fetches all entities matching an ID prefix from
// ENTITY_STATES via graph-ingest. Returns a map of entityID → predicate →
// []values, preserving every value written for multi-valued predicates (e.g.
// RequirementDependsOn, ScenarioThen, PlanDecisionMutates).
func (tw *TripleWriter) ReadEntitiesByPrefixMulti(ctx context.Context, prefix string, limit int) (map[string]map[string][]string, error) {
	if tw.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not configured")
	}

	if limit <= 0 {
		limit = 100
	}

	reqData, err := json.Marshal(map[string]any{"prefix": prefix, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("marshal prefix query: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.ingest.query.prefix", reqData, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("query prefix %s: %w", prefix, err)
	}

	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal prefix response: %w", err)
	}

	result := make(map[string]map[string][]string, len(resp.Entities))
	for _, entity := range resp.Entities {
		multi := make(map[string][]string, len(entity.Triples))
		for _, t := range entity.Triples {
			var s string
			switch v := t.Object.(type) {
			case string:
				s = v
			case float64:
				if v == float64(int64(v)) {
					s = fmt.Sprintf("%d", int64(v))
				} else {
					s = fmt.Sprintf("%g", v)
				}
			case bool:
				s = fmt.Sprintf("%t", v)
			default:
				data, _ := json.Marshal(v)
				s = string(data)
			}
			multi[t.Predicate] = append(multi[t.Predicate], s)
		}
		result[entity.ID] = multi
	}

	return result, nil
}
