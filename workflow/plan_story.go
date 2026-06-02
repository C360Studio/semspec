package workflow

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for ADR-043 Story and Task validation. Callers wrap with
// %w so plan-reviewer and recovery-agent can route on errors.Is, mirroring
// the ErrInvalidRequirementDAG / ErrInvalidFileOwnership pattern from
// plan_requirement.go.
var (
	ErrInvalidStoryDAG       = errors.New("invalid story DAG")
	ErrInvalidStoryStructure = errors.New("invalid story structure")
	ErrInvalidTaskDAG        = errors.New("invalid task DAG")
	ErrInvalidTaskStructure  = errors.New("invalid task structure")
)

// ValidateStoryDAG validates that DependsOn references within the provided
// stories form a valid directed acyclic graph. Mirrors
// ValidateRequirementDAG's three-color DFS shape.
func ValidateStoryDAG(stories []Story) error {
	idIndex := make(map[string]struct{}, len(stories))
	for _, s := range stories {
		idIndex[s.ID] = struct{}{}
	}

	for _, s := range stories {
		for _, dep := range s.DependsOn {
			if dep == s.ID {
				return fmt.Errorf("%w: story %q depends on itself", ErrInvalidStoryDAG, s.ID)
			}
			if _, ok := idIndex[dep]; !ok {
				return fmt.Errorf("%w: story %q depends on unknown story %q", ErrInvalidStoryDAG, s.ID, dep)
			}
		}
	}

	adj := make(map[string][]string, len(stories))
	for _, s := range stories {
		adj[s.ID] = s.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(stories))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("%w: cycle detected — story %q and story %q are in a cycle", ErrInvalidStoryDAG, id, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, s := range stories {
		if color[s.ID] == white {
			if err := visit(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateStory checks structural invariants for a single Story:
//   - ID and RequirementID and Title non-empty
//   - FilesOwned non-empty when Sarah has signed off (Status empty OR != pending)
//   - FilesOwned has at least one source-code file when sign-off requires
//     it (docs-only is rejected by Sarah's readiness gate)
//   - At least one Task present when Sarah has signed off
//
// Empty Status is Sarah's emission shape — `omitempty` on the wire elides
// the field after she's signed off (b7r50o9ov 2026-05-08). The readiness
// invariants apply to empty AND non-pending statuses; only StoryStatusPending
// (Sarah explicitly mid-flight) is exempt. Pre-fix the empty-Status branch
// ALSO bypassed the gate, which meant Sarah's primary readiness layer was
// silently disabled — every defect rode through to plan-reviewer R3.
//
// Plan-reviewer R3 rules (story.missing_files_owned, story.docs_only_files_owned,
// task.missing_within_story) remain the defensive backstop layer. Now Sarah's
// readiness gate actually fires first, matching the doc contract at
// workflow/story_task.go:88. Closes go-reviewer Pass-3 S-C1 / Pass-4 P4-C4.
func ValidateStory(s Story) error {
	if s.ID == "" {
		return fmt.Errorf("%w: story missing ID", ErrInvalidStoryStructure)
	}
	if s.RequirementID == "" {
		return fmt.Errorf("%w: story %q missing requirement_id", ErrInvalidStoryStructure, s.ID)
	}
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf("%w: story %q missing title", ErrInvalidStoryStructure, s.ID)
	}

	// Pending stories are in-flight by Sarah and may not yet have files /
	// tasks populated. The readiness invariants apply on every other shape,
	// including the empty-Status emission Sarah produces post-sign-off.
	if s.Status == StoryStatusPending {
		return nil
	}

	if len(s.FilesOwned) == 0 {
		return fmt.Errorf("%w: story %q has empty files_owned — readiness gate requires at least one workspace-relative path",
			ErrInvalidStoryStructure, s.ID)
	}
	if !hasSourceFile(s.FilesOwned) {
		return fmt.Errorf("%w: story %q files_owned %v contains only documentation files — readiness gate requires at least one source-code file",
			ErrInvalidStoryStructure, s.ID, s.FilesOwned)
	}
	if len(s.Tasks) == 0 {
		return fmt.Errorf("%w: story %q has empty tasks — readiness gate requires at least one task",
			ErrInvalidStoryStructure, s.ID)
	}
	return nil
}

// hasSourceFile reports whether the given paths contain at least one
// source-code file (anything not matched by IsDocumentationPath). Empty
// slice returns false. Used by Sarah's readiness gate logic + plan-reviewer
// story.docs_only_files_owned rule.
func hasSourceFile(paths []string) bool {
	for _, p := range paths {
		if !IsDocumentationPath(p) {
			return true
		}
	}
	return false
}

// ValidateStories runs ValidateStory on each entry, ValidateStoryDAG across
// the set, and additionally validates intra-Story task DAGs. Mode 1 (per-Story
// structural invariants) runs first so callers see the most specific failure;
// Mode 2 (cross-Story DAG) runs second; Mode 3 (per-Story task DAG) runs last.
//
// Plan-reviewer R3 invokes this on the preparing_stories → ready_for_execution
// boundary; story-preparer (Sarah) invokes it inside her readiness gate.
func ValidateStories(stories []Story) error {
	for _, s := range stories {
		if err := ValidateStory(s); err != nil {
			return err
		}
	}
	if err := ValidateStoryDAG(stories); err != nil {
		return err
	}
	for _, s := range stories {
		if err := ValidateTaskDAG(s.ID, s.Tasks); err != nil {
			return err
		}
		for _, t := range s.Tasks {
			if err := ValidateTask(s.ID, t); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateTask checks structural invariants for a single Task:
//   - ID non-empty
//   - StoryID matches the provided parent story ID
//   - Description non-empty
//
// Per-story Task ordering (intra-story DependsOn) is validated by
// ValidateTaskDAG.
func ValidateTask(parentStoryID string, t Task) error {
	if t.ID == "" {
		return fmt.Errorf("%w: task in story %q missing ID", ErrInvalidTaskStructure, parentStoryID)
	}
	if t.StoryID != parentStoryID {
		return fmt.Errorf("%w: task %q claims story_id %q but is nested under story %q",
			ErrInvalidTaskStructure, t.ID, t.StoryID, parentStoryID)
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("%w: task %q missing description", ErrInvalidTaskStructure, t.ID)
	}
	return nil
}

// ValidateTaskDAG validates that DependsOn references within a single
// Story's Tasks form a valid DAG. parentStoryID is included in errors so
// the caller knows which Story tripped the validation.
func ValidateTaskDAG(parentStoryID string, tasks []Task) error {
	idIndex := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		idIndex[t.ID] = struct{}{}
	}

	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if dep == t.ID {
				return fmt.Errorf("%w: task %q in story %q depends on itself", ErrInvalidTaskDAG, t.ID, parentStoryID)
			}
			if _, ok := idIndex[dep]; !ok {
				return fmt.Errorf("%w: task %q in story %q depends on unknown task %q", ErrInvalidTaskDAG, t.ID, parentStoryID, dep)
			}
		}
	}

	adj := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		adj[t.ID] = t.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(tasks))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("%w: cycle detected in story %q — task %q and task %q are in a cycle",
					ErrInvalidTaskDAG, parentStoryID, id, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, t := range tasks {
		if color[t.ID] == white {
			if err := visit(t.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateCapabilityCoverage checks that every Capability declared in the
// exploration is implemented by at least one ComponentDef whose Capabilities
// list contains the capability's Name (ADR-043 Move 1, plan-reviewer R2
// rule capability.unresolved_in_architecture). Empty exploration or empty
// components slice is treated as "nothing to validate yet" — pre-PR 2
// plans and back-compat reads pass through clean. The check fires once the
// exploration is populated AND the architecture has components.
//
// Returns ErrInvalidStoryStructure wrapped with the first unresolved
// capability name. The architecture-generator surfaces this back to Winston
// via retry-feedback so the LLM can extend the offending component or add
// a new one in the next cycle.
func ValidateCapabilityCoverage(exp *Exploration, components []ComponentDef) error {
	if exp == nil || len(exp.Capabilities) == 0 || len(components) == 0 {
		return nil
	}
	covered := make(map[string]struct{}, len(exp.Capabilities))
	for _, c := range components {
		for _, capName := range c.Capabilities {
			covered[capName] = struct{}{}
		}
	}
	for _, cap := range exp.Capabilities {
		if _, ok := covered[cap.Name]; !ok {
			return fmt.Errorf("%w: capability %q has no component whose capabilities list contains it — every capability declared by the analyst must be implemented by at least one component",
				ErrInvalidStoryStructure, cap.Name)
		}
	}
	return nil
}

// ValidateComponentImplementationFiles checks that every ComponentDef in
// the architecture document declares at least one ImplementationFiles entry,
// that at least one of those entries is a source-code file (docs-only
// rejected), AND that every component declares at least one capability
// (the per-component cardinality check; cross-capability coverage is
// ValidateCapabilityCoverage's job). Skips ComponentDefs with empty Name
// (downstream architecture validators flag those separately).
//
// Mirrors the docs-only rule for requirements
// (plan_capability.FindDocsOnlyCapabilities) at the architect-side layer
// (ADR-043 Move 6 — plan-reviewer R2 rules
// architecture.component_missing_implementation_files and
// architecture.component_implementation_files_doc_only).
//
// Returns nil when components are nil/empty so plans that pre-date ADR-043
// PR 2's Winston-extension schema enforcement still validate clean — PR 2
// adds the schema-required guard so post-PR-2 plans always have populated
// fields. The schema cannot enforce minItems (OpenAI strict-mode subset
// excludes it), so the min-1 check lives here.
func ValidateComponentImplementationFiles(components []ComponentDef) error {
	for _, c := range components {
		if c.Name == "" {
			continue
		}
		if len(c.ImplementationFiles) == 0 {
			return fmt.Errorf("%w: component %q has empty implementation_files — every component must own at least one workspace-relative source path",
				ErrInvalidStoryStructure, c.Name)
		}
		if !hasSourceFile(c.ImplementationFiles) {
			return fmt.Errorf("%w: component %q implementation_files %v contains only documentation files — every component must own at least one source-code file",
				ErrInvalidStoryStructure, c.Name, c.ImplementationFiles)
		}
		if len(c.Capabilities) == 0 {
			return fmt.Errorf("%w: component %q has empty capabilities — every component must implement at least one capability from plan.exploration.capabilities[]",
				ErrInvalidStoryStructure, c.Name)
		}
	}
	return nil
}
