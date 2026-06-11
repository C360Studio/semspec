package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	sscache "github.com/c360studio/semstreams/pkg/cache"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// requirementExecutionEntityType is the message.Type used when writing
// requirement-execution entities to ENTITY_STATES via UpsertEntityIfChanged.
// The value mirrors RequirementExecutionPayloadType in the requirement-executor
// package (workflow/requirement-execution/v1). Defined locally to avoid a
// cross-package import cycle between execution-manager and requirement-executor.
var requirementExecutionEntityType = message.Type{
	Domain:   "workflow",
	Category: "requirement-execution",
	Version:  "v1",
}

// executionStore owns the lifecycle of execution entities in EXECUTION_STATES.
// It follows the 3-layer manager pattern:
//
//  1. cache.Cache — TTL cache, all runtime reads go here first
//  2. jetstream.KeyValue (EXECUTION_STATES) — observable, durable write-through;
//     the write IS the event (KV twofer). May be nil in tests / no-NATS mode.
//  3. *graphutil.TripleWriter — global graph truth for rules and cross-component queries.
//
// Two entity types share the bucket with distinct key prefixes:
//   - task.<slug>.<taskID>   → TaskExecution
//   - req.<slug>.<reqID>     → RequirementExecution
type executionStore struct {
	taskCache    sscache.Cache[*workflow.TaskExecution]
	reqCache     sscache.Cache[*workflow.RequirementExecution]
	kvStore      *natsclient.KVStore // EXECUTION_STATES — may be nil (tests, no NATS)
	tripleWriter *graphutil.TripleWriter
	logger       *slog.Logger
}

// newExecutionStore creates an execution store backed by TTL in-memory caches.
// Cache is a performance optimization — KV is the durable read source.
// Executions that outlive the TTL are served via KV fallback on cache miss
// (see getTask/getReq). This is the reference pattern.
// kvStore may be nil — store operates in cache+graph-only mode when absent.
func newExecutionStore(ctx context.Context, kvStore *natsclient.KVStore, tw *graphutil.TripleWriter, logger *slog.Logger) (*executionStore, error) {
	tc, err := sscache.NewTTL[*workflow.TaskExecution](ctx, 30*time.Minute, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("create task cache: %w", err)
	}
	rc, err := sscache.NewTTL[*workflow.RequirementExecution](ctx, 30*time.Minute, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("create req cache: %w", err)
	}
	return &executionStore{
		taskCache:    tc,
		reqCache:     rc,
		kvStore:      kvStore,
		tripleWriter: tw,
		logger:       logger,
	}, nil
}

// ---------------------------------------------------------------------------
// Task Execution — CRUD
// ---------------------------------------------------------------------------

// getTask returns a shallow copy of a task execution by KV key.
// Refuses non-task keys to prevent cross-cache pollution: a req.* key's bytes
// happen to deserialize cleanly into a TaskExecution (Go's JSON ignores unknown
// fields and shared fields like Stage/Slug populate), and the resulting stale
// entry would get cached in taskCache. handleExecClaimMutation's "try task,
// then req" dispatch then short-circuits on the polluted task hit and never
// reaches getReq for what is genuinely a req key.
func (s *executionStore) getTask(key string) (*workflow.TaskExecution, bool) {
	if !strings.HasPrefix(key, "task.") {
		return nil, false
	}
	if exec, ok := s.taskCache.Get(key); ok {
		e := *exec
		return &e, true
	}
	if s.kvStore != nil {
		entry, err := s.kvStore.Get(context.Background(), key)
		if err == nil {
			var exec workflow.TaskExecution
			if json.Unmarshal(entry.Value, &exec) == nil {
				s.taskCache.Set(key, &exec) //nolint:errcheck
				e := exec
				return &e, true
			}
		}
	}
	return nil, false
}

// saveTask persists a task execution through all three layers.
func (s *executionStore) saveTask(ctx context.Context, key string, exec *workflow.TaskExecution) error {
	exec.UpdatedAt = time.Now()

	// 1. Update cache.
	s.taskCache.Set(key, exec) //nolint:errcheck

	// 2. Write to KV bucket (observable — this IS the event).
	if s.kvStore != nil {
		data, err := json.Marshal(exec)
		if err != nil {
			return fmt.Errorf("marshal task execution for KV: %w", err)
		}
		if _, err := s.kvStore.Put(ctx, key, data); err != nil {
			s.logger.Warn("KV put failed for task execution (cache and graph still updated)",
				"key", key, "error", err)
		}
	}

	// 3. Write to graph (supplementary — failures logged, not fatal).
	if err := s.writeTaskTriples(ctx, exec); err != nil {
		s.logger.Warn("Task triple write failed (KV is primary)",
			"key", key, "error", err)
	}

	return nil
}

