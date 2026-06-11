// Package graphutil provides shared graph write helpers used across orchestrator
// components. Centralising writeTriple and portSubject here removes the
// verbatim copy that previously existed in review-orchestrator,
// execution-orchestrator, scenario-executor, plan-coordinator, and
// plan-decision-handler.
package graphutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
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
//
// Phase 3a dirty-tracking: UpsertEntityIfChanged uses the dirtyHashes map to
// skip re-persisting an entity whose content-hash has not changed since the
// last successful write. This is the primary lever for suppressing ENTITY_STATES
// write-amplification. The map is nil-safe: a nil map (zero-value TripleWriter)
// means "always persist", matching the best-effort observability contract.
type TripleWriter struct {
	NATSClient    *natsclient.Client
	Logger        *slog.Logger
	ComponentName string

	// dirtyHashes tracks the sha256 content-hash of the last successfully
	// persisted triple set per entity. Protected by dirtyMu. nil means "always
	// persist" (zero-value safe, first-write always persists).
	dirtyMu     sync.Mutex
	dirtyHashes map[string]string // entityID → last-persisted content hash
}

// upsertResult is returned by the internal upsertEntityWithResult, exposing
// the Degraded flag so UpsertEntityIfChanged can avoid marking the cache clean
// on a degraded write (where the mirror state is uncertain).
type upsertResult struct {
	// Degraded is true when the write committed but the read-back confirmation
	// failed. The write is durable, but we cannot be sure what the graph holds,
	// so UpsertEntityIfChanged should leave the entity dirty for a future retry.
	Degraded bool
}

// Evict removes entityID from the dirty-hash map so the next
// UpsertEntityIfChanged call re-persists regardless of content.
// Must be called on the delete path to avoid a stale-skip after deletion
// and re-creation of the same entity ID.
func (tw *TripleWriter) Evict(entityID string) {
	tw.dirtyMu.Lock()
	delete(tw.dirtyHashes, entityID)
	tw.dirtyMu.Unlock()
}

