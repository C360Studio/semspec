//go:build integration

package workflow

import (
	"context"
	"testing"
)

func TestSavePlanDecisions_InvalidSlug(t *testing.T) {
	err := SavePlanDecisions(context.Background(), nil, []PlanDecision{}, "invalid slug!")
	if err == nil {
		t.Error("SavePlanDecisions() with invalid slug should return error")
	}
}
