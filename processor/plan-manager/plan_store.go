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

// create creates a new plan and persists it through all three layers. The
// qaLevel parameter is snapshotted onto the plan so its QA policy is
// immutable under project-level config changes. Pass QALevelSynthesis (or
// empty) for the safe default.
// autoRejectOverride is an optional per-plan override for component config's
// AutoRejectOnExhaustion. nil (the default) means "use component config",
// preserving existing production behaviour. Plumbed through create() so the
// field is set on the in-memory plan before its first KV write — avoids a
// double-write on the create path.
func (s *planStore) create(ctx context.Context, slug, title string, qaLevel workflow.QALevel, autoRejectOverride *bool) (*workflow.Plan, error) {
	if err := workflow.ValidateSlug(slug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, workflow.ErrTitleRequired
	}
	if s.exists(slug) {
		return nil, fmt.Errorf("%w: %s", workflow.ErrPlanExists, slug)
	}

	// Keep the full title in KV — the planner doesn't emit a title field
	// (planSchema only has goal/context/scope), so any truncation here
	// becomes the persistent plan.Title that plan-reviewer marshals into
	// its review prompt. The reviewer LLM then flags the truncation as a
	// completeness violation and rejects the plan, repeating until
	// revision-cap escalation. Caught 2026-05-08 bgbigq271 — three review
	// rounds rejected with "title contains truncation ('endpoi...')"
	// before terminal rejection. UI can truncate at display time when
	// length matters; the canonical title in KV stays full.
	displayTitle := title

	if qaLevel == "" {
		qaLevel = workflow.QALevelSynthesis
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
		QALevel:                qaLevel,
		AutoRejectOnExhaustion: autoRejectOverride,
	}

	if err := s.save(ctx, plan); err != nil {
		return nil, fmt.Errorf("save new plan: %w", err)
	}
	return plan, nil
}

