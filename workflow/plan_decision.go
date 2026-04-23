package workflow

import (
	"context"
	"fmt"
	"time"

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

// writePlanDecisionTriples writes all PlanDecision fields as individual triples.
func writePlanDecisionTriples(ctx context.Context, tw *graphutil.TripleWriter, p *PlanDecision) error {
	if tw == nil {
		return nil
	}
	entityID := PlanDecisionEntityID(p.ID)

	title := p.Title
	if len([]rune(title)) > 100 {
		title = string([]rune(title)[:97]) + "..."
	}

	_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionTitle, p.Title)
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, title)
	if err := tw.WriteTriple(ctx, entityID, semspec.PlanDecisionStatus, string(p.Status)); err != nil {
		return fmt.Errorf("write change proposal status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionProposedBy, p.ProposedBy)
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionPlan, p.PlanID)
	_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionCreatedAt, p.CreatedAt.Format(time.RFC3339))

	if p.Rationale != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionRationale, p.Rationale)
	}
	if p.DecidedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionDecidedAt, p.DecidedAt.Format(time.RFC3339))
	}

	// Write each affected requirement ID as an individual triple (proper graph edges).
	for _, reqID := range p.AffectedReqIDs {
		_ = tw.WriteTriple(ctx, entityID, semspec.PlanDecisionMutates, reqID)
	}

	return nil
}
