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

// TestMergeArchitectureFindings_StubRiskUntestedCapability fires
// architecture.component_stub_risk (ADR-049) on the evidence shape of the
// 2026-06-13 stub trap: a multi-capability component where one capability has a
// requirement + scenario (a forcing test) and a sibling capability has NO
// scenario. With no failing test for the second capability the dev builds the
// first and stubs the second; this rule catches it at plan review — by EVIDENCE,
// not file count.
func TestMergeArchitectureFindings_StubRiskUntestedCapability(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "mavsdk-stub-risk",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{
			{Name: "mavsdk-telemetry", Lifecycle: workflow.CapabilityNew, Description: "T."},
			{Name: "mavsdk-control", Lifecycle: workflow.CapabilityNew, Description: "C."},
		}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				// Cohesive single-file driver mapping two capabilities — the
				// shape the retired file-count rule wrongly penalized. Here it
				// is flagged ONLY because mavsdk-control has no scenario.
				{
					Name:                "MavsdkDriver",
					Capabilities:        []string{"mavsdk-telemetry", "mavsdk-control"},
					ImplementationFiles: []string{"src/MavsdkDriver.java"},
				},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "req-tel", CapabilityName: "mavsdk-telemetry", Title: "Telemetry"},
			{ID: "req-ctl", CapabilityName: "mavsdk-control", Title: "Control"},
		},
		Scenarios: []workflow.Scenario{
			// Only telemetry has a scenario; control is unevidenced.
			{ID: "sc-tel-1", RequirementID: "req-tel"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	var f *workflow.PlanReviewFinding
	for i := range result.Findings {
		if result.Findings[i].SOPID == "architecture.component_stub_risk" {
			f = &result.Findings[i]
		}
	}
	if f == nil {
		t.Fatalf("expected component_stub_risk finding, got: %+v", result.Findings)
	}
	if f.TargetID != "MavsdkDriver" {
		t.Errorf("target = %q, want MavsdkDriver", f.TargetID)
	}
	if !strings.Contains(f.Issue, "mavsdk-control") {
		t.Errorf("issue should name the unevidenced capability mavsdk-control: %q", f.Issue)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("verdict = %q, want needs_changes", result.Verdict)
	}
}

// TestMergeArchitectureFindings_CohesiveComponentWithEvidenceNotFlagged is the
// load-bearing ADR-049 regression: a cohesive component mapping TWO capabilities
// to a SINGLE file does NOT fire stub-risk when BOTH capabilities have scenario
// evidence. The retired file-count rule (architecture.component_overloaded_
// capabilities) would have wrongly flagged this exact shape (2 caps, 1 file) —
// the over-correction that drove the 2026-06-14 over-split wedge.
func TestMergeArchitectureFindings_CohesiveComponentWithEvidenceNotFlagged(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "cohesive-evidenced",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{
			{Name: "auth", Lifecycle: workflow.CapabilityNew, Description: "A."},
			{Name: "session", Lifecycle: workflow.CapabilityNew, Description: "S."},
		}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{
					Name:                "identity",
					Capabilities:        []string{"auth", "session"},
					ImplementationFiles: []string{"src/identity.go"}, // ONE file, TWO caps
				},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "req-auth", CapabilityName: "auth", Title: "Auth"},
			{ID: "req-sess", CapabilityName: "session", Title: "Session"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "sc-auth", RequirementID: "req-auth"},
			{ID: "sc-sess", RequirementID: "req-sess"},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "architecture.component_stub_risk" {
			t.Errorf("a cohesive 2-cap/1-file component with evidence for BOTH caps must not fire stub-risk, got: %+v", f)
		}
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want approved (cohesive driver is legal under ADR-049)", result.Verdict)
	}
}

// TestMergeArchitectureFindings_StubRiskEvidenceBlindNoFire confirms the guard:
// with NO scenarios on the plan (R1 / pre-scenario-gen), stub-risk never fires —
// it must not regress into a file-count proxy when the evidence layer is absent.
func TestMergeArchitectureFindings_StubRiskEvidenceBlindNoFire(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "no-scenarios-yet",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{
			{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A."},
			{Name: "b", Lifecycle: workflow.CapabilityNew, Description: "B."},
		}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "combo", Capabilities: []string{"a", "b"}, ImplementationFiles: []string{"src/combo.go"}},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "req-a", CapabilityName: "a"}, {ID: "req-b", CapabilityName: "b"},
		},
		// Scenarios deliberately empty.
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "architecture.component_stub_risk" {
			t.Errorf("stub-risk must not fire with no scenarios (evidence-blind), got: %+v", f)
		}
	}
}

