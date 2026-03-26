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
func (s *executionStore) getTask(key string) (*workflow.TaskExecution, bool) {
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

// getTaskByTaskID finds a task execution by its TaskID field.
// Scans the cache — O(n) but sufficient for typical execution counts.
func (s *executionStore) getTaskByTaskID(taskID string) (*workflow.TaskExecution, string, bool) {
	for _, key := range s.taskCache.Keys() {
		if exec, ok := s.taskCache.Get(key); ok && exec.TaskID == taskID {
			e := *exec
			return &e, key, true
		}
	}
	return nil, "", false
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

	// 3. Write to graph (global truth).
	if err := s.writeTaskTriples(ctx, exec); err != nil {
		return fmt.Errorf("write task triples: %w", err)
	}

	return nil
}

// deleteTask removes a task execution from cache and KV.
func (s *executionStore) deleteTask(ctx context.Context, key string) {
	s.taskCache.Delete(key) //nolint:errcheck
	if s.kvStore != nil {
		_ = s.kvStore.Delete(ctx, key)
	}
}

// listTasks returns all task executions from the cache.
func (s *executionStore) listTasks() []*workflow.TaskExecution {
	var out []*workflow.TaskExecution
	for _, key := range s.taskCache.Keys() {
		if exec, ok := s.taskCache.Get(key); ok {
			out = append(out, exec)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Requirement Execution — CRUD
// ---------------------------------------------------------------------------

// getReq returns a shallow copy of a requirement execution by KV key.
func (s *executionStore) getReq(key string) (*workflow.RequirementExecution, bool) {
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

	// 3. Write to graph (global truth).
	if err := s.writeReqTriples(ctx, exec); err != nil {
		return fmt.Errorf("write req triples: %w", err)
	}

	return nil
}

// deleteReq removes a requirement execution from cache and KV.
func (s *executionStore) deleteReq(ctx context.Context, key string) {
	s.reqCache.Delete(key) //nolint:errcheck
	if s.kvStore != nil {
		_ = s.kvStore.Delete(ctx, key)
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
					if json.Unmarshal(entry.Value, &exec) == nil && !workflow.IsTerminalTaskPhase(exec.Phase) {
						s.taskCache.Set(key, &exec) //nolint:errcheck
						tasks++
					}
				} else if strings.HasPrefix(key, "req.") {
					var exec workflow.RequirementExecution
					if json.Unmarshal(entry.Value, &exec) == nil && !workflow.IsTerminalReqPhase(exec.Phase) {
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

	// Task executions
	taskPrefix := workflow.EntityPrefix() + ".exec.task.run."
	taskEntities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, taskPrefix, 500)
	if err != nil {
		s.logger.Warn("Task execution reconciliation from graph failed", "error", err)
	} else {
		tasks := 0
		for _, triples := range taskEntities {
			phase := triples[wf.Phase]
			if workflow.IsTerminalTaskPhase(phase) {
				continue
			}
			exec := taskFromTripleMap(triples)
			if exec.Slug == "" || exec.TaskID == "" {
				continue
			}
			key := workflow.TaskExecutionKey(exec.Slug, exec.TaskID)
			s.taskCache.Set(key, exec) //nolint:errcheck
			tasks++
		}
		if tasks > 0 {
			s.logger.Info("Task executions reconciled from graph", "count", tasks)
		}
	}

	// Requirement executions
	reqPrefix := workflow.EntityPrefix() + ".exec.req.run."
	reqEntities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, reqPrefix, 500)
	if err != nil {
		s.logger.Warn("Req execution reconciliation from graph failed", "error", err)
	} else {
		reqs := 0
		for _, triples := range reqEntities {
			phase := triples[wf.Phase]
			if workflow.IsTerminalReqPhase(phase) {
				continue
			}
			exec := reqFromTripleMap(triples)
			if exec.Slug == "" || exec.RequirementID == "" {
				continue
			}
			key := workflow.RequirementExecutionKey(exec.Slug, exec.RequirementID)
			s.reqCache.Set(key, exec) //nolint:errcheck
			reqs++
		}
		if reqs > 0 {
			s.logger.Info("Req executions reconciled from graph", "count", reqs)
		}
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
	if err := tw.WriteTriple(ctx, entityID, wf.Phase, exec.Phase); err != nil {
		return fmt.Errorf("write phase: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, wf.Iteration, exec.Iteration)
	_ = tw.WriteTriple(ctx, entityID, wf.MaxIterations, exec.MaxIterations)
	if exec.TraceID != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.TraceID, exec.TraceID)
	}
	if exec.WorktreePath != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.WorktreePath, exec.WorktreePath)
	}
	if exec.WorktreeBranch != "" {
		_ = tw.WriteTriple(ctx, entityID, wf.WorktreeBranch, exec.WorktreeBranch)
	}
	if len(exec.FilesModified) > 0 {
		if data, err := json.Marshal(exec.FilesModified); err == nil {
			_ = tw.WriteTriple(ctx, entityID, wf.FilesModified, string(data))
		}
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
	// Agent/model metadata (string literals, no vocabulary constant)
	if exec.AgentID != "" {
		_ = tw.WriteTriple(ctx, entityID, "workflow.execution.agent_id", exec.AgentID)
	}
	if exec.BlueTeamID != "" {
		_ = tw.WriteTriple(ctx, entityID, "workflow.execution.blue_team_id", exec.BlueTeamID)
	}
	if exec.Model != "" {
		_ = tw.WriteTriple(ctx, entityID, "workflow.execution.model", exec.Model)
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
	if err := tw.WriteTriple(ctx, entityID, wf.Phase, exec.Phase); err != nil {
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
		Slug:    triples[wf.Slug],
		TaskID:  triples[wf.TaskID],
		Phase:   triples[wf.Phase],
		Title:   triples[wf.Title],
		ProjectID: triples[wf.ProjectID],
		TraceID:   triples[wf.TraceID],
		Model:     triples["workflow.execution.model"],
		AgentID:   triples["workflow.execution.agent_id"],
		BlueTeamID: triples["workflow.execution.blue_team_id"],
		WorktreePath:   triples[wf.WorktreePath],
		WorktreeBranch: triples[wf.WorktreeBranch],
	}
	if exec.Slug != "" && exec.TaskID != "" {
		exec.EntityID = workflow.TaskExecutionEntityID(exec.Slug, exec.TaskID)
	}
	if v := triples[wf.Iteration]; v != "" {
		fmt.Sscanf(v, "%d", &exec.Iteration)
	}
	if v := triples[wf.MaxIterations]; v != "" {
		fmt.Sscanf(v, "%d", &exec.MaxIterations)
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
		Phase:         triples[wf.Phase],
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
