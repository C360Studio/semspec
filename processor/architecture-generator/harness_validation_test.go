package architecturegenerator

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// TestHarnessResolutionHint pins that the arch-gen retry hint matches the
// validation failure CLASS, so the architect's next cycle gets the right
// correction (go-reviewer H1). Errors are produced by the real catalog
// validator so the test tracks its actual messages, not a guessed shape.
func TestHarnessResolutionHint(t *testing.T) {
	cat, err := harnesscatalog.LoadBuiltIn()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		sel       []workflow.HarnessProfileSelection
		wantInHnt string
	}{
		{
			name:      "unknown id (the docker.generic hallucination) → list valid catalog IDs",
			sel:       []workflow.HarnessProfileSelection{{ProfileID: "docker.generic"}},
			wantInHnt: "select ONLY from the available catalog profiles",
		},
		{
			name: "duplicate valid id → consolidate, not 'select from catalog'",
			sel: []workflow.HarnessProfileSelection{
				{ProfileID: "mavlink.px4-sitl.mavsdk-smoke"},
				{ProfileID: "mavlink.px4-sitl.mavsdk-smoke"},
			},
			wantInHnt: "at most once",
		},
		{
			name:      "empty id → drop the empty entry",
			sel:       []workflow.HarnessProfileSelection{{ProfileID: ""}},
			wantInHnt: "empty profile_id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verr := cat.ValidateSelections(tc.sel)
			if verr == nil {
				t.Fatalf("precondition: expected ValidateSelections to reject %v", tc.sel)
			}
			hint := harnessResolutionHint(cat, verr)
			if !strings.Contains(hint, tc.wantInHnt) {
				t.Errorf("hint = %q, want it to contain %q", hint, tc.wantInHnt)
			}
			// The duplicate hint must NOT misdirect to "select from catalog".
			if tc.name[:9] == "duplicate" && strings.Contains(hint, "select ONLY from") {
				t.Errorf("duplicate hint wrongly says 'select from catalog': %q", hint)
			}
		})
	}
}

// TestValidateGeneratedArchitecture_HarnessGate pins the end-to-end gate: a
// hallucinated harness profile_id is rejected with rule=harness_profile_resolution
// and the retry message carries the valid options; a clean architecture passes.
func TestValidateGeneratedArchitecture_HarnessGate(t *testing.T) {
	c := &Component{}

	bad := &workflow.ArchitectureDocument{
		ComponentBoundaries: []workflow.ComponentDef{
			{Name: "driver", ImplementationFiles: []string{"src/Driver.java"}, Capabilities: []string{"cap-a"}},
		},
		HarnessProfiles: []workflow.HarnessProfileSelection{{ProfileID: "docker.generic"}},
	}
	rule, msg := c.validateGeneratedArchitecture(bad, nil)
	if rule != "harness_profile_resolution" {
		t.Fatalf("rule = %q, want harness_profile_resolution (msg=%q)", rule, msg)
	}
	if !strings.Contains(msg, "mavlink.px4-sitl.mavsdk-smoke") {
		t.Errorf("retry msg should list valid catalog IDs, got %q", msg)
	}

	good := &workflow.ArchitectureDocument{
		ComponentBoundaries: []workflow.ComponentDef{
			{Name: "driver", ImplementationFiles: []string{"src/Driver.java"}, Capabilities: []string{"cap-a"}},
		},
		HarnessProfiles: []workflow.HarnessProfileSelection{{ProfileID: "mavlink.px4-sitl.mavsdk-smoke"}},
	}
	if rule, msg := c.validateGeneratedArchitecture(good, nil); rule != "" {
		t.Errorf("clean architecture rejected: rule=%q msg=%q", rule, msg)
	}
}
