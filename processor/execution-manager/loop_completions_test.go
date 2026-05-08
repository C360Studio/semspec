package executionmanager

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
)

func TestBuildLoopFailureFeedback_MaxIterationsNamesBudgetAndSubmitWork(t *testing.T) {
	event := &agentic.LoopCompletedEvent{
		Outcome:    "failed",
		Iterations: 50,
		Metadata: map[string]any{
			"error":          "agentic-loop.HandleModelResponse: check max iterations failed: max iterations (50) reached",
			"max_iterations": 50,
		},
	}
	got := buildLoopFailureFeedback(event)
	for _, want := range []string{"50 of 50", "submit_work", "iteration budget"} {
		if !strings.Contains(got, want) {
			t.Errorf("feedback missing %q\ngot: %s", want, got)
		}
	}
}

func TestBuildLoopFailureFeedback_GenericErrorIncludesMessage(t *testing.T) {
	event := &agentic.LoopCompletedEvent{
		Outcome: "failed",
		Metadata: map[string]any{
			"error": "tool dispatch panic: index out of range",
		},
	}
	got := buildLoopFailureFeedback(event)
	if !strings.Contains(got, "tool dispatch panic") {
		t.Errorf("feedback should quote the error\ngot: %s", got)
	}
	if !strings.Contains(got, "submit_work") {
		t.Errorf("feedback should still nudge submit_work\ngot: %s", got)
	}
}

func TestBuildLoopFailureFeedback_NoMetadataFallsBackOnOutcome(t *testing.T) {
	event := &agentic.LoopCompletedEvent{Outcome: "cancelled"}
	got := buildLoopFailureFeedback(event)
	if !strings.Contains(got, "outcome=cancelled") {
		t.Errorf("feedback should include outcome\ngot: %s", got)
	}
	if !strings.Contains(got, "submit_work") {
		t.Errorf("feedback should still nudge submit_work\ngot: %s", got)
	}
}

// TestBuildValidationFailureFeedback_RendersFailedRequiredCheck pins the
// take-21 follow-up: replace raw json.Marshal of CheckResult slice with
// bounded human-readable markdown wrapped in untrusted-content delimiters.
// Validates the basic happy shape: failed required check renders with name,
// command, exit code, and stderr inside the delimiter block.
func TestBuildValidationFailureFeedback_RendersFailedRequiredCheck(t *testing.T) {
	results := []payloads.CheckResult{
		{
			Name:     "go-tests-exist-for-changes",
			Passed:   false,
			Required: true,
			Command:  "find ./. -maxdepth 1 -name '*_test.go' -type f",
			ExitCode: 1,
			Stderr:   "packages with non-test .go changes but no *_test.go file present: ./.",
			Duration: "12ms",
		},
	}

	got := buildValidationFailureFeedback(results)

	mustContain := []string{
		"Structural validation failed",
		"## Check: go-tests-exist-for-changes (FAILED, required)",
		"Command: `find ./. -maxdepth 1 -name '*_test.go' -type f`",
		"Exit code: 1",
		"Duration: 12ms",
		`<validation-output trust="untrusted">`,
		"packages with non-test .go changes but no *_test.go file present: ./.",
		"</validation-output>",
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in feedback\ngot:\n%s", s, got)
		}
	}
}

// TestBuildValidationFailureFeedback_TruncatesLongOutput guards prompt
// explosion: a verbose `go test -v` failure can dump 10K+ chars of stack
// trace. Each Stdout/Stderr field must be tail-truncated at the cap.
func TestBuildValidationFailureFeedback_TruncatesLongOutput(t *testing.T) {
	long := strings.Repeat("X", maxValidationOutputBytes*2) + "ACTIONABLE_TAIL"
	results := []payloads.CheckResult{
		{
			Name:     "go-test",
			Passed:   false,
			Required: true,
			Stderr:   long,
		},
	}

	got := buildValidationFailureFeedback(results)

	if !strings.Contains(got, "[...truncated") {
		t.Errorf("expected truncation marker in output\ngot:\n%s", got)
	}
	if !strings.Contains(got, "ACTIONABLE_TAIL") {
		t.Errorf("tail-truncation must preserve the END of the output (failures fail at the end)\ngot last 200 chars:\n%s", got[max(0, len(got)-200):])
	}
	if strings.Count(got, "X") > maxValidationOutputBytes+100 {
		t.Errorf("X count = %d, expected ≤ %d (truncation should drop most Xs)", strings.Count(got, "X"), maxValidationOutputBytes+100)
	}
}

// TestBuildValidationFailureFeedback_SkipsAdvisoryAndPassed confirms only
// FAILED REQUIRED checks render — passed checks (success cases) and
// advisory checks (Required=false; e.g. anti-mock warnings) don't bloat
// the retry prompt.
func TestBuildValidationFailureFeedback_SkipsAdvisoryAndPassed(t *testing.T) {
	results := []payloads.CheckResult{
		{Name: "go-build", Passed: true, Required: true, Stdout: "all packages built"},
		{Name: "anti-mock", Passed: false, Required: false, Stderr: "advisory: mock detected"},
		{Name: "go-test", Passed: false, Required: true, Stderr: "FAIL: TestFoo"},
	}

	got := buildValidationFailureFeedback(results)

	if strings.Contains(got, "go-build") {
		t.Errorf("passed check leaked into feedback:\n%s", got)
	}
	if strings.Contains(got, "anti-mock") || strings.Contains(got, "advisory: mock detected") {
		t.Errorf("advisory (Required=false) check leaked into feedback:\n%s", got)
	}
	if !strings.Contains(got, "go-test") || !strings.Contains(got, "FAIL: TestFoo") {
		t.Errorf("expected go-test failure to be rendered\ngot:\n%s", got)
	}
}

// TestBuildValidationFailureFeedback_DefensiveOnEmptyResults guards the
// caller-checked-Passed=false-but-no-required-check-failed shape. The
// feedback must surface the wiring inconsistency rather than emit an empty
// retry prompt that would just spawn an undirected dev cycle.
func TestBuildValidationFailureFeedback_DefensiveOnEmptyResults(t *testing.T) {
	got := buildValidationFailureFeedback(nil)

	if !strings.Contains(got, "no required check reported a failure") {
		t.Errorf("expected wiring-issue defensive message\ngot:\n%s", got)
	}
	if strings.Contains(got, "## Check:") {
		t.Errorf("empty results should not render any check sections\ngot:\n%s", got)
	}
}

// TestTailTruncate_PreservesEndAndAnnounces validates the truncation
// mechanic in isolation. Short input passes through untouched; long input
// drops the head with an explicit byte-count marker.
func TestTailTruncate_PreservesEndAndAnnounces(t *testing.T) {
	short := "short message"
	if got := tailTruncate(short, 100); got != short {
		t.Errorf("short input should pass through; got %q", got)
	}

	long := strings.Repeat("a", 50) + "tail"
	got := tailTruncate(long, 10)
	if !strings.HasSuffix(got, "tail") {
		t.Errorf("truncation should preserve the suffix; got %q", got)
	}
	if !strings.Contains(got, "[...truncated") {
		t.Errorf("truncation should announce itself; got %q", got)
	}
}