// TestMergeArchitectureFindings_CohesionViolationOverSplit fires the ADR-049
// inverse check (warning) on the 2026-06-14 over-split shape: two single-
// capability components that both integrate into one integration_target but
// declare fully disjoint implementation_files — they likely converge on one
// undeclared framework entry class and collide at assembly. Severity warning →
// surfaced but does NOT flip the verdict (the dev gate, move 3, is the hard
// backstop).
func TestMergeArchitectureFindings_CohesionViolationOverSplit(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "osh-over-split",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "telemetry", Capabilities: []string{"telemetry"}, ImplementationFiles: []string{"src/driver/mavsdk/Telemetry.java"}, UpstreamRefs: []string{"OSH Core"}},
				{Name: "control", Capabilities: []string{"control"}, ImplementationFiles: []string{"src/driver/mavsdk/Control.java"}, UpstreamRefs: []string{"OSH Core"}},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "OSH Core", Coordinate: "github.com/opensensorhub/osh-core@v2", Role: "integration_target", UsedBy: []string{"telemetry", "control"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	var f *workflow.PlanReviewFinding
	for i := range result.Findings {
		if result.Findings[i].SOPID == "architecture.components_share_entry_point" {
			f = &result.Findings[i]
		}
	}
	if f == nil {
		t.Fatalf("expected components_share_entry_point warning, got: %+v", result.Findings)
	}
	if f.Severity != "warning" {
		t.Errorf("cohesion-violation severity = %q, want warning (non-blocking)", f.Severity)
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want approved (a warning must not block — move 3 is the hard gate)", result.Verdict)
	}
}

// TestMergeArchitectureFindings_CohesionViolationSatisfiedByDeclaredShare
// confirms the escape: when the two components DECLARE the shared entry file
// (the same path in each implementation_files), the cohesion check does not
// fire — DeriveStoryScheduling serializes them, which is exactly move 1's third
// option. A plain (non-integration_target) shared library also must not fire.
func TestMergeArchitectureFindings_CohesionViolationSatisfiedByDeclaredShare(t *testing.T) {
	shared := &workflow.Plan{
		Slug: "declared-share",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "telemetry", Capabilities: []string{"telemetry"}, ImplementationFiles: []string{"src/driver/mavsdk/MavsdkDriver.java", "src/driver/mavsdk/Telemetry.java"}, UpstreamRefs: []string{"OSH Core"}},
				{Name: "control", Capabilities: []string{"control"}, ImplementationFiles: []string{"src/driver/mavsdk/MavsdkDriver.java", "src/driver/mavsdk/Control.java"}, UpstreamRefs: []string{"OSH Core"}},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "OSH Core", Coordinate: "github.com/opensensorhub/osh-core@v2", Role: "integration_target", UsedBy: []string{"telemetry", "control"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeArchitectureFindings(shared, result)
	for _, f := range result.Findings {
		if f.SOPID == "architecture.components_share_entry_point" {
			t.Errorf("declared shared entry file (MavsdkDriver.java in both) must not fire cohesion-violation: %+v", f)
		}
	}

	// A plain runtime library shared by two components must NOT fire.
	lib := &workflow.Plan{
		Slug: "shared-lib-ok",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "a", Capabilities: []string{"a"}, ImplementationFiles: []string{"src/A.go"}, UpstreamRefs: []string{"logging"}},
				{Name: "b", Capabilities: []string{"b"}, ImplementationFiles: []string{"src/B.go"}, UpstreamRefs: []string{"logging"}},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "logging", Coordinate: "go.uber.org/zap", Role: "runtime_dep", UsedBy: []string{"a", "b"}},
			},
		},
	}
	result2 := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeArchitectureFindings(lib, result2)
	for _, f := range result2.Findings {
		if f.SOPID == "architecture.components_share_entry_point" {
			t.Errorf("a shared runtime_dep library must not fire cohesion-violation: %+v", f)
		}
	}
}

