package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// writePlanTriples persists all Plan fields to ENTITY_STATES as a single
// metadata-bearing entity upsert: update_with_triples with a create_with_triples
// fallback on first write. This ensures the plan node carries a MessageType
// (PlanEntityType) from its very first write and survives the incoming semstreams
// triple.add must-exist change — see docs/audit/mutation-api-graphable-bypass.md
// ("Impact" section) and issue #154 (slice #4).
//
// NOTE: this is the test-seed path only (called by CreateProjectPlan, which has
// zero production callers). Production plan creation goes through
// processor/plan-manager planStore.writePlanTriples which already uses
// UpsertEntityIfChanged.
func writePlanTriples(ctx context.Context, tw *graphutil.TripleWriter, plan *Plan) error {
	if tw == nil {
		return nil
	}
	entityID := PlanEntityID(plan.Slug)
	if err := tw.UpsertEntity(ctx, PlanEntityType, entityID, buildPlanTriples(entityID, plan)); err != nil {
		return fmt.Errorf("write plan triples: %w", err)
	}
	return nil
}

// buildPlanTriples constructs the full []message.Triple for a plan entity.
// It is a pure function so it can be unit-tested independently of NATS.
//
// Required scalars (PlanSlug, PlanTitle, DCTitle, PredicatePlanStatus,
// PlanCreatedAt, PlanApproved) are always emitted. Conditional scalars
// (ProjectID, Goal, Context, ApprovedAt, the five Review fields, the two
// Error fields) are emitted only when their Plan fields are non-zero/non-nil
// to keep entities lean. List predicates (PlanScopeInclude/Exclude/Protected/
// Create, PlanExecutionTraceID) emit one triple per element — never
// JSON-encoded — per feedback_no_json_in_triples. UpsertEntity's
// RemoveTriples = distinctPredicates(addTriples) replaces the whole list,
// which is the same net effect as the prior ReplaceTripleList calls.
//
//revive:disable-next-line:function-length // sequential triple builder; predicate order is the contract.
func buildPlanTriples(entityID string, plan *Plan) []message.Triple {
	t := func(pred, obj string) message.Triple {
		return message.Triple{Subject: entityID, Predicate: pred, Object: obj}
	}

	triples := []message.Triple{
		t(semspec.PlanSlug, plan.Slug),
		t(semspec.PlanTitle, plan.Title),
		t(semspec.DCTitle, plan.Title),
		t(semspec.PredicatePlanStatus, string(plan.EffectiveStatus())),
		t(semspec.PlanCreatedAt, plan.CreatedAt.Format(time.RFC3339)),
		t(semspec.PlanApproved, fmt.Sprintf("%t", plan.Approved)),
	}

	// Project association.
	if plan.ProjectID != "" {
		triples = append(triples, t(semspec.PlanProject, plan.ProjectID))
	}

	// Plan content.
	if plan.Goal != "" {
		triples = append(triples, t(semspec.PlanGoal, plan.Goal))
	}
	if plan.Context != "" {
		triples = append(triples, t(semspec.PlanContext, plan.Context))
	}

	// Approval timestamp.
	if plan.ApprovedAt != nil {
		triples = append(triples, t(semspec.PlanApprovedAt, plan.ApprovedAt.Format(time.RFC3339)))
	}

	// Review fields.
	if plan.ReviewVerdict != "" {
		triples = append(triples, t(semspec.PlanReviewVerdict, plan.ReviewVerdict))
	}
	if plan.ReviewSummary != "" {
		triples = append(triples, t(semspec.PlanReviewSummary, plan.ReviewSummary))
	}
	if plan.ReviewedAt != nil {
		triples = append(triples, t(semspec.PlanReviewedAt, plan.ReviewedAt.Format(time.RFC3339)))
	}
	if plan.ReviewFormattedFindings != "" {
		triples = append(triples, t(semspec.PlanReviewFormattedFindings, plan.ReviewFormattedFindings))
	}
	if plan.ReviewIteration > 0 {
		triples = append(triples, t(semspec.PlanReviewIteration, strconv.Itoa(plan.ReviewIteration)))
	}

	// Error annotations.
	if plan.LastError != "" {
		triples = append(triples, t(semspec.PlanLastError, plan.LastError))
	}
	if plan.LastErrorAt != nil {
		triples = append(triples, t(semspec.PlanLastErrorAt, plan.LastErrorAt.Format(time.RFC3339)))
	}
	if plan.Contract != nil {
		if plan.Contract.ID != "" {
			triples = append(triples, t(semspec.PlanContractID, plan.Contract.ID))
		}
		if blob, err := json.Marshal(plan.Contract); err == nil {
			triples = append(triples, t(semspec.PlanContract, string(blob)))
		}
		for _, constraint := range plan.Contract.Constraints {
			triples = append(triples, t(semspec.PlanContractConstraint, constraint))
		}
		for _, fact := range plan.Contract.TopologyFacts {
			if blob, err := json.Marshal(fact); err == nil {
				triples = append(triples, t(semspec.PlanContractTopology, string(blob)))
			}
		}
		for _, amendment := range plan.Contract.Amendments {
			if blob, err := json.Marshal(amendment); err == nil {
				triples = append(triples, t(semspec.PlanContractAmendment, string(blob)))
			}
		}
		for _, finding := range plan.Contract.ValidationFindings {
			if blob, err := json.Marshal(finding); err == nil {
				triples = append(triples, t(semspec.PlanContractValidationFinding, string(blob)))
			}
		}
	}

	// Scope lists — one triple per element (no JSON encoding).
	for _, v := range plan.Scope.Include {
		triples = append(triples, t(semspec.PlanScopeInclude, v))
	}
	for _, v := range plan.Scope.Exclude {
		triples = append(triples, t(semspec.PlanScopeExclude, v))
	}
	for _, v := range plan.Scope.DoNotTouch {
		triples = append(triples, t(semspec.PlanScopeProtected, v))
	}
	for _, v := range plan.Scope.Create {
		triples = append(triples, t(semspec.PlanScopeCreate, v))
	}

	// Execution trace IDs — one triple per ID.
	for _, v := range plan.ExecutionTraceIDs {
		triples = append(triples, t(semspec.PlanExecutionTraceID, v))
	}

	return triples
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
	if v := triples[semspec.PlanContract]; v != "" {
		var contract ContractPacket
		if err := json.Unmarshal([]byte(v), &contract); err == nil {
			plan.Contract = &contract
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

	// ADR-040: Exploration snapshot from triples. Restoring this is what
	// rescues a plan that hit StatusExplored before its KV bucket was wiped
	// (first-startup reconcile from ENTITY_STATES, or operator-triggered
	// rehydrate). Without restoration, EffectiveStatus() would fall back to
	// StatusCreated and re-run the analyst sub-phase, losing the prior
	// capability identity.
	if v := triples[semspec.PlanExploration]; v != "" {
		var exp Exploration
		if err := json.Unmarshal([]byte(v), &exp); err == nil {
			plan.Exploration = &exp
		}
	}

	return plan
}
