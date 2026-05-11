package executionmanager

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestSummarizeCycleHistory_Empty pins the zero-history case: cycle 0
// has no PriorCycles, so dispatchDeveloperLocked must see "" and skip
// the block entirely.
func TestSummarizeCycleHistory_Empty(t *testing.T) {
	if got := summarizeCycleHistory(nil); got != "" {
		t.Errorf("nil cycles: got %q, want \"\"", got)
	}
	if got := summarizeCycleHistory([]cycleSnapshot{}); got != "" {
		t.Errorf("empty cycles: got %q, want \"\"", got)
	}
}

// TestSummarizeCycleHistory_SingleCycle pins single-cycle rendering.
// No patterns fire on N=1; just the per-cycle block + the closing
// advisory line.
func TestSummarizeCycleHistory_SingleCycle(t *testing.T) {
	cycles := []cycleSnapshot{{
		Cycle:         0,
		LoopID:        "loop-1",
		Verdict:       "rejected",
		RejectionType: "fixable",
		Feedback:      "Missing test for the error branch.",
		FilesModified: []string{"a.go"},
	}}
	got := summarizeCycleHistory(cycles)

	mustContain := []string{
		"PRIOR ATTEMPTS (1 cycle before this one)",
		"Cycle 0: rejected (fixable)",
		"a.go",
		"Missing test for the error branch.",
		"Do not repeat any approach above",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\nfull:\n%s", want, got)
		}
	}
	// Patterns fire only on N>=2.
	if strings.Contains(got, "Pattern across attempts:") {
		t.Error("pattern section should not appear for N=1")
	}
}

// TestSummarizeCycleHistory_PatternAllRejected fires when every cycle
// hits rejected verdict.
func TestSummarizeCycleHistory_PatternAllRejected(t *testing.T) {
	cycles := []cycleSnapshot{
		{Cycle: 0, Verdict: "rejected", RejectionType: "fixable", Feedback: "first"},
		{Cycle: 1, Verdict: "rejected", RejectionType: "fixable", Feedback: "second"},
		{Cycle: 2, Verdict: "rejected", RejectionType: "fixable", Feedback: "third"},
	}
	got := summarizeCycleHistory(cycles)
	if !strings.Contains(got, "All 3 prior cycles were rejected.") {
		t.Errorf("missing all-rejected pattern\nfull:\n%s", got)
	}
}

// TestSummarizeCycleHistory_PatternSameFiles fires when every cycle
// touches the same file set. Order-insensitive comparison.
func TestSummarizeCycleHistory_PatternSameFiles(t *testing.T) {
	cycles := []cycleSnapshot{
		{Cycle: 0, Verdict: "rejected", FilesModified: []string{"a.go", "b.go"}},
		{Cycle: 1, Verdict: "rejected", FilesModified: []string{"b.go", "a.go"}}, // reordered
		{Cycle: 2, Verdict: "rejected", FilesModified: []string{"a.go", "b.go"}},
	}
	got := summarizeCycleHistory(cycles)
	if !strings.Contains(got, "All cycles modified the same files:") {
		t.Errorf("missing same-files pattern\nfull:\n%s", got)
	}
}

// TestSummarizeCycleHistory_NoFalsePositivePatterns ensures the
// same-files pattern doesn't fire when the file sets differ between
// cycles. Cross-pattern check: avoids the Goodhart risk of patterns
// being technically true but misleading.
func TestSummarizeCycleHistory_NoFalsePositivePatterns(t *testing.T) {
	cycles := []cycleSnapshot{
		{Cycle: 0, Verdict: "rejected", FilesModified: []string{"a.go"}},
		{Cycle: 1, Verdict: "rejected", FilesModified: []string{"b.go"}},
		{Cycle: 2, Verdict: "rejected", FilesModified: []string{"c.go"}},
	}
	got := summarizeCycleHistory(cycles)
	if strings.Contains(got, "All cycles modified the same files:") {
		t.Errorf("same-files pattern fired despite distinct file sets\nfull:\n%s", got)
	}
}

// TestSummarizeCycleHistory_CommonRejectionType pins "all rejections
// were the same kind" — the canonical case is fixable→fixable→fixable
// (developer iterating in good faith but never satisfying the reviewer)
// vs the cross-shape pattern (fixable→restructure) that ADR-037 already
// catches via the reviewer-retry fragment.
func TestSummarizeCycleHistory_CommonRejectionType(t *testing.T) {
	cycles := []cycleSnapshot{
		{Cycle: 0, Verdict: "rejected", RejectionType: "fixable"},
		{Cycle: 1, Verdict: "rejected", RejectionType: "fixable"},
		{Cycle: 2, Verdict: "rejected", RejectionType: "fixable"},
	}
	got := summarizeCycleHistory(cycles)
	if !strings.Contains(got, "All rejections were rejection_type=fixable.") {
		t.Errorf("missing common-rejection-type pattern\nfull:\n%s", got)
	}
}

