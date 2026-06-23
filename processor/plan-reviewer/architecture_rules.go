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
//   - architecture.topology_unapproved_build_root: a ComponentDef declares a
//     build/workspace/package manifest that is neither present in the contract's
//     detected topology facts nor explicitly allowed by the root contract scope.
//     This blocks clean-room standalone project shapes before developer
//     execution while still permitting contract-authorized new modules.
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

	// Component-boundary rules need at least one component to check.
	if len(plan.Architecture.ComponentBoundaries) > 0 {
		result.Findings = append(result.Findings,
			architectureImplementationFileFindings(plan.Architecture.ComponentBoundaries)...)
		result.Findings = append(result.Findings,
			componentStubRiskFindings(plan)...)
		result.Findings = append(result.Findings,
			componentCohesionViolationFindings(plan.Architecture)...)
		result.Findings = append(result.Findings,
			componentFileNamespaceFindings(plan.Architecture)...)
		result.Findings = append(result.Findings,
			capabilityUnresolvedInArchitectureFindings(plan)...)
		result.Findings = append(result.Findings,
			scopedFileOwnershipFindings(plan.Scope, plan.Architecture.ComponentBoundaries, len(plan.Stories) > 0)...)
		result.Findings = append(result.Findings,
			topologyContractFindings(plan.Contract, plan.Architecture.ComponentBoundaries)...)
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

// componentFileNamespaceFindings flags a likely file→component MISPLACEMENT
// (#237): a file owned by component A whose path contains a directory segment
// matching a DIFFERENT component B's distinctive name token. This catches the
// STRUCTURAL class of #237 — a file physically placed under another component's
// package namespace — generalizably, from the plan's own component names, with
// no hardcoded domain dictionary.
//
// "Distinctive" = a token carried by exactly ONE component name. Tokens shared
// across components (e.g. "mavsdk"/"semantic" in both mavsdk-semantic-datastreams
// and mavsdk-semantic-controlstreams) are NOT domain-discriminating and are
// ignored, so only a segment that uniquely identifies another component fires.
//
// Warning severity, matching componentCohesionViolationFindings: the reviewer
// sees only DECLARED data and a single cohesive component can legitimately own
// multiple domains (an OSH driver entry class owns telemetry + control), so this
// NUDGES rather than blocks. It deliberately does NOT attempt the SEMANTIC case
// (a control-logic file under a neutral path with no domain keyword, e.g. the
// 2026-06-19 ConstAltitudeLLA.java/PD.java in processing/) — that placement
// judgment is the LLM reviewer's job (planReviewerCompletenessR2 criterion 6a),
// not a deterministic check, because catching it would require a project-specific
// domain dictionary that does not generalize.
func componentFileNamespaceFindings(arch *workflow.ArchitectureDocument) []workflow.PlanReviewFinding {
	if arch == nil || len(arch.ComponentBoundaries) < 2 {
		return nil
	}

	// token → set of component names carrying it (from the component NAME).
	tokenOwners := make(map[string]map[string]struct{})
	for _, c := range arch.ComponentBoundaries {
		if c.Name == "" {
			continue
		}
		for tok := range componentNameTokens(c.Name) {
			if tokenOwners[tok] == nil {
				tokenOwners[tok] = make(map[string]struct{})
			}
			tokenOwners[tok][c.Name] = struct{}{}
		}
	}
	distinctiveOwner := func(tok string) (string, bool) {
		owners := tokenOwners[tok]
		if len(owners) != 1 {
			return "", false
		}
		for n := range owners {
			return n, true
		}
		return "", false
	}

	var findings []workflow.PlanReviewFinding
	for _, c := range arch.ComponentBoundaries {
		if c.Name == "" {
			continue
		}
		ownTokens := componentNameTokens(c.Name)
		for _, f := range c.ImplementationFiles {
			for _, seg := range pathDirSegments(f) {
				if _, isOwn := ownTokens[seg]; isOwn {
					continue // segment matches the owner's own namespace — fine
				}
				owner, ok := distinctiveOwner(seg)
				if !ok || owner == c.Name {
					continue
				}
				findings = append(findings, workflow.PlanReviewFinding{
					SOPID:       "architecture.file_under_foreign_component_namespace",
					SOPTitle:    "Implementation file sits under another component's package namespace (#237)",
					Severity:    "warning",
					Status:      "violation",
					Category:    "structural",
					Phase:       "architecture",
					TargetID:    f,
					Action:      "move",
					TargetField: fmt.Sprintf("component_boundaries[%s].implementation_files", c.Name),
					TargetValue: fmt.Sprintf("%s → %s", f, owner),
					Issue:       fmt.Sprintf("File %q is owned by component %q but its path lies under the %q namespace, which uniquely identifies component %q. It likely belongs to %q (the 2026-06-19 #237 misplacement class). If %q genuinely shares a cohesive entry class with %q's other files, ignore this warning.", f, c.Name, seg, owner, owner, f, c.Name),
					Suggestion:  fmt.Sprintf("Move %q to component %q's implementation_files, or confirm the placement is intentional (cohesive shared entry class). Warning, not a block — the LLM reviewer's placement-coherence criterion (6a) and the dev-review ownership gate are the deeper checks.", f, owner),
				})
				break // one finding per file
			}
		}
	}
	return findings
}

// componentNameTokens splits a component name into distinctive lowercase tokens
// (split on any non-alphanumeric run; tokens shorter than 5 chars are dropped as
// too generic to be a reliable namespace signal). Returned as a set.
func componentNameTokens(name string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(tok) >= 5 {
			out[tok] = struct{}{}
		}
	}
	return out
}

