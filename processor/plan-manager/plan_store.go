package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
	sscache "github.com/c360studio/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
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
	repoPath     string
}

// newPlanStore creates a plan store backed by a TTL in-memory cache.
// Cache is a performance optimization — KV is the durable read source.
// Plans that outlive the TTL (e.g., waiting for human review) are served
// via KV fallback on cache miss (see get()). This is the reference pattern.
// kv may be nil — store operates in cache+graph-only mode when absent.
func newPlanStore(ctx context.Context, kv jetstream.KeyValue, tw *graphutil.TripleWriter, logger *slog.Logger, repoPath ...string) (*planStore, error) {
	c, err := sscache.NewTTL[*workflow.Plan](ctx, 30*time.Minute, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("create plan cache: %w", err)
	}
	var root string
	if len(repoPath) > 0 {
		root = strings.TrimSpace(repoPath[0])
	}
	return &planStore{
		cache:        c,
		kvBucket:     kv,
		tripleWriter: tw,
		logger:       logger,
		repoPath:     root,
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
func (s *planStore) create(ctx context.Context, slug, title, brief string, qaLevel workflow.QALevel, autoRejectOverride *bool) (*workflow.Plan, error) {
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
	if brief == "" {
		brief = title
	}
	plan.EnsureContractPacket(brief, now)
	s.ensureRuntimeContractFacts(plan)

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
	if plan.Contract == nil {
		brief := plan.Context
		if brief == "" {
			brief = plan.Title
		}
		plan.EnsureContractPacket(brief, now)
	}
	s.ensureRuntimeContractFacts(plan)
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

func (s *planStore) ensureRuntimeContractFacts(plan *workflow.Plan) {
	if plan == nil || plan.Contract == nil {
		return
	}
	if len(plan.Contract.TopologyFacts) > 0 {
		return
	}
	repoPath := strings.TrimSpace(s.repoPath)
	if repoPath == "" {
		return
	}
	result, err := workflow.NewFileSystemDetector().Detect(repoPath)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to detect topology facts for plan contract", "slug", plan.Slug, "repo_path", repoPath, "error", err)
		}
		return
	}
	if result == nil || len(result.TopologyFacts) == 0 {
		return
	}
	plan.Contract.TopologyFacts = append([]workflow.TopologyFact(nil), result.TopologyFacts...)
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

	// Evict the plan entity from the dirty-hash map so a re-created plan with
	// the same slug re-persists its graph entity rather than being silently
	// skipped by dirty-track (UpsertEntityIfChanged would see a hash match on
	// the "deleted" content and skip the first real write after re-creation).
	if s.tripleWriter != nil {
		s.tripleWriter.Evict(workflow.PlanEntityID(slug))
	}

	s.cache.Delete(slug) //nolint:errcheck // cache delete is best-effort
	if s.kvBucket != nil {
		_ = s.kvBucket.Delete(ctx, slug)
	}
	return nil
}

// writeTriples writes the plan parent entity to ENTITY_STATES as a single
// atomic batch via UpsertEntityIfChanged (Phase 3a Levers 1+2), then writes
// all child entities (requirements, scenarios, capabilities, decisions).
//
// During execution, plan children (requirements, scenarios, capabilities) are
// static — only the plan parent's status/lastError fields change. In that case
// the child dirty-track gates skip all ~19 unchanged children, yielding ≈0
// ENTITY_STATES writes for them. On a pure SSE/convergence bump where only a
// non-triple-mapped field changes (e.g. ExecutionSummary), the plan parent's
// hash also does not change → ZERO ENTITY_STATES writes total. The "plan parent
// re-persists on every mutation" framing is only true when a triple-mirrored
// field actually changes. Combined reduction on a typical execution save:
// ~20× fewer writes, ~14.5× fewer NATS round-trips.
func (s *planStore) writeTriples(ctx context.Context, plan *workflow.Plan) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.PlanEntityID(plan.Slug)

	// Build the complete predicate set for the plan parent entity.
	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.PlanSlug, Object: plan.Slug},
		{Subject: entityID, Predicate: semspec.PlanTitle, Object: plan.Title},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: plan.Title},
		{Subject: entityID, Predicate: semspec.PredicatePlanStatus, Object: string(plan.EffectiveStatus())},
		{Subject: entityID, Predicate: semspec.PlanCreatedAt, Object: plan.CreatedAt.Format(time.RFC3339)},
		// Approval (bool written unconditionally so it lands in RemoveTriples).
		{Subject: entityID, Predicate: semspec.PlanApproved, Object: fmt.Sprintf("%t", plan.Approved)},
	}

	if plan.ProjectID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanProject, Object: plan.ProjectID})
	}
	if plan.Goal != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanGoal, Object: plan.Goal})
	}
	if plan.Context != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContext, Object: plan.Context})
	}
	if plan.ApprovedAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanApprovedAt, Object: plan.ApprovedAt.Format(time.RFC3339)})
	}
	if plan.ReviewVerdict != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanReviewVerdict, Object: plan.ReviewVerdict})
	}
	if plan.ReviewSummary != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanReviewSummary, Object: plan.ReviewSummary})
	}
	if plan.ReviewedAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanReviewedAt, Object: plan.ReviewedAt.Format(time.RFC3339)})
	}
	if plan.ReviewFormattedFindings != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanReviewFormattedFindings, Object: plan.ReviewFormattedFindings})
	}
	if plan.ReviewIteration > 0 {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanReviewIteration, Object: plan.ReviewIteration})
	}
	if plan.LastError != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanLastError, Object: plan.LastError})
	}
	if plan.LastErrorAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanLastErrorAt, Object: plan.LastErrorAt.Format(time.RFC3339)})
	}
	if plan.Contract != nil {
		if plan.Contract.ID != "" {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContractID, Object: plan.Contract.ID})
		}
		if blob, err := json.Marshal(plan.Contract); err == nil {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContract, Object: string(blob)})
		}
		for _, constraint := range plan.Contract.Constraints {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContractConstraint, Object: constraint})
		}
		for _, fact := range plan.Contract.TopologyFacts {
			if blob, err := json.Marshal(fact); err == nil {
				triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContractTopology, Object: string(blob)})
			}
		}
		for _, amendment := range plan.Contract.Amendments {
			if blob, err := json.Marshal(amendment); err == nil {
				triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContractAmendment, Object: string(blob)})
			}
		}
		for _, finding := range plan.Contract.ValidationFindings {
			if blob, err := json.Marshal(finding); err == nil {
				triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContractValidationFinding, Object: string(blob)})
			}
		}
	}

	// Scope lists (replace-as-a-set — emit full current list each write).
	for _, v := range plan.Scope.Include {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanScopeInclude, Object: v})
	}
	for _, v := range plan.Scope.Exclude {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanScopeExclude, Object: v})
	}
	for _, v := range plan.Scope.DoNotTouch {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanScopeProtected, Object: v})
	}
	for _, v := range plan.Scope.Create {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanScopeCreate, Object: v})
	}

	// ADR-040: Exploration snapshot (JSON blob + open questions list).
	if plan.Exploration != nil {
		if blob, err := json.Marshal(plan.Exploration); err == nil {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanExploration, Object: string(blob)})
		}
		for _, q := range plan.Exploration.OpenQuestions {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanOpenQuestions, Object: q})
		}
	}

	// Execution trace IDs.
	for _, traceID := range plan.ExecutionTraceIDs {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanExecutionTraceID, Object: traceID})
	}

	// Single batched write for the plan parent — skips if content unchanged.
	// OwnedPredicates covers the full set of predicates this writer may emit,
	// including list predicates that may be empty on a given save (C1 fix).
	if _, err := tw.UpsertEntityIfChanged(ctx, workflow.PlanEntityType, entityID, triples, graphutil.UpsertOpts{
		OwnedPredicates: []string{
			semspec.PlanSlug,
			semspec.PlanTitle,
			semspec.DCTitle,
			semspec.PredicatePlanStatus,
			semspec.PlanCreatedAt,
			semspec.PlanApproved,
			semspec.PlanProject,
			semspec.PlanGoal,
			semspec.PlanContext,
			semspec.PlanApprovedAt,
			semspec.PlanReviewVerdict,
			semspec.PlanReviewSummary,
			semspec.PlanReviewedAt,
			semspec.PlanReviewFormattedFindings,
			semspec.PlanReviewIteration,
			semspec.PlanLastError,
			semspec.PlanLastErrorAt,
			semspec.PlanContractID,
			semspec.PlanContract,
			semspec.PlanContractConstraint,
			semspec.PlanContractTopology,
			semspec.PlanContractAmendment,
			semspec.PlanContractValidationFinding,
			semspec.PlanScopeInclude,
			semspec.PlanScopeExclude,
			semspec.PlanScopeProtected,
			semspec.PlanScopeCreate,
			semspec.PlanExploration,
			semspec.PlanOpenQuestions,
			semspec.PlanExecutionTraceID,
		},
	}); err != nil {
		return fmt.Errorf("write plan triples: %w", err)
	}

	// Write child entities (requirements, scenarios, capabilities, decisions).
	// Each child uses its own UpsertEntityIfChanged gate — only dirty children
	// produce NATS calls.
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
