package executionmanager

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// TestResolvedHarnessProfilesToPromptPropagatesOrchestration pins the
// catalog→prompt-context seam. A regression that dropped Profile.Orchestration
// from the constructed ResolvedHarnessProfileContext would silently feed the
// developer agent profiles without the orchestration directive, leaving the
// agent to guess whether the operator's CI brings services up or the test
// fixture owns the stack. ADR-039 Phase 1a depends on this propagating
// end-to-end.
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
