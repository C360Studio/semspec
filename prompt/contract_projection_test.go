package prompt

import (
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestContractProjectionBuildersAreRoleSpecific(t *testing.T) {
	plan := testContractProjectionPlan()

	tests := []struct {
		name          string
		build         func(*workflow.Plan) *ContractProjection
		wantProfile   ContractProjectionProfile
		wantTopology  bool
		wantFindings  bool
		wantSourceRef bool
	}{
		{
			name:          "planner sees source but not topology findings",
			build:         PlannerContractProjection,
			wantProfile:   ContractProjectionPlanner,
			wantSourceRef: true,
		},
		{
			name:         "architect sees topology",
			build:        ArchitectContractProjection,
			wantProfile:  ContractProjectionArchitect,
			wantTopology: true,
		},
		{
			name:        "requirement generator stays contract scoped without topology inventory",
			build:       RequirementGeneratorContractProjection,
			wantProfile: ContractProjectionRequirementGenerator,
		},
		{
			name:         "story preparer sees topology",
			build:        StoryPreparerContractProjection,
			wantProfile:  ContractProjectionStoryPreparer,
			wantTopology: true,
		},
		{
			name:        "scenario generator sees obligations without topology inventory",
			build:       ScenarioGeneratorContractProjection,
			wantProfile: ContractProjectionScenarioGenerator,
		},
		{
			name:         "developer sees topology",
			build:        DeveloperContractProjection,
			wantProfile:  ContractProjectionDeveloper,
			wantTopology: true,
		},
		{
			name:         "reviewer sees topology and findings",
			build:        ReviewerContractProjection,
			wantProfile:  ContractProjectionReviewer,
			wantTopology: true,
			wantFindings: true,
		},
		{
			name:          "recovery sees full audit trail",
			build:         RecoveryContractProjection,
			wantProfile:   ContractProjectionRecovery,
			wantTopology:  true,
			wantFindings:  true,
			wantSourceRef: true,
		},
		{
			name:          "qa sees full audit trail",
			build:         QAContractProjection,
			wantProfile:   ContractProjectionQA,
			wantTopology:  true,
			wantFindings:  true,
			wantSourceRef: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj := tt.build(plan)
			if proj == nil {
				t.Fatal("projection is nil")
			}
			if proj.Profile != tt.wantProfile {
				t.Fatalf("Profile = %q, want %q", proj.Profile, tt.wantProfile)
			}
			if proj.ID != plan.Contract.ID || proj.Version != plan.Contract.Version {
				t.Fatalf("contract identity = (%q,%d), want (%q,%d)", proj.ID, proj.Version, plan.Contract.ID, plan.Contract.Version)
			}
			if len(proj.Constraints) == 0 {
				t.Fatal("projection dropped contract constraints")
			}
			if len(proj.AcceptanceObligations) == 0 && proj.Profile != ContractProjectionArchitect {
				t.Fatal("projection dropped acceptance obligations")
			}
			if got := len(proj.TopologyFacts) > 0; got != tt.wantTopology {
				t.Fatalf("TopologyFacts present = %t, want %t (%#v)", got, tt.wantTopology, proj.TopologyFacts)
			}
			if got := len(proj.ValidationFindings) > 0; got != tt.wantFindings {
				t.Fatalf("ValidationFindings present = %t, want %t (%#v)", got, tt.wantFindings, proj.ValidationFindings)
			}
			if got := len(proj.SourceRefs) > 0; got != tt.wantSourceRef {
				t.Fatalf("SourceRefs present = %t, want %t (%#v)", got, tt.wantSourceRef, proj.SourceRefs)
			}
		})
	}
}

func TestContractProjectionProfileForReviewerFamily(t *testing.T) {
	for _, role := range []Role{RolePlanReviewer, RoleTaskReviewer, RoleReviewer, RoleScenarioReviewer, RoleValidator} {
		if got := ContractProjectionProfileForRole(role); got != ContractProjectionReviewer {
			t.Fatalf("role %q profile = %q, want reviewer", role, got)
		}
	}
	for _, role := range []Role{RoleQA, RolePlanQAReviewer} {
		if got := ContractProjectionProfileForRole(role); got != ContractProjectionQA {
			t.Fatalf("role %q profile = %q, want qa", role, got)
		}
	}
}

func TestBuildContractProjectionCopiesSlices(t *testing.T) {
	plan := testContractProjectionPlan()
	proj := DeveloperContractProjection(plan)

	plan.Contract.Constraints[0] = "mutated"
	plan.Contract.Scope.Create[0] = "mutated.go"
	plan.Contract.TopologyFacts[0].Evidence[0] = "mutated"
	plan.Contract.Amendments[0].Impact.AffectedIDs[0] = "mutated"

	if proj.Constraints[0] != "preserve existing baseline" {
		t.Fatalf("constraints aliased packet: %#v", proj.Constraints)
	}
	if proj.Scope.Create[0] != "adapter.go" {
		t.Fatalf("scope aliased packet: %#v", proj.Scope)
	}
	if proj.TopologyFacts[0].Evidence[0] != "settings.gradle" {
		t.Fatalf("topology evidence aliased packet: %#v", proj.TopologyFacts)
	}
	if proj.Amendments[0].AffectedIDs[0] != "story.demo.1" {
		t.Fatalf("amendment affected IDs aliased packet: %#v", proj.Amendments)
	}
}

func TestBuildContractProjectionNilSafe(t *testing.T) {
	if got := BuildContractProjection(nil, RoleDeveloper); got != nil {
		t.Fatalf("nil plan projection = %#v, want nil", got)
	}
	if got := BuildContractProjection(&workflow.Plan{}, RoleDeveloper); got != nil {
		t.Fatalf("nil packet projection = %#v, want nil", got)
	}
}

func testContractProjectionPlan() *workflow.Plan {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	return &workflow.Plan{
		Slug: "demo",
		Contract: &workflow.ContractPacket{
			ID:          workflow.PlanContractID("demo"),
			Version:     1,
			Brief:       "Extend the existing baseline.",
			SourceRefs:  []workflow.ContractSourceRef{{Kind: "user_brief", Ref: "demo"}},
			Constraints: []string{"preserve existing baseline"},
			AcceptanceObligations: []string{
				"deliver integration coverage",
			},
			ForbiddenMoves: []string{
				"do not create a standalone clean-room project",
			},
			Scope: workflow.ContractScopeSnapshot{
				Include:    []string{"baseline.go"},
				DoNotTouch: []string{"README.md"},
				Create:     []string{"adapter.go"},
			},
			TopologyFacts: []workflow.TopologyFact{{
				Kind:     "forbidden_file",
				Path:     "settings.gradle",
				Evidence: []string{"settings.gradle"},
			}},
			Amendments: []workflow.ContractAmendment{{
				ID:             "amendment-1",
				PlanDecisionID: "plan-decision.demo.1",
				Impact: workflow.ContractImpact{
					Kind:        workflow.ContractImpactRefine,
					Summary:     "target one affected story",
					AffectedIDs: []string{"story.demo.1"},
				},
				CreatedAt: now,
			}},
			ValidationFindings: []workflow.ContractValidationFinding{{
				ID:        "finding-1",
				Severity:  "error",
				Category:  "topology",
				Message:   "standalone build root conflicts with baseline",
				Evidence:  []string{"settings.gradle"},
				CreatedAt: now,
			}},
			CreatedAt: now,
		},
	}
}
