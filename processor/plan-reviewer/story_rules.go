package planreviewer

import (
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// mergeStoryFindings runs the ADR-043 PR 3 story-round (R3) structural
// rules over a plan + review result and appends any deterministic findings.
// These rules layer on top of workflow.ValidateStories which story-preparer
// runs at parse time as Sarah's readiness gate — the rules here are a
// defensive backstop for the case where an operator-edited plan or a
// future relaxed validator lets a malformed story set reach review.
//
// Rules (R3 story round):
//   - story.missing_files_owned: Story with empty FilesOwned.
//   - story.docs_only_files_owned: Story whose FilesOwned contains only
//     documentation files. The story-preparer-side mirror of
//     architecture.component_implementation_files_doc_only and
//     capability.orphan.docs_only.
//   - story.unresolved_component: Story.ComponentName doesn't match any
//     ComponentDef.Name in the architecture (ADR-044 anchor).
//   - story.requirement_orphan: Story.RequirementIDs entry that doesn't
//     match any Requirement.ID in the plan (ADR-044 M:N coverage).
//   - story.depends_on_orphan: Story.DependsOn entry that doesn't match
//     any other Story.ID in the plan.
//   - story.depends_on_cycle: DAG cycle in the cross-story DependsOn graph.
//   - story.files_owned_outside_component: Story.FilesOwned contains a file not
//     owned by the Story's selected architecture component or deterministic
//     companion-test expansion.
//   - story.topology_unapproved_build_root: Story.FilesOwned contains a
//     build/workspace/package manifest not authorized by the contract topology.
//   - contract.scope_missing: Root contract scope deliverable is absent from
//     current plan scope without an accepted contract-changing amendment.
//   - story.contract_scope_uncovered: Root contract scope deliverable is not
//     owned by any Story without an accepted contract-changing amendment.
//   - task.missing_within_story: Story with empty Tasks list.
//   - task.depends_on_cycle: DAG cycle in a Story's intra-story Tasks
//     DependsOn graph.
//
// Skipped entirely when plan.Stories is empty (legacy plans without
// story-preparer have no stories to check). Pending-status stories are
// allowed to have empty FilesOwned / Tasks since Sarah's readiness gate
// hasn't signed off yet; the rule fires only once the story status
// indicates sign-off (ready/executing/complete/failed) OR the status is
// empty (a Sarah-emitted story carries empty status until persistence
// — see workflow.Story doc comment for the b7r50o9ov asymmetry rationale).
//
// Side effect: calls result.NormalizeVerdict() so the verdict reflects the
// merged findings ("approved" → "needs_changes" when error findings appear).
func mergeStoryFindings(plan *workflow.Plan, result *workflow.PlanReviewResult) {
	if plan == nil || result == nil {
		return
	}
	if len(plan.Stories) == 0 {
		return
	}

	original := len(result.Findings)
	result.Findings = append(result.Findings, storyStructuralFindings(plan)...)
	result.Findings = append(result.Findings, storyOwnershipFindings(plan)...)
	result.Findings = append(result.Findings, storyContractCoverageFindings(plan)...)
	result.Findings = append(result.Findings, storyDependsOnFindings(plan)...)
	result.Findings = append(result.Findings, taskDependsOnFindings(plan)...)

	if len(result.Findings) > original {
		result.NormalizeVerdict()
	}
}

// storyStructuralFindings emits per-story findings for the structural
// invariants (FilesOwned non-empty + has source file, Tasks non-empty,
// Components resolve, RequirementID resolves).
func storyStructuralFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	componentNames := architectureComponentNames(plan)
	requirementIDs := planRequirementIDs(plan)

	var findings []workflow.PlanReviewFinding
	for _, s := range plan.Stories {
		// RequirementIDs resolution — ADR-044: check every entry in the M:N
		// coverage join. Always checked regardless of story status.
		for _, rid := range s.RequirementIDs {
			if _, ok := requirementIDs[rid]; !ok {
				findings = append(findings, workflow.PlanReviewFinding{
					SOPID:       "story.requirement_orphan",
					SOPTitle:    "Story references a requirement that doesn't exist (ADR-044 M:N coverage)",
					Severity:    "error",
					Status:      "violation",
					Category:    "structural",
					Phase:       "stories",
					TargetID:    s.ID,
					Action:      "rename",
					TargetField: fmt.Sprintf("story.%s.requirement_ids", s.ID),
					TargetValue: fmt.Sprintf("%s → <existing requirement.id>", rid),
					Issue:       fmt.Sprintf("Story %s has requirement_id=%q in requirement_ids but no Requirement with that ID exists in the plan.", s.ID, rid),
					Suggestion:  "Either remove the orphan entry from the Story's requirement_ids, or flag the missing requirement back to the planner step.",
				})
				break // report first orphan per story; re-run after fixing
			}
		}

		// Component resolution — ADR-044: Story anchors to ONE component_name.
		if s.ComponentName != "" {
			if _, ok := componentNames[s.ComponentName]; !ok {
				findings = append(findings, workflow.PlanReviewFinding{
					SOPID:       "story.unresolved_component",
					SOPTitle:    "Story references a component not declared in the architecture (ADR-044 anchor)",
					Severity:    "error",
					Status:      "violation",
					Category:    "structural",
					Phase:       "stories",
					TargetID:    s.ID,
					Action:      "rename",
					TargetField: fmt.Sprintf("story.%s.component_name", s.ID),
					TargetValue: fmt.Sprintf("%s → <one of architecture.component_boundaries[].name>", s.ComponentName),
					Issue:       fmt.Sprintf("Story %s anchors to component_name=%q but no ComponentDef with that name exists in the architecture.", s.ID, s.ComponentName),
					Suggestion:  "Either rename the Story's component_name to one of the declared component names, or flag a missing component back to the architect.",
				})
			}
		}

		// Readiness-gated invariants fire when Sarah's gate should have run.
		// Empty Status is Sarah's omitempty emission shape after sign-off
		// (b7r50o9ov) — workflow.ValidateStory enforces the same invariants
		// at the mutation boundary post Train-D step 1 (Pass-3 S-C1), so
		// by the time R3 runs here those defects are already caught. R3
		// remains the defensive backstop layer: pending stories (Sarah
		// explicitly mid-flight) are exempt, every other shape is checked.
		if s.Status == workflow.StoryStatusPending {
			continue
		}

		if len(s.FilesOwned) == 0 {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "story.missing_files_owned",
				SOPTitle:    "Story has empty files_owned (ADR-043 Move 2)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "stories",
				TargetID:    s.ID,
				Action:      "add",
				TargetField: fmt.Sprintf("story.%s.files_owned", s.ID),
				TargetValue: "union of selected components' implementation_files",
				Issue:       fmt.Sprintf("Story %s has empty files_owned. Sarah's readiness gate requires the union of the selected components' implementation_files.", s.ID),
				Suggestion:  fmt.Sprintf("Populate story.%s.files_owned with the workspace-relative paths owned by the components in story.%s.components. At least one entry must be a source-code file.", s.ID, s.ID),
			})
		} else if !hasSourceFile(s.FilesOwned) {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "story.docs_only_files_owned",
				SOPTitle:    "Story files_owned contains only documentation (ADR-043 Move 2)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "stories",
				TargetID:    s.ID,
				Action:      "add",
				TargetField: fmt.Sprintf("story.%s.files_owned", s.ID),
				TargetValue: "at least one source-code file",
				Issue:       fmt.Sprintf("Story %s files_owned %v contains only documentation (*.md, *.txt, README*). A story without source code is not a unit of dispatch.", s.ID, s.FilesOwned),
				Suggestion:  fmt.Sprintf("Either extend story.%s.components to select components whose implementation_files include source code, or flag the upstream architecture rule (architecture.component_implementation_files_doc_only) that should have caught this.", s.ID),
			})
		}

		if len(s.Tasks) == 0 {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "task.missing_within_story",
				SOPTitle:    "Story has no tasks (ADR-043 Move 2)",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "stories",
				TargetID:    s.ID,
				Action:      "add",
				TargetField: fmt.Sprintf("story.%s.tasks", s.ID),
				TargetValue: "ordered TDD checklist of 3-5 tasks",
				Issue:       fmt.Sprintf("Story %s has no tasks. Sarah authors the TDD DAG at plan time; an empty tasks list means execution has no work to dispatch.", s.ID),
				Suggestion:  fmt.Sprintf("Populate story.%s.tasks with 3-5 ordered tasks (write failing test, implement, integration smoke, verify scenarios).", s.ID),
			})
		}
	}
	return findings
}

