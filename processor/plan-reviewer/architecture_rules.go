package planreviewer

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// mergeArchitectureFindings runs the ADR-043 PR 2 architecture-round structural
// rules over a plan + review result and appends any deterministic findings.
// These rules layer on top of the workflow validators that architecture-generator
// runs at parse-time — the architect should fail-fast there, but the rules
// here are a defensive backstop for the case where an operator-edited plan
// or a future relaxed validator lets a malformed architecture reach review.
//
// Rules (R2 architecture round):
//   - architecture.component_missing_implementation_files: ComponentDef with
//     empty ImplementationFiles. One finding per offending component.
//   - architecture.component_implementation_files_doc_only: ComponentDef whose
//     ImplementationFiles contain only documentation-extension files. The
//     architect-side equivalent of capability.orphan.docs_only — catches the
//     run-#3 docs-only fingerprint upstream of Sarah's story-prep phase so
//     the regen cycle hits Winston, not John.
//   - capability.unresolved_in_architecture: a Capability.Name from
//     plan.Exploration whose name doesn't appear in any ComponentDef's
//     Capabilities list. Winston didn't map this capability to a component.
//   - architecture.component_overloaded_capabilities: ComponentDef mapping
//     ≥2 capabilities but declaring fewer SOURCE (non-doc) implementation
//     files than capabilities — it has no distinct implementation surface per
//     capability, so one dev loop implements one and stubs the rest. The
//     inverse of the missing-files / docs-only rules (an OVER-loaded
//     component). The 2026-06-13 mavlink-hard MavsdkDriver fingerprint:
//     caps [bootstrap, telemetry, control] mapped to 2 source files → dev
//     built only Position+Takeoff, QA caught the gap.
//   - architecture.upstream_source_build_incomplete_contract: a source_build
//     UpstreamResolution that names a class/interface in its APIs but resolves
//     ZERO method/function signatures — the dev then reverse-engineers the
//     method contract through compile errors (ADR-047). The 2026-06-13
//     mavlink-hard ICommandStatus fingerprint. Scoped to source_build;
//     maven_central (jar-verified) and unresolved (honest flag) never fire.
//
// Skipped entirely when plan.Architecture is nil (legacy plans without
// the architecture-generator phase have no components to check), when
// plan.Exploration is nil (no capabilities to cross-reference), or when
// ComponentBoundaries is empty.
//
// Side effect: calls result.NormalizeVerdict() so the verdict reflects the
// merged findings ("approved" → "needs_changes" when error findings appear).
func mergeArchitectureFindings(plan *workflow.Plan, result *workflow.PlanReviewResult) {
	if plan == nil || plan.Architecture == nil || result == nil {
		return
	}
	if len(plan.Architecture.ComponentBoundaries) == 0 {
		return
	}

	original := len(result.Findings)
	result.Findings = append(result.Findings,
		architectureImplementationFileFindings(plan.Architecture.ComponentBoundaries)...)
	result.Findings = append(result.Findings,
		componentOverloadedCapabilityFindings(plan.Architecture.ComponentBoundaries)...)
	result.Findings = append(result.Findings,
		capabilityUnresolvedInArchitectureFindings(plan)...)
	result.Findings = append(result.Findings,
		upstreamSourceBuildContractFindings(plan.Architecture.UpstreamResolutions)...)

	if len(result.Findings) > original {
		result.NormalizeVerdict()
	}
}

// architectureImplementationFileFindings emits one finding per ComponentDef
// whose ImplementationFiles violate the ADR-043 PR 2 invariants:
//   - empty ImplementationFiles → architecture.component_missing_implementation_files
//   - docs-only ImplementationFiles → architecture.component_implementation_files_doc_only
//
// Components with an empty Name are skipped here — a separate architecture
// validator covers unnamed components and surfacing a "missing files" finding
// on a nameless component would be useless to the regen LLM.
func architectureImplementationFileFindings(components []workflow.ComponentDef) []workflow.PlanReviewFinding {
	var findings []workflow.PlanReviewFinding
	for _, c := range components {
		if c.Name == "" {
			continue
		}
		if len(c.ImplementationFiles) == 0 {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "architecture.component_missing_implementation_files",
				SOPTitle:    "Component declares no implementation files (ADR-043 Move 1)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "architecture",
				TargetID:    c.Name,
				Action:      "add",
				TargetField: fmt.Sprintf("component_boundaries.%s.implementation_files", c.Name),
				TargetValue: "≥1 workspace-relative source-code path",
				Issue:       fmt.Sprintf("Component %q has an empty implementation_files list. Every component must own at least one workspace-relative path — Sarah cannot shard a requirement into a story without knowing which files implement it.", c.Name),
				Suggestion:  fmt.Sprintf("Populate component_boundaries[%q].implementation_files with the source paths this component owns. Source these from plan.scope.create for new components or the existing project tree for modified components.", c.Name),
			})
			continue
		}
		if !hasSourceFile(c.ImplementationFiles) {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "architecture.component_implementation_files_doc_only",
				SOPTitle:    "Component implementation_files contain only documentation (ADR-043 Move 1)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "architecture",
				TargetID:    c.Name,
				Action:      "add",
				TargetField: fmt.Sprintf("component_boundaries.%s.implementation_files", c.Name),
				TargetValue: "at least one source-code file (.go/.java/.ts/.py/.rs/etc.)",
				Issue:       fmt.Sprintf("Component %q has implementation_files %v containing only documentation (*.md, *.txt, README*). A component without source code is a documentation artifact, not a unit of dispatch.", c.Name, c.ImplementationFiles),
				Suggestion:  fmt.Sprintf("Add the source-code files component %q implements. Documentation companion files may remain alongside the source but never alone.", c.Name),
			})
		}
	}
	return findings
}

