package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
)

func TestContractProjectionFragmentReachesCoreRoles(t *testing.T) {
	reg := prompt.NewRegistry()
	reg.RegisterAll(Software()...)
	assembler := prompt.NewAssembler(reg)
	packet := testContractPacket()

	roles := []prompt.Role{
		prompt.RolePlanner,
		prompt.RoleArchitect,
		prompt.RoleRequirementGenerator,
		prompt.RoleStoryPreparer,
		prompt.RoleScenarioGenerator,
		prompt.RoleDeveloper,
		prompt.RoleReviewer,
		prompt.RoleRecoveryAgent,
		prompt.RolePlanQAReviewer,
	}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			out := assembler.Assemble(&prompt.AssemblyContext{
				Role:               role,
				Provider:           prompt.ProviderOpenAI,
				ContractProjection: prompt.BuildContractProjectionFromPacket(packet, role),
			})
			system := out.SystemMessage
			for _, want := range []string{
				"AUTHORITATIVE CONTRACT PACKET",
				packet.ID,
				"preserve existing baseline modules",
				"deliver integration coverage",
				"do not create a standalone clean-room project",
			} {
				if !strings.Contains(system, want) {
					t.Fatalf("system prompt for %s missing %q\n--- system ---\n%s", role, want, system)
				}
			}
		})
	}
}

func TestContractProjectionFragmentKeepsTopologyRoleScoped(t *testing.T) {
	reg := prompt.NewRegistry()
	reg.RegisterAll(Software()...)
	assembler := prompt.NewAssembler(reg)
	packet := testContractPacket()

	tests := []struct {
		role         prompt.Role
		wantTopology bool
		wantFindings bool
	}{
		{role: prompt.RolePlanner},
		{role: prompt.RoleRequirementGenerator},
		{role: prompt.RoleScenarioGenerator},
		{role: prompt.RoleArchitect, wantTopology: true},
		{role: prompt.RoleStoryPreparer, wantTopology: true},
		{role: prompt.RoleDeveloper, wantTopology: true},
		{role: prompt.RoleReviewer, wantTopology: true, wantFindings: true},
		{role: prompt.RoleRecoveryAgent, wantTopology: true, wantFindings: true},
		{role: prompt.RolePlanQAReviewer, wantTopology: true, wantFindings: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			out := assembler.Assemble(&prompt.AssemblyContext{
				Role:               tt.role,
				Provider:           prompt.ProviderOpenAI,
				ContractProjection: prompt.BuildContractProjectionFromPacket(packet, tt.role),
			})
			system := out.SystemMessage
			if got := strings.Contains(system, "Topology obligations"); got != tt.wantTopology {
				t.Fatalf("topology rendered = %t, want %t\n--- system ---\n%s", got, tt.wantTopology, system)
			}
			if got := strings.Contains(system, "Contract validation findings"); got != tt.wantFindings {
				t.Fatalf("findings rendered = %t, want %t\n--- system ---\n%s", got, tt.wantFindings, system)
			}
		})
	}
}

func testContractPacket() *workflow.ContractPacket {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	return &workflow.ContractPacket{
		ID:          workflow.PlanContractID("contract-render"),
		Version:     1,
		Brief:       "Extend the brownfield baseline.",
		SourceRefs:  []workflow.ContractSourceRef{{Kind: "user_brief", Ref: "contract-render"}},
		Constraints: []string{"preserve existing baseline modules"},
		AcceptanceObligations: []string{
			"deliver integration coverage",
		},
		ForbiddenMoves: []string{
			"do not create a standalone clean-room project",
		},
		Scope: workflow.ContractScopeSnapshot{
			Include: []string{"baseline.gradle"},
			Create:  []string{"src/main/java/example/Adapter.java"},
		},
		TopologyFacts: []workflow.TopologyFact{{
			Kind:     "forbidden_file",
			Path:     "settings.gradle",
			Evidence: []string{"qa composite build substitutes repository root"},
		}},
		Amendments: []workflow.ContractAmendment{{
			ID:             "amendment-1",
			PlanDecisionID: "plan-decision.contract-render.1",
			Impact: workflow.ContractImpact{
				Kind:        workflow.ContractImpactRefine,
				Summary:     "Refine one Story without dropping baseline work.",
				AffectedIDs: []string{"story.contract-render.1"},
			},
			CreatedAt: now,
		}},
		ValidationFindings: []workflow.ContractValidationFinding{{
			ID:        "finding-1",
			Severity:  "error",
			Category:  "topology",
			Message:   "standalone settings.gradle conflicts with the composite baseline",
			Evidence:  []string{"settings.gradle"},
			CreatedAt: now,
		}},
		CreatedAt: now,
	}
}
