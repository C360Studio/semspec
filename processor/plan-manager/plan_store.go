package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// planStore owns the lifecycle of plan data entities (wf.plan.plan.*).
// It follows the same 3-layer pattern as execution-manager:
//
//  1. sync.Map — hot cache, all runtime reads go here
//  2. WriteTriple — durable write-through to ENTITY_STATES via graph-ingest
//  3. reconcile — startup-only recovery from ENTITY_STATES
//
// Runtime reads NEVER hit the graph. The graph is for durability, rules,
// and crash recovery.
type planStore struct {
	cache        sync.Map // slug → *workflow.Plan
	tripleWriter *graphutil.TripleWriter
	logger       *slog.Logger
}

// newPlanStore creates a plan store with the given triple writer.
func newPlanStore(tw *graphutil.TripleWriter, logger *slog.Logger) *planStore {
	return &planStore{
		tripleWriter: tw,
		logger:       logger,
	}
}

// reconcile populates the cache from ENTITY_STATES on startup.
// This is the only time we read from the graph — all subsequent reads
// go through the cache.
func (s *planStore) reconcile(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	prefix := workflow.EntityPrefix() + ".wf.plan.plan."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, prefix, 500)
	if err != nil {
		s.logger.Info("No plan state to reconcile (expected on first start)", "error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		if triples[semspec.PredicatePlanStatus] == "deleted" {
			continue
		}

		plan := planFromTripleMap(entityID, triples)
		if plan.Slug == "" {
			continue
		}

		s.cache.Store(plan.Slug, plan)
		recovered++
	}

	if recovered > 0 {
		s.logger.Info("Plan cache reconciled from graph", "count", recovered)
	}
}

// get returns a plan by slug from the cache.
func (s *planStore) get(slug string) (*workflow.Plan, bool) {
	val, ok := s.cache.Load(slug)
	if !ok {
		return nil, false
	}
	return val.(*workflow.Plan), true
}

// list returns all plans from the cache, sorted by creation time (newest first).
func (s *planStore) list() []*workflow.Plan {
	var plans []*workflow.Plan
	s.cache.Range(func(_, value any) bool {
		plans = append(plans, value.(*workflow.Plan))
		return true
	})
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	return plans
}

// exists checks if a plan exists in the cache (not deleted).
func (s *planStore) exists(slug string) bool {
	_, ok := s.cache.Load(slug)
	return ok
}

// create creates a new plan in the cache and writes triples to ENTITY_STATES.
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

	now := time.Now()
	plan := &workflow.Plan{
		ID:        workflow.PlanEntityID(slug),
		Slug:      slug,
		Title:     title,
		ProjectID: workflow.ProjectEntityID(workflow.DefaultProjectSlug),
		Approved:  false,
		CreatedAt: now,
		Scope: workflow.Scope{
			Include:    []string{},
			Exclude:    []string{},
			DoNotTouch: []string{},
		},
	}

	if err := s.writeTriples(ctx, plan); err != nil {
		return nil, fmt.Errorf("write plan triples: %w", err)
	}

	s.cache.Store(slug, plan)
	return plan, nil
}

// save updates a plan in the cache and writes triples to ENTITY_STATES.
func (s *planStore) save(ctx context.Context, plan *workflow.Plan) error {
	if err := workflow.ValidateSlug(plan.Slug); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.writeTriples(ctx, plan); err != nil {
		return fmt.Errorf("write plan triples: %w", err)
	}

	s.cache.Store(plan.Slug, plan)
	return nil
}

// setStatus transitions a plan to a new status. Validates the transition,
// writes the status triple, and updates the cache.
func (s *planStore) setStatus(ctx context.Context, slug string, target workflow.Status) error {
	plan, ok := s.get(slug)
	if !ok {
		return fmt.Errorf("%w: %s", workflow.ErrPlanNotFound, slug)
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s → %s", workflow.ErrInvalidTransition, current, target)
	}

	plan.Status = target
	return s.save(ctx, plan)
}

