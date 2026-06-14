package planreviewer

import (
	"fmt"
	"path"
	"strings"

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
//   - architecture.component_stub_risk (ADR-049): ComponentDef mapping ≥2
//     capabilities where one or more capability has NO scenario evidence
//     (no requirement whose capability_name matches that has any scenario).
//     A capability with no scenario has no failing test forcing the dev to
//     build it, so one dev loop builds the evidenced capabilities and stubs
//     the rest — the 2026-06-13 stub trap, now caught by EVIDENCE, not file
//     count. Replaces ADR-044's architecture.component_overloaded_capabilities
//     (a file-count proxy that wrongly penalized cohesive drivers — a single
//     entry class legitimately backing several tested capabilities). Fires
//     only when plan.Scenarios is non-empty (it needs the scenario layer, so
//     it is an R2 check; never fires evidence-blind).
//   - architecture.components_share_entry_point (ADR-049, the inverse, WARNING):
//     ≥2 components that integrate into the same integration_target upstream
//     resolution but declare fully disjoint implementation_files. If they
//     implement one framework entry class (a driver/plugin registered into the
//     shared target), each parallel story may independently create that same
//     undeclared entry file and collide at assembly (the 2026-06-14 over-split
//     wedge). Best-effort: the reviewer sees only DECLARED data, so this is a
//     non-blocking nudge — the dev-review ownership gate (ADR-049 move 3) is the
//     hard backstop. Severity warning so it never false-rejects a legitimate
//     architecture (the over-correction ADR-049 exists to prevent).
//   - architecture.upstream_source_build_incomplete_contract: a source_build
//     UpstreamResolution that names a class/interface/type in its APIs but
//     resolves ZERO method/function signatures — the dev then reverse-engineers
//     the method contract through compile errors (ADR-047). The 2026-06-13
//     mavlink-hard ICommandStatus fingerprint. Scoped to source_build (explicit
//     OR a VCS-source-shaped coordinate when the kind is omitted); maven_central
//     (jar-verified) and unresolved (honest flag) never fire.
//
// The component-boundary rules are skipped when plan.Architecture is nil (legacy
// plans without the architecture-generator phase have no components to check),
// when plan.Exploration is nil (no capabilities to cross-reference), or when
// ComponentBoundaries is empty. The upstream-resolution rule is INDEPENDENT of
// ComponentBoundaries (it reads only UpstreamResolutions) and runs whenever
// Architecture is present, so a degenerate "resolutions but no components"
// architecture is still gated.
//
// Side effect: calls result.NormalizeVerdict() so the verdict reflects the
// merged findings ("approved" → "needs_changes" when error findings appear).
func mergeArchitectureFindings(plan *workflow.Plan, result *workflow.PlanReviewResult) {
	if plan == nil || plan.Architecture == nil || result == nil {
		return
	}

	original := len(result.Findings)

	// Upstream-resolution rule is component-independent — it reads only
	// UpstreamResolutions, so it runs even when ComponentBoundaries is empty.
	result.Findings = append(result.Findings,
		upstreamSourceBuildContractFindings(plan.Architecture.UpstreamResolutions)...)

	// Component-boundary rules need at least one component to check.
	if len(plan.Architecture.ComponentBoundaries) > 0 {
		result.Findings = append(result.Findings,
			architectureImplementationFileFindings(plan.Architecture.ComponentBoundaries)...)
		result.Findings = append(result.Findings,
			componentStubRiskFindings(plan)...)
		result.Findings = append(result.Findings,
			componentCohesionViolationFindings(plan.Architecture)...)
		result.Findings = append(result.Findings,
			capabilityUnresolvedInArchitectureFindings(plan)...)
		result.Findings = append(result.Findings,
			scopedFileOwnershipFindings(plan.Scope, plan.Architecture.ComponentBoundaries)...)
	}

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

// componentStubRiskFindings flags a multi-capability ComponentDef where one or
// more of its mapped capabilities has NO scenario evidence — no requirement
// whose capability_name matches that has at least one scenario. A capability
// with no scenario has no failing test forcing the dev to implement it, so a
// single dev loop builds the evidenced capabilities and stubs the rest (the
// 2026-06-13 stub trap).
//
// ADR-049: this REPLACES the ADR-044 file-count proxy
// (architecture.component_overloaded_capabilities, "N capabilities need N
// source files"). That proxy was backwards for framework code — a cohesive
// driver (one AbstractSensorModule entry class) legitimately has FEWER files
// than capabilities, yet the file-count rule rewarded inventing files and
// splitting, which drove the 2026-06-14 over-split wedge. The real signal is
// EVIDENCE: scenarios are the forcing functions of TDD. A cohesive component
// with one file and three TESTED capabilities is fine (each scenario is a
// failing test the dev must satisfy); a component with an UNTESTED capability
// is the facade risk regardless of file count.
//
// Fires only when plan.Scenarios is non-empty — the check needs the scenario
// layer, so it is effectively an R2 check and never fires evidence-blind (e.g.
// at architecture-generation time before scenarios exist). Single-capability
// components are never flagged (the common, healthy shape).
func componentStubRiskFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || plan.Architecture == nil || len(plan.Scenarios) == 0 {
		return nil
	}
	var findings []workflow.PlanReviewFinding
	for _, c := range plan.Architecture.ComponentBoundaries {
		if c.Name == "" || len(c.Capabilities) < 2 {
			continue
		}
		var unevidenced []string
		for _, capName := range c.Capabilities {
			if !capabilityHasScenarioEvidence(plan, capName) {
				unevidenced = append(unevidenced, capName)
			}
		}
		if len(unevidenced) == 0 {
			continue
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "architecture.component_stub_risk",
			SOPTitle:    "Multi-capability component has capabilities with no test evidence (ADR-049)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "architecture",
			TargetID:    c.Name,
			Action:      "add",
			TargetField: fmt.Sprintf("component_boundaries.%s.capabilities", c.Name),
			TargetValue: "a scenario (forcing test) for every capability the component maps",
			Issue:       fmt.Sprintf("Component %q maps %d capabilities but %d of them have no scenario evidence (%v). A capability with no scenario has no failing test forcing the dev to implement it, so a single dev loop builds the evidenced capabilities and stubs the rest (the 2026-06-13 stub trap). A cohesive component with FEWER files than capabilities is fine — the gate is missing EVIDENCE, not file count.", c.Name, len(c.Capabilities), len(unevidenced), unevidenced),
			Suggestion:  fmt.Sprintf("Ensure every capability mapped to %q has at least one scenario (via a requirement whose capability_name matches it), so each becomes a forcing test the dev cannot stub. If a capability belongs to a different component, move it; if it needs no behavior, remove it from this component's capabilities.", c.Name),
		})
	}
	return findings
}

