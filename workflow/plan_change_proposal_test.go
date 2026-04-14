//go:build integration

package workflow

import (
	"context"
	"testing"
)

func TestSaveChangeProposals_InvalidSlug(t *testing.T) {
	err := SaveChangeProposals(context.Background(), nil, []ChangeProposal{}, "invalid slug!")
	if err == nil {
		t.Error("SaveChangeProposals() with invalid slug should return error")
	}
}
