package planreviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

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
			{ID: "r1", Title: "Auth req", CapabilityName: "user-auth", FilesOwned: []string{"auth.go"}},
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

// TestCapabilityOrphan_DocsOnlyFingerprint is the deterministic encoding
// of run #3: the Capability has a Requirement, but the Requirement only
// owns *.md files. The implementation code is missing.
func TestCapabilityOrphan_DocsOnlyFingerprint(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "coverage-matrix-tooling", Lifecycle: workflow.CapabilityNew, Description: "Track MAVLink coverage."},
			},
		},
		Requirements: []workflow.Requirement{
			{
				ID:             "r1",
				Title:          "Document coverage",
				CapabilityName: "coverage-matrix-tooling",
				FilesOwned:     []string{"README.md", "docs/coverage.md"},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict upgraded to needs_changes, got %q", result.Verdict)
	}
	found := false
	for _, f := range result.Findings {
		if f.SOPID == "capability.orphan.docs_only" && f.TargetID == "coverage-matrix-tooling" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected capability.orphan.docs_only finding, got: %+v", result.Findings)
	}
}

// TestCapabilityOrphan_MixedFilesPasses confirms a Requirement that owns
// both implementation AND documentation files is NOT flagged docs-only.
func TestCapabilityOrphan_MixedFilesPasses(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "test",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "auth", Lifecycle: workflow.CapabilityNew, Description: "Auth."},
			},
		},
		Requirements: []workflow.Requirement{
			{
				ID:             "r1",
				Title:          "Auth",
				CapabilityName: "auth",
				FilesOwned:     []string{"auth.go", "auth.md"},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeCapabilityFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "capability.orphan.docs_only" {
			t.Errorf("did not expect docs_only finding for mixed-files requirement, got: %+v", f)
		}
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved as approved, got %q", result.Verdict)
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
			{ID: "r1", CapabilityName: "user-auth", FilesOwned: []string{"auth.go"}},
			{ID: "r2", CapabilityName: "ghost-capability", FilesOwned: []string{"ghost.go"}},
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
			{ID: "r1", CapabilityName: "a", FilesOwned: []string{"a.go"}},
			{ID: "r2", CapabilityName: "b", FilesOwned: []string{"b.go"}},
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
			{ID: "r1", CapabilityName: "a", FilesOwned: []string{"a.go"}},
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
			{ID: "r1", CapabilityName: "user-auth", FilesOwned: []string{"auth.go", "auth_test.go"}},
			{ID: "r2", CapabilityName: "session-store", FilesOwned: []string{"session.go"}, DependsOn: []string{"r1"}},
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