// capabilityHasScenarioEvidence reports whether any requirement that owns the
// given capability (Requirement.CapabilityName) has at least one scenario. This
// is the evidence the stub-risk check requires per capability.
func capabilityHasScenarioEvidence(plan *workflow.Plan, capName string) bool {
	for _, req := range plan.Requirements {
		if req.CapabilityName != capName {
			continue
		}
		if len(plan.ScenariosForRequirement(req.ID)) > 0 {
			return true
		}
	}
	return false
}

// componentCohesionViolationFindings is the inverse of stub-risk: it flags an
// OVER-split where ≥2 components integrate into the same integration_target
// upstream resolution but declare fully disjoint implementation_files. If those
// components implement one framework entry class (a driver/plugin registered
// into the shared target), each parallel story may independently CREATE that
// same undeclared entry file and collide at the terminal assembly merge — the
// 2026-06-14 mavlink-hard wedge (four single-capability components each building
// the canonical MavsdkDriver.java that no component declared).
//
// ADR-049: this is a best-effort, NON-BLOCKING (warning) nudge. The reviewer
// sees only DECLARED data, so it cannot detect the actual collision when the
// architect fabricates disjoint non-canonical paths (exactly what happened on
// 2026-06-14) — the dev-review ownership gate (move 3) is the deterministic hard
// backstop. Warning severity keeps it from false-rejecting a legitimate
// architecture (e.g. several genuinely-separate services that share an
// integration target but really do own disjoint files), which would be the same
// over-correction this ADR exists to prevent.
//
// Scope: only integration_target resolutions fire (Role == "integration_target")
// — a plain runtime/build library shared by many components is the normal case
// and must not warn. The "share an entry file" escape is satisfied when the
// group declares any common path in their implementation_files (which
// DeriveStoryScheduling then serializes), so the architect can resolve the
// warning by declaring the shared entry file OR merging the components.
func componentCohesionViolationFindings(arch *workflow.ArchitectureDocument) []workflow.PlanReviewFinding {
	if arch == nil {
		return nil
	}
	byName := make(map[string]workflow.ComponentDef, len(arch.ComponentBoundaries))
	for _, c := range arch.ComponentBoundaries {
		if c.Name != "" {
			byName[c.Name] = c
		}
	}

	var findings []workflow.PlanReviewFinding
	for _, r := range arch.UpstreamResolutions {
		if !strings.EqualFold(strings.TrimSpace(r.Role), "integration_target") {
			continue
		}
		group := integratingComponents(r, arch.ComponentBoundaries, byName)
		if len(group) < 2 {
			continue
		}
		if componentsShareAFile(group) {
			continue // a shared entry file is declared → scheduler serializes → fine
		}
		names := componentNames(group)
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "architecture.components_share_entry_point",
			SOPTitle:    "Components integrate into one target but declare no shared entry file (ADR-049)",
			Severity:    "warning",
			Status:      "violation",
			Category:    "structural",
			Phase:       "architecture",
			TargetID:    r.Name,
			Action:      "review",
			TargetField: "component_boundaries[].implementation_files",
			TargetValue: "merge the components, or declare the shared framework entry file on each component's implementation_files",
			Issue:       fmt.Sprintf("Components %v all integrate into %q (an integration_target) but declare fully disjoint implementation_files. If they implement one framework entry class registered into %q, each parallel story may independently create that same undeclared entry file and collide at assembly (the 2026-06-14 over-split wedge).", names, r.Name, r.Name),
			Suggestion:  fmt.Sprintf("If these are facets of one cohesive unit, merge them into a single component so one Story builds the shared entry class. If they are genuinely separate but share an entry file, declare that file in each component's implementation_files so DeriveStoryScheduling serializes the sharing stories. This is a warning, not a block — the dev-review ownership gate (ADR-049 move 3) is the hard backstop if the collision actually occurs."),
		})
	}
	return findings
}