// storyOwnershipFindings keeps Sarah's Story ownership derived from the
// selected architecture component and the root contract topology. It rejects
// baseline-erasing "own everything" stories and standalone build-root smuggling
// before scenario-orchestrator can dispatch a developer.
func storyOwnershipFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || len(plan.Stories) == 0 {
		return nil
	}

	components := architectureComponentsByName(plan)
	knownTopology := map[string]struct{}{}
	explicitTopology := map[string]struct{}{}
	hasTopologyContract := plan.Contract != nil && len(plan.Contract.TopologyFacts) > 0
	if hasTopologyContract {
		knownTopology = contractTopologyManifestPaths(plan.Contract.TopologyFacts)
		explicitTopology = contractExplicitTopologyCreatePaths(plan.Contract.Scope)
	}

	var findings []workflow.PlanReviewFinding
	seen := map[string]struct{}{}
	for _, s := range plan.Stories {
		if s.Status == workflow.StoryStatusPending {
			continue
		}

		component, ok := components[s.ComponentName]
		if !ok {
			continue // story.unresolved_component reports this shape.
		}
		componentFiles := componentOwnedFiles(component)
		for _, raw := range s.FilesOwned {
			file := workflow.NormalizeFilePath(raw)
			if file == "" {
				continue
			}

			if hasTopologyContract && isTopologyControlledPath(file) {
				if _, known := knownTopology[file]; !known {
					if _, explicit := explicitTopology[file]; !explicit {
						key := "topology\x00" + s.ID + "\x00" + file
						if _, dup := seen[key]; !dup {
							seen[key] = struct{}{}
							findings = append(findings, workflow.PlanReviewFinding{
								SOPID:       "story.topology_unapproved_build_root",
								SOPTitle:    "Story owns an unapproved build/workspace root",
								Severity:    "error",
								Status:      "violation",
								Category:    "structural",
								Phase:       "stories",
								TargetID:    s.ID,
								Action:      "remove",
								TargetField: fmt.Sprintf("story.%s.files_owned", s.ID),
								TargetValue: file,
								Issue:       fmt.Sprintf("Story %s owns topology-controlled file %q, but the root contract has no detected topology fact and no scope.create/include entry authorizing that build/workspace/package manifest.", s.ID, file),
								Suggestion:  fmt.Sprintf("Remove %q from story.%s.files_owned and route it back through architecture/contract amendment if a new build root is genuinely required.", file, s.ID),
								Evidence:    fmt.Sprintf("contract.topology_facts does not contain %q", file),
							})
						}
					}
				}
			}

			if _, owned := componentFiles[file]; owned {
				continue
			}
			key := "component\x00" + s.ID + "\x00" + file
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "story.files_owned_outside_component",
				SOPTitle:    "Story owns a file outside its selected component",
				Severity:    "error",
				Status:      "violation",
				Category:    "structural",
				Phase:       "stories",
				TargetID:    s.ID,
				Action:      "remove",
				TargetField: fmt.Sprintf("story.%s.files_owned", s.ID),
				TargetValue: file,
				Issue:       fmt.Sprintf("Story %s anchors to component %q but files_owned includes %q, which is not in that component's implementation_files or deterministic companion-test expansion. Sarah must not widen ownership beyond the architecture component; that erases the brownfield/file-ownership partition before execution.", s.ID, s.ComponentName, file),
				Suggestion:  fmt.Sprintf("Remove %q from story.%s.files_owned. If component %q truly owns it, revise architecture.component_boundaries[%q].implementation_files first, then re-prepare stories.", file, s.ID, s.ComponentName, s.ComponentName),
			})
		}
	}
	return findings
}