// TestMergeArchitectureFindings_SourceBuildIncompleteContract fires
// architecture.upstream_source_build_incomplete_contract on the 2026-06-13
// mavlink-hard ICommandStatus shape: a source_build dependency that names an
// interface (+ a lifecycle string) but resolves zero method signatures, so the
// dev reverse-engineers the contract through compile errors (ADR-047).
func TestMergeArchitectureFindings_SourceBuildIncompleteContract(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "osh-incomplete-contract",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "control", Capabilities: []string{"control"}, ImplementationFiles: []string{"src/RawMavlinkControl.java"}},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{
					Name:           "OpenSensorHub Core",
					Coordinate:     "github.com/opensensorhub/osh-core@v2.0.0",
					SourceRef:      "/sources/osh-core",
					ResolutionKind: "source_build",
					UsedBy:         []string{"control"},
					APIs: []workflow.APISurface{
						{Symbol: "ICommandStatus", Kind: "interface", Signature: "public interface ICommandStatus", Lifecycle: "implement getProgress()/getExecutionTime()", Citation: "/sources/osh-core/.../ICommandStatus.java"},
					},
				},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	var f *workflow.PlanReviewFinding
	for i := range result.Findings {
		if result.Findings[i].SOPID == "architecture.upstream_source_build_incomplete_contract" {
			f = &result.Findings[i]
		}
	}
	if f == nil {
		t.Fatalf("expected upstream_source_build_incomplete_contract finding, got: %+v", result.Findings)
	}
	if f.TargetID != "OpenSensorHub Core" {
		t.Errorf("target = %q, want OpenSensorHub Core", f.TargetID)
	}
	if !strings.Contains(f.Issue, "ICommandStatus") {
		t.Errorf("issue should name the unresolved type ICommandStatus: %q", f.Issue)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("verdict = %q, want needs_changes", result.Verdict)
	}
}

// TestMergeArchitectureFindings_SourceBuildWithMethodsNotFlagged confirms the
// rule does NOT fire once the source_build resolution carries method signatures
// for the interface it names — the honest complete shape.
func TestMergeArchitectureFindings_SourceBuildWithMethodsNotFlagged(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "osh-complete-contract",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "control", Capabilities: []string{"control"}, ImplementationFiles: []string{"src/RawMavlinkControl.java"}},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{
					Name: "OpenSensorHub Core", Coordinate: "github.com/opensensorhub/osh-core@v2.0.0",
					SourceRef: "/sources/osh-core", ResolutionKind: "source_build", UsedBy: []string{"control"},
					APIs: []workflow.APISurface{
						{Symbol: "ICommandStatus", Kind: "interface", Signature: "public interface ICommandStatus", Citation: "c"},
						{Symbol: "ICommandStatus.getProgress", Kind: "method", Signature: "int getProgress()", Citation: "c"},
						{Symbol: "ICommandStatus.getExecutionTime", Kind: "method", Signature: "TimeExtent getExecutionTime()", Citation: "c"},
					},
				},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "architecture.upstream_source_build_incomplete_contract" {
			t.Errorf("source_build with method signatures should not fire: %+v", f)
		}
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want approved", result.Verdict)
	}
}

// TestMergeArchitectureFindings_NonSourceBuildContractNotFlagged confirms the
// rule is scoped to source_build: a maven_central resolution (resolved
// completely upstream, jar-verified) and an honest unresolved flag both carry a
// type surface with no methods, and neither trips the gate.
func TestMergeArchitectureFindings_NonSourceBuildContractNotFlagged(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "non-source-build",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "driver", Capabilities: []string{"drive"}, ImplementationFiles: []string{"src/Driver.java"}},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "MAVSDK", Coordinate: "io.mavsdk:mavsdk:3.0.0", ResolutionKind: "maven_central",
					APIs: []workflow.APISurface{{Symbol: "System", Kind: "class", Signature: "System()", Citation: "c"}}},
				{Name: "Mystery", Coordinate: "mystery-lib", ResolutionKind: "unresolved",
					APIs: []workflow.APISurface{{Symbol: "Mystery", Kind: "interface", Signature: "interface Mystery", Citation: "c"}}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	for _, f := range result.Findings {
		if f.SOPID == "architecture.upstream_source_build_incomplete_contract" {
			t.Errorf("only source_build should fire; got finding on %s: %+v", f.TargetID, f)
		}
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want approved", result.Verdict)
	}
}

