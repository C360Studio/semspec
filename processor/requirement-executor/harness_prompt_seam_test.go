package requirementexecutor

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// TestResolvedHarnessProfilesToPromptPropagatesOrchestration pins the same
// seam as the execution-manager copy of this helper — both must populate
// ResolvedHarnessProfileContext.Orchestration from the catalog profile's
// effective orchestration. See execution-manager/harness_prompt_seam_test.go
// for the rationale.
func TestResolvedHarnessProfilesToPromptPropagatesOrchestration(t *testing.T) {
	cat, err := harnesscatalog.LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn() error = %v", err)
	}
	resolved, err := cat.ResolveSelections([]workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
		{ProfileID: "mavlink.raw-mavlink-direct", Purpose: "round-trip frames"},
	})
	if err != nil {
		t.Fatalf("ResolveSelections() error = %v", err)
	}

	out := resolvedHarnessProfilesToPrompt(resolved)
	if len(out) != 2 {
		t.Fatalf("got %d prompt contexts, want 2", len(out))
	}

	wantOrchestration := map[string]string{
		"mavlink.px4-sitl.mavsdk-smoke": harnesscatalog.OrchestrationServices,
		"mavlink.raw-mavlink-direct":    harnesscatalog.OrchestrationPureFixture,
	}
	for _, ctx := range out {
		want, ok := wantOrchestration[ctx.ProfileID]
		if !ok {
			t.Errorf("unexpected profile %q in output", ctx.ProfileID)
			continue
		}
		if ctx.Orchestration != want {
			t.Errorf("profile %q orchestration = %q, want %q", ctx.ProfileID, ctx.Orchestration, want)
		}
	}
}
