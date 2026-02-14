package prompts

import (
	"strings"
	"testing"
)

func TestExplorerSystemPrompt(t *testing.T) {
	prompt := ExplorerSystemPrompt()

	// Should include key sections
	sections := []string{
		"Your Objective",
		"Process",
		"Asking Questions",
		"Output Format",
		"Guidelines",
		"Tools Available",
		"Knowledge Gaps", // From GapDetectionInstructions
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("ExplorerSystemPrompt missing section: %s", section)
		}
	}

	// Should include gap detection format
	if !strings.Contains(prompt, "<gap>") {
		t.Error("ExplorerSystemPrompt should include gap detection XML format")
	}

	// Should include output JSON structure
	requiredFields := []string{
		"goal",
		"context",
		"scope",
		"include",
		"exclude",
		"do_not_touch",
	}
	for _, field := range requiredFields {
		if !strings.Contains(prompt, field) {
			t.Errorf("ExplorerSystemPrompt missing output field: %s", field)
		}
	}

	// Should include key tools
	tools := []string{
		"file_read",
		"file_list",
		"workflow_query_graph",
	}
	for _, tool := range tools {
		if !strings.Contains(prompt, tool) {
			t.Errorf("ExplorerSystemPrompt missing tool: %s", tool)
		}
	}
}

func TestExplorerPromptWithTopic(t *testing.T) {
	topic := "authentication system refactoring"
	prompt := ExplorerPromptWithTopic(topic)

	// Should include the topic
	if !strings.Contains(prompt, topic) {
		t.Errorf("ExplorerPromptWithTopic should include topic: %s", topic)
	}

	// Should mention exploring/exploration
	if !strings.Contains(prompt, "Explore") && !strings.Contains(prompt, "explore") {
		t.Error("ExplorerPromptWithTopic should instruct to explore")
	}

	// Should mention Goal/Context/Scope output
	if !strings.Contains(prompt, "Goal/Context/Scope") {
		t.Error("ExplorerPromptWithTopic should reference Goal/Context/Scope structure")
	}

	// Should mention asking questions
	if !strings.Contains(prompt, "question") {
		t.Error("ExplorerPromptWithTopic should mention asking questions")
	}
}

func TestExplorerPromptWithTopic_EmptyTopic(t *testing.T) {
	// Should handle empty topic gracefully
	prompt := ExplorerPromptWithTopic("")

	// Should still be a valid prompt
	if len(prompt) == 0 {
		t.Error("ExplorerPromptWithTopic should return a prompt even with empty topic")
	}

	// Should still have structure references
	if !strings.Contains(prompt, "Goal/Context/Scope") {
		t.Error("ExplorerPromptWithTopic should reference structure even with empty topic")
	}
}

func TestExplorerPromptWithTopic_SpecialCharacters(t *testing.T) {
	// Test with special characters in topic
	topic := "add \"quotes\" and <brackets> handling"
	prompt := ExplorerPromptWithTopic(topic)

	// Should include the topic with special characters
	if !strings.Contains(prompt, topic) {
		t.Errorf("ExplorerPromptWithTopic should include topic with special chars: %s", topic)
	}
}