func componentOwnedFiles(component workflow.ComponentDef) map[string]struct{} {
	files := workflow.ExpandFileScopeWithCompanionTests(component.ImplementationFiles)
	out := make(map[string]struct{}, len(files))
	for _, file := range files {
		if normalized := workflow.NormalizeFilePath(file); normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	return out
}

type contractScopeObligation struct {
	Path   string
	Origin string
}

// storyContractCoverageFindings compares the current mutable plan shape to the
// immutable root contract plus accepted amendments. It catches the "current
// scope got smaller, therefore everything looks complete" failure class before
// execution starts.
func storyContractCoverageFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if plan == nil || plan.Contract == nil || len(plan.Stories) == 0 {
		return nil
	}

	amended := contractChangedAffectedIDs(plan.Contract)
	currentScope := scopedDeliverableSet(plan.Scope.Create, plan.Scope.Include, plan.Scope.DoNotTouch)
	storyCoverage := storyOwnedFileSet(plan.Stories)

	var findings []workflow.PlanReviewFinding
	for _, obligation := range contractScopeDeliverables(plan.Contract.Scope) {
		if contractPathAmended(obligation.Path, amended) {
			continue
		}
		if _, ok := currentScope[obligation.Path]; !ok {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "contract.scope_missing",
				SOPTitle:    "Current plan scope dropped a root contract deliverable",
				Severity:    "error",
				Status:      "violation",
				Category:    "contract",
				Phase:       "stories",
				TargetID:    obligation.Path,
				Action:      "add",
				TargetField: fmt.Sprintf("plan.scope.%s", obligation.Origin),
				TargetValue: obligation.Path,
				Issue:       fmt.Sprintf("Root contract %s includes deliverable %q, but the current plan scope no longer includes it and no accepted contract-changing amendment names that path.", obligation.Origin, obligation.Path),
				Suggestion:  fmt.Sprintf("Restore %q to the current plan scope or accept a PlanDecision whose contract_impact.kind=change and affected_ids names this obligation.", obligation.Path),
				Evidence:    fmt.Sprintf("contract.scope.%s=%q; accepted amendment affected_ids did not authorize dropping it", obligation.Origin, obligation.Path),
			})
		}
		if _, ok := storyCoverage[obligation.Path]; !ok {
			findings = append(findings, workflow.PlanReviewFinding{
				SOPID:       "story.contract_scope_uncovered",
				SOPTitle:    "Story coverage omits a root contract deliverable",
				Severity:    "error",
				Status:      "violation",
				Category:    "contract",
				Phase:       "stories",
				TargetID:    obligation.Path,
				Action:      "add",
				TargetField: "stories[].files_owned",
				TargetValue: obligation.Path,
				Issue:       fmt.Sprintf("Root contract %s includes deliverable %q, but no Story files_owned entry covers it and no accepted contract-changing amendment names that path.", obligation.Origin, obligation.Path),
				Suggestion:  fmt.Sprintf("Assign %q to the Story/component that owns the work, or accept a contract-changing PlanDecision amendment that explicitly removes the obligation.", obligation.Path),
				Evidence:    fmt.Sprintf("story.files_owned union missing %q", obligation.Path),
			})
		}
	}
	return findings
}

