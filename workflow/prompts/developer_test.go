package prompts

import (
	"strings"
	"testing"
)

func TestDeveloperPrompt(t *testing.T) {
	prompt := DeveloperPrompt()

	// Should include key sections
	sections := []string{
		"Your Objective",
		"Context Gathering",
		"Implementation Rules",
		"Output Format",
		"Tools Available",
		"Knowledge Gaps", // From GapDetectionInstructions
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("DeveloperPrompt missing section: %s", section)
		}
	}

	// Should include SOP query instructions
	if !strings.Contains(prompt, "workflow_query_graph") {
		t.Error("DeveloperPrompt should include workflow_query_graph for SOP queries")
	}

	// Should include graph query example
	if !strings.Contains(prompt, "source.doc") {
		t.Error("DeveloperPrompt should include source.doc predicate for SOP queries")
	}

	// Should include output format
	if !strings.Contains(prompt, "files_modified") {
		t.Error("DeveloperPrompt should include structured output format")
	}
}

func TestDeveloperRetryPrompt(t *testing.T) {
	feedback := "Missing error handling in CreateUser function"
	prompt := DeveloperRetryPrompt(feedback)

	// Should include the feedback
	if !strings.Contains(prompt, feedback) {
		t.Error("DeveloperRetryPrompt should include the provided feedback")
	}

	// Should include fix instructions
	if !strings.Contains(prompt, "Fix EVERY issue") {
		t.Error("DeveloperRetryPrompt should instruct to fix all issues")
	}

	// Should include gap detection
	if !strings.Contains(prompt, "Knowledge Gaps") {
		t.Error("DeveloperRetryPrompt should include gap detection instructions")
	}
}
