package planreviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestMergeArchitectureFindings_NoArchitectureIsNoop pins the back-compat
// contract: legacy plans without an architecture-generator phase produce
// no findings on the architecture round.
func TestMergeArchitectureFindings_NoArchitectureIsNoop(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "legacy",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A."},
			},
		},
		// Architecture is nil
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings for plan without Architecture, got %d", len(result.Findings))
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved, got %q", result.Verdict)
	}
}

// TestMergeArchitectureFindings_EmptyComponentsIsNoop covers the case where
// the architecture document exists but declares no components — nothing to
// validate yet.
func TestMergeArchitectureFindings_EmptyComponentsIsNoop(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "empty-arch",
		Exploration:  &workflow.Exploration{Capabilities: []workflow.Capability{{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A."}}},
		Architecture: &workflow.ArchitectureDocument{},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)
	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Findings))
	}
}

// TestMergeArchitectureFindings_MissingImplementationFiles fires the
// architecture.component_missing_implementation_files rule once per
// offending component.
func TestMergeArchitectureFindings_MissingImplementationFiles(t *testing.T) {
	plan := &workflow.Plan{
		Slug:        "missing-files",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{{Name: "auth", Lifecycle: workflow.CapabilityNew, Description: "A."}}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: nil, Capabilities: []string{"auth"}},
				{Name: "session-store", ImplementationFiles: nil, Capabilities: []string{"auth"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	count := 0
	for _, f := range result.Findings {
		if f.SOPID == "architecture.component_missing_implementation_files" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 missing-files findings, got %d: %+v", count, result.Findings)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict bumped to needs_changes, got %q", result.Verdict)
	}
}

// TestMergeArchitectureFindings_DocsOnlyFiles fires the
// architecture.component_implementation_files_doc_only rule when a
// component's files are all *.md/*.txt/README-shape.
func TestMergeArchitectureFindings_DocsOnlyFiles(t *testing.T) {
	plan := &workflow.Plan{
		Slug:        "docs-only",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{{Name: "telemetry", Lifecycle: workflow.CapabilityNew, Description: "T."}}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "cs-telemetry", ImplementationFiles: []string{"CoverageMatrix.md", "README.md"}, Capabilities: []string{"telemetry"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	found := false
	for _, f := range result.Findings {
		if f.SOPID == "architecture.component_implementation_files_doc_only" {
			found = true
			if !strings.Contains(f.Issue, "documentation") {
				t.Errorf("docs_only finding missing 'documentation' phrase: %+v", f)
			}
		}
	}
	if !found {
		t.Errorf("expected docs_only finding, got: %+v", result.Findings)
	}
}

// TestMergeArchitectureFindings_DocsOnlyDoesNotDoubleFire ensures a
// component with empty implementation_files only fires the missing-files
// rule, not docs-only on top of it.
func TestMergeArchitectureFindings_DocsOnlyDoesNotDoubleFire(t *testing.T) {
	plan := &workflow.Plan{
		Slug:        "empty-not-docs",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{{Name: "x", Lifecycle: workflow.CapabilityNew, Description: "X."}}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "x-component", ImplementationFiles: nil, Capabilities: []string{"x"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "architecture.component_implementation_files_doc_only" {
			t.Errorf("empty implementation_files should not also fire docs_only, got: %+v", f)
		}
	}
}

// TestMergeArchitectureFindings_CapabilityUnresolved fires
// capability.unresolved_in_architecture when a Capability has no component
// mapping.
func TestMergeArchitectureFindings_CapabilityUnresolved(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "unresolved-cap",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "auth", Lifecycle: workflow.CapabilityNew, Description: "A."},
				{Name: "session", Lifecycle: workflow.CapabilityNew, Description: "S."},
				{Name: "telemetry", Lifecycle: workflow.CapabilityNew, Description: "T."},
			},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: []string{"src/auth.go"}, Capabilities: []string{"auth", "session"}},
				// telemetry is unmapped
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	found := false
	for _, f := range result.Findings {
		if f.SOPID == "capability.unresolved_in_architecture" && f.TargetID == "telemetry" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected capability.unresolved_in_architecture for telemetry, got: %+v", result.Findings)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict bumped to needs_changes, got %q", result.Verdict)
	}
}

