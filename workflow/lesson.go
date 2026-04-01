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
	// Values: "reviewer-feedback", "validation-failure", "approved-pattern"
	Source string

	// ScenarioID is which scenario or plan slug produced this lesson.
	ScenarioID string

	// Summary is a 1-2 sentence actionable lesson.
	Summary string

	// CategoryIDs are linked error categories (e.g. "missing_tests", "sop_violation").
	// Used for filtering: only lessons matching the current error patterns are injected.
	CategoryIDs []string

	// Role is the pipeline role this lesson applies to: "planner", "developer", "reviewer", etc.
	// All components sharing a role see the same lessons.
	Role string

	// CreatedAt is when this lesson was recorded.
	CreatedAt time.Time
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
