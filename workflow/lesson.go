package workflow

import (
	"sort"
	"time"
)

// Lesson is a project-level learning extracted from a reviewer rejection.
// Lessons are scoped to a pipeline role (planner, developer, reviewer, etc.)
// and injected into future prompts for that role via the fragment assembler.
type Lesson struct {
	// ID is a UUID for this lesson.
	ID string

	// Source identifies where this lesson came from.
	// Values: "reviewer-feedback", "structural-validation", "plan-review", "decomposer".
	Source string

	// ScenarioID is which scenario or plan slug produced this lesson.
	ScenarioID string

	// Summary is a 1-2 sentence actionable lesson.
	Summary string

	// CategoryIDs are linked error categories (e.g. "missing_tests", "sop_violation").
	// Used for filtering: only lessons matching the current error patterns are injected.
	CategoryIDs []string

	// Role is the proximate pipeline role this lesson applies to (where the
	// failure surfaced): "planner", "developer", "reviewer", etc. All
	// components sharing a role see the same lessons.
	Role string

	// CreatedAt is when this lesson was recorded.
	CreatedAt time.Time

	// --- ADR-033 Phase 1+ fields. Empty/zero for legacy lessons. ---

	// Detail is a long-form root-cause narrative. Used for audit and human
	// review only — never injected verbatim into prompts. May be 1KB+.
	// Populated by the decomposer (Phase 2+).
	Detail string

	// InjectionForm is the compressed case-study text rendered into prompts.
	// Hard-capped at ~80 tokens by the decomposer. When empty, the
	// team-knowledge fragment falls back to Summary (Phase 4 wires this).
	InjectionForm string

	// EvidenceSteps cites trajectory steps that captured the failure,
	// making the lesson auditable. Required by the writer in Phase 3+;
	// warn-only in Phase 1.
	EvidenceSteps []StepRef

	// EvidenceFiles cites file regions where the failure manifested.
	// Used by the retirement sweep (Phase 5) to expire lessons whose
	// cited code has been rewritten or deleted.
	EvidenceFiles []FileRef

	// RootCauseRole identifies the role responsible for the upstream
	// defect, which may differ from Role. Cross-role attribution lands
	// with the decomposer (Phase 2+); empty for legacy lessons.
	RootCauseRole string

	// Positive marks best-practice lessons emitted from approved-on-first-try
	// trajectories with rating >= 4. Renders as "BEST PRACTICE" rather than
	// "AVOID" in the team-knowledge fragment (Phase 6).
	Positive bool

	// RetiredAt marks a lesson as no longer eligible for injection. Set by
	// the retirement sweep (Phase 5) when evidence files are deleted/
	// rewritten or when the lesson has not been injected in N weeks.
	// Nil = active.
	RetiredAt *time.Time

	// LastInjectedAt is updated each time the lesson is rendered into a
	// prompt. Used for low-relevance pruning by the retirement sweep.
	LastInjectedAt *time.Time
}

// StepRef points to a single step in an agentic-loop trajectory. Used in
// Lesson.EvidenceSteps to make a lesson auditable — a reviewer can click
// through to the cited step in the trajectory viewer.
type StepRef struct {
	LoopID    string `json:"loop_id"`
	StepIndex int    `json:"step_index"`
}

// FileRef points to a region of a file at a specific commit. Used in
// Lesson.EvidenceFiles so the retirement sweep can expire lessons whose
// cited code has moved or disappeared. Zero LineStart/LineEnd means the
// whole file is cited.
type FileRef struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
}

// RoleLessonCounts tracks per-category error occurrence counts for a single role.
// Used for threshold-based notification: when any category count exceeds the
// configured threshold, a warning is emitted.
type RoleLessonCounts struct {
	// Counts maps error category IDs to their occurrence count.
	Counts map[ErrorCategory]int `json:"counts"`
}

// Increment adds 1 to each of the given category IDs. The Counts map is
// initialised lazily on first call.
func (c *RoleLessonCounts) Increment(categoryIDs []string) {
	if c.Counts == nil {
		c.Counts = make(map[ErrorCategory]int, len(categoryIDs))
	}
	for _, id := range categoryIDs {
		c.Counts[id]++
	}
}

// ExceedsThreshold returns the first category ID whose count meets or exceeds
// the threshold. Returns ("", false) if no category exceeds the threshold.
func (c *RoleLessonCounts) ExceedsThreshold(threshold int) (string, bool) {
	for id, count := range c.Counts {
		if count >= threshold {
			return id, true
		}
	}
	return "", false
}

// FilterLessons returns lessons matching the given role OR any of the given
// categories, up to limit entries. Lessons with an empty Role and empty
// CategoryIDs are universal and always included. Results are ordered most
// recent first. If limit is zero or negative, all matching lessons are returned.
func FilterLessons(lessons []Lesson, role string, categories []string, limit int) []Lesson {
	categorySet := make(map[string]bool, len(categories))
	for _, c := range categories {
		categorySet[c] = true
	}

	var matched []Lesson
	for _, lesson := range lessons {
		if matchesLesson(lesson, role, categorySet) {
			matched = append(matched, lesson)
		}
	}

	// Most recent first.
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	if limit > 0 && len(matched) > limit {
		return matched[:limit]
	}
	return matched
}

// matchesLesson returns true if the lesson should be included for the given
// role and category set. Universal lessons (no role and no categories) always
// match. Otherwise the lesson matches if its Role equals the requested role
// OR if any of its CategoryIDs appear in the category set.
func matchesLesson(lesson Lesson, role string, categorySet map[string]bool) bool {
	isUniversal := lesson.Role == "" && len(lesson.CategoryIDs) == 0
	if isUniversal {
		return true
	}

	if role != "" && lesson.Role == role {
		return true
	}

	for _, id := range lesson.CategoryIDs {
		if categorySet[id] {
			return true
		}
	}

	return false
}
