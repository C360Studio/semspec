package planreviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// makeCatalog returns a Catalog containing the given profiles, indexed by ID.
func makeCatalog(profiles ...harnesscatalog.Profile) *harnesscatalog.Catalog {
	c := &harnesscatalog.Catalog{Profiles: map[string]harnesscatalog.Profile{}}
	for _, p := range profiles {
		c.Profiles[p.ID] = p
	}
	return c
}

// makeReq is a small constructor for plan tests.
func makeReq(id, title, capability string) workflow.Requirement {
	return workflow.Requirement{ID: id, Title: title, CapabilityName: capability}
}

// makeScenario constructs a scenario with the given fields. tags + harness
// bindings are passed positionally to keep test cases compact.
func makeScenario(id, reqID, given string, tags, harness []string) workflow.Scenario {
	return workflow.Scenario{
		ID:                id,
		RequirementID:     reqID,
		Given:             given,
		When:              "the agent runs",
		Then:              []string{"the expected outcome is observed"},
		Tags:              tags,
		HarnessProfileIDs: harness,
	}
}

// TestMergeScenarioTagFindings_NoScenariosSkipsEverything pins the R1-firing
// guard: plan-reviewer's R1 round runs BEFORE scenario generation, so a
// plan with len(plan.Scenarios)==0 must produce zero findings from this
// rule set. Otherwise R1 reviews would reject every plan with N "missing
// @unit coverage" findings — the same failure mode capability rules had
// before the R1 guard was added (2026-05-30).
func TestMergeScenarioTagFindings_NoScenariosSkipsEverything(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "r1-test",
		Requirements: []workflow.Requirement{makeReq("r1", "auth", "user-auth")},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}

	mergeScenarioTagFindings(plan, result)

	if len(result.Findings) != 0 {
		t.Errorf("R1 pre-scenario-gen review should produce zero findings, got: %+v", result.Findings)
	}
	if result.Verdict != "approved" {
		t.Errorf("verdict should be preserved as approved, got %q", result.Verdict)
	}
}

// TestScenarioMissingTierTag_MultipleFailureModes pins that scenarios with
// zero, two, or three tier tags all surface findings. The validator at
// PR 1 already enforces this at parse time, but plan-reviewer re-checks
// defensively per ADR-041 Move 4 (operator-edited PLAN_STATES can bypass
// parse).
func TestScenarioMissingTierTag_MultipleFailureModes(t *testing.T) {
	plan := &workflow.Plan{
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "given", nil, nil),                                                               // zero tier tags
			makeScenario("s2", "r1", "given", []string{"@flaky"}, nil),                                                // zero tier tags (only facet)
			makeScenario("s3", "r1", "given", []string{workflow.TierUnit, workflow.TierIntegration}, nil),             // two tier tags
			makeScenario("s4", "r1", "given", []string{workflow.TierUnit, workflow.TierE2E, workflow.TierSmoke}, nil), // three
			makeScenario("ok", "r1", "given", []string{workflow.TierUnit}, nil),                                       // valid
		},
	}
	findings := scenarioMissingTierTagFindings(plan)

	wantIDs := []string{"s1", "s2", "s3", "s4"}
	if len(findings) != len(wantIDs) {
		t.Fatalf("expected %d findings, got %d: %+v", len(wantIDs), len(findings), findings)
	}
	for i, id := range wantIDs {
		if findings[i].TargetID != id {
			t.Errorf("findings[%d].TargetID = %q, want %q", i, findings[i].TargetID, id)
		}
		if findings[i].SOPID != "scenario.missing_tier_tag" {
			t.Errorf("findings[%d].SOPID = %q, want scenario.missing_tier_tag", i, findings[i].SOPID)
		}
		if findings[i].Severity != "error" {
			t.Errorf("findings[%d] should be error severity, got %q", i, findings[i].Severity)
		}
	}
}

// TestScenarioMissingUnitCoverage_FiresPerRequirement pins ADR-041's
// load-bearing rule: every requirement needs ≥1 @unit scenario as
// baseline coverage. Without this, a requirement with only @integration
// scenarios would never have its in-process logic validated at the dev
// tier.
func TestScenarioMissingUnitCoverage_FiresPerRequirement(t *testing.T) {
	plan := &workflow.Plan{
		Requirements: []workflow.Requirement{
			makeReq("r1", "has-unit", "cap-a"),
			makeReq("r2", "no-unit-only-integration", "cap-b"),
			makeReq("r3", "no-scenarios", "cap-c"),
		},
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "given", []string{workflow.TierUnit}, nil),
			makeScenario("s2", "r2", "given", []string{workflow.TierIntegration}, []string{"p1"}),
		},
	}
	findings := scenarioMissingUnitCoverageFindings(plan)

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (r2 + r3), got %d: %+v", len(findings), findings)
	}
	want := map[string]bool{"r2": false, "r3": false}
	for _, f := range findings {
		if _, ok := want[f.TargetID]; !ok {
			t.Errorf("unexpected finding target %q", f.TargetID)
			continue
		}
		want[f.TargetID] = true
		if f.SOPID != "scenario.missing_unit_coverage" {
			t.Errorf("finding for %s: SOPID = %q, want scenario.missing_unit_coverage", f.TargetID, f.SOPID)
		}
	}
	for id, hit := range want {
		if !hit {
			t.Errorf("expected finding for requirement %s, none surfaced", id)
		}
	}
}

