package requirementgenerator

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow/payloads"
)

func TestBuildUserPrompt_NoReviewFindings(t *testing.T) {
	c := &Component{}
	trigger := &payloads.RequirementGeneratorRequest{
		Slug:    "test",
		Title:   "Test Plan",
		Goal:    "Add endpoint",
		Context: "Flask API",
	}

	prompt := c.buildUserPrompt(trigger, "")

	if strings.Contains(prompt, "Review Findings") {
		t.Error("prompt should NOT contain review findings when not provided")
	}
	if !strings.Contains(prompt, "Add endpoint") {
		t.Error("prompt should contain the goal")
	}
}

func TestBuildUserPrompt_WithReviewFindings(t *testing.T) {
	c := &Component{}
	trigger := &payloads.RequirementGeneratorRequest{
		Slug: "test",
		Goal: "Add endpoint",
	}

	findings := "### Violations\n- Missing coverage for error handling"
	prompt := c.buildUserPrompt(trigger, "", findings)

	if !strings.Contains(prompt, "Previous Review Findings") {
		t.Error("prompt should contain review findings header")
	}
	if !strings.Contains(prompt, "Missing coverage for error handling") {
		t.Error("prompt should contain the findings text")
	}
}

func TestBuildUserPrompt_EmptyReviewFindings(t *testing.T) {
	c := &Component{}
	trigger := &payloads.RequirementGeneratorRequest{
		Slug: "test",
		Goal: "Add endpoint",
	}

	prompt := c.buildUserPrompt(trigger, "", "")

	if strings.Contains(prompt, "Review Findings") {
		t.Error("prompt should NOT contain review findings when empty string passed")
	}
}