// integratingComponents returns the components that integrate into resolution r,
// via either r.UsedBy (resolution → component) or ComponentDef.UpstreamRefs
// (component → resolution) — the bidirectional link. Components with no declared
// implementation files are excluded (they are flagged by the missing-files rule;
// counting them here would produce redundant noise).
func integratingComponents(r workflow.UpstreamResolution, all []workflow.ComponentDef, byName map[string]workflow.ComponentDef) []workflow.ComponentDef {
	seen := make(map[string]struct{})
	var group []workflow.ComponentDef
	add := func(c workflow.ComponentDef) {
		if c.Name == "" || len(c.ImplementationFiles) == 0 {
			return
		}
		if _, dup := seen[c.Name]; dup {
			return
		}
		seen[c.Name] = struct{}{}
		group = append(group, c)
	}
	for _, name := range r.UsedBy {
		if c, ok := byName[name]; ok {
			add(c)
		}
	}
	for _, c := range all {
		for _, ref := range c.UpstreamRefs {
			if ref == r.Name {
				add(c)
			}
		}
	}
	return group
}

// componentsShareAFile reports whether any normalized implementation-file path
// is declared by two or more components in the group — the "declared shared
// ownership" that satisfies the cohesion check (the scheduler serializes such
// stories).
func componentsShareAFile(group []workflow.ComponentDef) bool {
	count := make(map[string]int)
	for _, c := range group {
		fileSeen := make(map[string]struct{})
		for _, f := range workflow.NormalizeFilePaths(c.ImplementationFiles) {
			if _, dup := fileSeen[f]; dup {
				continue
			}
			fileSeen[f] = struct{}{}
			count[f]++
			if count[f] >= 2 {
				return true
			}
		}
	}
	return false
}

