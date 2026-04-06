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
		"Response Format",
		"Tools Available",
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("DeveloperPrompt missing section: %s", section)
		}
	}

	// Should include SOP query instructions
	if !strings.Contains(prompt, "graph_search") {
		t.Error("DeveloperPrompt should include graph_search for SOP queries")
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

	// Gap detection removed — verify NOT present.
	if strings.Contains(prompt, "Knowledge Gaps") {
		t.Error("DeveloperRetryPrompt should NOT include gap detection (removed)")
	}
}
