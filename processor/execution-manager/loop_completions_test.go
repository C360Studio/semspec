package executionmanager

import (
	"strings"
	"testing"

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