// listTasksForSlug returns task executions matching the given plan slug.
// Keys are formatted as "task.<slug>.<taskID>".
// When the cache scan returns nothing, falls back to KV to handle post-TTL
// expiry and restarts where terminal stages were skipped during reconciliation.
func (s *executionStore) listTasksForSlug(ctx context.Context, slug string) []*workflow.TaskExecution {
	prefix := "task." + slug + "."
	var out []*workflow.TaskExecution
	for _, key := range s.taskCache.Keys() {
		if strings.HasPrefix(key, prefix) {
			if exec, ok := s.taskCache.Get(key); ok {
				out = append(out, exec)
			}
		}
	}
	if len(out) == 0 && s.kvStore != nil {
		s.logger.Debug("listTasksForSlug: cache miss, falling back to KV", "slug", slug)
		keys, err := s.kvStore.KeysByPrefix(ctx, prefix)
		if err == nil {
			for _, key := range keys {
				entry, err := s.kvStore.Get(ctx, key)
				if err != nil {
					continue
				}
				var exec workflow.TaskExecution
				if json.Unmarshal(entry.Value, &exec) == nil {
					s.taskCache.Set(key, &exec) //nolint:errcheck
					e := exec
					out = append(out, &e)
				}
			}
		}
	}
	return out
}

