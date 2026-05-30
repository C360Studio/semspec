package planreviewer

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// mergeCapabilityFindings runs the ADR-040 Move 2 structural rules over a
// plan + review result and appends any deterministic findings to the result.
// The reviewer agent's LLM-driven verdict is treated as advisory on this
// surface — structural violations are non-negotiable and surface even when
// the LLM said "approved".
//
// Load-bearing distinction (per go-reviewer PR 2 audit):
//   - capability_orphan.docs_only is the PRIMARY rule that only plan-reviewer
//     can enforce. plan-manager's handleRequirementsMutation has no docs-only
//     check; without this rule, run #3's failure mode (every Requirement owns
//     only *.md files) would slip through to execution.
//   - The other rules (capability_orphan, capability.requirement_orphan,
//     capability_dependency_cycle, capability_dependency_orphan) are
//     defensive backstops. plan-manager rejects all four conditions at
//     handleRequirementsMutation BEFORE the plan reaches scenarios_generated,
//     so these rule paths fire only in pathological cases (operator-edited
//     PLAN_STATES that bypassed the mutation handler, or a future refactor
//     that relaxed the front-line guard). Keeping them is cheap insurance.
//
// Rules:
//   - capability_orphan: a Capability with no implementing Requirement, OR a
//     Capability whose Requirements only own documentation files. This is
//     the run-#3 failure mode encoded deterministically.
//   - capability_dependency_cycle: a cycle in Plan.Exploration depends_on
//     edges. Hard reject — parallel work on cyclic capabilities never
//     converges. Layered on top of ValidateCapabilitySet at the analyst
//     sub-phase as a defensive backstop.
//   - capability_dependency_orphan: a Capability.DependsOn reference to a
//     name not declared in Plan.Exploration. Same defensive backstop layered
//     on top of ValidateCapabilitySet.
//
// Skipped entirely when Plan.Exploration is nil (legacy plans without the
// analyst sub-phase have no capabilities to check).
//
// Side effect: calls result.NormalizeVerdict() so the verdict reflects the
// merged findings ("approved" → "needs_changes" when error findings appear).
func mergeCapabilityFindings(plan *workflow.Plan, result *workflow.PlanReviewResult) {
	if plan == nil || plan.Exploration == nil || result == nil {
		return
	}

	original := len(result.Findings)
	result.Findings = append(result.Findings, capabilityOrphanFindings(plan)...)
	result.Findings = append(result.Findings, capabilityDependencyCycleFindings(plan)...)
	result.Findings = append(result.Findings, capabilityDependencyOrphanFindings(plan)...)

	if len(result.Findings) > original {
		// Any structural finding is error-severity and a violation; the
		// LLM's verdict must be upgraded to needs_changes.
		result.NormalizeVerdict()
	}
}

// capabilityOrphanFindings flags each Capability that has no implementing
// Requirement OR whose Requirements only touch documentation files. Returns
// one finding per offending capability so the regen LLM has per-capability
// actionable directives rather than a single bundled complaint.
//
// The "docs-only" sub-rule is what catches run #3's failure mode: the planner
// declared a `coverage-matrix-tooling` capability and the requirement-generator
// produced only README-modification requirements for it. The Requirement
// existed (so capability_orphan-strict wouldn't fire), but every file in
// FilesOwned was a *.md path. ADR-040 marks this as the run-#3 fingerprint.
func capabilityOrphanFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || plan.Exploration == nil {
		return nil
	}

	var findings []workflow.PlanReviewFinding

	// (1) Capabilities with zero Requirements.
	for _, name := range workflow.FindUncoveredCapabilities(plan.Exploration, plan.Requirements) {
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "capability.orphan",
			SOPTitle:    "Capability without implementing Requirement (ADR-040)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "requirements",
			TargetID:    name,
			Action:      "add",
			TargetField: "requirements",
			TargetValue: fmt.Sprintf("requirement for capability `%s`", name),
			Issue:       fmt.Sprintf("Capability %q is declared in Plan.Exploration but no Requirement claims it via capability_name. Every capability must own at least one implementation Requirement.", name),
			Suggestion:  fmt.Sprintf("Generate one Requirement with capability_name=%q that owns the implementation files for this capability.", name),
		})
	}

	// (2) Capabilities whose Requirements only touch documentation files.
	for _, name := range workflow.FindDocsOnlyCapabilities(plan.Exploration, plan.Requirements) {
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "capability.orphan.docs_only",
			SOPTitle:    "Capability has only documentation-owning Requirements (ADR-040 run-#3 fingerprint)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "requirements",
			TargetID:    name,
			Action:      "add",
			TargetField: "requirements",
			TargetValue: fmt.Sprintf("implementation files for capability `%s`", name),
			Issue:       fmt.Sprintf("Capability %q has Requirements but every files_owned entry is documentation (*.md, *.txt, README*). The capability needs at least one Requirement whose files_owned includes implementation source files.", name),
			Suggestion:  fmt.Sprintf("Extend the Requirement(s) for capability %q to include the implementation source files (e.g. *.go, *.ts, *.java) — documentation is supplementary, not the deliverable.", name),
		})
	}

	// (3) Orphan Requirements whose CapabilityName doesn't resolve.
	for _, r := range workflow.FindOrphanRequirementCapabilities(plan.Exploration, plan.Requirements) {
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "capability.requirement_orphan",
			SOPTitle:    "Requirement references a capability not in Plan.Exploration (ADR-040)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "requirements",
			TargetID:    r.ID,
			Action:      "rename",
			TargetField: fmt.Sprintf("requirement.%s.capability_name", r.ID),
			TargetValue: fmt.Sprintf("%s → <one of Plan.Exploration.Capabilities[].Name>", r.CapabilityName),
			Issue:       fmt.Sprintf("Requirement %s has capability_name=%q but no capability with that name is declared in Plan.Exploration.", r.ID, r.CapabilityName),
			Suggestion:  "Either rename the Requirement's capability_name to one of the declared capabilities, or flag a missing capability back to the analyst sub-phase.",
		})
	}

	return findings
}

