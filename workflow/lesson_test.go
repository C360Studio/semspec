package workflow

import (
	"testing"
	"time"
)

func TestRoleLessonCounts_Increment(t *testing.T) {
	var counts RoleLessonCounts
	counts.Increment([]string{"missing_tests", "wrong_pattern"})
	counts.Increment([]string{"missing_tests"})

	if counts.Counts["missing_tests"] != 2 {
		t.Errorf("expected missing_tests=2, got %d", counts.Counts["missing_tests"])
	}
	if counts.Counts["wrong_pattern"] != 1 {
		t.Errorf("expected wrong_pattern=1, got %d", counts.Counts["wrong_pattern"])
	}
}

func TestRoleLessonCounts_ExceedsThreshold(t *testing.T) {
	var counts RoleLessonCounts
	counts.Increment([]string{"missing_tests", "missing_tests", "missing_tests"})

	catID, exceeded := counts.ExceedsThreshold(3)
	if !exceeded {
		t.Fatal("expected threshold to be exceeded")
	}
	if catID != "missing_tests" {
		t.Errorf("expected missing_tests, got %q", catID)
	}

	_, exceeded = counts.ExceedsThreshold(4)
	if exceeded {
		t.Error("expected threshold NOT to be exceeded with threshold=4")
	}
}

func TestFilterLessons_ByRole(t *testing.T) {
	now := time.Now()
	lessons := []Lesson{
		{ID: "1", Role: "developer", Summary: "dev lesson", CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "2", Role: "planner", Summary: "plan lesson", CreatedAt: now.Add(-1 * time.Minute)},
		{ID: "3", Role: "developer", Summary: "dev lesson 2", CreatedAt: now},
	}

	result := FilterLessons(lessons, "developer", nil, 0)
	if len(result) != 2 {
		t.Fatalf("expected 2 developer lessons, got %d", len(result))
	}
	// Most recent first.
	if result[0].ID != "3" {
		t.Errorf("expected most recent first, got ID=%s", result[0].ID)
	}
}

func TestFilterLessons_ByCategory(t *testing.T) {
	lessons := []Lesson{
		{ID: "1", Role: "developer", CategoryIDs: []string{"missing_tests"}, CreatedAt: time.Now()},
		{ID: "2", Role: "planner", CategoryIDs: []string{"sop_violation"}, CreatedAt: time.Now()},
	}

	result := FilterLessons(lessons, "", []string{"sop_violation"}, 0)
	if len(result) != 1 || result[0].ID != "2" {
		t.Errorf("expected 1 lesson matching sop_violation, got %d", len(result))
	}
}

func TestFilterLessons_UniversalIncluded(t *testing.T) {
	lessons := []Lesson{
		{ID: "1", Role: "", CategoryIDs: nil, Summary: "universal", CreatedAt: time.Now()},
		{ID: "2", Role: "developer", Summary: "dev", CreatedAt: time.Now()},
	}

	result := FilterLessons(lessons, "planner", nil, 0)
	if len(result) != 1 || result[0].ID != "1" {
		t.Errorf("expected only universal lesson for planner role, got %d", len(result))
	}
}

func TestFilterLessons_Limit(t *testing.T) {
	now := time.Now()
	lessons := []Lesson{
		{ID: "1", Role: "developer", CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "2", Role: "developer", CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "3", Role: "developer", CreatedAt: now.Add(-1 * time.Minute)},
	}

	result := FilterLessons(lessons, "developer", nil, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 lessons with limit=2, got %d", len(result))
	}
	// Most recent first.
	if result[0].ID != "3" || result[1].ID != "2" {
		t.Errorf("expected [3, 2], got [%s, %s]", result[0].ID, result[1].ID)
	}
}
