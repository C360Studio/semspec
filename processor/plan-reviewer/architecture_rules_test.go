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
			got := scopedUnownedByTarget(scopedFileOwnershipFindings(tc.scope, tc.components, true))
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

// TestScopedFileOwnership_ArchPhaseSkipsCreate pins the ADR-051 Slice 3 phase
// gating: at the architecture-review round (no Stories yet) scope.create is
// draft-partial, so its ownership is NOT checked (checkCreate=false) — only
// scope.include is. Once Stories exist (R2) the create pass runs. Without this
// the architecture round would false-reject every greenfield plan whose
// scope.create has not yet been reconciled with component implementation_files
// by ensureScopeCreateCoversStories.
func TestScopedFileOwnership_ArchPhaseSkipsCreate(t *testing.T) {
	comp := func(name string, files ...string) workflow.ComponentDef {
		return workflow.ComponentDef{Name: name, ImplementationFiles: files, Capabilities: []string{"c"}}
	}
	scope := workflow.Scope{
		Create:  []string{"src/Unreconciled.java"}, // owned by NO component yet (pre-stories)
		Include: []string{"README.md"},             // owned by NO component → always an orphan
	}
	components := []workflow.ComponentDef{comp("c1", "src/Other.java")}

	t.Run("arch phase (no stories) flags include only", func(t *testing.T) {
		got := scopedUnownedByTarget(scopedFileOwnershipFindings(scope, components, false))
		if _, ok := got["src/Unreconciled.java"]; ok {
			t.Error("scope.create file must NOT be flagged before stories reconcile it (false positive)")
		}
		if _, ok := got["README.md"]; !ok {
			t.Error("scope.include orphan must still be flagged at the architecture phase")
		}
	})

	t.Run("stories present flags both", func(t *testing.T) {
		got := scopedUnownedByTarget(scopedFileOwnershipFindings(scope, components, true))
		if _, ok := got["src/Unreconciled.java"]; !ok {
			t.Error("scope.create orphan must be flagged once stories exist")
		}
		if _, ok := got["README.md"]; !ok {
			t.Error("scope.include orphan must be flagged")
		}
	})

	t.Run("mergeArchitectureFindings gates create on plan.Stories", func(t *testing.T) {
		plan := &workflow.Plan{
			Slug:         "demo",
			Scope:        scope,
			Architecture: &workflow.ArchitectureDocument{ComponentBoundaries: components},
		}
		// No Stories → create pass skipped; the unowned create file must not fire.
		pre := &workflow.PlanReviewResult{Verdict: "approved"}
		mergeArchitectureFindings(plan, pre)
		if _, ok := scopedUnownedByTarget(pre.Findings)["src/Unreconciled.java"]; ok {
			t.Error("pre-stories mergeArchitectureFindings must skip the scope.create ownership check")
		}

		// Stories present → create pass runs.
		plan.Stories = []workflow.Story{{ID: "story.demo.1.1"}}
		post := &workflow.PlanReviewResult{Verdict: "approved"}
		mergeArchitectureFindings(plan, post)
		if _, ok := scopedUnownedByTarget(post.Findings)["src/Unreconciled.java"]; !ok {
			t.Error("post-stories mergeArchitectureFindings must run the scope.create ownership check")
		}
	})
}
