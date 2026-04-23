package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPlanDecisionStatus_IsValid(t *testing.T) {
	tests := []struct {
		status PlanDecisionStatus
		want   bool
	}{
		{PlanDecisionStatusProposed, true},
		{PlanDecisionStatusUnderReview, true},
		{PlanDecisionStatusAccepted, true},
		{PlanDecisionStatusRejected, true},
		{PlanDecisionStatusArchived, true},
		{"", false},
		{"unknown", false},
		{"Proposed", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("PlanDecisionStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestPlanDecisionStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from PlanDecisionStatus
		to   PlanDecisionStatus
		want bool
	}{
		// proposed transitions
		{PlanDecisionStatusProposed, PlanDecisionStatusUnderReview, true},
		{PlanDecisionStatusProposed, PlanDecisionStatusAccepted, true}, // auto-accept shortcut
		{PlanDecisionStatusProposed, PlanDecisionStatusRejected, false},
		// proposed → archived supports plan-manager auto-closing decisions
		// whose subject requirement reaches a non-failed terminal state
		// (item 4 design: exhaustion decisions aren't accepted, they archive).
		{PlanDecisionStatusProposed, PlanDecisionStatusArchived, true},
		// under_review transitions
		{PlanDecisionStatusUnderReview, PlanDecisionStatusAccepted, true},
		{PlanDecisionStatusUnderReview, PlanDecisionStatusRejected, true},
		{PlanDecisionStatusUnderReview, PlanDecisionStatusProposed, false},
		{PlanDecisionStatusUnderReview, PlanDecisionStatusArchived, true},
		// accepted transitions
		{PlanDecisionStatusAccepted, PlanDecisionStatusArchived, true},
		{PlanDecisionStatusAccepted, PlanDecisionStatusProposed, false},
		{PlanDecisionStatusAccepted, PlanDecisionStatusRejected, false},
		// rejected transitions
		{PlanDecisionStatusRejected, PlanDecisionStatusArchived, true},
		{PlanDecisionStatusRejected, PlanDecisionStatusProposed, false},
		{PlanDecisionStatusRejected, PlanDecisionStatusAccepted, false},
		// archived is terminal
		{PlanDecisionStatusArchived, PlanDecisionStatusProposed, false},
		{PlanDecisionStatusArchived, PlanDecisionStatusAccepted, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("(%q).CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestPlanDecision_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reviewedAt := now.Add(1 * time.Hour)
	decidedAt := now.Add(2 * time.Hour)

	proposal := PlanDecision{
		ID:             "plan-decision.my-plan.1",
		PlanID:         "semspec.local.project.default.plan.my-plan",
		Title:          "Expand authentication scope",
		Rationale:      "OAuth support is needed for enterprise customers",
		Status:         PlanDecisionStatusProposed,
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

	var got PlanDecision
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

func TestPlanDecision_JSONRoundTrip_NilOptionalFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	proposal := PlanDecision{
		ID:             "plan-decision.my-plan.2",
		PlanID:         "semspec.local.project.default.plan.my-plan",
		Title:          "Add logging requirement",
		Rationale:      "Audit trail needed",
		Status:         PlanDecisionStatusProposed,
		ProposedBy:     "agent",
		AffectedReqIDs: []string{"requirement.my-plan.3"},
		CreatedAt:      now,
	}

	data, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var got PlanDecision
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
