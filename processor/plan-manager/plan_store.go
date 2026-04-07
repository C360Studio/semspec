package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	sscache "github.com/c360studio/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"
)

// planStore owns the lifecycle of plan data entities (wf.plan.plan.*).
// It follows the 3-layer manager pattern:
//
//  1. cache.Cache[*workflow.Plan] — TTL cache, all runtime reads go here first
//  2. jetstream.KeyValue (PLAN_STATES) — observable, durable write-through;
//     the write IS the event (KV twofer). May be nil in tests / no-NATS mode.
//  3. *graphutil.TripleWriter — global graph truth for rules and cross-component
//     queries. Still the fallback source during startup reconciliation.
//
// Runtime reads never hit the graph. Reconcile prefers KV on restart; falls
// back to graph only on first startup (empty KV bucket).
// Requirements and Scenarios are carried inline on the Plan struct — no sibling stores.
type planStore struct {
	cache        sscache.Cache[*workflow.Plan]
	kvBucket     jetstream.KeyValue // PLAN_STATES — may be nil (tests, no NATS)
	tripleWriter *graphutil.TripleWriter
	logger       *slog.Logger
}

// newPlanStore creates a plan store backed by a TTL in-memory cache.
// Cache is a performance optimization — KV is the durable read source.
// Plans that outlive the TTL (e.g., waiting for human review) are served
// via KV fallback on cache miss (see get()). This is the reference pattern.
// kv may be nil — store operates in cache+graph-only mode when absent.
func newPlanStore(ctx context.Context, kv jetstream.KeyValue, tw *graphutil.TripleWriter, logger *slog.Logger) (*planStore, error) {
	c, err := sscache.NewTTL[*workflow.Plan](ctx, 30*time.Minute, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("create plan cache: %w", err)
	}
	return &planStore{
		cache:        c,
		kvBucket:     kv,
		tripleWriter: tw,
		logger:       logger,
	}, nil
}

// reconcile populates the cache on startup.
// Prefers KV (fast, local, operational source of truth). Falls back to the
// graph when the KV bucket is absent or empty (e.g., first ever startup).
func (s *planStore) reconcile(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// --- KV path (preferred) ---
	if s.kvBucket != nil {
		keys, err := s.kvBucket.Keys(reconcileCtx)
		if err == nil && len(keys) > 0 {
			recovered := 0
			for _, key := range keys {
				entry, err := s.kvBucket.Get(reconcileCtx, key)
				if err != nil {
					continue
				}
				var plan workflow.Plan
				if json.Unmarshal(entry.Value(), &plan) == nil {
					s.cache.Set(plan.Slug, &plan) //nolint:errcheck // cache set is best-effort
					recovered++
				}
			}
			if recovered > 0 {
				s.logger.Info("Plan cache reconciled from KV", "count", recovered)
				return
			}
		}
	}

	// --- Graph fallback (first startup or empty KV) ---
	prefix := workflow.EntityPrefix() + ".wf.plan.plan."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, prefix, 500)
	if err != nil {
		s.logger.Warn("Plan reconciliation failed (cache will be empty until plans are created/mutated)",
			"error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		if triples[semspec.PredicatePlanStatus] == "deleted" {
			continue
		}
		plan := workflow.PlanFromTripleMap(entityID, triples)
		if plan.Slug == "" {
			continue
		}
		s.cache.Set(plan.Slug, plan) //nolint:errcheck // cache set is best-effort
		recovered++
	}

	if recovered > 0 {
		s.logger.Info("Plan cache reconciled from graph", "count", recovered)
	}
}

// get returns a shallow copy of a plan by slug.
// Cache is checked first; on miss the KV bucket is queried and the result is
// back-filled into the cache.
func (s *planStore) get(slug string) (*workflow.Plan, bool) {
	// 1. Cache hit — shallow copy to prevent races.
	if plan, ok := s.cache.Get(slug); ok {
		p := *plan
		return &p, true
	}

	// 2. KV fallback on cache miss.
	if s.kvBucket != nil {
		entry, err := s.kvBucket.Get(context.Background(), slug)
		if err == nil {
			var plan workflow.Plan
			if json.Unmarshal(entry.Value(), &plan) == nil {
				s.cache.Set(plan.Slug, &plan) //nolint:errcheck // cache set is best-effort
				p := plan
				return &p, true
			}
		}
	}

	return nil, false
}