// TestScenarioMissingIntegrationForServices_FiresWhenProfileUncovered is
// the load-bearing rule that closes the issue-#37 gap: when the architect
// binds a services-class profile, the scenario-generator MUST emit at
// least one @integration scenario tagging that profile per requirement.
// Without this rule, the dev sees integration-tier obligations only in
// review feedback (too late) instead of in the scenario set.
func TestScenarioMissingIntegrationForServices_FiresWhenProfileUncovered(t *testing.T) {
	catalog := makeCatalog(
		harnesscatalog.Profile{ID: "mavlink.px4-sitl", Orchestration: harnesscatalog.OrchestrationServices},
	)
	plan := &workflow.Plan{
		Architecture: &workflow.ArchitectureDocument{
			HarnessProfiles: []workflow.HarnessProfileSelection{{ProfileID: "mavlink.px4-sitl"}},
		},
		Requirements: []workflow.Requirement{
			makeReq("r1", "covered", "cap-a"),
			makeReq("r2", "uncovered", "cap-b"),
		},
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "given", []string{workflow.TierUnit}, nil),
			makeScenario("s2", "r1", "given", []string{workflow.TierIntegration}, []string{"mavlink.px4-sitl"}),
			makeScenario("s3", "r2", "given", []string{workflow.TierUnit}, nil),
			// r2 has no @integration scenario — this is the failure mode
		},
	}
	findings := scenarioMissingIntegrationForServicesFindings(plan, catalog)

	if len(findings) != 1 {
		t.Fatalf("expected exactly one finding for r2, got %d: %+v", len(findings), findings)
	}
	if findings[0].TargetID != "r2" {
		t.Errorf("expected finding on r2, got %q", findings[0].TargetID)
	}
	if !strings.Contains(findings[0].TargetValue, "mavlink.px4-sitl") {
		t.Errorf("finding TargetValue should mention the profile_id, got %q", findings[0].TargetValue)
	}
}

// TestScenarioMissingIntegrationForServices_PureFixtureNoFinding pins that
// pure-fixture profiles don't trigger the rule — they don't imply a peer
// process, so no @integration scenario is required.
func TestScenarioMissingIntegrationForServices_PureFixtureNoFinding(t *testing.T) {
	catalog := makeCatalog(
		harnesscatalog.Profile{ID: "mavlink.raw-mavlink-direct", Orchestration: harnesscatalog.OrchestrationPureFixture},
	)
	plan := &workflow.Plan{
		Architecture: &workflow.ArchitectureDocument{
			HarnessProfiles: []workflow.HarnessProfileSelection{{ProfileID: "mavlink.raw-mavlink-direct"}},
		},
		Requirements: []workflow.Requirement{makeReq("r1", "any", "cap-a")},
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "given", []string{workflow.TierUnit}, nil),
		},
	}
	findings := scenarioMissingIntegrationForServicesFindings(plan, catalog)
	if len(findings) != 0 {
		t.Errorf("pure-fixture profile should not require @integration; got: %+v", findings)
	}
}

// TestScenarioHarnessIDUnresolved_FiresPerEntry pins that each unresolved
// binding ID surfaces its own finding so the regen LLM gets per-binding
// directives instead of a bundled "fix all your IDs" complaint.
func TestScenarioHarnessIDUnresolved_FiresPerEntry(t *testing.T) {
	catalog := makeCatalog(
		harnesscatalog.Profile{ID: "real.profile", Orchestration: harnesscatalog.OrchestrationServices},
	)
	plan := &workflow.Plan{
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "given", []string{workflow.TierIntegration}, []string{"real.profile"}),              // OK
			makeScenario("s2", "r1", "given", []string{workflow.TierIntegration}, []string{"ghost.one"}),                 // bad
			makeScenario("s3", "r1", "given", []string{workflow.TierIntegration}, []string{"ghost.two", "real.profile"}), // ghost.two bad
		},
	}
	findings := scenarioHarnessIDUnresolvedFindings(plan, catalog)

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (s2/ghost.one + s3/ghost.two), got %d: %+v", len(findings), findings)
	}
	wantPairs := map[string]string{"s2": "ghost.one", "s3": "ghost.two"}
	for _, f := range findings {
		want, ok := wantPairs[f.TargetID]
		if !ok {
			t.Errorf("unexpected finding on scenario %q: %+v", f.TargetID, f)
			continue
		}
		if f.TargetValue != want {
			t.Errorf("scenario %s finding TargetValue = %q, want %q", f.TargetID, f.TargetValue, want)
		}
	}
}

