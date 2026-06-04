package planreviewer

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// mergeScenarioTagFindings runs the ADR-041 Move 4 structural rules over a
// plan + review result and appends any deterministic findings to the result.
// Mirrors mergeCapabilityFindings — the LLM reviewer's verdict is advisory;
// these structural checks are non-negotiable and bump "approved" to
// "needs_changes" when any error finding lands.
//
// The five rules:
//
//   - scenario.missing_tier_tag — a scenario has zero or more than one of
//     the four tier tags (@unit/@integration/@smoke/@e2e). Caught by the
//     scenario-generator's parse-time ValidateScenarioTags too, but the
//     plan-reviewer re-checks defensively so an operator-edited PLAN_STATES
//     can't sneak past.
//   - scenario.missing_unit_coverage — a requirement has zero @unit
//     scenarios. Every requirement needs unit coverage as a baseline per
//     ADR-041.
//   - scenario.missing_integration_for_services — the architect bound a
//     services-class or testcontainers-class harness profile to the plan
//     but no scenario tags @integration with that profile_id. The
//     binding-relevance logic mirrors processor/scenario-generator's
//     classifier.go (integrationEmissions); if you change one, change the
//     other or extract to a shared package.
//   - scenario.harness_id_unresolved — a Scenario.HarnessProfileIDs entry
//     names a profile_id that doesn't resolve through the harness catalog.
//   - scenario.unit_mentions_services (warn) — a @unit scenario's
//     given/when/then prose contains real-service tokens (SITL, profile
//     names, "real", "live", "container"). Heuristic: the LLM's persona
//     prompt forbids these tokens at the @unit tier; the rule catches
//     drift the persona didn't enforce.
//
// All rules gate on len(plan.Scenarios) > 0 to avoid the R1-firing bug
// pattern from ADR-040 (capability_orphan rules fired on R1 plans with no
// scenarios). Plan-reviewer R1 reviews the drafted plan pre-scenario-gen;
// R2 reviews after scenarios land. Move 4 rules only make sense on R2.
//
// Skipped entirely when plan.Architecture is nil for rules that need
// architecture context. Skipped entirely when the catalog fails to load
// (harness_id_unresolved degrades to "we can't verify; the catalog-
// dependent rules are silent").
func mergeScenarioTagFindings(plan *workflow.Plan, result *workflow.PlanReviewResult) {
	if plan == nil || result == nil {
		return
	}
	if len(plan.Scenarios) == 0 {
		return
	}
	original := len(result.Findings)

	result.Findings = append(result.Findings, scenarioMissingTierTagFindings(plan)...)
	result.Findings = append(result.Findings, scenarioMissingUnitCoverageFindings(plan)...)

	catalog, err := harnesscatalog.Load("")
	if err == nil {
		result.Findings = append(result.Findings, scenarioMissingIntegrationForServicesFindings(plan, catalog)...)
		result.Findings = append(result.Findings, scenarioHarnessIDUnresolvedFindings(plan, catalog)...)
	}

	result.Findings = append(result.Findings, scenarioUnitMentionsServicesFindings(plan, catalog)...)

	if len(result.Findings) > original {
		result.NormalizeVerdict()
	}
}

// scenarioMissingTierTagFindings flags scenarios with zero or more than one
// tier tag. One finding per offending scenario.
func scenarioMissingTierTagFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	var findings []workflow.PlanReviewFinding
	for _, s := range plan.Scenarios {
		tierCount := 0
		for _, tag := range s.Tags {
			if workflow.IsTierTag(tag) {
				tierCount++
			}
		}
		if tierCount == 1 {
			continue
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "scenario.missing_tier_tag",
			SOPTitle:    "Scenario missing or duplicating tier tag (ADR-041 Move 4)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "scenarios",
			TargetID:    s.ID,
			Action:      "replace",
			TargetField: fmt.Sprintf("scenario.%s.tags", s.ID),
			TargetValue: "[exactly one of @unit / @integration / @smoke / @e2e]",
			Issue:       fmt.Sprintf("Scenario %s has %d tier tags; exactly one is required.", s.ID, tierCount),
			Suggestion:  "Replace the tags with exactly one tier tag. Use @unit for in-process behavior with fakes, @integration when an integration test environment is required, @e2e for full-system UI flows, @smoke only when explicitly directed.",
		})
	}
	return findings
}

