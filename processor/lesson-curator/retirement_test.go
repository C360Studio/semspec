package lessoncurator

import (
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestShouldRetire_AlreadyRetiredIsNoop(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{now: now, idleThreshold: 30 * 24 * time.Hour, minAgeBeforeRetire: 7 * 24 * time.Hour}
	// Way past idle threshold but already retired — must skip.
	old := now.Add(-365 * 24 * time.Hour)
	lesson := workflow.Lesson{
		CreatedAt:      old,
		LastInjectedAt: &old,
		RetiredAt:      &now,
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("already-retired lessons must not be retired again")
	}
}

func TestShouldRetire_GracePeriodForNewLessons(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{now: now, idleThreshold: 30 * 24 * time.Hour, minAgeBeforeRetire: 7 * 24 * time.Hour}
	// Created 1 day ago, never injected. Should be safe under grace.
	lesson := workflow.Lesson{CreatedAt: now.Add(-24 * time.Hour)}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("brand-new uninjected lesson must not be retired during grace period")
	}
}

func TestShouldRetire_NeverInjectedPastGrace(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{now: now, idleThreshold: 30 * 24 * time.Hour, minAgeBeforeRetire: 7 * 24 * time.Hour}
	// Created 10 days ago, never injected — past 7-day grace.
	lesson := workflow.Lesson{CreatedAt: now.Add(-10 * 24 * time.Hour)}
	ok, reason := rc.shouldRetire(lesson)
	if !ok {
		t.Error("never-injected lesson past grace period must retire")
	}
	if reason != "never_injected_past_grace" {
		t.Errorf("reason = %q, want %q", reason, "never_injected_past_grace")
	}
}

func TestShouldRetire_IdlePastThreshold(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{now: now, idleThreshold: 30 * 24 * time.Hour, minAgeBeforeRetire: 7 * 24 * time.Hour}
	// Last injected 60 days ago — past the 30-day threshold.
	last := now.Add(-60 * 24 * time.Hour)
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
	}
	ok, reason := rc.shouldRetire(lesson)
	if !ok {
		t.Error("lesson idle past threshold must retire")
	}
	if reason != "idle_past_threshold" {
		t.Errorf("reason = %q, want %q", reason, "idle_past_threshold")
	}
}

func TestShouldRetire_RecentlyInjectedIsKept(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{now: now, idleThreshold: 30 * 24 * time.Hour, minAgeBeforeRetire: 7 * 24 * time.Hour}
	last := now.Add(-3 * 24 * time.Hour)
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("recently-injected lesson must not be retired")
	}
}

func TestShouldRetire_MissingCreatedAtFallsThrough(t *testing.T) {
	// Defensive: legacy lessons without CreatedAt skip the grace check.
	// Behaviour falls through to the LastInjectedAt branches.
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{now: now, idleThreshold: 30 * 24 * time.Hour, minAgeBeforeRetire: 7 * 24 * time.Hour}
	last := now.Add(-90 * 24 * time.Hour)
	lesson := workflow.Lesson{LastInjectedAt: &last}
	if ok, _ := rc.shouldRetire(lesson); !ok {
		t.Error("legacy lesson with stale injection must retire even without CreatedAt")
	}
}

func TestShouldRetire_BoundaryAtIdleThreshold(t *testing.T) {
	// Exactly at threshold counts as retire (>=). Just under does not.
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	threshold := 30 * 24 * time.Hour
	rc := retirementCriteria{now: now, idleThreshold: threshold, minAgeBeforeRetire: 7 * 24 * time.Hour}

	atThreshold := now.Add(-threshold)
	lesson := workflow.Lesson{CreatedAt: now.Add(-90 * 24 * time.Hour), LastInjectedAt: &atThreshold}
	if ok, _ := rc.shouldRetire(lesson); !ok {
		t.Error("lesson at exact threshold should retire (>=)")
	}

	justUnder := now.Add(-threshold + time.Minute)
	lesson.LastInjectedAt = &justUnder
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("lesson just under threshold should not retire")
	}
}
