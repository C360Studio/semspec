package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChangeProposalStatus_IsValid(t *testing.T) {
	tests := []struct {
		status ChangeProposalStatus
		want   bool
	}{
		{ChangeProposalStatusProposed, true},
		{ChangeProposalStatusUnderReview, true},
		{ChangeProposalStatusAccepted, true},
		{ChangeProposalStatusRejected, true},
		{ChangeProposalStatusArchived, true},
		{"", false},
		{"unknown", false},
		{"Proposed", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("ChangeProposalStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestChangeProposalStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from ChangeProposalStatus
		to   ChangeProposalStatus
		want bool
	}{
		// proposed transitions
		{ChangeProposalStatusProposed, ChangeProposalStatusUnderReview, true},
		{ChangeProposalStatusProposed, ChangeProposalStatusAccepted, true}, // auto-accept shortcut
		{ChangeProposalStatusProposed, ChangeProposalStatusRejected, false},
		{ChangeProposalStatusProposed, ChangeProposalStatusArchived, false},
		// under_review transitions
		{ChangeProposalStatusUnderReview, ChangeProposalStatusAccepted, true},
		{ChangeProposalStatusUnderReview, ChangeProposalStatusRejected, true},
		{ChangeProposalStatusUnderReview, ChangeProposalStatusProposed, false},
		{ChangeProposalStatusUnderReview, ChangeProposalStatusArchived, false},
		// accepted transitions
		{ChangeProposalStatusAccepted, ChangeProposalStatusArchived, true},
		{ChangeProposalStatusAccepted, ChangeProposalStatusProposed, false},
		{ChangeProposalStatusAccepted, ChangeProposalStatusRejected, false},
		// rejected transitions
		{ChangeProposalStatusRejected, ChangeProposalStatusArchived, true},
		{ChangeProposalStatusRejected, ChangeProposalStatusProposed, false},
		{ChangeProposalStatusRejected, ChangeProposalStatusAccepted, false},
		// archived is terminal
		{ChangeProposalStatusArchived, ChangeProposalStatusProposed, false},
		{ChangeProposalStatusArchived, ChangeProposalStatusAccepted, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("(%q).CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestChangeProposal_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reviewedAt := now.Add(1 * time.Hour)
	decidedAt := now.Add(2 * time.Hour)

	proposal := ChangeProposal{
		ID:             "change-proposal.my-plan.1",
		PlanID:         "semspec.local.project.default.plan.my-plan",
		Title:          "Expand authentication scope",
		Rationale:      "OAuth support is needed for enterprise customers",
		Status:         ChangeProposalStatusProposed,
		ProposedBy:     "user",
		AffectedReqIDs: []string{"requirement.my-plan.1", "requirement.my-plan.2"},
		CreatedAt:      now,
		ReviewedAt:     &reviewedAt,
		DecidedAt:      &decidedAt,
	}

	data, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var got ChangeProposal
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if got.ID != proposal.ID {
		t.Errorf("ID = %q, want %q", got.ID, proposal.ID)
	}
	if got.PlanID != proposal.PlanID {
		t.Errorf("PlanID = %q, want %q", got.PlanID, proposal.PlanID)
	}
	if got.Title != proposal.Title {
		t.Errorf("Title = %q, want %q", got.Title, proposal.Title)
	}
	if got.Rationale != proposal.Rationale {
		t.Errorf("Rationale = %q, want %q", got.Rationale, proposal.Rationale)
	}
	if got.Status != proposal.Status {
		t.Errorf("Status = %q, want %q", got.Status, proposal.Status)
	}
	if got.ProposedBy != proposal.ProposedBy {
		t.Errorf("ProposedBy = %q, want %q", got.ProposedBy, proposal.ProposedBy)
	}
	if len(got.AffectedReqIDs) != len(proposal.AffectedReqIDs) {
		t.Fatalf("AffectedReqIDs len = %d, want %d", len(got.AffectedReqIDs), len(proposal.AffectedReqIDs))
	}
	for i, id := range proposal.AffectedReqIDs {
		if got.AffectedReqIDs[i] != id {
			t.Errorf("AffectedReqIDs[%d] = %q, want %q", i, got.AffectedReqIDs[i], id)
		}
	}
	if !got.CreatedAt.Equal(proposal.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, proposal.CreatedAt)
	}
	if got.ReviewedAt == nil || !got.ReviewedAt.Equal(*proposal.ReviewedAt) {
		t.Errorf("ReviewedAt = %v, want %v", got.ReviewedAt, proposal.ReviewedAt)
	}
	if got.DecidedAt == nil || !got.DecidedAt.Equal(*proposal.DecidedAt) {
		t.Errorf("DecidedAt = %v, want %v", got.DecidedAt, proposal.DecidedAt)
	}
}

func TestChangeProposal_JSONRoundTrip_NilOptionalFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	proposal := ChangeProposal{
		ID:             "change-proposal.my-plan.2",
		PlanID:         "semspec.local.project.default.plan.my-plan",
		Title:          "Add logging requirement",
		Rationale:      "Audit trail needed",
		Status:         ChangeProposalStatusProposed,
		ProposedBy:     "agent",
		AffectedReqIDs: []string{"requirement.my-plan.3"},
		CreatedAt:      now,
	}

	data, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var got ChangeProposal
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if got.ReviewedAt != nil {
		t.Errorf("ReviewedAt = %v, want nil", got.ReviewedAt)
	}
	if got.DecidedAt != nil {
		t.Errorf("DecidedAt = %v, want nil", got.DecidedAt)
	}
}
