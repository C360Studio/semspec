package planreviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestMergeCapabilityFindings_R1PreReqGenSkipsOrphanRules pins the fix
// from the 2026-05-30 mock e2e plan-phase smoke: capability_orphan rules
// must NOT fire on plans with populated Exploration but zero Requirements
// (plan-reviewer R1 round, BEFORE req-gen has run). Otherwise R1 reviews
// reject every analyst-sub-phase plan with N orphan findings, blocking
// approval entirely. Dependency-only rules (cycle, dep_orphan) can still
// fire pre-req-gen because they inspect Capability.DependsOn directly.
func TestMergeCapabilityFindings_R1PreReqGenSkipsOrphanRules(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "r1-test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A."},
				{Name: "b", Lifecycle: workflow.CapabilityNew, Description: "B."},
			},
		},
		Requirements: nil, // R1 review fires BEFORE req-gen
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "capability.orphan" || f.SOPID == "capability.orphan.docs_only" || f.SOPID == "capability.requirement_orphan" {
			t.Errorf("R1 pre-req-gen review should not fire requirement-coupled rule %s, got: %+v", f.SOPID, f)
		}
	}
	if result.Verdict != "approved" {
		t.Errorf("expected R1 approved verdict preserved, got %q", result.Verdict)
	}
}

// TestMergeCapabilityFindings_CycleFiresEvenPreReqGen pins that
// dependency-only rules (which don't need requirements) still fire on
// R1 reviews — cycle detection has full information from Capabilities
// alone.
func TestMergeCapabilityFindings_CycleFiresEvenPreReqGen(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "r1-cycle",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A.", DependsOn: []string{"b"}},
				{Name: "b", Lifecycle: workflow.CapabilityNew, Description: "B.", DependsOn: []string{"a"}},
			},
		},
		Requirements: nil,
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	found := false
	for _, f := range result.Findings {
		if f.SOPID == "capability.dependency_cycle" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dependency_cycle to fire on R1 pre-req-gen, got: %+v", result.Findings)
	}
}

// TestMergeCapabilityFindings_NoExplorationIsNoop pins the back-compat
// contract: legacy plans without the analyst sub-phase produce no
// structural findings.
func TestMergeCapabilityFindings_NoExplorationIsNoop(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "legacy",
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "Legacy req"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved", Summary: "looks fine"}

	mergeCapabilityFindings(plan, result)

	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings for plan without Exploration, got %d", len(result.Findings))
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved, got %q", result.Verdict)
	}
}

// TestCapabilityOrphan_NoImplementingReq covers the headline run-#3 fix
// shape: a Capability exists in Plan.Exploration but no Requirement claims
// it via capability_name.
func TestCapabilityOrphan_NoImplementingReq(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "user-auth", Lifecycle: workflow.CapabilityNew, Description: "Auth users."},
				{Name: "session-store", Lifecycle: workflow.CapabilityNew, Description: "Store sessions."},
			},
		},
		Requirements: []workflow.Requirement{
			// Only user-auth has an implementing requirement.
			{ID: "r1", Title: "Auth req", CapabilityName: "user-auth"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict upgraded to needs_changes, got %q", result.Verdict)
	}
	found := false
	for _, f := range result.Findings {
		if f.SOPID == "capability.orphan" && f.TargetID == "session-store" {
			found = true
			if f.Action != "add" {
				t.Errorf("expected Action=add, got %q", f.Action)
			}
		}
	}
	if !found {
		t.Errorf("expected capability.orphan finding for session-store, got: %+v", result.Findings)
	}
}

// TestCapabilityOrphanDocsOnly_NoLongerFires pins that after ADR-043 Move 4
// removed Requirement.FilesOwned, the docs-only rule (which depended on
// inspecting Requirement file paths) no longer fires on the capability
// surface. The equivalent shape is now caught at the architecture layer
// (architecture.component_implementation_files_doc_only, PR 2) and the
// story layer (story.docs_only_files_owned, PR 3) upstream of where this
// rule used to fire.
func TestCapabilityOrphanDocsOnly_NoLongerFires(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "coverage-matrix-tooling", Lifecycle: workflow.CapabilityNew, Description: "Track MAVLink coverage."},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "Document coverage", CapabilityName: "coverage-matrix-tooling"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeCapabilityFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "capability.orphan.docs_only" {
			t.Errorf("post-ADR-043, capability.orphan.docs_only should not fire — architecture + story rules handle this shape now: %+v", f)
		}
	}
}

// TestRequirementCapability_Orphan flags a Requirement whose CapabilityName
// doesn't resolve to any declared capability.
func TestRequirementCapability_Orphan(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "user-auth", Lifecycle: workflow.CapabilityNew, Description: "Auth."},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "r1", CapabilityName: "user-auth"},
			{ID: "r2", CapabilityName: "ghost-capability"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	found := false
	for _, f := range result.Findings {
		if f.SOPID == "capability.requirement_orphan" && f.TargetID == "r2" {
			found = true
			if !strings.Contains(f.TargetValue, "ghost-capability") {
				t.Errorf("expected TargetValue to mention ghost-capability, got %q", f.TargetValue)
			}
		}
	}
	if !found {
		t.Errorf("expected capability.requirement_orphan finding for r2, got: %+v", result.Findings)
	}
}

func TestCapabilityDependencyCycle(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A.", DependsOn: []string{"b"}},
				{Name: "b", Lifecycle: workflow.CapabilityNew, Description: "B.", DependsOn: []string{"a"}},
			},
		},
		// Make capabilities covered so orphan rules don't fire and pollute.
		Requirements: []workflow.Requirement{
			{ID: "r1", CapabilityName: "a"},
			{ID: "r2", CapabilityName: "b"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	found := false
	for _, f := range result.Findings {
		if f.SOPID == "capability.dependency_cycle" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected capability.dependency_cycle finding, got: %+v", result.Findings)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict upgraded, got %q", result.Verdict)
	}
}

// TestCapabilityDependencyOrphan_PerEdge confirms one finding per offending
// edge rather than a single bundled complaint.
func TestCapabilityDependencyOrphan_PerEdge(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A.", DependsOn: []string{"ghost-one", "ghost-two"}},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "r1", CapabilityName: "a"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	var orphanFindings []workflow.PlanReviewFinding
	for _, f := range result.Findings {
		if f.SOPID == "capability.dependency_orphan" {
			orphanFindings = append(orphanFindings, f)
		}
	}
	if got := len(orphanFindings); got != 2 {
		t.Errorf("expected 2 per-edge orphan findings, got %d: %+v", got, orphanFindings)
	}
	// Confirm both edges were named distinctly.
	values := map[string]bool{}
	for _, f := range orphanFindings {
		values[f.TargetValue] = true
	}
	if !values["ghost-one"] || !values["ghost-two"] {
		t.Errorf("expected findings for both ghost-one and ghost-two, got: %v", values)
	}
}

func TestMergeCapabilityFindings_HealthyPlanPasses(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "user-auth", Lifecycle: workflow.CapabilityNew, Description: "Auth."},
				{Name: "session-store", Lifecycle: workflow.CapabilityNew, Description: "Sessions.", DependsOn: []string{"user-auth"}},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "r1", CapabilityName: "user-auth"},
			{ID: "r2", CapabilityName: "session-store", DependsOn: []string{"r1"}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved", Summary: "ok"}

	mergeCapabilityFindings(plan, result)

	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings on healthy plan, got %d: %+v", len(result.Findings), result.Findings)
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved, got %q", result.Verdict)
	}
}