// componentOverloadedCapabilityFindings flags a ComponentDef that claims more
// independently-testable capabilities than it has SOURCE (non-doc)
// implementation files — it has no distinct implementation surface per
// capability, so a single developer loop will implement one capability and stub
// the rest. This is the inverse of the missing-files / docs-only rules: those
// catch a component with too LITTLE, this catches one asked to do too MUCH.
//
// Heuristic: a component mapping N capabilities needs at least N source files
// (one surface per capability). Fewer source files than capabilities means the
// architect collapsed distinct behavior surfaces behind a facade. The
// 2026-06-13 mavlink-hard MavsdkDriver fingerprint: capabilities
// [mavsdk-bootstrap, mavsdk-telemetry, mavsdk-control] mapped to two source
// files (UnmannedSystem.java + UnmannedConfig.java) → the dev built only
// Position telemetry + Takeoff and stubbed the rest; QA (Murat) caught it but
// only after a full execution + assembly. This rule catches it at plan review.
//
// Single-capability components are never flagged (the common, healthy shape).
func componentOverloadedCapabilityFindings(components []workflow.ComponentDef) []workflow.PlanReviewFinding {
	var findings []workflow.PlanReviewFinding
	for _, c := range components {
		if c.Name == "" {
			continue
		}
		capCount := len(c.Capabilities)
		if capCount < 2 {
			continue
		}
		src := sourceFileCount(c.ImplementationFiles)
		if src >= capCount {
			continue
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "architecture.component_overloaded_capabilities",
			SOPTitle:    "Component maps more capabilities than it has implementation surfaces (ADR-044)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "architecture",
			TargetID:    c.Name,
			Action:      "split",
			TargetField: fmt.Sprintf("component_boundaries.%s", c.Name),
			TargetValue: "one component per independently-testable capability (or ≥1 source file per capability)",
			Issue:       fmt.Sprintf("Component %q maps %d capabilities (%v) but declares only %d source implementation file(s) — it has no distinct implementation surface per capability, so a single dev loop will build one and stub the rest.", c.Name, capCount, c.Capabilities, src),
			Suggestion:  fmt.Sprintf("Split component %q into one component per independently-testable capability, each with its own implementation_files. If the capabilities genuinely share one implementation surface, add a distinct source file per capability so the mapping is honest.", c.Name),
		})
	}
	return findings
}

// capabilityUnresolvedInArchitectureFindings emits one finding per Capability
// declared by Mary's analyst sub-phase that has no implementing component.
// The architect-side mirror of capability.orphan (which flags capabilities
// with no Requirement). After ADR-043, the capability → component mapping
// must be explicit; capabilities that don't appear in any component's
// Capabilities list indicate Winston declared an architecture that pretends
// to cover them without code.
func capabilityUnresolvedInArchitectureFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || plan.Exploration == nil || plan.Architecture == nil {
		return nil
	}

	covered := make(map[string]struct{}, len(plan.Exploration.Capabilities))
	for _, c := range plan.Architecture.ComponentBoundaries {
		for _, capName := range c.Capabilities {
			covered[capName] = struct{}{}
		}
	}

	var findings []workflow.PlanReviewFinding
	for _, cap := range plan.Exploration.Capabilities {
		if _, ok := covered[cap.Name]; ok {
			continue
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "capability.unresolved_in_architecture",
			SOPTitle:    "Capability has no implementing component (ADR-043 Move 1)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "architecture",
			TargetID:    cap.Name,
			Action:      "add",
			TargetField: "component_boundaries[].capabilities",
			TargetValue: fmt.Sprintf("capability %q on ≥1 component", cap.Name),
			Issue:       fmt.Sprintf("Capability %q is declared in Plan.Exploration but no ComponentDef's capabilities list contains it. Winston must map every capability to at least one component.", cap.Name),
			Suggestion:  fmt.Sprintf("Either extend an existing component_boundaries[].capabilities to include %q, or declare a new component that implements it. If the capability cannot be mapped to any component, flag it back to the analyst sub-phase rather than emitting an implementation-less abstract.", cap.Name),
		})
	}
	return findings
}