// approve transitions a plan to approved status.
func (s *planStore) approve(ctx context.Context, plan *workflow.Plan) error {
	if plan.Approved {
		return fmt.Errorf("%w: %s", workflow.ErrAlreadyApproved, plan.Slug)
	}

	now := time.Now()
	plan.Approved = true
	plan.ApprovedAt = &now
	plan.Status = workflow.StatusApproved

	return s.save(ctx, plan)
}

// delete tombstones a plan by setting its status to "deleted" and removing from cache.
func (s *planStore) delete(ctx context.Context, slug string) error {
	plan, ok := s.get(slug)
	if !ok {
		return fmt.Errorf("%w: %s", workflow.ErrPlanNotFound, slug)
	}

	plan.Status = "deleted"
	if err := s.writeTriples(ctx, plan); err != nil {
		return fmt.Errorf("write delete tombstone: %w", err)
	}

	s.cache.Delete(slug)
	return nil
}

// put updates the cache for a plan that was mutated externally (e.g., via workflow functions).
// This is a transitional method — once all writes go through the store, this can be removed.
func (s *planStore) put(plan *workflow.Plan) {
	if plan != nil && plan.Slug != "" {
		s.cache.Store(plan.Slug, plan)
	}
}

// remove removes a plan from the cache. Used when a plan is deleted externally.
func (s *planStore) remove(slug string) {
	s.cache.Delete(slug)
}

// writeTriples writes all plan fields as individual triples to ENTITY_STATES.
// This is the durable write-through — the cache is the read path.
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

	return nil
}

// planFromTripleMap reconstructs a Plan from a predicate→value map.
// Used by reconcile to rebuild cache from ENTITY_STATES.
func planFromTripleMap(entityID string, triples map[string]string) *workflow.Plan {
	plan := &workflow.Plan{
		ID:   entityID,
		Slug: triples[semspec.PlanSlug],
	}

	if v := triples[semspec.PlanTitle]; v != "" {
		plan.Title = v
	}
	if v := triples[semspec.PredicatePlanStatus]; v != "" {
		plan.Status = workflow.Status(v)
	}
	if v := triples[semspec.PlanGoal]; v != "" {
		plan.Goal = v
	}
	if v := triples[semspec.PlanContext]; v != "" {
		plan.Context = v
	}
	if v := triples[semspec.PlanProject]; v != "" {
		plan.ProjectID = v
	}
	if v := triples[semspec.PlanCreatedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			plan.CreatedAt = t
		}
	}

	// Approval
	plan.Approved = triples[semspec.PlanApproved] == "true"
	if v := triples[semspec.PlanApprovedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			plan.ApprovedAt = &t
		}
	}

	// Review
	plan.ReviewVerdict = triples[semspec.PlanReviewVerdict]
	plan.ReviewSummary = triples[semspec.PlanReviewSummary]
	if v := triples[semspec.PlanReviewedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			plan.ReviewedAt = &t
		}
	}
	if v := triples[semspec.PlanReviewFindings]; v != "" {
		plan.ReviewFindings = json.RawMessage(v)
	}
	plan.ReviewFormattedFindings = triples[semspec.PlanReviewFormattedFindings]
	if v := triples[semspec.PlanReviewIteration]; v != "" {
		fmt.Sscanf(v, "%d", &plan.ReviewIteration)
	}

	// Error annotations
	plan.LastError = triples[semspec.PlanLastError]
	if v := triples[semspec.PlanLastErrorAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			plan.LastErrorAt = &t
		}
	}

	// Scope
	if v := triples[semspec.PlanScope]; v != "" {
		_ = json.Unmarshal([]byte(v), &plan.Scope)
	}

	// Execution trace IDs
	if v := triples[semspec.PlanExecutionTraceIDs]; v != "" {
		_ = json.Unmarshal([]byte(v), &plan.ExecutionTraceIDs)
	}

	// LLM call history
	if v := triples[semspec.PlanLLMCallHistory]; v != "" {
		var history workflow.LLMCallHistory
		if err := json.Unmarshal([]byte(v), &history); err == nil {
			plan.LLMCallHistory = &history
		}
	}

	return plan
}