// list returns all non-expired plans from the cache, sorted newest-first.
func (s *planStore) list() []*workflow.Plan {
	plans := make([]*workflow.Plan, 0)
	for _, key := range s.cache.Keys() {
		if plan, ok := s.cache.Get(key); ok {
			plans = append(plans, plan)
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	return plans
}

// exists reports whether a plan is present in the cache (not deleted).
func (s *planStore) exists(slug string) bool {
	_, ok := s.cache.Get(slug)
	return ok
}

// create creates a new plan and persists it through all three layers.
func (s *planStore) create(ctx context.Context, slug, title string) (*workflow.Plan, error) {
	if err := workflow.ValidateSlug(slug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, workflow.ErrTitleRequired
	}
	if s.exists(slug) {
		return nil, fmt.Errorf("%w: %s", workflow.ErrPlanExists, slug)
	}

	// Truncate raw prompt to 60 chars for initial title — the planner will
	// replace this with an LLM-generated title when the plan is drafted.
	displayTitle := title
	if len(displayTitle) > 60 {
		displayTitle = displayTitle[:57] + "..."
	}

	now := time.Now()
	plan := &workflow.Plan{
		ID:        workflow.PlanEntityID(slug),
		Slug:      slug,
		Title:     displayTitle,
		ProjectID: workflow.ProjectEntityID(workflow.DefaultProjectSlug),
		Approved:  false,
		CreatedAt: now,
		Scope: workflow.Scope{
			Include:    []string{},
			Exclude:    []string{},
			DoNotTouch: []string{},
		},
	}

	if err := s.save(ctx, plan); err != nil {
		return nil, fmt.Errorf("save new plan: %w", err)
	}
	return plan, nil
}

// save persists a plan through all three layers in order:
// cache → KV bucket → graph triples.
// KV write failures are logged but do not abort the operation — cache and
// graph remain the authoritative copies when KV is temporarily unavailable.
func (s *planStore) save(ctx context.Context, plan *workflow.Plan) error {
	if err := workflow.ValidateSlug(plan.Slug); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// 1. Update cache.
	s.cache.Set(plan.Slug, plan) //nolint:errcheck // cache set is best-effort

	// 2. Write to KV bucket (observable — this IS the event).
	// Requirements and Scenarios are already inline on the plan struct.
	if s.kvBucket != nil {
		data, err := json.Marshal(plan)
		if err != nil {
			return fmt.Errorf("marshal plan for KV: %w", err)
		}
		if _, err := s.kvBucket.Put(ctx, plan.Slug, data); err != nil {
			s.logger.Warn("KV put failed (cache and graph still updated)",
				"slug", plan.Slug, "error", err)
		}
	}

	// 3. Write to graph (global truth).
	if err := s.writeTriples(ctx, plan); err != nil {
		return fmt.Errorf("write plan triples: %w", err)
	}

	return nil
}

// approve transitions a plan to approved status and writes through all layers.
// Validates the state machine transition — only StatusReviewed can transition
// to StatusApproved.
func (s *planStore) approve(ctx context.Context, plan *workflow.Plan) error {
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusApproved) {
		if plan.Approved {
			return fmt.Errorf("%w: %s", workflow.ErrAlreadyApproved, plan.Slug)
		}
		return fmt.Errorf("%w: %s → approved", workflow.ErrInvalidTransition, current)
	}

	now := time.Now()
	plan.Approved = true
	plan.ApprovedAt = &now
	plan.Status = workflow.StatusApproved

	return s.save(ctx, plan)
}

// delete tombstones a plan by setting its status to "deleted", writing a graph
// tombstone, then removing it from cache and KV.
func (s *planStore) delete(ctx context.Context, slug string) error {
	plan, ok := s.get(slug)
	if !ok {
		return fmt.Errorf("%w: %s", workflow.ErrPlanNotFound, slug)
	}

	plan.Status = "deleted"
	if err := s.writeTriples(ctx, plan); err != nil {
		return fmt.Errorf("write delete tombstone: %w", err)
	}

	s.cache.Delete(slug) //nolint:errcheck // cache delete is best-effort
	if s.kvBucket != nil {
		_ = s.kvBucket.Delete(ctx, slug)
	}
	return nil
}

// writeTriples writes all plan fields as individual triples to ENTITY_STATES.
// This is the durable write-through to the global graph. Unchanged from the
// previous implementation.
func (s *planStore) writeTriples(ctx context.Context, plan *workflow.Plan) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.PlanEntityID(plan.Slug)

	// Core identity
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanSlug, plan.Slug)
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanTitle, plan.Title)
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, plan.Title)
	if err := tw.WriteTriple(ctx, entityID, semspec.PredicatePlanStatus, string(plan.EffectiveStatus())); err != nil {
		return fmt.Errorf("write plan status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanCreatedAt, plan.CreatedAt.Format(time.RFC3339))

	// Project association
	if plan.ProjectID != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanProject, plan.ProjectID)
	}

	// Plan content
	if plan.Goal != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanGoal, plan.Goal)
	}
	if plan.Context != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanContext, plan.Context)
	}

	// Approval
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanApproved, fmt.Sprintf("%t", plan.Approved))
	if plan.ApprovedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanApprovedAt, plan.ApprovedAt.Format(time.RFC3339))
	}

	// Review
	if plan.ReviewVerdict != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanReviewVerdict, plan.ReviewVerdict)
	}
	if plan.ReviewSummary != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanReviewSummary, plan.ReviewSummary)
	}
	if plan.ReviewedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanReviewedAt, plan.ReviewedAt.Format(time.RFC3339))
	}
	if len(plan.ReviewFindings) > 0 {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanReviewFindings, string(plan.ReviewFindings))
	}
	if plan.ReviewFormattedFindings != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanReviewFormattedFindings, plan.ReviewFormattedFindings)
	}
	if plan.ReviewIteration > 0 {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanReviewIteration, plan.ReviewIteration)
	}

	// Error annotations
	if plan.LastError != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanLastError, plan.LastError)
	}
	if plan.LastErrorAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanLastErrorAt, plan.LastErrorAt.Format(time.RFC3339))
	}

	// Scope (JSON string)
	if scopeJSON, err := json.Marshal(plan.Scope); err == nil && string(scopeJSON) != "{}" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanScope, string(scopeJSON))
	}

	// Execution trace IDs (JSON array)
	if len(plan.ExecutionTraceIDs) > 0 {
		if traceJSON, err := json.Marshal(plan.ExecutionTraceIDs); err == nil {
			_ = tw.WriteTriple(ctx, entityID, semspec.PlanExecutionTraceIDs, string(traceJSON))
		}
	}

	// LLM call history (JSON)
	if plan.LLMCallHistory != nil {
		if historyJSON, err := json.Marshal(plan.LLMCallHistory); err == nil {
			_ = tw.WriteTriple(ctx, entityID, semspec.PlanLLMCallHistory, string(historyJSON))
		}
	}

	// Requirements and Scenarios — write individual entity triples so the graph
	// stays consistent when the plan is updated.
	s.writeChildTriples(ctx, tw, plan)
	return nil
}

// writeChildTriples writes requirement, scenario, and change proposal triples.
func (s *planStore) writeChildTriples(ctx context.Context, tw *graphutil.TripleWriter, plan *workflow.Plan) {
	if len(plan.Requirements) > 0 {
		if err := workflow.SaveRequirements(ctx, tw, plan.Requirements, plan.Slug); err != nil {
			s.logger.Warn("Failed to write requirement triples", "slug", plan.Slug, "error", err)
		}
	}
	if len(plan.Scenarios) > 0 {
		if err := workflow.SaveScenarios(ctx, tw, plan.Scenarios, plan.Slug); err != nil {
			s.logger.Warn("Failed to write scenario triples", "slug", plan.Slug, "error", err)
		}
	}
	if len(plan.ChangeProposals) > 0 {
		if err := workflow.SaveChangeProposals(ctx, tw, plan.ChangeProposals, plan.Slug); err != nil {
			s.logger.Warn("Failed to write change proposal triples", "slug", plan.Slug, "error", err)
		}
	}
}