// upstreamSourceBuildContractFindings flags a source_build UpstreamResolution
// that names a class/interface extension point in its APIs but resolves ZERO
// method/function signatures (ADR-047). Without the method contract the
// developer reverse-engineers it through compile errors — the 2026-06-13
// mavlink-hard fingerprint: OSH ICommandStatus resolved as a bare interface +
// lifecycle string (no getProgress()/getExecutionTime() signatures), and
// gemini-pro burned ~3.5M tokens rediscovering them one compile at a time.
//
// Deterministic floor: the reviewer has no /sources/ access at review time, so
// it cannot verify the method SET is complete against the upstream source — only
// that SOME callable/implementable surface exists. A source_build resolution
// with ≥1 class/interface surface and zero method/function surfaces is therefore
// the detectable incompleteness ("named the type, resolved no methods"). A
// fabricated or partial method set passes this floor but is caught far more
// cheaply at the dev's compile than by the unbounded discovery loop this rule
// prevents — the same cost trade ValidateUpstreamImports already makes.
//
// Scoped to source_build via EffectiveResolutionKind: maven_central resolves
// completely (jar-verified) and unresolved is a first-class honest flag —
// neither trips this rule. Severity error to match the sibling structural rules;
// dial to a warning if it proves noisy in practice.
func upstreamSourceBuildContractFindings(resolutions []workflow.UpstreamResolution) []workflow.PlanReviewFinding {
	var findings []workflow.PlanReviewFinding
	for _, r := range resolutions {
		if r.EffectiveResolutionKind() != workflow.ResolutionKindSourceBuild {
			continue
		}
		var hasType, hasMethod bool
		var types []string
		for _, api := range r.APIs {
			switch api.Kind {
			case "class", "interface":
				hasType = true
				if api.Symbol != "" {
					types = append(types, api.Symbol)
				}
			case "method", "function":
				hasMethod = true
			}
		}
		if !hasType || hasMethod {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.Coordinate
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "architecture.upstream_source_build_incomplete_contract",
			SOPTitle:    "source_build dependency names a type but resolves no method contract (ADR-047)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "architecture",
			TargetID:    name,
			Action:      "add",
			TargetField: fmt.Sprintf("upstream_resolutions.%s.apis", name),
			TargetValue: "≥1 method/function APISurface with a full signature for each member a subclass calls or implements",
			Issue:       fmt.Sprintf("source_build dependency %q names type(s) %v but resolves zero method/function signatures. A subclass or caller needs each method's exact signature; without it the developer reverse-engineers the contract through compile errors (the 2026-06-13 ICommandStatus thrash burned ~3.5M tokens rediscovering getProgress()->int, getExecutionTime()->TimeExtent).", name, types),
			Suggestion:  fmt.Sprintf("Read %q's source at its source_ref (mounted under /sources/ when WITH_EPIC) and add an apis[] entry (kind=method) with a full signature for each constructor and abstract/interface method a subclass must implement, plus any config/parameter types those signatures reference.", name),
		})
	}
	return findings
}

// hasSourceFile reports whether the given workspace-relative paths contain at
// least one source-code file. The architecture-generator pre-publish validator
// and this rule must classify identically — divergence would let an
// architecture that the architect-side validator accepted slip through to
// downstream phases while the reviewer-side rule rejected it (or vice
// versa). Both sides delegate to workflow.IsDocumentationPath.
func hasSourceFile(paths []string) bool {
	return sourceFileCount(paths) > 0
}

// sourceFileCount returns the number of source-code (non-documentation) files
// in the given workspace-relative paths, delegating to the same
// workflow.IsDocumentationPath classifier as hasSourceFile. Used by
// componentOverloadedCapabilityFindings to compare a component's source-surface
// count against the number of capabilities it claims.
func sourceFileCount(paths []string) int {
	n := 0
	for _, p := range paths {
		if !workflow.IsDocumentationPath(p) {
			n++
		}
	}
	return n
}
