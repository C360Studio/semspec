package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestStoryGateContext_FaithfulContextRenders verifies the re-homed Murat Story
// gate (RoleScenarioReviewer) now assembles the faithful context it previously
// lacked: plan/requirement framing, the architecture surface, project
// standards, and team lessons (go-reviewer #3 — the gate used to judge
// scenarios in isolation).
func TestStoryGateContext_FaithfulContextRenders(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	result := assembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleScenarioReviewer,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: prompt.FilterTools(allTools, prompt.RoleScenarioReviewer),
		SupportsTools:  true,
		Standards: &prompt.StandardsContext{
			Items: []prompt.StandardsItem{{ID: "STD-1", Severity: "error", Text: "always defer unlock"}},
		},
		LessonsLearned: &prompt.LessonsLearned{
			Lessons: []prompt.LessonEntry{{Summary: "watch for nil bucket", Role: "scenario-reviewer"}},
		},
		ScenarioReviewContext: &prompt.ScenarioReviewContext{
			PlanTitle:           "MAVSDK Driver",
			PlanGoal:            "control a PX4 vehicle",
			RequirementTitle:    "bootstrap the driver",
			ArchitectureContext: "## Architecture Context\n\n### Resolved Upstream Dependencies\n\n- **MAVSDK** `io.mavsdk:mavsdk:3.16.0`\n",
			Scenarios: []prompt.ScenarioSpec{
				{ID: "sc-1", Given: "a config", When: "boot", Then: []string{"connected"}},
			},
		},
	})

	for _, want := range []struct{ s, why string }{
		{"MAVSDK Driver", "plan titleframing"},
		{"bootstrap the driver", "requirement framing"},
		{"io.mavsdk:mavsdk:3.16.0", "architecture/upstream surface"},
		{"always defer unlock", "project standards"},
		{"watch for nil bucket", "team lessons"},
	} {
		if !strings.Contains(result.SystemMessage, want.s) {
			t.Errorf("Story gate prompt missing %q (%s)\n--- prompt ---\n%s", want.s, want.why, result.SystemMessage)
		}
	}
}