// TestMergeArchitectureFindings_HappyPath validates a fully-conformant
// architecture produces zero findings and preserves the LLM verdict.
func TestMergeArchitectureFindings_HappyPath(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "happy",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "auth", Lifecycle: workflow.CapabilityNew, Description: "A."},
				{Name: "session", Lifecycle: workflow.CapabilityNew, Description: "S."},
			},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: []string{"src/auth.go", "README.md"}, Capabilities: []string{"auth"}},
				{Name: "session-store", ImplementationFiles: []string{"src/session.go"}, Capabilities: []string{"session"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings on conformant arch, got %d: %+v", len(result.Findings), result.Findings)
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved, got %q", result.Verdict)
	}
}

// TestMergeArchitectureFindings_OverloadedComponent fires
// architecture.component_overloaded_capabilities on the 2026-06-13 mavlink-hard
// MavsdkDriver shape: three independently-testable capabilities collapsed onto
// one component backed by two source files (+ two doc files). A single dev loop
// built only one capability's surface and stubbed the rest; this rule catches
// the collapse at plan review instead of at the QA gate.
func TestMergeArchitectureFindings_OverloadedComponent(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "mavsdk-overloaded",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{
			{Name: "mavsdk-bootstrap", Lifecycle: workflow.CapabilityNew, Description: "B."},
			{Name: "mavsdk-telemetry", Lifecycle: workflow.CapabilityNew, Description: "T."},
			{Name: "mavsdk-control", Lifecycle: workflow.CapabilityNew, Description: "C."},
		}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{
					Name:                "MavsdkDriver",
					Capabilities:        []string{"mavsdk-bootstrap", "mavsdk-telemetry", "mavsdk-control"},
					ImplementationFiles: []string{"src/UnmannedSystem.java", "src/UnmannedConfig.java", "README.md", "CoverageMatrix.md"},
				},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	var f *workflow.PlanReviewFinding
	for i := range result.Findings {
		if result.Findings[i].SOPID == "architecture.component_overloaded_capabilities" {
			f = &result.Findings[i]
		}
	}
	if f == nil {
		t.Fatalf("expected component_overloaded_capabilities finding, got: %+v", result.Findings)
	}
	if f.TargetID != "MavsdkDriver" {
		t.Errorf("target = %q, want MavsdkDriver", f.TargetID)
	}
	if !strings.Contains(f.Issue, "3 capabilities") || !strings.Contains(f.Issue, "2 source") {
		t.Errorf("issue should name 3 capabilities / 2 source files: %q", f.Issue)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("verdict = %q, want needs_changes", result.Verdict)
	}
}

// TestMergeArchitectureFindings_CohesiveComponentNotFlagged confirms the rule
// does NOT fire when a multi-capability component declares one source file per
// capability — the honest exceptional shape (real shared module, distinct
// surface per capability).
func TestMergeArchitectureFindings_CohesiveComponentNotFlagged(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "cohesive-ok",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{
			{Name: "auth", Lifecycle: workflow.CapabilityNew, Description: "A."},
			{Name: "session", Lifecycle: workflow.CapabilityNew, Description: "S."},
		}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{
					Name:                "identity",
					Capabilities:        []string{"auth", "session"},
					ImplementationFiles: []string{"src/auth.go", "src/session.go", "README.md"},
				},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "architecture.component_overloaded_capabilities" {
			t.Errorf("2 caps + 2 source files should not fire overloaded rule, got: %+v", f)
		}
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want approved (no findings)", result.Verdict)
	}
}

// TestHasSourceFile_DelegatesToWorkflowClassifier guards the
// reviewer-side ↔ architecture-generator-side classification parity. If
// workflow.IsDocumentationPath ever drifts from this rule's expectations,
// adding to either side would let architectures slip through one layer
// while being rejected by the other.
func TestHasSourceFile_DelegatesToWorkflowClassifier(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  bool
	}{
		{"empty is not a source file set", nil, false},
		{"single source file", []string{"src/x.go"}, true},
		{"single doc file", []string{"README.md"}, false},
		{"mixed source + docs", []string{"docs/x.md", "src/x.go"}, true},
		{"all docs (multiple extensions)", []string{"README.md", "NOTES.txt", "guide.rst"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasSourceFile(tc.paths); got != tc.want {
				t.Errorf("hasSourceFile(%v) = %v, want %v", tc.paths, got, tc.want)
			}
		})
	}
}
