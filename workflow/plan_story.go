package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors for ADR-043 Story and Task validation. Callers wrap with
// %w so plan-reviewer and recovery-agent can route on errors.Is, mirroring
// the ErrInvalidRequirementDAG / ErrInvalidFileOwnership pattern from
// plan_requirement.go.
var (
	ErrInvalidStoryDAG           = errors.New("invalid story DAG")
	ErrInvalidStoryStructure     = errors.New("invalid story structure")
	ErrInvalidStoryFileOwnership = errors.New("invalid story file ownership")
	ErrInvalidTaskDAG            = errors.New("invalid task DAG")
	ErrInvalidTaskStructure      = errors.New("invalid task structure")

	// ErrSameComponentFileConflict is returned by DeriveStoryScheduling Pass 2
	// when two Stories anchor the same component and share files — an invalid
	// emission shape Sarah must fix by collapsing them into one Story.
	// Maps to the plan-reviewer signal "story.same_component_file_conflict".
	ErrSameComponentFileConflict = errors.New("story.same_component_file_conflict")

	// ErrCoveragePartitionCyclic is returned by DeriveStoryScheduling Pass 3
	// when cycle detection finds a cycle in the derived scheduler DAG — a
	// Story covering non-contiguous layers of the Requirement DAG.
	// Maps to the plan-reviewer signal "coverage_partition_cyclic".
	ErrCoveragePartitionCyclic = errors.New("coverage_partition_cyclic")
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
//   - ID non-empty
//   - ComponentName non-empty (ADR-044: 1:1 component anchor)
//   - RequirementIDs non-empty (ADR-044: M:N coverage — at least one requirement)
//   - Title non-empty
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
// workflow/story_task.go. Closes go-reviewer Pass-3 S-C1 / Pass-4 P4-C4.
func ValidateStory(s Story) error {
	if s.ID == "" {
		return fmt.Errorf("%w: story missing ID", ErrInvalidStoryStructure)
	}
	if len(s.RequirementIDs) == 0 {
		return fmt.Errorf("%w: story %q missing requirement_ids (ADR-044: at least one requirement must be covered)", ErrInvalidStoryStructure, s.ID)
	}
	if s.ComponentName == "" {
		return fmt.Errorf("%w: story %q missing component_name (ADR-044: every story must anchor to one component)", ErrInvalidStoryStructure, s.ID)
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
// the set, ValidateStoryFileOwnership across the set, and additionally
// validates intra-Story task DAGs. Mode 1 (per-Story structural invariants)
// runs first so callers see the most specific failure; Mode 2 (cross-Story
// DAG) runs second; Mode 3 (cross-Story file-ownership / race prevention)
// runs third; Mode 4 (per-Story task DAG) runs last.
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
	if err := ValidateStoryFileOwnership(stories); err != nil {
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

// ValidateStoryFileOwnership ensures that when two Stories share at least one
// file in FilesOwned, the DependsOn DAG sequences one before the other so the
// scenario-orchestrator's parallel dispatch (config max_concurrent=5) does NOT
// race-write the shared files.
//
// ADR-043 Move 4 retired the requirement-level partition validator on the
// premise that file-collision sequencing would move to Story.DependsOn after
// Sarah shards. Smoke 9 (2026-06-02 hybrid-gpt5 mavlink-hard) showed Sarah
// declaring overlapping files_owned across sibling Stories under different
// Requirements without the corresponding DependsOn edges — a latent
// write-race that today is hidden by serial Requirement DAG execution but
// would activate the moment parallel Requirement dispatch lands. This
// validator fills in the explicitly-deferred TODO from plan_requirement.go
// (ValidateFileOwnershipPartition no-op).
//
// The check is symmetric: for any pair (A, B) of distinct Stories sharing at
// least one normalized FilesOwned path, A must be in B's transitive
// DependsOn closure OR vice versa. Self-comparisons skipped. Empty FilesOwned
// short-circuits. Files normalized via NormalizeFilePath so `src/x.go` and
// `./src/x.go` compare equal.
//
// Returns ErrInvalidStoryFileOwnership wrapped with the offending Story IDs
// and the shared file(s). plan-reviewer R3 and Sarah's readiness gate route
// on errors.Is.
func ValidateStoryFileOwnership(stories []Story) error {
	if len(stories) < 2 {
		return nil
	}

	// Build the transitive DependsOn closure for each Story. BFS from each
	// Story over its direct DependsOn edges. O(n²) memory, O(n³) worst-case
	// time — acceptable for plan-level Story counts (typically <20).
	adj := make(map[string][]string, len(stories))
	for _, s := range stories {
		adj[s.ID] = s.DependsOn
	}
	closure := make(map[string]map[string]struct{}, len(stories))
	for _, s := range stories {
		seen := make(map[string]struct{})
		queue := append([]string(nil), s.DependsOn...)
		for len(queue) > 0 {
			head := queue[0]
			queue = queue[1:]
			if _, ok := seen[head]; ok {
				continue
			}
			seen[head] = struct{}{}
			queue = append(queue, adj[head]...)
		}
		closure[s.ID] = seen
	}

	// Pre-normalize each Story's FilesOwned into a set so pair-wise checks
	// don't re-normalize. Empty paths (NormalizeFilePath returns "" for
	// escapes / "..") are dropped.
	normalized := make(map[string]map[string]struct{}, len(stories))
	for _, s := range stories {
		set := make(map[string]struct{}, len(s.FilesOwned))
		for _, f := range s.FilesOwned {
			if n := NormalizeFilePath(f); n != "" {
				set[n] = struct{}{}
			}
		}
		normalized[s.ID] = set
	}

	for i := 0; i < len(stories); i++ {
		a := stories[i]
		for j := i + 1; j < len(stories); j++ {
			b := stories[j]
			shared := sharedFiles(normalized[a.ID], normalized[b.ID])
			if len(shared) == 0 {
				continue
			}
			_, aDependsOnB := closure[a.ID][b.ID]
			_, bDependsOnA := closure[b.ID][a.ID]
			if !aDependsOnB && !bDependsOnA {
				return fmt.Errorf("%w: stories %q and %q share file(s) %v but neither depends on the other (transitively) — would race-write at parallel dispatch; add a depends_on edge from the later Story to the earlier one",
					ErrInvalidStoryFileOwnership, a.ID, b.ID, shared)
			}
		}
	}
	return nil
}

// sharedFiles returns the sorted intersection of two normalized file sets.
// Returns nil when there is no overlap. Sorted output makes test assertions
// deterministic and the error message reproducible.
func sharedFiles(a, b map[string]struct{}) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	// Iterate the smaller set for the membership test.
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	var out []string
	for f := range small {
		if _, ok := large[f]; ok {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
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

// codeSymbolImportKinds are the APISurface kinds the developer imports by a
// fully-qualified reference. message/config_field surfaces are not imported and
// need no import field.
var codeSymbolImportKinds = map[string]bool{
	"class": true, "interface": true, "type": true,
	"function": true, "annotation": true, "constant": true,
}

// ValidateUpstreamImports rejects an upstream resolution APISurface whose
// `import` is a bare, unqualified symbol for a code-symbol kind — a provably
// wrong value the developer cannot actually import (a real import names a
// package: it contains ".", "/", or "::"). This is the deterministic,
// non-gameable backstop for the 2026-06-07 wedge where only the bare symbol
// "System" was resolved and the dev burned 3.4M tokens rediscovering
// io.mavsdk.System via javap.
//
// EMPTY imports are intentionally NOT rejected here: the architect prompt + the
// plan-reviewer rule 7a-c2 own the presence judgment, which a deterministic
// presence gate would Goodhart into fabricated package paths. This check fires
// only when an import IS present but is just the bare symbol again.
func ValidateUpstreamImports(resolutions []UpstreamResolution) error {
	for _, u := range resolutions {
		for _, a := range u.APIs {
			if !codeSymbolImportKinds[a.Kind] {
				continue
			}
			imp := strings.TrimSpace(a.Import)
			if imp == "" {
				continue
			}
			if !strings.ContainsAny(imp, "./") && !strings.Contains(imp, "::") {
				return fmt.Errorf("%w: upstream resolution %q api %q has a bare import %q with no package qualifier — the dev cannot import it; provide the fully-qualified reference (e.g. io.mavsdk.System) verified against the artifact",
					ErrInvalidStoryStructure, u.Name, a.Symbol, imp)
			}
		}
	}
	return nil
}
