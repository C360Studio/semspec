package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestTaskUpstreamResolutionsFragment_AssemblerWiring runs the
// software.task-upstream-resolutions fragment through the full Software()
// pipeline to verify (a) it is registered, (b) the Condition fires only when
// TaskContext.UpstreamResolutions is non-empty, and (c) Roles gate to
// Developer + Validator + Reviewer. This is the run #6 root-cause fix: the dev
// must see the architect's resolved coordinate on the assembled prompt instead
// of re-hallucinating it.
func TestTaskUpstreamResolutionsFragment_AssemblerWiring(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	const coordinate = "io.mavsdk:mavsdk:3.16.0"

	taskCtx := func() *prompt.TaskContext {
		return &prompt.TaskContext{
			UpstreamResolutions: []prompt.UpstreamResolutionInfo{{
				Name:       "MAVSDK",
				Coordinate: coordinate,
				Role:       "runtime_dep",
				APIs: []prompt.APISurfaceInfo{{
					Symbol:    "System.connect",
					Signature: "void connect(String url)",
				}},
			}},
		}
	}

	fires := []prompt.Role{prompt.RoleDeveloper, prompt.RoleValidator, prompt.RoleReviewer}
	for _, role := range fires {
		role := role
		t.Run("fires/"+string(role), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&prompt.AssemblyContext{
				Role:           role,
				Provider:       prompt.ProviderAnthropic,
				AvailableTools: prompt.FilterTools(allTools, role),
				SupportsTools:  true,
				TaskContext:    taskCtx(),
			})
			if !strings.Contains(result.SystemMessage, coordinate) {
				t.Errorf("resolved coordinate %q missing from assembled prompt for role %s (fragment unregistered, mis-roled, or Condition broken)\n--- prompt ---\n%s", coordinate, role, result.SystemMessage)
			}
		})
	}

	t.Run("skips/empty-resolutions", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderAnthropic,
			AvailableTools: prompt.FilterTools(allTools, prompt.RoleDeveloper),
			SupportsTools:  true,
			TaskContext:    &prompt.TaskContext{},
		})
		if strings.Contains(result.SystemMessage, "Resolved Upstream Dependencies") {
			t.Errorf("upstream section rendered with empty resolutions (Condition should elide)\n--- prompt ---\n%s", result.SystemMessage)
		}
	})

	t.Run("skips/planner-role", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RolePlanner,
			Provider:       prompt.ProviderAnthropic,
			AvailableTools: prompt.FilterTools(allTools, prompt.RolePlanner),
			SupportsTools:  true,
			TaskContext:    taskCtx(),
		})
		if strings.Contains(result.SystemMessage, coordinate) {
			t.Errorf("upstream fragment leaked into planner prompt (Roles filter broken)")
		}
	})
}
