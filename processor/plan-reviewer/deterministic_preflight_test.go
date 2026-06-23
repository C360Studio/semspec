package planreviewer

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestDeterministicPreflightReview_HardStructuralErrorShortCircuits(t *testing.T) {
	// scope.include orphan: fires regardless of pipeline phase (unlike
	// scope.create, which is only checked once Stories reconcile it — see
	// TestScopedFileOwnership_ArchPhaseSkipsCreate). Used here so the preflight
	// short-circuit mechanism is exercised without a Stories dependency.
	plan := &workflow.Plan{
		Slug:        "preflight-hard-error",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{{Name: "telemetry", Lifecycle: workflow.CapabilityNew, Description: "Telemetry."}}},
		Scope:       workflow.Scope{Include: []string{"src/Telemetry.java"}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "telemetry", ImplementationFiles: []string{"src/Other.java"}, Capabilities: []string{"telemetry"}},
			},
		},
	}

	result := deterministicPreflightReview(plan)

	if result == nil {
		t.Fatal("expected deterministic preflight to reject unowned scope.include file")
	}
	if result.Verdict != "needs_changes" {
		t.Fatalf("verdict = %q, want needs_changes", result.Verdict)
	}
	if len(result.ErrorFindings()) != 1 {
		t.Fatalf("error findings = %d, want 1: %+v", len(result.ErrorFindings()), result.Findings)
	}
	finding := result.ErrorFindings()[0]
	if finding.SOPID != "architecture.scoped_file_unowned" {
		t.Fatalf("SOPID = %q, want architecture.scoped_file_unowned", finding.SOPID)
	}
	if finding.TargetField != "component_boundaries[].implementation_files" || finding.TargetValue != "src/Telemetry.java" {
		t.Fatalf("finding action target = %q/%q, want component_boundaries[].implementation_files/src/Telemetry.java",
			finding.TargetField, finding.TargetValue)
	}
}

func TestDeterministicPreflightReview_ContractDeliverableUncoveredShortCircuits(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "preflight-uncovered-contract-file",
		Scope: workflow.Scope{
			Create: []string{"src/main/java/RequiredDriver.java", "src/main/java/Helper.java"},
		},
		Contract: &workflow.ContractPacket{
			Scope: workflow.ContractScopeSnapshot{
				Create: []string{"src/main/java/RequiredDriver.java", "src/main/java/Helper.java"},
			},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{
					Name:                "driver",
					ImplementationFiles: []string{"src/main/java/RequiredDriver.java", "src/main/java/Helper.java"},
					Capabilities:        []string{"driver"},
				},
			},
		},
		Requirements: []workflow.Requirement{{ID: "req.driver", CapabilityName: "driver", Title: "Driver"}},
		Stories: []workflow.Story{{
			ID:             "story.driver",
			RequirementIDs: []string{"req.driver"},
			ComponentName:  "driver",
			Title:          "Build driver",
			Status:         workflow.StoryStatusReady,
			FilesOwned:     []string{"src/main/java/Helper.java"},
			Tasks:          []workflow.Task{{ID: "task.driver.test", StoryID: "story.driver", Description: "Write failing test"}},
		}},
	}

	result := deterministicPreflightReview(plan)

	if result == nil {
		t.Fatal("expected deterministic preflight to reject uncovered root contract deliverable")
	}
	finding := firstFinding(result.Findings, "story.contract_scope_uncovered")
	if finding == nil {
		t.Fatalf("expected story.contract_scope_uncovered finding, got %+v", result.Findings)
	}
	if finding.TargetValue != "src/main/java/RequiredDriver.java" {
		t.Fatalf("TargetValue = %q, want src/main/java/RequiredDriver.java", finding.TargetValue)
	}
	if result.Verdict != "needs_changes" {
		t.Fatalf("verdict = %q, want needs_changes", result.Verdict)
	}
}

func TestDeterministicPreflightReview_WarningOnlyDoesNotShortCircuit(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "preflight-warning-only",
		Scenarios: []workflow.Scenario{
			{
				ID:            "scenario.warn",
				RequirementID: "req.warn",
				Given:         "the SITL container is running",
				When:          "the unit boundary is exercised",
				Then:          []string{"state is mapped"},
				Tags:          []string{workflow.TierUnit},
			},
		},
	}

	if result := deterministicPreflightReview(plan); result != nil {
		t.Fatalf("warning-only deterministic findings should not short-circuit LLM review: %+v", result)
	}
}

func TestMergeDeterministicFindings_PostLLMBackstopStillNormalizes(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "post-llm-backstop",
		// scope.include orphan — phase-independent (see the preflight test above).
		Scope: workflow.Scope{Include: []string{"src/Telemetry.java"}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "telemetry", ImplementationFiles: []string{"src/Other.java"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved", Summary: "LLM approved."}

	mergeDeterministicFindings(plan, result)

	if result.Verdict != "needs_changes" {
		t.Fatalf("post-LLM deterministic merge should still normalize verdict, got %q", result.Verdict)
	}
	if len(result.ErrorFindings()) == 0 {
		t.Fatal("expected deterministic backstop to append error findings")
	}
}