func contractScopeDeliverables(scope workflow.ContractScopeSnapshot) []contractScopeObligation {
	protected := map[string]struct{}{}
	for _, p := range workflow.NormalizeFilePaths(scope.DoNotTouch) {
		if p != "" {
			protected[p] = struct{}{}
		}
	}

	seen := map[string]struct{}{}
	var obligations []contractScopeObligation
	add := func(raw, origin string) {
		p := workflow.NormalizeFilePath(raw)
		if p == "" || !workflow.IsConcreteScopedFile(p) {
			return
		}
		if _, ok := protected[p]; ok {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		obligations = append(obligations, contractScopeObligation{Path: p, Origin: origin})
	}
	for _, raw := range scope.Create {
		add(raw, "create")
	}
	for _, raw := range scope.Include {
		add(raw, "include")
	}
	return obligations
}

func scopedDeliverableSet(create, include, doNotTouch []string) map[string]struct{} {
	out := map[string]struct{}{}
	protected := map[string]struct{}{}
	for _, p := range workflow.NormalizeFilePaths(doNotTouch) {
		if p != "" {
			protected[p] = struct{}{}
		}
	}
	add := func(raw string) {
		p := workflow.NormalizeFilePath(raw)
		if p == "" || !workflow.IsConcreteScopedFile(p) {
			return
		}
		if _, ok := protected[p]; ok {
			return
		}
		out[p] = struct{}{}
	}
	for _, raw := range create {
		add(raw)
	}
	for _, raw := range include {
		add(raw)
	}
	return out
}

func storyOwnedFileSet(stories []workflow.Story) map[string]struct{} {
	out := map[string]struct{}{}
	for _, story := range stories {
		if story.Status == workflow.StoryStatusPending {
			continue
		}
		for _, p := range workflow.ExpandFileScopeWithCompanionTests(story.FilesOwned) {
			if p != "" {
				out[p] = struct{}{}
			}
		}
	}
	return out
}

func contractChangedAffectedIDs(contract *workflow.ContractPacket) map[string]struct{} {
	out := map[string]struct{}{}
	if contract == nil {
		return out
	}
	for _, amendment := range contract.Amendments {
		if amendment.Impact.Kind != workflow.ContractImpactChange {
			continue
		}
		for _, id := range amendment.Impact.AffectedIDs {
			addContractAffectedID(out, id)
		}
	}
	return out
}

func addContractAffectedID(out map[string]struct{}, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	out[id] = struct{}{}
	if p := workflow.NormalizeFilePath(id); p != "" {
		out[p] = struct{}{}
	}
	for _, prefix := range []string{
		"scope:",
		"scope.create:",
		"scope.include:",
		"contract.scope:",
		"contract.scope.create:",
		"contract.scope.include:",
	} {
		if strings.HasPrefix(id, prefix) {
			if p := workflow.NormalizeFilePath(strings.TrimPrefix(id, prefix)); p != "" {
				out[p] = struct{}{}
			}
		}
	}
}

func contractPathAmended(path string, amended map[string]struct{}) bool {
	candidates := []string{
		path,
		"scope:" + path,
		"scope.create:" + path,
		"scope.include:" + path,
		"contract.scope:" + path,
		"contract.scope.create:" + path,
		"contract.scope.include:" + path,
	}
	for _, candidate := range candidates {
		if _, ok := amended[candidate]; ok {
			return true
		}
	}
	return false
}

// storyDependsOnFindings emits findings for cross-story DependsOn invariants
// (orphan refs + DAG cycles).
func storyDependsOnFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	if err := workflow.ValidateStoryDAG(plan.Stories); err != nil {
		return dagErrorToFindings(err, "stories",
			workflow.ErrInvalidStoryDAG,
			"story.depends_on_cycle",
			"story.depends_on_orphan",
			"Cross-story DependsOn DAG invalid (ADR-043 Move 2)",
			"depends_on",
		)
	}
	return nil
}

