package qareviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestReconcileSkipVerdict covers the deterministic safety net that prevents a
// run with skipped tests from ever being reported as a full all-green approval,
// and that strips a meaningless conditional approval when nothing was skipped.
func TestReconcileSkipVerdict(t *testing.T) {
	withSkips := &workflow.QARun{SkippedTests: []workflow.QASkippedTest{{Suite: "DriverIT", Name: "sitl"}}}
	noSkips := &workflow.QARun{}

	tests := []struct {
		name     string
		raw      string
		qaRun    *workflow.QARun
		want     workflow.QAVerdict
		wantNote bool
	}{
		{"approved+skips coerced to needs_changes", "approved", withSkips, workflow.QAVerdictNeedsChanges, true},
		{"approved+no-skips stays approved", "approved", noSkips, workflow.QAVerdictApproved, false},
		{"conditionally_approved+skips kept", "conditionally_approved", withSkips, workflow.QAVerdictConditionallyApproved, false},
		{"conditionally_approved+no-skips downgraded to approved", "conditionally_approved", noSkips, workflow.QAVerdictApproved, false},
		{"needs_changes+skips kept", "needs_changes", withSkips, workflow.QAVerdictNeedsChanges, false},
		{"rejected+skips kept", "rejected", withSkips, workflow.QAVerdictRejected, false},
		{"nil qaRun passes through", "approved", nil, workflow.QAVerdictApproved, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, note := reconcileSkipVerdict(tc.raw, tc.qaRun)
			if got != tc.want {
				t.Errorf("verdict = %q, want %q", got, tc.want)
			}
			if (note != "") != tc.wantNote {
				t.Errorf("note presence = %v (%q), want %v", note != "", note, tc.wantNote)
			}
			if tc.wantNote && !strings.Contains(note, "skip-guard") {
				t.Errorf("note should be a labeled skip-guard coercion, got %q", note)
			}
		})
	}
}

// TestBuildQAVerdictEventSkipGuard asserts the guard fires end-to-end through
// buildQAVerdictEvent: an agent that approves a run with skipped tests yields a
// needs_changes verdict (fail closed), never approved.
func TestBuildQAVerdictEventSkipGuard(t *testing.T) {
	plan := &workflow.Plan{
		ID:      "plan.test",
		Slug:    "test",
		QALevel: workflow.QALevelIntegration,
		QARun:   &workflow.QARun{Passed: true, SkippedTests: []workflow.QASkippedTest{{Suite: "MavsdkSmokeTest", Name: "sitl"}}},
	}
	result := &qaReviewOutput{Verdict: string(workflow.QAVerdictApproved), Summary: "looks good"}

	ev := buildQAVerdictEvent("test", plan, result)
	if ev.Verdict != workflow.QAVerdictNeedsChanges {
		t.Fatalf("Verdict = %q, want needs_changes (approved+skips must fail closed)", ev.Verdict)
	}
	if !strings.Contains(ev.Summary, "skip-guard") {
		t.Errorf("summary should carry the skip-guard coercion note, got %q", ev.Summary)
	}
}
