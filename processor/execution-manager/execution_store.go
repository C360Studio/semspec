package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/natsclient"
	sscache "github.com/c360studio/semstreams/pkg/cache"
)

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

func (s *executionStore) writeTaskTriples(ctx context.Context, exec *workflow.TaskExecution) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := exec.EntityID
	if entityID == "" {
		entityID = workflow.TaskExecutionEntityID(exec.Slug, exec.TaskID)
	}

	_ = tw.WriteTriple(ctx, entityID, wf.Type, "task-execution")
	_ = tw.WriteTriple(ctx, entityID, wf.Slug, exec.Slug)
	_ = tw.WriteTriple(ctx, entityID, wf.TaskID, exec.TaskID)
	_ = tw.WriteTriple(ctx, entityID, wf.Title, exec.Title)
	_ = tw.WriteTriple(ctx, entityID, wf.ProjectID, exec.ProjectID)
	if err := tw.WriteTriple(ctx, entityID, wf.Phase, exec.Stage); err != nil {
		return fmt.Errorf("write phase: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, wf.TDDCycle, exec.TDDCycle)
	_ = tw.WriteTriple(ctx, entityID, wf.MaxTDDCycles, exec.MaxTDDCycles)
	if exec.TraceID != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.TraceID, exec.TraceID)
	}
	if exec.WorktreePath != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.WorktreePath, exec.WorktreePath)
	}
	if exec.WorktreeBranch != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.WorktreeBranch, exec.WorktreeBranch)
	}
	// Files modified — one triple per path.
	for _, f := range exec.FilesModified {
		_ = tw.WriteTriple(ctx, entityID, wf.FilesModified, f)
	}
	if exec.ValidationPassed {
		_ = tw.WriteTriple(ctx, entityID, wf.ValidationPassed, "true")
	}
	if exec.Verdict != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.Verdict, exec.Verdict)
	}
	if exec.Feedback != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.Feedback, exec.Feedback)
	}
	if exec.RejectionType != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.RejectionType, exec.RejectionType)
	}
	if exec.ErrorReason != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.ErrorReason, exec.ErrorReason)
	}
	if exec.EscalationReason != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.EscalationReason, exec.EscalationReason)
	}
	if exec.AgentID != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.AgentID, exec.AgentID)
	}
	if exec.Model != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.Model, exec.Model)
	}

	return nil
}

func (s *executionStore) writeReqTriples(ctx context.Context, exec *workflow.RequirementExecution) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := exec.EntityID
	if entityID == "" {
		entityID = workflow.RequirementExecutionEntityID(exec.Slug, exec.RequirementID)
	}

	_ = tw.WriteTriple(ctx, entityID, wf.Type, "requirement-execution")
	_ = tw.WriteTriple(ctx, entityID, wf.Slug, exec.Slug)
	_ = tw.WriteTriple(ctx, entityID, wf.RequirementID, exec.RequirementID)
	_ = tw.WriteTriple(ctx, entityID, wf.ProjectID, exec.ProjectID)
	if err := tw.WriteTriple(ctx, entityID, wf.Phase, exec.Stage); err != nil {
		return fmt.Errorf("write phase: %w", err)
	}
	if exec.TraceID != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.TraceID, exec.TraceID)
	}
	if exec.NodeCount > 0 {
		_ = tw.WriteTriple(ctx, entityID, wf.NodeCount, exec.NodeCount)
	}
	if exec.ErrorReason != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.ErrorReason, exec.ErrorReason)
	}
	if exec.ReviewVerdict != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.Verdict, exec.ReviewVerdict)
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
