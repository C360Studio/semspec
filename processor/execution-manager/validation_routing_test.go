package executionmanager

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow/payloads"
)

// TestOwnershipPlanningGap pins the detector that distinguishes an ADR-049
// ownership/planning gap (a new source/test file outside the story's declared
// territory — not dev-fixable) from an ordinary, dev-fixable validation failure.
// Misclassifying either way is the regression we guard: a false positive
// fast-fails honest dev work to recovery; a false negative re-opens the
// dev-retry thrash on a partition defect.
func TestOwnershipPlanningGap(t *testing.T) {
	tests := []struct {
		name      string
		results   []payloads.CheckResult
		wantOK    bool
		wantInDet string
	}{
		{
			name:      "planning-gap required failure detected, detail carried",
			results:   []payloads.CheckResult{{Name: payloads.CheckFileOwnershipPlanningGap, Required: true, Passed: false, Stderr: "outside scope: src/x/New.java"}},
			wantOK:    true,
			wantInDet: "src/x/New.java",
		},
		{
			name:    "containment-only failure is NOT a planning gap (dev can fix scratch/doc)",
			results: []payloads.CheckResult{{Name: payloads.CheckFileOwnershipContainment, Required: true, Passed: false, Stderr: "patch.diff"}},
			wantOK:  false,
		},
		{
			name:    "advisory-only is not a planning gap",
			results: []payloads.CheckResult{{Name: payloads.CheckFileOwnershipAdvisory, Required: false, Passed: false}},
			wantOK:  false,
		},
		{
			name:    "a passing planning-gap row is not a failure",
			results: []payloads.CheckResult{{Name: payloads.CheckFileOwnershipPlanningGap, Required: true, Passed: true}},
			wantOK:  false,
		},
		{
			name:    "empty results",
			results: nil,
			wantOK:  false,
		},
		{
			name:      "gap with empty stderr falls back to a default detail",
			results:   []payloads.CheckResult{{Name: payloads.CheckFileOwnershipPlanningGap, Required: true, Passed: false}},
			wantOK:    true,
			wantInDet: "outside the story's declared file scope",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detail, ok := ownershipPlanningGap(tc.results)
			if ok != tc.wantOK {
				t.Fatalf("ownershipPlanningGap ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantInDet != "" && !strings.Contains(detail, tc.wantInDet) {
				t.Errorf("detail = %q, want it to contain %q", detail, tc.wantInDet)
			}
		})
	}
}

// TestHandleValidationFailedLocked_OwnershipGapFastFailsToRecovery proves the
// ADR-049 move-3 routing: a planning-gap validation failure escalates straight
// to recovery (firing RecoveryRequested with the offending path so the
// recovery-agent can route architecture_revise vs story_reprepare) EVEN WHEN the
// TDD budget still has cycles — the whole point is to not burn the budget
// thrashing the dev on a partition defect it cannot fix.
func TestHandleValidationFailedLocked_OwnershipGapFastFailsToRecovery(t *testing.T) {
	c := newTestComponent(t)
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	exec := newTestExec("gap-slug", "task-gap")
	exec.RequirementID = "req-gap-1"
	exec.DeveloperLoopID = "loop-gap-1"
	exec.TraceID = "trace-gap-1"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3 // budget REMAINS — proves the gap skips dev-retry
	exec.ValidationPassed = false
	exec.ValidationResults = []payloads.CheckResult{
		{Name: payloads.CheckFileOwnershipContainment, Required: true, Passed: true},
		{Name: payloads.CheckFileOwnershipPlanningGap, Required: true, Passed: false,
			Stderr: "New source/test file(s) created outside this story's declared file scope: src/x/New.java."},
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.handleValidationFailedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	got := fetch()
	if len(got) != 1 {
		t.Fatalf("expected 1 RecoveryRequested (fast-fail to recovery despite remaining TDD budget), got %d", len(got))
	}
	if !strings.Contains(got[0].EscalationReason, "planning gap") {
		t.Errorf("EscalationReason = %q, want it to name the planning gap", got[0].EscalationReason)
	}
	if !strings.Contains(got[0].EscalationReason, "src/x/New.java") {
		t.Errorf("EscalationReason should carry the offending path for the recovery-agent, got %q", got[0].EscalationReason)
	}
	if exec.Stage != phaseEscalated {
		t.Errorf("exec.Stage = %q, want %q (fast-failed, not dev-retried)", exec.Stage, phaseEscalated)
	}
}
