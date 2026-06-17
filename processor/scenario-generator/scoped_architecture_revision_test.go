package scenariogenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestScenarioGenerationRequirementScope_ScopedArchitectureRevision(t *testing.T) {
	plan := &workflow.Plan{
		PendingArchitectureRevision: &workflow.ArchitectureRevisionScope{
			RequirementIDs: []string{"req.demo.6", "", "req.demo.7"},
		},
	}

	got := scenarioGenerationRequirementScope(plan)
	if len(got) != 2 {
		t.Fatalf("scope len = %d, want 2: %v", len(got), got)
	}
	for _, id := range []string{"req.demo.6", "req.demo.7"} {
		if !got[id] {
			t.Fatalf("scope = %v, missing %q", got, id)
		}
	}
	if got["req.demo.1"] {
		t.Fatalf("scope = %v, should not include unrelated requirement", got)
	}
}

func TestScenarioGenerationRequirementScope_FullPlan(t *testing.T) {
	if got := scenarioGenerationRequirementScope(&workflow.Plan{}); got != nil {
		t.Fatalf("scope = %v, want nil for full-plan generation", got)
	}
}