// pathDirSegments returns the lowercase directory segments of a file path
// (excluding the filename itself), each split on any non-alphanumeric run so a
// segment like "control-streams" yields "control"/"streams". Segments shorter
// than 5 chars are dropped to match componentNameTokens.
func pathDirSegments(file string) []string {
	dir := path.Dir(strings.ReplaceAll(file, "\\", "/"))
	if dir == "." || dir == "/" || dir == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(dir, "/") {
		for _, tok := range strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
		}) {
			if len(tok) >= 5 {
				out = append(out, tok)
			}
		}
	}
	return out
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
// must belong to at least one component's implementation_files. Files shared by
// multiple components are intentionally listed on each sharing component so the
// scheduler (workflow.DeriveStoryScheduling) serializes those stories. A file
// owned by NO component is written by every parallel story and produces an
// unmergeable conflict at assembly — the 2026-06-13 mavlink-hard
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
// checkCreate gates the scope.create ownership pass. scope.create is only
// fully reconciled against component implementation_files once Sarah's Stories
// are saved (ensureScopeCreateCoversStories augments scope.create from
// Story.FilesOwned). Before that — at the ADR-051 architecture-review round,
// which runs at architecture_generated before Stories exist — scope.create is
// draft-partial and checking its ownership false-positives (ADR-051: create
// stays at stories/R2; only scope.include is gated early, by the
// architecture-generator's UnownedScopedIncludeFiles check). Callers pass
// len(plan.Stories) > 0 so the create pass runs once and only once the data is
// ready; scope.include is always safe to check.
func scopedFileOwnershipFindings(scope workflow.Scope, components []workflow.ComponentDef, checkCreate bool) []workflow.PlanReviewFinding {
	// Ownership universe: every file any component declares, regardless of the
	// component's Name (an unnamed component is flagged elsewhere, but its files
	// still count as owned — otherwise we'd emit spurious orphan findings).
	owned := make(map[string]struct{})
	for _, c := range components {
		for _, f := range workflow.ExpandFileScopeWithCompanionTests(c.ImplementationFiles) {
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
		if f == "" || !workflow.IsConcreteScopedFile(f) {
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
			TargetValue: f,
			Issue:       fmt.Sprintf("Scoped file %q is a deliverable (scope.%s) but appears in no component's implementation_files. Every file the build writes must have at least one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).", f, origin),
			Suggestion:  fmt.Sprintf("Add %q to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If %q is a read-only reference, move it to scope.do_not_touch instead.", f, f),
		})
	}

	if checkCreate {
		for _, f := range scope.Create {
			consider(f, "create")
		}
	}
	for _, f := range scope.Include {
		consider(f, "include")
	}
	return findings
}

