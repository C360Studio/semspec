//go:build integration

package workflow

import (
	"context"
	"testing"
	"time"
)

func TestSaveLoadChangeProposals_RoundTrip(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	plan, err := CreatePlan(ctx, nil, "test-plan", "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	reviewedAt := now.Add(30 * time.Minute)

	proposals := []ChangeProposal{
		{
			ID:             "change-proposal.test-plan.1",
			PlanID:         plan.ID,
			Title:          "Expand auth scope",
			Rationale:      "OAuth is needed",
			Status:         ChangeProposalStatusProposed,
			ProposedBy:     "user",
			AffectedReqIDs: []string{"requirement.test-plan.1"},
			CreatedAt:      now,
		},
		{
			ID:             "change-proposal.test-plan.2",
			PlanID:         plan.ID,
			Title:          "Add rate limiting",
			Rationale:      "Prevent abuse",
			Status:         ChangeProposalStatusUnderReview,
			ProposedBy:     "agent",
			AffectedReqIDs: []string{"requirement.test-plan.1", "requirement.test-plan.2"},
			CreatedAt:      now,
			ReviewedAt:     &reviewedAt,
		},
	}

	if err := SaveChangeProposals(ctx, nil, proposals, plan.Slug); err != nil {
		t.Fatalf("SaveChangeProposals() error: %v", err)
	}

	got, err := LoadChangeProposals(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadChangeProposals() error: %v", err)
	}

	if len(got) != len(proposals) {
		t.Fatalf("LoadChangeProposals() returned %d items, want %d", len(got), len(proposals))
	}

	for i, want := range proposals {
		if got[i].ID != want.ID {
			t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, want.ID)
		}
		if got[i].PlanID != want.PlanID {
			t.Errorf("[%d] PlanID = %q, want %q", i, got[i].PlanID, want.PlanID)
		}
		if got[i].Title != want.Title {
			t.Errorf("[%d] Title = %q, want %q", i, got[i].Title, want.Title)
		}
		if got[i].Status != want.Status {
			t.Errorf("[%d] Status = %q, want %q", i, got[i].Status, want.Status)
		}
		if len(got[i].AffectedReqIDs) != len(want.AffectedReqIDs) {
			t.Errorf("[%d] AffectedReqIDs len = %d, want %d", i, len(got[i].AffectedReqIDs), len(want.AffectedReqIDs))
		}
		if !got[i].CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("[%d] CreatedAt = %v, want %v", i, got[i].CreatedAt, want.CreatedAt)
		}
	}

	// Verify nil and non-nil ReviewedAt round-trips correctly
	if got[0].ReviewedAt != nil {
		t.Errorf("[0] ReviewedAt = %v, want nil", got[0].ReviewedAt)
	}
	if got[1].ReviewedAt == nil || !got[1].ReviewedAt.Equal(reviewedAt) {
		t.Errorf("[1] ReviewedAt = %v, want %v", got[1].ReviewedAt, reviewedAt)
	}
}

func TestLoadChangeProposals_MissingFile_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	plan, err := CreatePlan(ctx, nil, "new-plan", "New Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	got, err := LoadChangeProposals(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadChangeProposals() on missing file should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadChangeProposals() = %d items, want 0", len(got))
	}
}

func TestSaveChangeProposals_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	err := SaveChangeProposals(ctx, nil, []ChangeProposal{}, "invalid slug!")
	if err == nil {
		t.Error("SaveChangeProposals() with invalid slug should return error")
	}
}

func TestLoadChangeProposals_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	_, err := LoadChangeProposals(ctx, nil, "invalid slug!")
	if err == nil {
		t.Error("LoadChangeProposals() with invalid slug should return error")
	}
}