// listReqsForSlug returns requirement executions matching the given plan slug.
// Keys are formatted as "req.<slug>.<reqID>".
// When the cache scan returns nothing, falls back to KV to handle post-TTL
// expiry and restarts where terminal stages were skipped during reconciliation.
func (s *executionStore) listReqsForSlug(ctx context.Context, slug string) []*workflow.RequirementExecution {
	prefix := "req." + slug + "."
	var out []*workflow.RequirementExecution
	for _, key := range s.reqCache.Keys() {
		if strings.HasPrefix(key, prefix) {
			if exec, ok := s.reqCache.Get(key); ok {
				out = append(out, exec)
			}
		}
	}
	if len(out) == 0 && s.kvStore != nil {
		s.logger.Debug("listReqsForSlug: cache miss, falling back to KV", "slug", slug)
		keys, err := s.kvStore.KeysByPrefix(ctx, prefix)
		if err == nil {
			for _, key := range keys {
				entry, err := s.kvStore.Get(ctx, key)
				if err != nil {
					continue
				}
				var exec workflow.RequirementExecution
				if json.Unmarshal(entry.Value, &exec) == nil {
					s.reqCache.Set(key, &exec) //nolint:errcheck
					e := exec
					out = append(out, &e)
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Requirement Execution — CRUD
// ---------------------------------------------------------------------------

// getReq returns a shallow copy of a requirement execution by KV key.
// Refuses non-req keys for symmetry with getTask's prefix guard.
func (s *executionStore) getReq(key string) (*workflow.RequirementExecution, bool) {
	if !strings.HasPrefix(key, "req.") {
		return nil, false
	}
	if exec, ok := s.reqCache.Get(key); ok {
		e := *exec
		return &e, true
	}
	if s.kvStore != nil {
		entry, err := s.kvStore.Get(context.Background(), key)
		if err == nil {
			var exec workflow.RequirementExecution
			if json.Unmarshal(entry.Value, &exec) == nil {
				s.reqCache.Set(key, &exec) //nolint:errcheck
				e := exec
				return &e, true
			}
		}
	}
	return nil, false
}

// saveReq persists a requirement execution through all three layers.
func (s *executionStore) saveReq(ctx context.Context, key string, exec *workflow.RequirementExecution) error {
	exec.UpdatedAt = time.Now()

	// 1. Update cache.
	s.reqCache.Set(key, exec) //nolint:errcheck

	// 2. Write to KV bucket (observable — this IS the event).
	if s.kvStore != nil {
		data, err := json.Marshal(exec)
		if err != nil {
			return fmt.Errorf("marshal req execution for KV: %w", err)
		}
		if _, err := s.kvStore.Put(ctx, key, data); err != nil {
			s.logger.Warn("KV put failed for req execution (cache and graph still updated)",
				"key", key, "error", err)
		}
	}

	// 3. Write to graph (supplementary — failures logged, not fatal).
	if err := s.writeReqTriples(ctx, exec); err != nil {
		s.logger.Warn("Req triple write failed (KV is primary)",
			"key", key, "error", err)
	}

	return nil
}

// deleteReq removes a requirement execution from cache and KV.
// Cache delete errors are non-fatal (cache may already be cleared via TTL).
// KV delete errors are logged at INFO — a silent failure here can leave the
// KV entry stranded and break post-/retry recovery (the orchestrator's
// subsequent re-create would fail with "req execution already exists").
func (s *executionStore) deleteReq(ctx context.Context, key string) {
	s.reqCache.Delete(key) //nolint:errcheck
	if s.kvStore != nil {
		if err := s.kvStore.Delete(ctx, key); err != nil {
			s.logger.Info("deleteReq: KV delete failed (cache cleared, KV may have stranded entry)",
				"key", key, "error", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Reconciliation
// ---------------------------------------------------------------------------

// reconcile populates caches on startup. Prefers KV (fast, local).
// Falls back to graph when KV bucket is absent or empty.
func (s *executionStore) reconcile(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// --- KV path (preferred) ---
	if s.kvStore != nil {
		keys, err := s.kvStore.Keys(reconcileCtx)
		if err == nil && len(keys) > 0 {
			tasks, reqs := 0, 0
			for _, key := range keys {
				entry, err := s.kvStore.Get(reconcileCtx, key)
				if err != nil {
					continue
				}
				if strings.HasPrefix(key, "task.") {
					var exec workflow.TaskExecution
					if json.Unmarshal(entry.Value, &exec) == nil && !workflow.IsTerminalTaskStage(exec.Stage) {
						s.taskCache.Set(key, &exec) //nolint:errcheck
						tasks++
					}
				} else if strings.HasPrefix(key, "req.") {
					var exec workflow.RequirementExecution
					if json.Unmarshal(entry.Value, &exec) == nil && !workflow.IsTerminalReqStage(exec.Stage) {
						s.reqCache.Set(key, &exec) //nolint:errcheck
						reqs++
					}
				}
			}
			if tasks > 0 || reqs > 0 {
				s.logger.Info("Execution cache reconciled from KV",
					"tasks", tasks, "requirements", reqs)
				return
			}
		}
	}

	// --- Graph fallback (first startup or empty KV) ---
	if s.tripleWriter == nil {
		return
	}
	s.reconcileTasksFromGraph(reconcileCtx)
	s.reconcileReqsFromGraph(reconcileCtx)
}

// reconcileTasksFromGraph loads active task executions from graph triples.
func (s *executionStore) reconcileTasksFromGraph(ctx context.Context) {
	prefix := workflow.EntityPrefix() + ".exec.task.run."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(ctx, prefix, 500)
	if err != nil {
		s.logger.Warn("Task execution reconciliation from graph failed", "error", err)
		return
	}
	count := 0
	for _, triples := range entities {
		if workflow.IsTerminalTaskStage(triples[wf.Phase]) {
			continue
		}
		exec := taskFromTripleMap(triples)
		if exec.Slug == "" || exec.TaskID == "" {
			continue
		}
		s.taskCache.Set(workflow.TaskExecutionKey(exec.Slug, exec.TaskID), exec) //nolint:errcheck
		count++
	}
	if count > 0 {
		s.logger.Info("Task executions reconciled from graph", "count", count)
	}
}

// reconcileReqsFromGraph loads active requirement executions from graph triples.
func (s *executionStore) reconcileReqsFromGraph(ctx context.Context) {
	prefix := workflow.EntityPrefix() + ".exec.req.run."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(ctx, prefix, 500)
	if err != nil {
		s.logger.Warn("Req execution reconciliation from graph failed", "error", err)
		return
	}
	count := 0
	for _, triples := range entities {
		if workflow.IsTerminalReqStage(triples[wf.Phase]) {
			continue
		}
		exec := reqFromTripleMap(triples)
		if exec.Slug == "" || exec.RequirementID == "" {
			continue
		}
		s.reqCache.Set(workflow.RequirementExecutionKey(exec.Slug, exec.RequirementID), exec) //nolint:errcheck
		count++
	}
	if count > 0 {
		s.logger.Info("Req executions reconciled from graph", "count", count)
	}
}

// ---------------------------------------------------------------------------
// Graph triple writes
// ---------------------------------------------------------------------------

// writeTaskTriples writes the complete predicate set for a task execution to
// ENTITY_STATES as a single atomic batch via UpsertEntityIfChanged (Phase 3a).
//
// Each task execution transitions through many phases (developing → validating
// → reviewing → approved/rejected/escalated/error), each triggering a saveTask
// call. The per-phase dirty-track gate skips re-persisting if only the
// component.go per-triple UpdateTriple sites (not yet removed — deferred follow-up)
// changed without a corresponding saveTask; on saveTask the full current field
// set is captured here.
//
// OwnedPredicates lists the FULL set of predicates this writer owns, including
// conditional ones that may be absent when their field is empty. This ensures
// that when a field transitions set→empty (e.g. Feedback cleared between TDD
// cycles, FilesModified emptied at start) the predicate still lands in
// RemoveTriples and the stale value is stripped (C1 stale-on-empty contract).
//
// Foreign predicates written by other writers (RelPlan, RelTask, RelProject,
// RelLoop from task_watcher.go / publishEntity) are NOT listed here — they are
// preserved by the replace-own / preserve-foreign contract.
func (s *executionStore) writeTaskTriples(ctx context.Context, exec *workflow.TaskExecution) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := exec.EntityID
	if entityID == "" {
		entityID = workflow.TaskExecutionEntityID(exec.Slug, exec.TaskID)
	}

	// Build the complete predicate set. Always-present scalars written
	// unconditionally so they land in RemoveTriples on every save.
	triples := []message.Triple{
		{Subject: entityID, Predicate: wf.Type, Object: "task-execution"},
		{Subject: entityID, Predicate: wf.Slug, Object: exec.Slug},
		{Subject: entityID, Predicate: wf.TaskID, Object: exec.TaskID},
		{Subject: entityID, Predicate: wf.Title, Object: exec.Title},
		{Subject: entityID, Predicate: wf.ProjectID, Object: exec.ProjectID},
		{Subject: entityID, Predicate: wf.Phase, Object: exec.Stage},
		{Subject: entityID, Predicate: wf.TDDCycle, Object: exec.TDDCycle},
		{Subject: entityID, Predicate: wf.MaxTDDCycles, Object: exec.MaxTDDCycles},
	}

	// Conditional scalars — present when set.
	if exec.TraceID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.TraceID, Object: exec.TraceID})
	}
	if exec.Model != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.Model, Object: exec.Model})
	}
	if exec.AgentID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.AgentID, Object: exec.AgentID})
	}
	if exec.WorktreePath != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.WorktreePath, Object: exec.WorktreePath})
	}
	if exec.WorktreeBranch != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.WorktreeBranch, Object: exec.WorktreeBranch})
	}
	if exec.ValidationPassed {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.ValidationPassed, Object: "true"})
	}
	if exec.Verdict != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.Verdict, Object: exec.Verdict})
	}
	if exec.RejectionType != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.RejectionType, Object: exec.RejectionType})
	}
	if exec.Feedback != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.Feedback, Object: exec.Feedback})
	}
	if exec.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.ErrorReason, Object: exec.ErrorReason})
	}
	if exec.EscalationReason != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.EscalationReason, Object: exec.EscalationReason})
	}

	// Multi-valued list — emit all current values.
	for _, f := range exec.FilesModified {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.FilesModified, Object: f})
	}

	// Single batched write. OwnedPredicates is the full set this writer may
	// emit so that cleared fields still appear in RemoveTriples (C1 fix).
	_, err := tw.UpsertEntityIfChanged(ctx, TaskExecutionPayloadType, entityID, triples, graphutil.UpsertOpts{
		OwnedPredicates: []string{
			wf.Type,
			wf.Slug,
			wf.TaskID,
			wf.Title,
			wf.ProjectID,
			wf.Phase,
			wf.TDDCycle,
			wf.MaxTDDCycles,
			wf.TraceID,
			wf.Model,
			wf.AgentID,
			wf.WorktreePath,
			wf.WorktreeBranch,
			wf.ValidationPassed,
			wf.Verdict,
			wf.RejectionType,
			wf.Feedback,
			wf.ErrorReason,
			wf.EscalationReason,
			wf.FilesModified,
		},
	})
	if err != nil {
		return fmt.Errorf("write task-exec triples %s/%s: %w", exec.Slug, exec.TaskID, err)
	}
	return nil
}

