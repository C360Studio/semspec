package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// writePlanTriples writes all Plan fields as individual triples to ENTITY_STATES.
// Same pattern as execution-orchestrator: one WriteTriple call per field.
func writePlanTriples(ctx context.Context, tw *graphutil.TripleWriter, plan *Plan) error {
	if tw == nil {
		return nil
	}
	entityID := PlanEntityID(plan.Slug)

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

	// Scope (atomic triples — one per entry to avoid JSON objects as KV keys)
	for _, inc := range plan.Scope.Include {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanScopeInclude, inc)
	}
	for _, exc := range plan.Scope.Exclude {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanScopeExclude, exc)
	}
	for _, dnt := range plan.Scope.DoNotTouch {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanScopeProtected, dnt)
	}
	for _, cr := range plan.Scope.Create {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanScopeCreate, cr)
	}

	// Execution trace IDs (one triple per ID — avoid JSON arrays as KV keys)
	for _, traceID := range plan.ExecutionTraceIDs {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanExecutionTraceID, traceID)
	}

	return nil
}

// PlanFromTripleMap reconstructs a Plan from a predicate→value map.
// Same pattern as execution-orchestrator reconciliation.
func PlanFromTripleMap(entityID string, triples map[string]string) *Plan {
	plan := &Plan{
		ID:   entityID,
		Slug: triples[semspec.PlanSlug],
	}

	if v := triples[semspec.PlanTitle]; v != "" {
		plan.Title = v
	}
	if v := triples[semspec.PredicatePlanStatus]; v != "" {
		plan.Status = Status(v)
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
		plan.ReviewIteration, _ = strconv.Atoi(v)
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
		var history LLMCallHistory
		if err := json.Unmarshal([]byte(v), &history); err == nil {
			plan.LLMCallHistory = &history
		}
	}

	return plan
}