// scenarioMissingUnitCoverageFindings flags requirements that have zero
// @unit scenarios. One finding per offending requirement.
func scenarioMissingUnitCoverageFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if len(plan.Requirements) == 0 {
		return nil
	}
	byReq := scenariosByRequirement(plan)
	var findings []workflow.PlanReviewFinding
	for _, req := range plan.Requirements {
		if hasTierScenario(byReq[req.ID], workflow.TierUnit) {
			continue
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "scenario.missing_unit_coverage",
			SOPTitle:    "Requirement has no @unit scenario (ADR-041 Move 4)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "scenarios",
			TargetID:    req.ID,
			Action:      "add",
			TargetField: fmt.Sprintf("requirement.%s.scenarios", req.ID),
			TargetValue: "at least one @unit scenario",
			Issue:       fmt.Sprintf("Requirement %s (%q) has no @unit scenario. Every requirement needs unit coverage as a baseline so the dev tier can observe correct behavior.", req.ID, req.Title),
			Suggestion:  "Add at least one @unit scenario exercising the requirement's logic at function/class boundary with fakes or in-process state. No real services, no SITL, no databases — those belong to @integration.",
		})
	}
	return findings
}

// scenarioMissingIntegrationForServicesFindings flags requirements bound to
// services-class or testcontainers-class harness profiles that have no
// @integration scenario tagging that profile.
//
// Binding semantics mirror processor/scenario-generator/classifier.go
// (integrationEmissions): every architecturally-selected services-class or
// testcontainers-class profile is treated as relevant to every requirement.
// Coarse but correct (over-emits rather than under-detects). Tighten when
// capability-component binding lands.
func scenarioMissingIntegrationForServicesFindings(plan *workflow.Plan, catalog *harnesscatalog.Catalog) []workflow.PlanReviewFinding {
	if plan == nil || plan.Architecture == nil || catalog == nil {
		return nil
	}
	servicesIDs := servicesClassProfileIDs(plan.Architecture, catalog)
	if len(servicesIDs) == 0 {
		return nil
	}
	byReq := scenariosByRequirement(plan)
	var findings []workflow.PlanReviewFinding
	for _, req := range plan.Requirements {
		for _, profileID := range servicesIDs {
			if hasIntegrationScenarioForProfile(byReq[req.ID], profileID) {
				continue
			}
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "scenario.missing_integration_for_services",
				SOPTitle:    "Requirement bound to services-class harness lacks @integration scenario (ADR-041 Move 4)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "scenarios",
				TargetID:    req.ID,
				Action:      "add",
				TargetField: fmt.Sprintf("requirement.%s.scenarios", req.ID),
				TargetValue: fmt.Sprintf("@integration scenario with harness_profile_ids containing %q", profileID),
				Issue:       fmt.Sprintf("Requirement %s has no @integration scenario tagging harness profile %q. The architect selected this integration evidence target; without an @integration scenario Murat cannot trace whether runtime proof is present, missing, or deferred.", req.ID, profileID),
				Suggestion:  fmt.Sprintf("Add at least one scenario tagged @integration with harness_profile_ids containing %q. The scenario's Given assumes the integration environment is available and its endpoint is read from environment variables; the scenario does NOT instruct test code to start external services.", profileID),
			})
		}
	}
	return findings
}

// scenarioHarnessIDUnresolvedFindings flags scenarios whose HarnessProfileIDs
// contain entries that don't resolve through the catalog. One finding per
// (scenario, unresolved_id) pair.
func scenarioHarnessIDUnresolvedFindings(plan *workflow.Plan, catalog *harnesscatalog.Catalog) []workflow.PlanReviewFinding {
	if plan == nil || catalog == nil {
		return nil
	}
	var findings []workflow.PlanReviewFinding
	for _, s := range plan.Scenarios {
		for _, id := range s.HarnessProfileIDs {
			if _, ok := catalog.Profiles[id]; ok {
				continue
			}
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "scenario.harness_id_unresolved",
				SOPTitle:    "Scenario harness_profile_id does not resolve through the catalog (ADR-041 Move 4)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "scenarios",
				TargetID:    s.ID,
				Action:      "fix",
				TargetField: fmt.Sprintf("scenario.%s.harness_profile_ids", s.ID),
				TargetValue: id,
				Issue:       fmt.Sprintf("Scenario %s lists harness_profile_id %q, which is not present in the harness catalog. QA cannot trace runtime evidence to a profile that doesn't exist.", s.ID, id),
				Suggestion:  fmt.Sprintf("Replace %q with a valid profile_id from the architecture's selected harness_profiles[], or remove the entry. Profile IDs are catalog cross-references (e.g. \"mavlink.px4-sitl.mavsdk-smoke\"), not free text.", id),
			})
		}
	}
	return findings
}