// writeReqTriples writes the complete predicate set for a requirement execution
// to ENTITY_STATES as a single atomic batch via UpsertEntityIfChanged (Phase 3a).
//
// CROSS-PROCESS NOTE: requirement-executor.publishEntity writes overlapping
// predicates (Type, Slug, Phase, TraceID, NodeCount, ErrorReason) on the same
// hashed subject. Per the deferred ownership decision, both writers coexist —
// each UpsertEntityIfChanged only removes its own OwnedPredicates, preserving
// what the other wrote. The rel-edge predicates (RelRequirement, RelProject,
// RelLoop) and FailureReason are written only by requirement-executor and are
// intentionally absent from OwnedPredicates here so they are never stripped.
func (s *executionStore) writeReqTriples(ctx context.Context, exec *workflow.RequirementExecution) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := exec.EntityID
	if entityID == "" {
		entityID = workflow.RequirementExecutionEntityID(exec.Slug, exec.RequirementID)
	}

	// Build the complete predicate set this writer owns.
	triples := []message.Triple{
		{Subject: entityID, Predicate: wf.Type, Object: "requirement-execution"},
		{Subject: entityID, Predicate: wf.Slug, Object: exec.Slug},
		{Subject: entityID, Predicate: wf.RequirementID, Object: exec.RequirementID},
		{Subject: entityID, Predicate: wf.ProjectID, Object: exec.ProjectID},
		{Subject: entityID, Predicate: wf.Phase, Object: exec.Stage},
	}

	if exec.TraceID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.TraceID, Object: exec.TraceID})
	}
	if exec.NodeCount > 0 {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.NodeCount, Object: exec.NodeCount})
	}
	if exec.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.ErrorReason, Object: exec.ErrorReason})
	}
	if exec.ReviewVerdict != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.Verdict, Object: exec.ReviewVerdict})
	}

	// Single batched write. OwnedPredicates covers the full set this writer may
	// emit — including conditional ones that may be empty — so cleared fields
	// still appear in RemoveTriples (C1 fix). Rel-edge predicates and FailureReason
	// are intentionally excluded (owned by requirement-executor.publishEntity).
	_, err := tw.UpsertEntityIfChanged(ctx, requirementExecutionEntityType, entityID, triples, graphutil.UpsertOpts{
		OwnedPredicates: []string{
			wf.Type,
			wf.Slug,
			wf.RequirementID,
			wf.ProjectID,
			wf.Phase,
			wf.TraceID,
			wf.NodeCount,
			wf.ErrorReason,
			wf.Verdict,
		},
	})
	if err != nil {
		return fmt.Errorf("write req-exec triples %s/%s: %w", exec.Slug, exec.RequirementID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Triple → struct reconstruction (graph fallback reconciliation)
// ---------------------------------------------------------------------------

func taskFromTripleMap(triples map[string]string) *workflow.TaskExecution {
	exec := &workflow.TaskExecution{
		Slug:           triples[wf.Slug],
		TaskID:         triples[wf.TaskID],
		Stage:          triples[wf.Phase],
		Title:          triples[wf.Title],
		ProjectID:      triples[wf.ProjectID],
		TraceID:        triples[wf.TraceID],
		Model:          triples[wf.Model],
		AgentID:        triples[wf.AgentID],
		WorktreePath:   triples[wf.WorktreePath],
		WorktreeBranch: triples[wf.WorktreeBranch],
	}
	if exec.Slug != "" && exec.TaskID != "" {
		exec.EntityID = workflow.TaskExecutionEntityID(exec.Slug, exec.TaskID)
	}
	if v := triples[wf.TDDCycle]; v != "" {
		fmt.Sscanf(v, "%d", &exec.TDDCycle)
	}
	if v := triples[wf.MaxTDDCycles]; v != "" {
		fmt.Sscanf(v, "%d", &exec.MaxTDDCycles)
	}
	exec.Verdict = triples[wf.Verdict]
	exec.Feedback = triples[wf.Feedback]
	exec.RejectionType = triples[wf.RejectionType]
	exec.ErrorReason = triples[wf.ErrorReason]
	exec.EscalationReason = triples[wf.EscalationReason]
	return exec
}

func reqFromTripleMap(triples map[string]string) *workflow.RequirementExecution {
	exec := &workflow.RequirementExecution{
		Slug:          triples[wf.Slug],
		RequirementID: triples[wf.RequirementID],
		Stage:         triples[wf.Phase],
		ProjectID:     triples[wf.ProjectID],
		TraceID:       triples[wf.TraceID],
	}
	if exec.Slug != "" && exec.RequirementID != "" {
		exec.EntityID = workflow.RequirementExecutionEntityID(exec.Slug, exec.RequirementID)
	}
	if v := triples[wf.NodeCount]; v != "" {
		fmt.Sscanf(v, "%d", &exec.NodeCount)
	}
	exec.ReviewVerdict = triples[wf.Verdict]
	exec.ErrorReason = triples[wf.ErrorReason]
	return exec
}
