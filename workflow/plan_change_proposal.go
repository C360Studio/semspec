package workflow

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/natsclient"
)

// ChangeProposalsJSONFile is the filename for machine-readable change proposal storage (JSON format).
const ChangeProposalsJSONFile = "change_proposals.json"

// SaveChangeProposals saves change proposals to ENTITY_STATES KV bucket.
// Each proposal is stored as a separate entity keyed by ChangeProposalEntityID.
func SaveChangeProposals(ctx context.Context, kv *natsclient.KVStore, proposals []ChangeProposal, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range proposals {
		if proposals[i].PlanID == "" {
			proposals[i].PlanID = PlanEntityID(slug)
		}
		if err := kvPut(ctx, kv, ChangeProposalEntityID(proposals[i].ID), proposals[i]); err != nil {
			return fmt.Errorf("save change proposal %s: %w", proposals[i].ID, err)
		}
	}

	return nil
}

// LoadChangeProposals loads change proposals for a plan from ENTITY_STATES KV bucket.
func LoadChangeProposals(ctx context.Context, kv *natsclient.KVStore, slug string) ([]ChangeProposal, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if kv == nil {
		return []ChangeProposal{}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := EntityPrefix() + ".wf.plan.proposal."
	keys, err := kv.KeysByPrefix(ctx, prefix)
	if err != nil {
		return []ChangeProposal{}, nil
	}

	planEntityID := PlanEntityID(slug)
	var proposals []ChangeProposal

	for _, key := range keys {
		var p ChangeProposal
		if err := kvGet(ctx, kv, key, &p); err != nil {
			continue
		}

		if p.PlanID == planEntityID {
			proposals = append(proposals, p)
		}
	}

	if proposals == nil {
		proposals = []ChangeProposal{}
	}

	return proposals, nil
}