// sourceBuildResolutionPlan builds a minimal plan with one clean single-cap
// component (so no component-boundary rule fires) and the given upstream
// resolutions, for isolating the upstream-contract rule. Exploration is nil so
// capability.unresolved_in_architecture never fires.
func sourceBuildResolutionPlan(resolutions ...workflow.UpstreamResolution) *workflow.Plan {
	return &workflow.Plan{
		Slug: "upstream-rule-iso",
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "driver", Capabilities: []string{"drive"}, ImplementationFiles: []string{"src/Driver.java"}},
			},
			UpstreamResolutions: resolutions,
		},
	}
}

func firedUpstreamContract(result *workflow.PlanReviewResult) *workflow.PlanReviewFinding {
	for i := range result.Findings {
		if result.Findings[i].SOPID == "architecture.upstream_source_build_incomplete_contract" {
			return &result.Findings[i]
		}
	}
	return nil
}

// TestMergeArchitectureFindings_SourceBuildByVCSShapeFiresWhenKindOmitted closes
// the go-reviewer HIGH gap: the gate's own target shape — a github-coordinate OSH
// dep — infers to "unknown" (NOT source_build) when resolution_kind is omitted,
// so without the VCS-shape widening the rule would silently miss it. Also
// exercises name-fallback to Coordinate (empty Name).
func TestMergeArchitectureFindings_SourceBuildByVCSShapeFiresWhenKindOmitted(t *testing.T) {
	plan := sourceBuildResolutionPlan(workflow.UpstreamResolution{
		// No ResolutionKind set; github coordinate is the de-facto source_build shape.
		Coordinate: "github.com/opensensorhub/osh-core@v2.0.0",
		SourceRef:  "/sources/osh-core",
		APIs: []workflow.APISurface{
			{Symbol: "ICommandStatus", Kind: "interface", Signature: "public interface ICommandStatus", Citation: "c"},
		},
	})
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	f := firedUpstreamContract(result)
	if f == nil {
		t.Fatalf("VCS-shaped source_build with omitted kind should fire, got: %+v", result.Findings)
	}
	if f.TargetID != "github.com/opensensorhub/osh-core@v2.0.0" {
		t.Errorf("TargetID should fall back to coordinate when Name is empty, got %q", f.TargetID)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("verdict = %q, want needs_changes", result.Verdict)
	}
}

// TestMergeArchitectureFindings_UnknownNonVCSShapeNotFlagged confirms the
// widening stays narrow: a registry-prefixed coordinate (npm:) infers to unknown
// but carries no VCS marker, so it is NOT treated as source_build even with a
// type-only surface — npm/pypi packages resolve completely and are not this
// gate's concern.
func TestMergeArchitectureFindings_UnknownNonVCSShapeNotFlagged(t *testing.T) {
	plan := sourceBuildResolutionPlan(workflow.UpstreamResolution{
		Name: "Some NPM Lib", Coordinate: "npm:some-lib@1.0.0",
		APIs: []workflow.APISurface{{Symbol: "Thing", Kind: "interface", Signature: "interface Thing", Citation: "c"}},
	})
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	if f := firedUpstreamContract(result); f != nil {
		t.Errorf("npm coordinate (unknown, non-VCS) should not fire: %+v", f)
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict = %q, want approved", result.Verdict)
	}
}

// TestMergeArchitectureFindings_SourceBuildEmptyAPIsNotFlagged: a source_build
// resolution with no APIs has no named type, so the rule does not fire (zero-API
// resolutions are a separate completeness concern, not this rule's floor).
func TestMergeArchitectureFindings_SourceBuildEmptyAPIsNotFlagged(t *testing.T) {
	plan := sourceBuildResolutionPlan(workflow.UpstreamResolution{
		Name: "OSH Core", Coordinate: "github.com/opensensorhub/osh-core@v2", ResolutionKind: "source_build",
	})
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	if f := firedUpstreamContract(result); f != nil {
		t.Errorf("source_build with empty APIs should not fire (no named type): %+v", f)
	}
}