// componentNames returns the names of the components in the group, in order.
func componentNames(group []workflow.ComponentDef) []string {
	out := make([]string, 0, len(group))
	for _, c := range group {
		out = append(out, c.Name)
	}
	return out
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

// isSourceBuildShaped reports whether a resolution should be treated as a
// source_build for the completeness gate. It fires on an EXPLICIT source_build
// kind, OR — because resolution_kind is omitempty and the architect prompt does
// not hard-enforce it — on an omitted/unknown kind whose coordinate is a
// VCS-source reference (github/gitlab/bitbucket / git@ / .git). The latter is
// the exact "github.com/org/repo@tag" shape of the OSH dep this gate targets,
// which EffectiveResolutionKind infers to "unknown" (NOT source_build) when the
// field is absent — so without this widening the gate would silently miss its
// own fingerprint. Maven GAV coordinates infer to maven_central (resolved
// completely) and npm:/pypi: coordinates carry no VCS marker, so neither path
// reaches here.
func isSourceBuildShaped(r workflow.UpstreamResolution) bool {
	switch r.EffectiveResolutionKind() {
	case workflow.ResolutionKindSourceBuild:
		return true
	case "": // unknown / inferred-unverified — accept only the VCS-source shape.
		return looksLikeVCSSource(r.Coordinate)
	default:
		return false
	}
}

// looksLikeVCSSource reports whether a coordinate is a version-control source
// reference rather than a published-registry coordinate. Deliberately narrow:
// it must NOT match Maven GAV, npm:, or pypi: shapes (those are resolved
// completely and are not this gate's concern).
func looksLikeVCSSource(coordinate string) bool {
	c := strings.ToLower(strings.TrimSpace(coordinate))
	for _, marker := range []string{"github.com", "gitlab.com", "bitbucket.org", "git@", ".git"} {
		if strings.Contains(c, marker) {
			return true
		}
	}
	return false
}

// upstreamSourceBuildContractFindings flags a source_build UpstreamResolution
// that names a class/interface/type extension point in its APIs but resolves
// ZERO method/function signatures (ADR-047). Without the method contract the
// developer reverse-engineers it through compile errors — the 2026-06-13
// mavlink-hard fingerprint: OSH ICommandStatus resolved as a bare interface +
// lifecycle string (no getProgress()/getExecutionTime() signatures), and
// gemini-pro burned ~3.5M tokens rediscovering them one compile at a time.
//
// Deterministic floor: the reviewer has no /sources/ access at review time, so
// it cannot verify the method SET is complete against the upstream source — only
// that SOME callable/implementable surface exists. A source_build resolution
// with ≥1 named-type surface (class/interface/type) and zero method/function
// surfaces is therefore the detectable incompleteness ("named the type, resolved
// no methods"). A fabricated or partial method set passes this floor but is
// caught far more cheaply at the dev's compile than by the unbounded discovery
// loop this rule prevents — the same cost trade ValidateUpstreamImports makes.
//
// Kind handling: the named-type set is {class, interface, type} — the shapes a
// subclass/caller must build against; annotation/constant/config_field are
// deliberately excluded (no method contract to resolve). Kind is normalised
// (lower/trim) because the value originates from an LLM.
//
// Accepted over-fire: a CONCRETE class whose constructor lives in its own class
// surface Signature (no separate method entry) will fire even though that
// constructor IS its contract. The reviewer cannot tell "extends, needs abstract
// methods" from "construct + call" without per-symbol extends/implements data,
// so it errs toward firing — the architect adds one method entry or splits the
// surface, cheap, vs. the discovery loop. Severity error to match the sibling
// structural rules; dial to a warning if it proves noisy in practice.
func upstreamSourceBuildContractFindings(resolutions []workflow.UpstreamResolution) []workflow.PlanReviewFinding {
	var findings []workflow.PlanReviewFinding
	for _, r := range resolutions {
		if !isSourceBuildShaped(r) {
			continue
		}
		var hasType, hasMethod bool
		var types []string
		for _, api := range r.APIs {
			switch strings.ToLower(strings.TrimSpace(api.Kind)) {
			case "class", "interface", "type":
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
// workflow.IsDocumentationPath classifier as hasSourceFile (which is its only
// caller — the ADR-044 file-count overload rule that also used it was retired by
// ADR-049 in favour of the evidence-based componentStubRiskFindings).
func sourceFileCount(paths []string) int {
	n := 0
	for _, p := range paths {
		if !workflow.IsDocumentationPath(p) {
			n++
		}
	}
	return n
}

// scopedFileOwnershipFindings emits one finding per scoped DELIVERABLE file that
// no component owns (issue #175). The invariant: every file the build will write
// must belong to exactly one component's implementation_files, so the scheduler
// (workflow.DeriveStoryScheduling) serializes the stories that share an owned
// file. A file owned by NO component is written by every parallel story and
// produces an unmergeable conflict at assembly — the 2026-06-13 mavlink-hard
// README wedge: README.md was a deliverable (scope.include) but appeared in no
// component's implementation_files, so all four parallel stories wrote it and
// assembleRequirementBranches conflicted.
//
// This is deliberately NOT docs-specific (a doc and a source file are treated
// identically — both must be owned). It is the architecture-side mirror of the
// developer-loop containment gate (structural-validator), which rejects a story
// that MODIFIES an unowned file. Prevention here; containment there.
//
// Set S of scoped deliverables (the files that must be owned):
//   - every concrete file in scope.create (creation intent ⇒ a deliverable), and
//   - every concrete file in scope.include that is NOT in scope.do_not_touch
//     (an in-scope existing file the plan will modify; do_not_touch marks the
//     read-only references, which are excluded).
//
// "Concrete file" = a literal path with a file extension and no glob meta — this
// excludes directory entries ("src/") and patterns ("src/**/*.java"), which a
// component cannot own as a single path and which would otherwise false-positive.
//
// Composes with architecture.component_implementation_files_doc_only: a README
// cannot satisfy this rule by becoming its own docs-only component (that rule
// rejects it) — it must ride as a companion file on the source component that
// produces it (single owner ⇒ single writer), or on several source components
// (⇒ the scheduler serializes them). Either outcome closes the wedge.
func scopedFileOwnershipFindings(scope workflow.Scope, components []workflow.ComponentDef) []workflow.PlanReviewFinding {
	// Ownership universe: every file any component declares, regardless of the
	// component's Name (an unnamed component is flagged elsewhere, but its files
	// still count as owned — otherwise we'd emit spurious orphan findings).
	owned := make(map[string]struct{})
	for _, c := range components {
		for _, f := range workflow.NormalizeFilePaths(c.ImplementationFiles) {
			owned[f] = struct{}{}
		}
	}

	doNotTouch := make(map[string]struct{})
	for _, f := range workflow.NormalizeFilePaths(scope.DoNotTouch) {
		doNotTouch[f] = struct{}{}
	}

	// Build S in deterministic order (create first, then include), recording the
	// origin scope list for the finding message. First-seen origin wins so a file
	// listed in both create and include is reported once as "create".
	seen := make(map[string]struct{})
	var findings []workflow.PlanReviewFinding

	consider := func(raw, origin string) {
		f := workflow.NormalizeFilePath(raw)
		if f == "" || !isConcreteScopedFile(f) {
			return
		}
		if _, dup := seen[f]; dup {
			return
		}
		seen[f] = struct{}{}
		if _, protected := doNotTouch[f]; protected {
			return
		}
		if _, ok := owned[f]; ok {
			return
		}
		findings = append(findings, workflow.PlanReviewFinding{
			SOPID:       "architecture.scoped_file_unowned",
			SOPTitle:    "Scoped deliverable file has no owning component (issue #175)",
			Severity:    "error",
			Status:      "violation",
			Category:    "structural",
			Phase:       "architecture",
			TargetID:    f,
			Action:      "add",
			TargetField: "component_boundaries[].implementation_files",
			TargetValue: fmt.Sprintf("path %q on exactly one component's implementation_files", f),
			Issue:       fmt.Sprintf("Scoped file %q is a deliverable (scope.%s) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).", f, origin),
			Suggestion:  fmt.Sprintf("Add %q to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If %q is a read-only reference, move it to scope.do_not_touch instead.", f, f),
		})
	}

	for _, f := range scope.Create {
		consider(f, "create")
	}
	for _, f := range scope.Include {
		consider(f, "include")
	}
	return findings
}

// wellKnownExtensionlessDeliverables are root-level deliverable files that carry
// no extension but are real owned artifacts a parallel story can collide on the
// same way README did. Without this set isConcreteScopedFile would exempt them
// (path.Ext == "") and Gate 1 would miss a Dockerfile/Makefile co-write.
var wellKnownExtensionlessDeliverables = map[string]bool{
	"Dockerfile":  true,
	"Makefile":    true,
	"Jenkinsfile": true,
	"Vagrantfile": true,
	"Procfile":    true,
}

// isConcreteScopedFile reports whether a normalized scoped path is a single
// literal file (not a directory entry or glob). It must either carry a file
// extension or be a well-known extensionless deliverable, and contain no glob
// metacharacters. Directory entries ("src") and patterns ("src/**/*.java")
// return false — a component owns concrete files, not dirs or patterns, so
// requiring those to be "owned" would false-positive.
func isConcreteScopedFile(p string) bool {
	if strings.ContainsAny(p, "*?[") {
		return false
	}
	if path.Ext(p) != "" {
		return true
	}
	return wellKnownExtensionlessDeliverables[path.Base(p)]
}