// taskDependsOnFindings emits findings for intra-story Task DependsOn DAG
// validation. One finding per offending Story (the validator short-circuits
// on the first failure within each story).
func taskDependsOnFindings(plan *workflow.Plan) []workflow.PlanReviewFinding {
	var findings []workflow.PlanReviewFinding
	for _, s := range plan.Stories {
		if len(s.Tasks) == 0 {
			continue
		}
		if err := workflow.ValidateTaskDAG(s.ID, s.Tasks); err != nil {
			findings = append(findings, dagErrorToFindings(err, "stories",
				workflow.ErrInvalidTaskDAG,
				"task.depends_on_cycle",
				"task.depends_on_orphan",
				fmt.Sprintf("Intra-story Task DependsOn DAG invalid for story %s (ADR-043 Move 2)", s.ID),
				fmt.Sprintf("story.%s.tasks.depends_on", s.ID),
			)...)
		}
	}
	return findings
}

// dagErrorToFindings converts a DAG-validator sentinel error into one or
// more PlanReviewFindings. The validators return one error at a time
// (DFS short-circuits), so this returns a single-element slice; the slice
// shape lets callers append uniformly with the multi-entity rules.
func dagErrorToFindings(err error, phase string, sentinel error, cycleSOP, orphanSOP, title, targetField string) []workflow.PlanReviewFinding {
	if err == nil {
		return nil
	}
	// The validator wraps a single sentinel; pick the rule based on the
	// message content. cycle / depends on itself → cycle rule; unknown →
	// orphan rule.
	msg := err.Error()
	sop := orphanSOP
	if errors.Is(err, sentinel) && (strings.Contains(msg, "cycle") || strings.Contains(msg, "depends on itself")) {
		sop = cycleSOP
	}
	return []workflow.PlanReviewFinding{{
		SOPID:       sop,
		SOPTitle:    title,
		Severity:    "error",
		Status:      "violation",
		Category:    "structural",
		Phase:       phase,
		Action:      "rename",
		TargetField: targetField,
		Issue:       msg,
		Suggestion:  "Resolve the DAG violation: either drop the offending depends_on edge, rename to a valid sibling ID, or reshape the dependency to break the cycle.",
	}}
}

// architectureComponentNames returns a set of all ComponentDef.Name entries
// declared in the architecture. Empty when the plan has no architecture
// document or empty component_boundaries — callers treat that as
// "no component validation to do."
func architectureComponentNames(plan *workflow.Plan) map[string]struct{} {
	if plan == nil || plan.Architecture == nil {
		return nil
	}
	names := make(map[string]struct{}, len(plan.Architecture.ComponentBoundaries))
	for _, c := range plan.Architecture.ComponentBoundaries {
		if c.Name != "" {
			names[c.Name] = struct{}{}
		}
	}
	return names
}

func architectureComponentsByName(plan *workflow.Plan) map[string]workflow.ComponentDef {
	if plan == nil || plan.Architecture == nil {
		return nil
	}
	components := make(map[string]workflow.ComponentDef, len(plan.Architecture.ComponentBoundaries))
	for _, c := range plan.Architecture.ComponentBoundaries {
		if c.Name != "" {
			components[c.Name] = c
		}
	}
	return components
}

// planRequirementIDs returns a set of all Requirement.ID entries on the plan.
func planRequirementIDs(plan *workflow.Plan) map[string]struct{} {
	if plan == nil {
		return nil
	}
	ids := make(map[string]struct{}, len(plan.Requirements))
	for _, r := range plan.Requirements {
		ids[r.ID] = struct{}{}
	}
	return ids
}
