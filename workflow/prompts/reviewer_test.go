package prompts

import (
	"strings"
	"testing"
)

func TestReviewerPrompt(t *testing.T) {
	prompt := ReviewerPrompt()

	// Should include key sections
	sections := []string{
		"Your Objective",
		"Context Gathering",
		"Review Checklist",
		"Rejection Types",
		"Output Format",
		"Tools Available",
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("ReviewerPrompt missing section: %s", section)
		}
	}

	// Should include SOP query instructions
	if !strings.Contains(prompt, "workflow_query_graph") {
		t.Error("ReviewerPrompt should include workflow_query_graph for SOP queries")
	}

	// Should include all rejection types
	rejectionTypes := []string{"fixable", "misscoped", "architectural", "too_big"}
	for _, rt := range rejectionTypes {
		if !strings.Contains(prompt, rt) {
			t.Errorf("ReviewerPrompt should include rejection type: %s", rt)
		}
	}

	// Should include structured output format
	if !strings.Contains(prompt, "sop_review") {
		t.Error("ReviewerPrompt should include sop_review in output format")
	}

	// Should include integrity rules
	if !strings.Contains(prompt, "Integrity Rules") {
		t.Error("ReviewerPrompt should include integrity rules")
	}

	// Should emphasize adversarial role
	if !strings.Contains(prompt, "TRUSTWORTHINESS") {
		t.Error("ReviewerPrompt should emphasize trustworthiness objective")
	}

	// Should be read-only
	if !strings.Contains(prompt, "Read-Only") || !strings.Contains(prompt, "READ-ONLY") {
		t.Error("ReviewerPrompt should emphasize read-only access")
	}
}

func TestReviewerPromptRejectionCriteria(t *testing.T) {
	prompt := ReviewerPrompt()

	// Should include rule about not approving with violations
	if !strings.Contains(prompt, "CANNOT approve if any SOP has status \"violated\"") {
		t.Error("ReviewerPrompt should prevent approving with SOP violations")
	}

	// Should include confidence threshold
	if !strings.Contains(prompt, "0.7") {
		t.Error("ReviewerPrompt should include confidence threshold (0.7)")
	}
}
