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

// ---------------------------------------------------------------------------
// Phase 5b: filesystem-existence retirement tests
// ---------------------------------------------------------------------------

// fakeFS returns a fileExists stub that recognises only the named paths.
func fakeFS(present ...string) func(string) bool {
	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func TestShouldRetire_AllEvidenceFilesMissing(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	last := now.Add(-1 * time.Hour) // recently injected — would otherwise be kept
	rc := retirementCriteria{
		now: now, idleThreshold: 30 * 24 * time.Hour,
		minAgeBeforeRetire: 7 * 24 * time.Hour,
		fileExists:         fakeFS(), // nothing exists
	}
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
		EvidenceFiles: []workflow.FileRef{
			{Path: "deleted/foo.go"},
			{Path: "renamed/bar.go"},
		},
	}
	ok, reason := rc.shouldRetire(lesson)
	if !ok {
		t.Error("lesson with all-missing evidence files should retire")
	}
	if reason != "evidence_files_missing" {
		t.Errorf("reason = %q, want %q", reason, "evidence_files_missing")
	}
}

func TestShouldRetire_PartialEvidenceFilesPresentKeeps(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	last := now.Add(-1 * time.Hour)
	rc := retirementCriteria{
		now: now, idleThreshold: 30 * 24 * time.Hour,
		minAgeBeforeRetire: 7 * 24 * time.Hour,
		fileExists:         fakeFS("kept/foo.go"),
	}
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
		EvidenceFiles: []workflow.FileRef{
			{Path: "kept/foo.go"},
			{Path: "deleted/bar.go"},
		},
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("partial evidence (one path still exists) should keep the lesson")
	}
}

func TestShouldRetire_NoEvidenceFilesSkipsCheck(t *testing.T) {
	// A lesson with EvidenceSteps only (no EvidenceFiles) must NOT be
	// retired by the missing-files criterion. Phase 5c will validate
	// trajectory steps separately.
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	last := now.Add(-1 * time.Hour)
	rc := retirementCriteria{
		now: now, idleThreshold: 30 * 24 * time.Hour,
		minAgeBeforeRetire: 7 * 24 * time.Hour,
		fileExists:         fakeFS(),
	}
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
		EvidenceSteps:  []workflow.StepRef{{LoopID: "abc", StepIndex: 1}},
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("lesson with no EvidenceFiles must skip Phase 5b check")
	}
}

func TestShouldRetire_NilFileExistsSkipsCheck(t *testing.T) {
	// When repoPath couldn't be resolved, fileExists is nil and the
	// component skips the filesystem criterion entirely. Otherwise the
	// curator would retire every cited file because every path "doesn't
	// exist" against an empty root.
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	last := now.Add(-1 * time.Hour)
	rc := retirementCriteria{
		now: now, idleThreshold: 30 * 24 * time.Hour,
		minAgeBeforeRetire: 7 * 24 * time.Hour,
		fileExists:         nil,
	}
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
		EvidenceFiles:  []workflow.FileRef{{Path: "anything.go"}},
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("nil fileExists should skip the filesystem check, not retire")
	}
}

func TestShouldRetire_EmptyPathsSkipped(t *testing.T) {
	// EvidenceFiles entries with empty Path are skipped (defensive — a
	// lesson shouldn't have empty paths but if it does, we treat it as
	// no evidence rather than always-missing evidence).
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	last := now.Add(-1 * time.Hour)
	rc := retirementCriteria{
		now: now, idleThreshold: 30 * 24 * time.Hour,
		minAgeBeforeRetire: 7 * 24 * time.Hour,
		fileExists:         fakeFS("real/foo.go"),
	}
	lesson := workflow.Lesson{
		CreatedAt:      now.Add(-90 * 24 * time.Hour),
		LastInjectedAt: &last,
		EvidenceFiles: []workflow.FileRef{
			{Path: ""}, // empty — skip
			{Path: "real/foo.go"},
		},
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("empty path entries should be skipped, surviving file keeps lesson")
	}
}

func TestShouldRetire_GraceBeatsMissingEvidence(t *testing.T) {
	// A brand-new lesson with all-missing evidence is still in grace —
	// the producer's bug should not bite immediately at the first sweep.
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rc := retirementCriteria{
		now: now, idleThreshold: 30 * 24 * time.Hour,
		minAgeBeforeRetire: 7 * 24 * time.Hour,
		fileExists:         fakeFS(),
	}
	lesson := workflow.Lesson{
		CreatedAt:     now.Add(-1 * time.Hour),
		EvidenceFiles: []workflow.FileRef{{Path: "deleted/foo.go"}},
	}
	if ok, _ := rc.shouldRetire(lesson); ok {
		t.Error("grace period must protect even from missing-evidence retirement")
	}
}