// capabilityDependencyCycleFindings flags cycles in Plan.Exploration's
// depends_on graph. The check duplicates workflow.ValidateCapabilitySet's
// cycle detector — having it at both layers (analyst-output validator AND
// plan-reviewer rule) is intentional: if a future refactor relaxes the
// front-line guard, the rule here still rejects the plan before execution
// burns tokens.
func capabilityDependencyCycleFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || plan.Exploration == nil {
		return nil
	}
	// ValidateCapabilitySet returns an error containing the cycle path when
	// it detects a cycle. We invoke it solely for the cycle check; the
	// orphan-deps check is split out below to produce per-edge findings.
	err := workflow.ValidateCapabilitySet(plan.Exploration.Capabilities)
	if err == nil {
		return nil
	}
	// The validator returns the first issue it finds (cycle or orphan).
	// We can't distinguish without re-running, so we duck-type on the
	// error message — "cycle" implies the cycle detector tripped. The
	// orphan-deps path is handled by capabilityDependencyOrphanFindings.
	// TODO(PR follow-up): replace duck-type with sentinel errors
	// (ErrCapabilityCycle / ErrCapabilityDepOrphan) in workflow/plan_capability.go
	// and switch via errors.Is.
	if !strings.Contains(err.Error(), "cycle") {
		return nil
	}
	return []workflow.PlanReviewFinding{{
		SOPID:       "capability.dependency_cycle",
		SOPTitle:    "Capability depends_on cycle (ADR-040 hard constraint)",
		Severity:    "error",
		Status:      "violation",
		Category:    "structural",
		Phase:       "requirements",
		Action:      "remove",
		TargetField: "exploration.capabilities[].depends_on",
		TargetValue: "edge participating in cycle",
		Issue:       err.Error(),
		Suggestion:  "Break the cycle by removing one depends_on edge or splitting one capability into two so the dependency graph is acyclic. Cycles never converge under parallel execution.",
	}}
}

// capabilityDependencyOrphanFindings flags depends_on references that name a
// capability not present in Plan.Exploration. Like the cycle rule, this is a
// defensive layer on top of ValidateCapabilitySet.
func capabilityDependencyOrphanFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || plan.Exploration == nil {
		return nil
	}
	declared := make(map[string]bool, len(plan.Exploration.Capabilities))
	for _, c := range plan.Exploration.Capabilities {
		declared[c.Name] = true
	}
	var findings []workflow.PlanReviewFinding
	for _, c := range plan.Exploration.Capabilities {
		for _, dep := range c.DependsOn {
			if !declared[dep] {
				findings = append(findings, workflow.PlanReviewFinding{
					SOPID:       "capability.dependency_orphan",
					SOPTitle:    "Capability depends_on names an undeclared capability (ADR-040)",
					Severity:    "error",
					Status:      "violation",
					Category:    "structural",
					Phase:       "requirements",
					TargetID:    c.Name,
					Action:      "remove",
					TargetField: fmt.Sprintf("exploration.capabilities[%s].depends_on", c.Name),
					TargetValue: dep,
					Issue:       fmt.Sprintf("Capability %q declares depends_on=%q but no capability with that name exists in Plan.Exploration.", c.Name, dep),
					Suggestion:  fmt.Sprintf("Either remove the depends_on reference, declare the missing capability, or fix the spelling. Common cause: the analyst sub-phase mis-named the dependency."),
				})
			}
		}
	}
	return findings
}