// TestMergeArchitectureFindings_SourceBuildTypeKindFires confirms the
// {class,interface,type} named-type set: a source_build resolution naming only a
// `type` surface (e.g. a protobuf message the dev builds against) with no methods
// is incomplete and fires — closing the go-reviewer Medium-2 type-kind gap.
func TestMergeArchitectureFindings_SourceBuildTypeKindFires(t *testing.T) {
	plan := sourceBuildResolutionPlan(workflow.UpstreamResolution{
		Name: "Meshtastic Proto", Coordinate: "github.com/meshtastic/protobufs@v2", ResolutionKind: "source_build",
		APIs: []workflow.APISurface{{Symbol: "ToRadio", Kind: "type", Signature: "message ToRadio", Citation: "c"}},
	})
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	if f := firedUpstreamContract(result); f == nil {
		t.Fatalf("source_build naming a `type` with no methods should fire, got: %+v", result.Findings)
	}
}

// TestMergeArchitectureFindings_UpstreamRuleRunsWithoutComponents confirms the
// upstream rule is component-independent (go-reviewer Low): an architecture with
// resolutions but zero ComponentBoundaries is still gated.
func TestMergeArchitectureFindings_UpstreamRuleRunsWithoutComponents(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "no-components",
		Architecture: &workflow.ArchitectureDocument{
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "OSH Core", Coordinate: "github.com/opensensorhub/osh-core@v2", ResolutionKind: "source_build",
					APIs: []workflow.APISurface{{Symbol: "ICommandStatus", Kind: "interface", Signature: "interface ICommandStatus", Citation: "c"}}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	if f := firedUpstreamContract(result); f == nil {
		t.Fatalf("upstream rule should run even with no components, got: %+v", result.Findings)
	}
}

// TestMergeArchitectureFindings_MultipleResolutionsIsolated confirms per-
// resolution isolation: one incomplete source_build fires while a sibling with
// methods stays clean (and the collected type list does not leak across loop
// iterations).
func TestMergeArchitectureFindings_MultipleResolutionsIsolated(t *testing.T) {
	plan := sourceBuildResolutionPlan(
		workflow.UpstreamResolution{
			Name: "Incomplete", Coordinate: "github.com/x/incomplete@v1", ResolutionKind: "source_build",
			APIs: []workflow.APISurface{{Symbol: "IFoo", Kind: "interface", Signature: "interface IFoo", Citation: "c"}},
		},
		workflow.UpstreamResolution{
			Name: "Complete", Coordinate: "github.com/x/complete@v1", ResolutionKind: "source_build",
			APIs: []workflow.APISurface{
				{Symbol: "IBar", Kind: "interface", Signature: "interface IBar", Citation: "c"},
				{Symbol: "IBar.run", Kind: "method", Signature: "void run()", Citation: "c"},
			},
		},
	)
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	var count int
	for _, f := range result.Findings {
		if f.SOPID == "architecture.upstream_source_build_incomplete_contract" {
			count++
			if f.TargetID != "Incomplete" {
				t.Errorf("only the incomplete resolution should fire, got TargetID %q", f.TargetID)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 upstream-contract finding, got %d", count)
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

func TestTopologyContractFindingsRejectsUnapprovedBuildRoot(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "topology-unapproved",
		Contract: &workflow.ContractPacket{
			TopologyFacts: []workflow.TopologyFact{
				{Kind: "workspace_root", Path: "settings.gradle", Value: "gradle_settings"},
				{Kind: "build_root", Path: "sensorhub-driver/build.gradle", Value: "gradle_project"},
				{Kind: "package_root", Path: "ui/package.json", Value: "node_package"},
			},
		},
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{{Name: "driver", Lifecycle: workflow.CapabilityNew, Description: "D."}},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{
					Name: "clean-room-driver",
					ImplementationFiles: []string{
						"osh-core/settings.gradle",
						"osh-core/build.gradle",
						"osh-core/gradlew",
						"sensorhub-driver/src/main/java/Driver.java",
					},
					Capabilities: []string{"driver"},
				},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	got := topologyFindingsByTarget(result.Findings)
	for _, want := range []string{"osh-core/settings.gradle", "osh-core/build.gradle", "osh-core/gradlew"} {
		f, ok := got[want]
		if !ok {
			t.Fatalf("expected topology finding for %q, got %v", want, keysOf(got))
		}
		if f.Severity != "error" || f.Action != "remove" {
			t.Errorf("finding %q has wrong shape: severity=%q action=%q", want, f.Severity, f.Action)
		}
		if f.TargetField != "component_boundaries.clean-room-driver.implementation_files" || f.TargetValue != want {
			t.Errorf("finding %q has non-executable action target: field=%q value=%q", want, f.TargetField, f.TargetValue)
		}
	}
	if _, ok := got["sensorhub-driver/build.gradle"]; ok {
		t.Errorf("existing contract topology path should not be flagged")
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict needs_changes, got %q", result.Verdict)
	}
}

func TestTopologyContractFindingsAllowsExplicitCreateScope(t *testing.T) {
	findings := topologyContractFindings(&workflow.ContractPacket{
		Scope: workflow.ContractScopeSnapshot{
			Create: []string{"plugins/new-driver/package.json"},
		},
		TopologyFacts: []workflow.TopologyFact{
			{Kind: "package_root", Path: "ui/package.json", Value: "node_package"},
		},
	}, []workflow.ComponentDef{
		{
			Name:                "new-driver",
			ImplementationFiles: []string{"plugins/new-driver/package.json", "plugins/new-driver/src/index.ts"},
			Capabilities:        []string{"driver"},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("explicit contract scope.create path should be allowed, got %+v", findings)
	}
}

func topologyFindingsByTarget(findings []workflow.PlanReviewFinding) map[string]workflow.PlanReviewFinding {
	out := make(map[string]workflow.PlanReviewFinding)
	for _, f := range findings {
		if f.SOPID == "architecture.topology_unapproved_build_root" {
			out[f.TargetID] = f
		}
	}
	return out
}

// --- Gate 1: scoped-file ownership (issue #175) -----------------------------

// countScopedUnowned returns the scoped_file_unowned findings keyed by TargetID.
func scopedUnownedByTarget(findings []workflow.PlanReviewFinding) map[string]workflow.PlanReviewFinding {
	out := make(map[string]workflow.PlanReviewFinding)
	for _, f := range findings {
		if f.SOPID == "architecture.scoped_file_unowned" {
			out[f.TargetID] = f
		}
	}
	return out
}

// TestScopedFileOwnershipFindings tables the prevention gate that forces every
// scoped deliverable file to be owned by some component. README.md being owned
// by no component is the 2026-06-13 mavlink-hard wedge fingerprint.
func TestScopedFileOwnershipFindings(t *testing.T) {
	comp := func(name string, files ...string) workflow.ComponentDef {
		return workflow.ComponentDef{Name: name, ImplementationFiles: files, Capabilities: []string{"c"}}
	}

	tests := []struct {
		name       string
		scope      workflow.Scope
		components []workflow.ComponentDef
		wantOrphan []string // TargetIDs expected to fire (order-independent)
	}{
		{
			name:       "create file unowned",
			scope:      workflow.Scope{Create: []string{"src/New.java"}},
			components: []workflow.ComponentDef{comp("c1", "src/Other.java")},
			wantOrphan: []string{"src/New.java"},
		},
		{
			name:       "include README unowned (the bug)",
			scope:      workflow.Scope{Include: []string{"build.gradle", "README.md"}},
			components: []workflow.ComponentDef{comp("c1", "build.gradle", "src/A.java")},
			wantOrphan: []string{"README.md"},
		},
		{
			name:       "include read-only ref in do_not_touch is excluded",
			scope:      workflow.Scope{Include: []string{"docs/SPEC.md"}, DoNotTouch: []string{"docs/SPEC.md"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: nil,
		},
		{
			name:       "include directory entry is not a concrete file",
			scope:      workflow.Scope{Include: []string{"src/", "gradle/"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: nil,
		},
		{
			name:       "include glob entry is not a concrete file",
			scope:      workflow.Scope{Include: []string{"src/**/*.java"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: nil,
		},
		{
			name:       "all create+include owned",
			scope:      workflow.Scope{Create: []string{"src/A.java"}, Include: []string{"README.md"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java"), comp("c2", "README.md", "src/B.java")},
			wantOrphan: nil,
		},
		{
			name:       "Java companion test derived from owned main class is owned",
			scope:      workflow.Scope{Create: []string{"src/test/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystemTest.java"}},
			components: []workflow.ComponentDef{comp("c1", "src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystem.java")},
			wantOrphan: nil,
		},
		{
			name:       "README owned as companion on a source component",
			scope:      workflow.Scope{Include: []string{"README.md"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java", "README.md")},
			wantOrphan: nil,
		},
		{
			name:       "README owned on two source components (scheduler serializes downstream)",
			scope:      workflow.Scope{Include: []string{"README.md"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java", "README.md"), comp("c2", "src/B.java", "README.md")},
			wantOrphan: nil,
		},
		{
			name:       "normalization collapses ./ prefix",
			scope:      workflow.Scope{Include: []string{"./README.md"}},
			components: []workflow.ComponentDef{comp("c1", "README.md")},
			wantOrphan: nil,
		},
		{
			name:       "empty scope greenfield",
			scope:      workflow.Scope{},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: nil,
		},
		{
			name:       "well-known extensionless deliverable (Makefile) IS gated",
			scope:      workflow.Scope{Create: []string{"Makefile"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: []string{"Makefile"}, // in the extensionless-deliverable allowlist
		},
		{
			name:       "unknown extensionless entry not gated (avoids dir false positives)",
			scope:      workflow.Scope{Include: []string{"scripts"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: nil, // no extension, not in allowlist -> treated as a dir
		},
		{
			name:       "file in both create and include reported once",
			scope:      workflow.Scope{Create: []string{"README.md"}, Include: []string{"README.md"}},
			components: []workflow.ComponentDef{comp("c1", "src/A.java")},
			wantOrphan: []string{"README.md"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := scopedUnownedByTarget(scopedFileOwnershipFindings(tc.scope, tc.components))
			if len(got) != len(tc.wantOrphan) {
				t.Fatalf("got %d orphan findings %v, want %d %v", len(got), keysOf(got), len(tc.wantOrphan), tc.wantOrphan)
			}
			for _, want := range tc.wantOrphan {
				f, ok := got[want]
				if !ok {
					t.Fatalf("expected orphan finding for %q, got %v", want, keysOf(got))
				}
				if f.Severity != "error" || f.Action != "add" {
					t.Errorf("finding %q has wrong shape: severity=%q action=%q", want, f.Severity, f.Action)
				}
				if f.TargetField != "component_boundaries[].implementation_files" || f.TargetValue != want {
					t.Errorf("finding %q has non-executable action target: field=%q value=%q", want, f.TargetField, f.TargetValue)
				}
				formatted := (&workflow.PlanReviewResult{Findings: []workflow.PlanReviewFinding{f}}).FormatFindings()
				wantAction := "Action: ADD `" + want + "` TO `component_boundaries[].implementation_files`"
				if !strings.Contains(formatted, wantAction) {
					t.Errorf("formatted scoped-file finding missing executable action %q:\n%s", wantAction, formatted)
				}
			}
		})
	}
}

func keysOf(m map[string]workflow.PlanReviewFinding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestScopedFileOwnership_IntegrationVerdictFlip confirms the rule fires through
// mergeArchitectureFindings and flips the verdict to needs_changes.
func TestScopedFileOwnership_IntegrationVerdictFlip(t *testing.T) {
	plan := &workflow.Plan{
		Slug:        "scoped-unowned",
		Exploration: &workflow.Exploration{Capabilities: []workflow.Capability{{Name: "telemetry", Lifecycle: workflow.CapabilityNew, Description: "T."}}},
		Scope:       workflow.Scope{Include: []string{"README.md"}, Create: []string{"src/Telemetry.java"}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				// Owns the source file but NOT README.md → README orphaned.
				{Name: "cs-telemetry", ImplementationFiles: []string{"src/Telemetry.java"}, Capabilities: []string{"telemetry"}},
			},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeArchitectureFindings(plan, result)

	got := scopedUnownedByTarget(result.Findings)
	if _, ok := got["README.md"]; !ok {
		t.Fatalf("expected README.md orphan finding through mergeArchitectureFindings, got %v", keysOf(got))
	}
	if _, ok := got["src/Telemetry.java"]; ok {
		t.Errorf("src/Telemetry.java is owned; should not be flagged")
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict needs_changes, got %q", result.Verdict)
	}
}
