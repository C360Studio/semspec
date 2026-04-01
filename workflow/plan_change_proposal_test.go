//go:build integration

package workflow

import (
	"context"
	"testing"
)

func TestLoadChangeProposals_NilTripleWriter_ReturnsEmpty(t *testing.T) {
	got, err := LoadChangeProposals(context.Background(), nil, "any-plan")
	if err != nil {
		t.Fatalf("LoadChangeProposals() with nil tw should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadChangeProposals() = %d items, want 0", len(got))
	}
}

func TestSaveChangeProposals_InvalidSlug(t *testing.T) {
	err := SaveChangeProposals(context.Background(), nil, []ChangeProposal{}, "invalid slug!")
	if err == nil {
		t.Error("SaveChangeProposals() with invalid slug should return error")
	}
}

func TestLoadChangeProposals_InvalidSlug(t *testing.T) {
	_, err := LoadChangeProposals(context.Background(), nil, "invalid slug!")
	if err == nil {
		t.Error("LoadChangeProposals() with invalid slug should return error")
	}
}