// tripleContentHash computes a stable sha256 hash over the semantic content of
// a triple set: (predicate, canonical-object) pairs only — deliberately
// excluding Triple.Timestamp, .Source, and .Confidence which are stamped fresh
// on every build and would make every hash unique (defeating dirty-track).
//
// Canonicalisation:
//   - Each predicate is paired with the canonical string form of its object
//     (string as-is; int/int64 as "%d"; float64 as "%g"; bool as "%t"; else
//     json.Marshal).
//   - For predicates that appear more than once (multi-valued), all values for
//     that predicate are collected and sorted so set-order churn does not flip
//     the hash.
//   - The predicate names are then sorted ascending so insertion-order churn
//     does not flip the hash.
//   - Any predicate listed in excludePredicates is excluded from the hash (it
//     is still written to the graph; it just does not gate dirtiness). Use this
//     for volatile predicates like semspec.RequirementUpdatedAt that increment
//     on every mutation without a semantic change.
func tripleContentHash(triples []message.Triple, excludePredicates ...string) string {
	// Build exclusion set.
	excluded := make(map[string]struct{}, len(excludePredicates))
	for _, p := range excludePredicates {
		excluded[p] = struct{}{}
	}

	// Collect canonical (predicate, value) pairs — one pair per triple value.
	type pv struct{ pred, val string }
	var pairs []pv
	for _, t := range triples {
		if _, skip := excluded[t.Predicate]; skip {
			continue
		}
		pairs = append(pairs, pv{pred: t.Predicate, val: canonicalObjectString(t.Object)})
	}

	// Group by predicate, sort values within each group (set semantics).
	byPred := make(map[string][]string, len(pairs))
	for _, p := range pairs {
		byPred[p.pred] = append(byPred[p.pred], p.val)
	}
	for pred := range byPred {
		sort.Strings(byPred[pred])
	}

	// Collect predicate names, sort ascending.
	preds := make([]string, 0, len(byPred))
	for pred := range byPred {
		preds = append(preds, pred)
	}
	sort.Strings(preds)

	// Build the canonical byte stream: "pred\x00val\x01pred\x00val\x01…"
	// using NUL as pred/val separator and SOH as entry separator.
	h := sha256.New()
	for _, pred := range preds {
		for _, val := range byPred[pred] {
			h.Write([]byte(pred))
			h.Write([]byte{0x00})
			h.Write([]byte(val))
			h.Write([]byte{0x01})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalObjectString converts a triple object to a stable string
// representation for hashing. The rules mirror the human-readable formatting
// used in ReadEntity / ReadEntitiesByPrefix for consistency.
func canonicalObjectString(obj any) string {
	switch v := obj.(type) {
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		// Preserve integer-valued floats as integers (matches ReadEntity).
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

// UpsertOpts carries options for UpsertEntityIfChanged.
//
// VolatilePredicates lists predicates that are written to the graph but
// excluded from the content-hash. Use for timestamps (e.g.
// semspec.RequirementUpdatedAt) that change on every mutation without a
// semantic difference — they would otherwise defeat dirty-track by always
// differing.
//
// OwnedPredicates is the FULL set of predicates this writer owns, including
// any that may be absent from the triples slice when the corresponding field is
// empty. These are unioned into RemoveTriples on every write so that a
// set→empty transition (e.g. Then=[], DependsOn=[]) causes the old values to
// be stripped from the graph rather than left stale. Predicates already present
// in triples are covered by distinctPredicates and need not be repeated here,
// but listing the complete owned set is safe and recommended.
type UpsertOpts struct {
	VolatilePredicates []string
	OwnedPredicates    []string
}

// upsertEntityCore is the internal write primitive shared by UpsertEntity and
// UpsertEntityIfChanged. It takes an explicit removePredicates set so callers
// can union in predicates that own lists which may currently be empty (the C1
// stale-on-empty fix): graph-ingest removes every predicate in removePredicates
// before appending addTriples, so an empty list still strips old values.
//
// The public UpsertEntity calls this with removePredicates = distinctPredicates(triples),
// preserving the existing "replace-own / preserve-foreign" contract.
func (tw *TripleWriter) upsertEntityCore(ctx context.Context, entityType message.Type, entityID string, addTriples []message.Triple, removePredicates []string) (upsertResult, error) {
	if tw.NATSClient == nil {
		return upsertResult{}, nil
	}
	s := upsertSenderWithDegraded{
		update: tw.sendUpdateWithTriplesWithDegraded,
		create: tw.sendCreateWithTriplesWithDegraded,
	}
	degraded, err := upsertEntityViaWithTriplesCore(ctx, s, entityType, entityID, addTriples, removePredicates)
	return upsertResult{Degraded: degraded}, err
}

// UpsertEntityIfChanged writes a full set of triples for an entity using the
// replace-own-predicates, preserve-foreign semantics — but ONLY if the
// content-hash of triples has changed since the last successful persist.
//
// Dirty-tracking (Phase 3a Lever 1): on every plan mutation the whole plan and
// ~20 children are currently re-persisted even when only one entity changed.
// This method gates each entity's write on a sha256 content hash of its
// (predicate, object) pairs, suppressing the ~19 unchanged writes and reducing
// ENTITY_STATES write-amplification by ~20×.
//
// Hash semantics:
//   - Only (predicate, canonical-object) is hashed — NOT the full Triple struct,
//     which carries a fresh Timestamp on every build that would defeat dirty-track.
//   - opts.VolatilePredicates are excluded from the hash (still written). Use for
//     timestamps that increment on every mutation without a semantic change.
//   - Multi-valued predicates: values are sorted before hashing so insertion-order
//     churn does not flip the hash (set semantics).
//   - First write (entity absent from map) always persists.
//
// RemoveTriples = union(distinctPredicates(triples), opts.OwnedPredicates).
// Listing the full owned predicate set in opts.OwnedPredicates ensures that
// predicates whose lists have emptied (e.g. Then=[], DependsOn=[]) are stripped
// from the graph even though they emit zero triples (C1 stale-on-empty fix).
//
// Mark-clean contract: the entity is only marked clean when the write returns
// nil AND the write was not Degraded. A degraded write (committed but read-back
// failed) leaves the entity dirty so the next save retries.
//
// Returns (true, nil) when the entity was persisted, (false, nil) when skipped.
func (tw *TripleWriter) UpsertEntityIfChanged(ctx context.Context, entityType message.Type, entityID string, triples []message.Triple, opts UpsertOpts) (persisted bool, err error) {
	hash := tripleContentHash(triples, opts.VolatilePredicates...)

	// Check if we already have a clean write for this hash.
	tw.dirtyMu.Lock()
	prev, seen := tw.dirtyHashes[entityID]
	tw.dirtyMu.Unlock()

	if seen && prev == hash {
		// Content unchanged — skip.
		return false, nil
	}

	// Build the remove-predicates set: union of what is written plus the full
	// owned set. This ensures predicates for emptied lists are still removed
	// from the graph (set→empty stale-value fix).
	removePredicates := unionPredicates(distinctPredicates(triples), opts.OwnedPredicates)

	// Content changed (or first write) — persist.
	result, err := tw.upsertEntityCore(ctx, entityType, entityID, triples, removePredicates)
	if err != nil {
		return false, err
	}

	// Mark clean only on a clean (non-degraded) success.
	if !result.Degraded {
		tw.dirtyMu.Lock()
		if tw.dirtyHashes == nil {
			tw.dirtyHashes = make(map[string]string)
		}
		tw.dirtyHashes[entityID] = hash
		tw.dirtyMu.Unlock()
	}

	return true, nil
}

// unionPredicates returns the sorted, deduplicated union of a and b.
// Used to merge distinctPredicates(written) with the caller's OwnedPredicates
// so that empty-list predicates are still present in RemoveTriples.
func unionPredicates(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, p := range a {
		seen[p] = struct{}{}
	}
	for _, p := range b {
		seen[p] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
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

// upsertSenderWithDegraded is a variant of upsertSender whose functions return
// the Degraded flag alongside the routing outcome. Used exclusively by
// upsertEntityViaWithDegraded so the dirty-track logic in UpsertEntityIfChanged
// can avoid marking an entity clean on a degraded write.
//
// The existing upsertSender + upsertEntityVia are kept unchanged so all
// existing tests and callers are unaffected.
type upsertSenderWithDegraded struct {
	update func(ctx context.Context, req graph.UpdateEntityWithTriplesRequest) (upsertOutcome, bool, error)
	create func(ctx context.Context, req graph.CreateEntityWithTriplesRequest) (upsertOutcome, bool, error)
}

// upsertEntityViaWithTriplesCore is the low-level routing function that accepts
// an explicit removePredicates slice. This allows callers to union in predicates
// for owned lists that may currently be empty (C1 stale-on-empty fix): the
// graph-ingest handler removes every predicate in RemoveTriples before
// appending AddTriples, so an empty-list predicate is still stripped.
//
// Used by upsertEntityCore (backing UpsertEntityIfChanged) and by
// upsertEntityViaWithDegraded (backwards-compat thin wrapper).
func upsertEntityViaWithTriplesCore(ctx context.Context, s upsertSenderWithDegraded, entityType message.Type, entityID string, addTriples []message.Triple, removePredicates []string) (degraded bool, err error) {
	entity := &graph.EntityState{
		ID:          entityID,
		MessageType: entityType,
	}
	updateReq := graph.UpdateEntityWithTriplesRequest{
		Entity:        entity,
		AddTriples:    addTriples,
		RemoveTriples: removePredicates,
	}

	outcome, deg, err := s.update(ctx, updateReq)
	if err != nil {
		return false, err
	}
	switch outcome {
	case upsertDone:
		return deg, nil
	case upsertNeedCreate:
		createReq := graph.CreateEntityWithTriplesRequest{
			Entity:  entity,
			Triples: addTriples,
		}
		createOutcome, createDeg, err := s.create(ctx, createReq)
		if err != nil {
			return false, err
		}
		switch createOutcome {
		case upsertDone:
			return createDeg, nil
		case upsertRetryUpdate:
			retryOutcome, retryDeg, err := s.update(ctx, updateReq)
			if err != nil {
				return false, err
			}
			if retryOutcome != upsertDone {
				return false, fmt.Errorf("upsert entity %s: unexpected outcome after create-exists retry: %s", entityID, retryOutcome)
			}
			return retryDeg, nil
		default:
			return false, fmt.Errorf("upsert entity %s: unexpected create outcome: %s", entityID, createOutcome)
		}
	default:
		return false, fmt.Errorf("upsert entity %s: unexpected update outcome: %s", entityID, outcome)
	}
}

// upsertEntityViaWithDegraded is a thin wrapper around upsertEntityViaWithTriplesCore
// that derives removePredicates from distinctPredicates(triples). Kept for the
// test seam (TestUpsertEntityViaWithDegraded_* tests call this directly).
func upsertEntityViaWithDegraded(ctx context.Context, s upsertSenderWithDegraded, entityType message.Type, entityID string, triples []message.Triple) (degraded bool, err error) {
	return upsertEntityViaWithTriplesCore(ctx, s, entityType, entityID, triples, distinctPredicates(triples))
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

// sendUpdateWithTriplesWithDegraded is the Degraded-aware variant of
// sendUpdateWithTriples. It returns (outcome, degraded, error) so the routing
// in upsertEntityViaWithDegraded can propagate the Degraded flag to
// UpsertEntityIfChanged's mark-clean decision.
func (tw *TripleWriter) sendUpdateWithTriplesWithDegraded(ctx context.Context, req graph.UpdateEntityWithTriplesRequest) (upsertOutcome, bool, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return 0, false, fmt.Errorf("marshal update_with_triples request: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, subjectEntityUpdateWithTriples, data, 2*time.Second, upsertRetryConfig)
	if err != nil {
		tw.Logger.Warn("update_with_triples request failed",
			"entity_id", req.Entity.ID, "error", err)
		return 0, false, fmt.Errorf("update_with_triples request: %w", err)
	}

	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, false, fmt.Errorf("unmarshal update_with_triples response: %w", err)
	}

	if resp.Degraded {
		// Write committed but read-back failed. Route as done but surface degraded
		// so the caller can skip marking the entity clean.
		return upsertDone, true, nil
	}
	outcome, err := decideUpdateOutcome(resp)
	return outcome, false, err
}

// sendCreateWithTriplesWithDegraded is the Degraded-aware variant of
// sendCreateWithTriples. Same contract as sendUpdateWithTriplesWithDegraded.
func (tw *TripleWriter) sendCreateWithTriplesWithDegraded(ctx context.Context, req graph.CreateEntityWithTriplesRequest) (upsertOutcome, bool, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return 0, false, fmt.Errorf("marshal create_with_triples request: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, subjectEntityCreateWithTriples, data, 2*time.Second, upsertRetryConfig)
	if err != nil {
		tw.Logger.Warn("create_with_triples request failed",
			"entity_id", req.Entity.ID, "error", err)
		return 0, false, fmt.Errorf("create_with_triples request: %w", err)
	}

	var resp graph.CreateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, false, fmt.Errorf("unmarshal create_with_triples response: %w", err)
	}

	if resp.Degraded {
		return upsertDone, true, nil
	}
	outcome, err := decideCreateOutcome(resp)
	return outcome, false, err
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