// TestScenarioUnitMentionsServices_HeuristicFiresAtWarn pins the WARN-level
// drift detector: an @unit scenario whose prose names real-service tokens
// (SITL, profile IDs, "real database") gets flagged. Severity is warning,
// not error, since this is a heuristic — operator can override.
func TestScenarioUnitMentionsServices_HeuristicFiresAtWarn(t *testing.T) {
	catalog := makeCatalog(
		harnesscatalog.Profile{ID: "mavlink.px4-sitl"},
	)
	plan := &workflow.Plan{
		Scenarios: []workflow.Scenario{
			makeScenario("clean", "r1", "the parser is constructed with default config", []string{workflow.TierUnit}, nil),
			makeScenario("sitl-in-given", "r1", "the SITL container is running", []string{workflow.TierUnit}, nil),
			makeScenario("real-db", "r1", "a real database is available at $DATABASE_URL", []string{workflow.TierUnit}, nil),
			makeScenario("profile-id-mention", "r1", "the mavlink.px4-sitl harness is up", []string{workflow.TierUnit}, nil),
			// Integration tier — same prose, no finding
			makeScenario("integ-with-sitl", "r1", "the SITL container is running", []string{workflow.TierIntegration}, []string{"mavlink.px4-sitl"}),
		},
	}
	findings := scenarioUnitMentionsServicesFindings(plan, catalog)

	wantIDs := map[string]bool{"sitl-in-given": false, "real-db": false, "profile-id-mention": false}
	if len(findings) != len(wantIDs) {
		t.Fatalf("expected %d findings, got %d: %+v", len(wantIDs), len(findings), findings)
	}
	for _, f := range findings {
		if _, ok := wantIDs[f.TargetID]; !ok {
			t.Errorf("unexpected finding on scenario %q", f.TargetID)
			continue
		}
		wantIDs[f.TargetID] = true
		if f.Severity != "warning" {
			t.Errorf("expected warning severity for heuristic rule, got %q on %s", f.Severity, f.TargetID)
		}
		if f.SOPID != "scenario.unit_mentions_services" {
			t.Errorf("SOPID mismatch on %s: %q", f.TargetID, f.SOPID)
		}
	}
	for id, hit := range wantIDs {
		if !hit {
			t.Errorf("expected finding on %s, none surfaced", id)
		}
	}
}

// TestMergeScenarioTagFindings_NormalizesVerdictOnError pins the
// load-bearing post-merge behavior: any error-severity structural finding
// bumps the LLM's verdict to needs_changes. Without NormalizeVerdict the
// LLM saying "approved" would let the plan slip through to the next phase
// despite structural violations.
func TestMergeScenarioTagFindings_NormalizesVerdictOnError(t *testing.T) {
	plan := &workflow.Plan{
		Requirements: []workflow.Requirement{makeReq("r1", "auth", "user-auth")},
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "given", nil, nil), // missing tier tag → error finding
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeScenarioTagFindings(plan, result)

	if result.Verdict == "approved" {
		t.Errorf("verdict should have been bumped from approved, got %q", result.Verdict)
	}
	if len(result.Findings) == 0 {
		t.Errorf("expected at least one finding")
	}
}

// TestMergeScenarioTagFindings_WarnAloneDoesNotBumpVerdict pins that a
// warning-only outcome (e.g., heuristic unit-mentions-services with no
// other rule firing) preserves the LLM's verdict. Warnings are operator
// signals, not gate failures.
func TestMergeScenarioTagFindings_WarnAloneDoesNotBumpVerdict(t *testing.T) {
	plan := &workflow.Plan{
		Requirements: []workflow.Requirement{makeReq("r1", "any", "cap-a")},
		Scenarios: []workflow.Scenario{
			makeScenario("s1", "r1", "container is running", []string{workflow.TierUnit}, nil),
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeScenarioTagFindings(plan, result)

	// Should have produced the warning AND not bumped the verdict
	// (NormalizeVerdict only escalates on error-severity findings).
	hasWarn := false
	hasError := false
	for _, f := range result.Findings {
		switch f.Severity {
		case "warning":
			hasWarn = true
		case "error":
			hasError = true
		}
	}
	if !hasWarn {
		t.Errorf("expected unit_mentions_services warning, got findings: %+v", result.Findings)
	}
	if hasError {
		// scenarioMissingUnitCoverageFindings won't fire because the
		// scenario IS tagged @unit. So no error should land.
		t.Errorf("did not expect any error-severity findings, got: %+v", result.Findings)
	}
}
