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

// changeProposalFromTripleMap reconstructs a ChangeProposal from a predicate→[]values map.
// Single-valued predicates use the first element; AffectedReqIDs collects all values.
func changeProposalFromTripleMap(entityID string, triples map[string][]string) ChangeProposal {
	first := func(pred string) string {
		if vs := triples[pred]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	p := ChangeProposal{
		ID:     extractChangeProposalID(entityID),
		PlanID: first(semspec.ChangeProposalPlan),
	}

	if v := first(semspec.ChangeProposalTitle); v != "" {
		p.Title = v
	}
	if v := first(semspec.ChangeProposalStatus); v != "" {
		p.Status = ChangeProposalStatus(v)
	}
	if v := first(semspec.ChangeProposalProposedBy); v != "" {
		p.ProposedBy = v
	}
	if v := first(semspec.ChangeProposalRationale); v != "" {
		p.Rationale = v
	}
	if v := first(semspec.ChangeProposalCreatedAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			p.CreatedAt = t
		}
	}
	if v := first(semspec.ChangeProposalDecidedAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			p.DecidedAt = &t
		}
	}
	// AffectedReqIDs are written as one triple per ID; collect all values.
	for _, reqID := range triples[semspec.ChangeProposalMutates] {
		if reqID != "" {
			p.AffectedReqIDs = append(p.AffectedReqIDs, reqID)
		}
	}
	if p.AffectedReqIDs == nil {
		p.AffectedReqIDs = []string{}
	}

	return p
}

// extractChangeProposalID extracts the raw change proposal ID from the entity ID.
// Entity ID format: {prefix}.wf.plan.proposal.{id}
func extractChangeProposalID(entityID string) string {
	prefix := EntityPrefix() + ".wf.plan.proposal."
	if len(entityID) > len(prefix) {
		return entityID[len(prefix):]
	}
	return entityID
}

// LoadChangeProposals loads change proposals for a plan from ENTITY_STATES triples.
func LoadChangeProposals(ctx context.Context, tw *graphutil.TripleWriter, slug string) ([]ChangeProposal, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if tw == nil {
		return []ChangeProposal{}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := EntityPrefix() + ".wf.plan.proposal."
	entities, err := tw.ReadEntitiesByPrefixMulti(ctx, prefix, 500)
	if err != nil {
		return []ChangeProposal{}, nil
	}

	planEntityID := PlanEntityID(slug)
	var proposals []ChangeProposal

	for entityID, triples := range entities {
		p := changeProposalFromTripleMap(entityID, triples)
		if p.PlanID == planEntityID {
			proposals = append(proposals, p)
		}
	}

	if proposals == nil {
		proposals = []ChangeProposal{}
	}

	return proposals, nil
}
