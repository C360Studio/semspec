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
//   - story.unresolved_components: Story.Components entry that doesn't
//     match any ComponentDef.Name in the architecture.
//   - story.requirement_orphan: Story.RequirementID that doesn't match any
//     Requirement.ID in the plan.
//   - story.depends_on_orphan: Story.DependsOn entry that doesn't match
//     any other Story.ID in the plan.
//   - story.depends_on_cycle: DAG cycle in the cross-story DependsOn graph.
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

		// Component resolution — always checked.
		for _, comp := range s.Components {
			if _, ok := componentNames[comp]; !ok {
				findings = append(findings, workflow.PlanReviewFinding{
					SOPID:       "story.unresolved_components",
					SOPTitle:    "Story references a component not declared in the architecture (ADR-043 Move 2)",
					Severity:    "error",
					Status:      "violation",
					Category:    "structural",
					Phase:       "stories",
					TargetID:    s.ID,
					Action:      "rename",
					TargetField: fmt.Sprintf("story.%s.components", s.ID),
					TargetValue: fmt.Sprintf("%s → <one of architecture.component_boundaries[].name>", comp),
					Issue:       fmt.Sprintf("Story %s lists component %q but no ComponentDef with that name exists in the architecture.", s.ID, comp),
					Suggestion:  "Either rename the Story's components entry to one of the declared component names, or flag a missing component back to the architect.",
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
