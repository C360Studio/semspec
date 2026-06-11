package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// PlanDecisionsJSONFile is the filename for machine-readable change proposal storage (JSON format).
const PlanDecisionsJSONFile = "plan_decisions.json"

// SavePlanDecisions saves change proposals to ENTITY_STATES as triples.
// Each proposal is stored as a separate entity keyed by PlanDecisionEntityID.
// Multi-valued fields (AffectedReqIDs) are written as individual triples.
func SavePlanDecisions(ctx context.Context, tw *graphutil.TripleWriter, proposals []PlanDecision, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	planEntityID := PlanEntityID(slug)
	for i := range proposals {
		if proposals[i].PlanID == "" {
			proposals[i].PlanID = planEntityID
		}
		if err := writePlanDecisionTriples(ctx, tw, &proposals[i]); err != nil {
			return fmt.Errorf("save change proposal %s: %w", proposals[i].ID, err)
		}
	}

	return nil
}

// writePlanDecisionTriples writes all PlanDecision fields as a single atomic
// batch via UpsertEntityIfChanged (Phase 3a).
func writePlanDecisionTriples(ctx context.Context, tw *graphutil.TripleWriter, p *PlanDecision) error {
	if tw == nil {
		return nil
	}
	entityID := PlanDecisionEntityID(p.ID)

	title := p.Title
	if len([]rune(title)) > 100 {
		title = string([]rune(title)[:97]) + "..."
	}

	// Build the complete predicate set.
	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.PlanDecisionTitle, Object: p.Title},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: title},
		{Subject: entityID, Predicate: semspec.PlanDecisionStatus, Object: string(p.Status)},
		{Subject: entityID, Predicate: semspec.PlanDecisionProposedBy, Object: p.ProposedBy},
		{Subject: entityID, Predicate: semspec.PlanDecisionPlan, Object: p.PlanID},
		{Subject: entityID, Predicate: semspec.PlanDecisionCreatedAt, Object: p.CreatedAt.Format(time.RFC3339)},
	}

	if p.Rationale != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanDecisionRationale, Object: p.Rationale})
	}
	if p.DecidedAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanDecisionDecidedAt, Object: p.DecidedAt.Format(time.RFC3339)})
	}

	// Affected requirement IDs (multi-valued edges).
	for _, reqID := range p.AffectedReqIDs {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanDecisionMutates, Object: reqID})
	}

	// Single batched write — skips if content unchanged, replaces own predicates.
	// OwnedPredicates covers Rationale/DecidedAt (set-once but conditionally
	// omitted) and AffectedReqIDs (multi-valued, may shrink) so they are always
	// included in RemoveTriples (C1 stale-on-empty fix).
	_, err := tw.UpsertEntityIfChanged(ctx, PlanDecisionEntityType, entityID, triples, graphutil.UpsertOpts{
		OwnedPredicates: []string{
			semspec.PlanDecisionTitle,
			semspec.DCTitle,
			semspec.PlanDecisionStatus,
			semspec.PlanDecisionProposedBy,
			semspec.PlanDecisionPlan,
			semspec.PlanDecisionCreatedAt,
			semspec.PlanDecisionRationale,
			semspec.PlanDecisionDecidedAt,
			semspec.PlanDecisionMutates,
		},
	})
	if err != nil {
		return fmt.Errorf("write plan decision %s: %w", p.ID, err)
	}
	return nil
}