// createImported persists a fully-formed Plan in one shot — used by the
// from-spec import handler (ADR-040 Move 4). Unlike create+save, which
// briefly leaves the plan at status="" between the two KV writes, this
// single-write path guarantees the first KV update already carries the
// translated Status/Goal/Context/Exploration/Requirements/Scenarios.
//
// Critical for race-freedom: the planner component's PLAN_STATES watcher
// (processor/planner/component.go::routePlanStateEntry) reads the first
// KV entry it sees for a slug. If that entry has status="", it claims the
// plan for analyst sub-phase dispatch. Imports MUST land status=explored
// on the first write so the planner watcher routes them to routeExplored
// (which dispatches the planner sub-phase, NOT the analyst).
//
// Per go-reviewer PR 4 audit blocker #2.
func (s *planStore) createImported(ctx context.Context, plan *workflow.Plan, qaLevel workflow.QALevel, autoRejectOverride *bool) error {
	if err := workflow.ValidateSlug(plan.Slug); err != nil {
		return err
	}
	if plan.Title == "" {
		return workflow.ErrTitleRequired
	}
	if s.exists(plan.Slug) {
		return fmt.Errorf("%w: %s", workflow.ErrPlanExists, plan.Slug)
	}
	if qaLevel == "" {
		qaLevel = workflow.QALevelSynthesis
	}
	now := time.Now()
	if plan.ID == "" {
		plan.ID = workflow.PlanEntityID(plan.Slug)
	}
	if plan.ProjectID == "" {
		plan.ProjectID = workflow.ProjectEntityID(workflow.DefaultProjectSlug)
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.QALevel = qaLevel
	plan.AutoRejectOnExhaustion = autoRejectOverride
	// Imports default to unapproved; the operator runs the normal approval
	// flow on the imported plan.
	plan.Approved = false

	if err := s.save(ctx, plan); err != nil {
		return fmt.Errorf("save imported plan: %w", err)
	}
	return nil
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

	// Single-valued scalars use UpdateTriple (remove+add upsert) so the entity
	// holds the LATEST value per predicate instead of accumulating every
	// historical value — graph-ingest AddTriple is append-only, so plain
	// WriteTriple on every plan mutation grew the entity past the 1 MiB KV cap
	// (2026-06-07 plan-prep wedge). Multi-valued lists use ReplaceTripleList.

	// Core identity
	_ = tw.UpdateTriple(ctx, entityID, semspec.PlanSlug, plan.Slug)
	_ = tw.UpdateTriple(ctx, entityID, semspec.PlanTitle, plan.Title)
	_ = tw.UpdateTriple(ctx, entityID, semspec.DCTitle, plan.Title)
	if err := tw.UpdateTriple(ctx, entityID, semspec.PredicatePlanStatus, string(plan.EffectiveStatus())); err != nil {
		return fmt.Errorf("write plan status: %w", err)
	}
	_ = tw.UpdateTriple(ctx, entityID, semspec.PlanCreatedAt, plan.CreatedAt.Format(time.RFC3339))

	// Project association
	if plan.ProjectID != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanProject, plan.ProjectID)
	}

	// Plan content
	if plan.Goal != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanGoal, plan.Goal)
	}
	if plan.Context != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanContext, plan.Context)
	}

	// Approval
	_ = tw.UpdateTriple(ctx, entityID, semspec.PlanApproved, fmt.Sprintf("%t", plan.Approved))
	if plan.ApprovedAt != nil {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanApprovedAt, plan.ApprovedAt.Format(time.RFC3339))
	}

	// Review
	if plan.ReviewVerdict != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanReviewVerdict, plan.ReviewVerdict)
	}
	if plan.ReviewSummary != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanReviewSummary, plan.ReviewSummary)
	}
	if plan.ReviewedAt != nil {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanReviewedAt, plan.ReviewedAt.Format(time.RFC3339))
	}
	if plan.ReviewFormattedFindings != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanReviewFormattedFindings, plan.ReviewFormattedFindings)
	}
	if plan.ReviewIteration > 0 {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanReviewIteration, plan.ReviewIteration)
	}

	// Error annotations
	if plan.LastError != "" {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanLastError, plan.LastError)
	}
	if plan.LastErrorAt != nil {
		_ = tw.UpdateTriple(ctx, entityID, semspec.PlanLastErrorAt, plan.LastErrorAt.Format(time.RFC3339))
	}

	// Scope (atomic triples — replace the whole list each write)
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.PlanScopeInclude, plan.Scope.Include)
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.PlanScopeExclude, plan.Scope.Exclude)
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.PlanScopeProtected, plan.Scope.DoNotTouch)
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.PlanScopeCreate, plan.Scope.Create)

	// ADR-040: Exploration snapshot + open question audit trail. The
	// individual Capability entities are written by writeChildTriples;
	// this block surfaces plan-level facts so a graph query on the plan
	// finds the exploration without traversing to capabilities.
	if plan.Exploration != nil {
		if blob, err := json.Marshal(plan.Exploration); err == nil {
			_ = tw.UpdateTriple(ctx, entityID, semspec.PlanExploration, string(blob))
		}
		_ = tw.ReplaceTripleList(ctx, entityID, semspec.PlanOpenQuestions, plan.Exploration.OpenQuestions)
	}

	// Execution trace IDs (replace the whole list each write)
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.PlanExecutionTraceID, plan.ExecutionTraceIDs)

	// Requirements and Scenarios — write individual entity triples so the graph
	// stays consistent when the plan is updated.
	s.writeChildTriples(ctx, tw, plan)
	return nil
}

// writeChildTriples writes requirement, scenario, capability, and change proposal triples.
func (s *planStore) writeChildTriples(ctx context.Context, tw *graphutil.TripleWriter, plan *workflow.Plan) {
	if plan.Exploration != nil && len(plan.Exploration.Capabilities) > 0 {
		// ADR-040: Capability entities + edges back to the plan.
		if err := workflow.SaveCapabilities(ctx, tw, plan.Exploration, plan.Slug); err != nil {
			s.logger.Warn("Failed to write capability triples", "slug", plan.Slug, "error", err)
		}
	}
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
	if len(plan.PlanDecisions) > 0 {
		if err := workflow.SavePlanDecisions(ctx, tw, plan.PlanDecisions, plan.Slug); err != nil {
			s.logger.Warn("Failed to write change proposal triples", "slug", plan.Slug, "error", err)
		}
	}
}
