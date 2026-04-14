package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// ChangeProposalsJSONFile is the filename for machine-readable change proposal storage (JSON format).
const ChangeProposalsJSONFile = "change_proposals.json"

// SaveChangeProposals saves change proposals to ENTITY_STATES as triples.
// Each proposal is stored as a separate entity keyed by ChangeProposalEntityID.
// Multi-valued fields (AffectedReqIDs) are written as individual triples.
func SaveChangeProposals(ctx context.Context, tw *graphutil.TripleWriter, proposals []ChangeProposal, slug string) error {
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
		if err := writeChangeProposalTriples(ctx, tw, &proposals[i]); err != nil {
			return fmt.Errorf("save change proposal %s: %w", proposals[i].ID, err)
		}
	}

	return nil
}

// writeChangeProposalTriples writes all ChangeProposal fields as individual triples.
func writeChangeProposalTriples(ctx context.Context, tw *graphutil.TripleWriter, p *ChangeProposal) error {
	if tw == nil {
		return nil
	}
	entityID := ChangeProposalEntityID(p.ID)

	title := p.Title
	if len([]rune(title)) > 100 {
		title = string([]rune(title)[:97]) + "..."
	}

	_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalTitle, p.Title)
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, title)
	if err := tw.WriteTriple(ctx, entityID, semspec.ChangeProposalStatus, string(p.Status)); err != nil {
		return fmt.Errorf("write change proposal status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalProposedBy, p.ProposedBy)
	_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalPlan, p.PlanID)
	_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalCreatedAt, p.CreatedAt.Format(time.RFC3339))

	if p.Rationale != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalRationale, p.Rationale)
	}
	if p.DecidedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalDecidedAt, p.DecidedAt.Format(time.RFC3339))
	}

	// Write each affected requirement ID as an individual triple (proper graph edges).
	for _, reqID := range p.AffectedReqIDs {
		_ = tw.WriteTriple(ctx, entityID, semspec.ChangeProposalMutates, reqID)
	}

	return nil
}