// TestSummarizeCycleHistory_MixedRejectionTypes suppresses the common-
// type pattern when cycles disagree.
func TestSummarizeCycleHistory_MixedRejectionTypes(t *testing.T) {
	cycles := []cycleSnapshot{
		{Cycle: 0, Verdict: "rejected", RejectionType: "fixable"},
		{Cycle: 1, Verdict: "rejected", RejectionType: "restructure"},
	}
	got := summarizeCycleHistory(cycles)
	if strings.Contains(got, "All rejections were rejection_type=") {
		t.Errorf("common-type pattern fired despite mixed types\nfull:\n%s", got)
	}
}

// TestSummarizeCycleHistory_GoodhartGuards pins the framing: the
// summarizer is OBSERVATION-only. If a future edit reintroduces
// prescriptive language ("you should X", "the right approach is Y")
// — the kind of synthesis that belongs to the recovery agent's
// diagnosis, not deterministic pattern matching — this test fails
// at PR time. Keeps the gradient on the agent's reasoning intact;
// prevents the summarizer from becoming a back-door safety net.
func TestSummarizeCycleHistory_GoodhartGuards(t *testing.T) {
	cycles := []cycleSnapshot{
		{Cycle: 0, Verdict: "rejected", RejectionType: "fixable", FilesModified: []string{"a.go"}},
		{Cycle: 1, Verdict: "rejected", RejectionType: "fixable", FilesModified: []string{"a.go"}},
		{Cycle: 2, Verdict: "rejected", RejectionType: "fixable", FilesModified: []string{"a.go"}},
	}
	got := summarizeCycleHistory(cycles)

	mustNotContain := []string{
		"you should",
		"You should",
		"the right approach",
		"try this instead",
		"the correct fix",
		"always",
		"never use",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(got, banned) {
			t.Errorf("summarizer carries prescriptive language %q (Goodhart guard — summarizer is observation-only, prescription belongs to recovery diagnosis)\nfull:\n%s",
				banned, got)
		}
	}
}

// TestAppendCycleSnapshot_Cap pins the priorCyclesCap behaviour.
// Once cycles exceed the cap, oldest entries are dropped — never grows
// unboundedly.
func TestAppendCycleSnapshot_Cap(t *testing.T) {
	exec := &taskExecution{
		TaskExecution: &workflow.TaskExecution{},
	}
	for i := 0; i < priorCyclesCap+5; i++ {
		exec.TDDCycle = i
		appendCycleSnapshot(exec, payloads.TaskCodeReviewResult{
			Verdict:       "rejected",
			RejectionType: "fixable",
			Feedback:      "x",
		})
	}
	if len(exec.PriorCycles) != priorCyclesCap {
		t.Errorf("PriorCycles length = %d, want %d (cap)", len(exec.PriorCycles), priorCyclesCap)
	}
	// Oldest dropped, newest kept — first entry should be cycle (total - cap).
	wantFirstCycle := (priorCyclesCap + 5) - priorCyclesCap
	if exec.PriorCycles[0].Cycle != wantFirstCycle {
		t.Errorf("oldest cycle in capped slice: got %d, want %d",
			exec.PriorCycles[0].Cycle, wantFirstCycle)
	}
}

// TestAppendCycleSnapshot_FeedbackCap pins per-cycle feedback truncation.
// A pathologically long feedback string (truncated graph_search dump,
// stack trace, etc) must not blow the in-memory state.
func TestAppendCycleSnapshot_FeedbackCap(t *testing.T) {
	exec := &taskExecution{
		TaskExecution: &workflow.TaskExecution{},
	}
	bigFeedback := strings.Repeat("x", snapshotFeedbackCap*3)
	appendCycleSnapshot(exec, payloads.TaskCodeReviewResult{
		Verdict:       "rejected",
		RejectionType: "fixable",
		Feedback:      bigFeedback,
	})

	if len(exec.PriorCycles) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(exec.PriorCycles))
	}
	got := exec.PriorCycles[0].Feedback
	// snapshotFeedbackCap is the budget for the prefix; the ellipsis
	// adds a few bytes for the multi-byte UTF-8 "…" rune (3 bytes).
	if len(got) > snapshotFeedbackCap+3 {
		t.Errorf("feedback not capped: len=%d, want <= %d", len(got), snapshotFeedbackCap+3)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("capped feedback should end with ellipsis, got tail %q", got[len(got)-10:])
	}
}