// IsConcreteScopedFile and the well-known-deliverable set moved to the workflow
// package (workflow/file_ownership.go) so these SOP rules and the
// architecture-generator's ADR-051 early ownership gate share ONE concreteness
// definition and the well-known set cannot drift between callers.

// topologyContractFindings rejects architecture component files that introduce
// new build/workspace/package root manifests outside the authoritative contract.
// The detector may be polyglot; this rule only compares paths and known
// manifest shapes, so it stays generic across Java/Gradle, Go, Node, Python,
// Rust, Maven, .NET, PHP, and Ruby workspaces.
func topologyContractFindings(contract *workflow.ContractPacket, components []workflow.ComponentDef) []workflow.PlanReviewFinding {
	if contract == nil || len(contract.TopologyFacts) == 0 {
		return nil
	}

	known := contractTopologyManifestPaths(contract.TopologyFacts)
	allowed := contractExplicitTopologyCreatePaths(contract.Scope)
	var findings []workflow.PlanReviewFinding
	seen := map[string]struct{}{}

	for _, c := range components {
		for _, raw := range c.ImplementationFiles {
			file := workflow.NormalizeFilePath(raw)
			if file == "" || !isTopologyControlledPath(file) {
				continue
			}
			if _, ok := known[file]; ok {
				continue
			}
			if _, ok := allowed[file]; ok {
				continue
			}
			key := c.Name + "\x00" + file
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}

			targetField := "component_boundaries[].implementation_files"
			if c.Name != "" {
				targetField = fmt.Sprintf("component_boundaries.%s.implementation_files", c.Name)
			}
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "architecture.topology_unapproved_build_root",
				SOPTitle:    "Architecture introduces an unapproved build/workspace root",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "architecture",
				TargetID:    file,
				Action:      "remove",
				TargetField: targetField,
				TargetValue: file,
				Issue:       fmt.Sprintf("Component %q declares topology-controlled file %q, but the root contract has no detected topology fact and no scope.create/include entry authorizing that build/workspace/package manifest. This is the standalone clean-room project failure class: the architecture replaces or forks the brownfield build shape instead of integrating with it.", c.Name, file),
				Suggestion:  fmt.Sprintf("Remove %q from %s and integrate through an existing topology fact path, or route an explicit contract amendment/scope.create entry before declaring a new build root.", file, targetField),
				Evidence:    fmt.Sprintf("contract.topology_facts does not contain %q", file),
			})
		}
	}
	return findings
}

func contractTopologyManifestPaths(facts []workflow.TopologyFact) map[string]struct{} {
	paths := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		switch fact.Kind {
		case "build_root", "package_root", "workspace_root":
			if p := workflow.NormalizeFilePath(fact.Path); p != "" {
				paths[p] = struct{}{}
			}
		}
	}
	return paths
}

func contractExplicitTopologyCreatePaths(scope workflow.ContractScopeSnapshot) map[string]struct{} {
	paths := map[string]struct{}{}
	for _, raw := range append(append([]string{}, scope.Create...), scope.Include...) {
		p := workflow.NormalizeFilePath(raw)
		if p == "" || !isTopologyControlledPath(p) {
			continue
		}
		paths[p] = struct{}{}
	}
	return paths
}

func isTopologyControlledPath(p string) bool {
	return workflow.IsTopologyControlledPath(p)
}