// scenarioUnitMentionsServicesFindings is the WARN-level heuristic: a @unit
// scenario's prose contains tokens that imply real-service observation,
// suggesting the scenario was tier-misclassified. Catches drift the persona
// prompt didn't enforce. One finding per offending scenario.
//
// Token list is conservative — matches obvious tier-crossing words. The
// catalog's profile IDs are also added so a scenario mentioning the
// architect's selected profile names at @unit is flagged. False positives
// are acceptable at warn-level; the operator can ignore.
func scenarioUnitMentionsServicesFindings(plan *workflow.Plan, catalog *harnesscatalog.Catalog) []workflow.PlanReviewFinding {
	if plan == nil {
		return nil
	}
	tokens := []string{
		"SITL", " sitl", "container",
		"real database", "real service", "real network",
		"live database", "live service", "live network",
		"running service", "running peer", "running harness",
		"docker run", "docker compose",
	}
	if catalog != nil {
		for id := range catalog.Profiles {
			if id != "" {
				tokens = append(tokens, id)
			}
		}
	}

	var findings []workflow.PlanReviewFinding
	for _, s := range plan.Scenarios {
		if !hasTag(s, workflow.TierUnit) {
			continue
		}
		matched := matchedToken(s, tokens)
		if matched == "" {
			continue
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "scenario.unit_mentions_services",
			SOPTitle:    "@unit scenario mentions real services (ADR-041 Move 4 warn)",
			Severity:    "warning",
			Status:      "violation",
			Category:    "structural",
			Phase:       "scenarios",
			TargetID:    s.ID,
			Action:      "review",
			TargetField: fmt.Sprintf("scenario.%s.{given,when,then,title}", s.ID),
			TargetValue: matched,
			Issue:       fmt.Sprintf("Scenario %s is tagged @unit but its prose contains %q, which implies real-service observation. @unit scenarios are observable at function/class boundary with fakes only.", s.ID, matched),
			Suggestion:  fmt.Sprintf("Either rewrite the scenario to describe in-process behavior (fakes, fixtures, no peer process), or move the obligation to a separate @integration scenario bound to the relevant harness profile. The %q reference belongs to the @integration tier.", matched),
		})
	}
	return findings
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scenariosByRequirement indexes the plan's scenarios by requirement ID for
// O(1) lookup during per-requirement rules.
func scenariosByRequirement(plan *workflow.Plan) map[string][]workflow.Scenario {
	out := make(map[string][]workflow.Scenario, len(plan.Scenarios))
	for _, s := range plan.Scenarios {
		out[s.RequirementID] = append(out[s.RequirementID], s)
	}
	return out
}

// hasTierScenario reports whether the list contains at least one scenario
// tagged with `tier`.
func hasTierScenario(scenarios []workflow.Scenario, tier string) bool {
	for _, s := range scenarios {
		if hasTag(s, tier) {
			return true
		}
	}
	return false
}

// hasIntegrationScenarioForProfile reports whether the list contains an
// @integration scenario binding the given profile ID.
func hasIntegrationScenarioForProfile(scenarios []workflow.Scenario, profileID string) bool {
	for _, s := range scenarios {
		if !hasTag(s, workflow.TierIntegration) {
			continue
		}
		for _, id := range s.HarnessProfileIDs {
			if id == profileID {
				return true
			}
		}
	}
	return false
}

// hasTag reports whether the scenario carries the given tag.
func hasTag(s workflow.Scenario, tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// servicesClassProfileIDs returns the set of profile IDs the architect
// selected whose catalog orchestration is services or testcontainers, with
// duplicates removed. Preserves architect-selected order.
func servicesClassProfileIDs(arch *workflow.ArchitectureDocument, catalog *harnesscatalog.Catalog) []string {
	if arch == nil || catalog == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, sel := range arch.HarnessProfiles {
		if _, dup := seen[sel.ProfileID]; dup {
			continue
		}
		profile, ok := catalog.Profiles[sel.ProfileID]
		if !ok {
			continue
		}
		orch := profile.EffectiveOrchestration()
		if orch != harnesscatalog.OrchestrationServices && orch != harnesscatalog.OrchestrationTestcontainers {
			continue
		}
		seen[sel.ProfileID] = struct{}{}
		out = append(out, sel.ProfileID)
	}
	return out
}

// matchedToken returns the first token from `tokens` that appears in any of
// the scenario's prose fields. Case-insensitive match. Returns "" when no
// token matches.
func matchedToken(s workflow.Scenario, tokens []string) string {
	corpus := strings.ToLower(strings.Join([]string{s.Given, s.When, strings.Join(s.Then, " ")}, " "))
	for _, t := range tokens {
		if t == "" {
			continue
		}
		if strings.Contains(corpus, strings.ToLower(t)) {
			return t
		}
	}
	return ""
}
