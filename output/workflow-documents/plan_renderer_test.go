package workflowdocuments

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderPlan_MinimalDrafted(t *testing.T) {
	plan := &workflow.Plan{
		Slug:      "test-plan",
		Title:     "Add /hello endpoint",
		Status:    workflow.StatusDrafted,
		CreatedAt: time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
		Goal:      "Add a /hello endpoint that returns a greeting",
		Context:   "The API needs a health-check style endpoint",
	}

	md := RenderPlan(plan)

	if !strings.Contains(md, "# Add /hello endpoint") {
		t.Error("missing title as H1")
	}
	if !strings.Contains(md, "**Status:** drafted") {
		t.Error("missing status")
	}
	if !strings.Contains(md, "## Goal") {
		t.Error("missing Goal section")
	}
	if !strings.Contains(md, "Add a /hello endpoint") {
		t.Error("missing goal text")
	}
	if !strings.Contains(md, "## Context") {
		t.Error("missing Context section")
	}
}

func TestRenderPlan_WithRequirements(t *testing.T) {
	plan := &workflow.Plan{
		Slug:   "req-test",
		Title:  "Auth Feature",
		Status: workflow.StatusRequirementsGenerated,
		Goal:   "Add auth",
		Requirements: []workflow.Requirement{
			{ID: "req-1", Title: "Login endpoint", Description: "POST /login with email+password", Status: "active"},
			{ID: "req-2", Title: "Token validation", Description: "Validate JWT on protected routes", Status: "active", DependsOn: []string{"req-1"}},
		},
	}

	md := RenderPlan(plan)

	if !strings.Contains(md, "## Requirements (2)") {
		t.Error("missing requirements count")
	}
	if !strings.Contains(md, "### Login endpoint") {
		t.Error("missing req-1 title as H3")
	}
	if !strings.Contains(md, "POST /login with email+password") {
		t.Error("missing req-1 description")
	}
	if !strings.Contains(md, "**Dependencies:** req-1") {
		t.Error("missing depends_on for req-2")
	}
}

func TestRenderPlan_WithScenarios(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "scenario-test",
		Title: "Auth",
		Goal:  "Add auth",
		Requirements: []workflow.Requirement{
			{ID: "req-1", Title: "Login"},
		},
		Scenarios: []workflow.Scenario{
			{
				ID:            "sc-1",
				RequirementID: "req-1",
				Given:         "a registered user with valid credentials",
				When:          "they submit the login form",
				Then:          []string{"response status is 200", "a JWT token is returned"},
			},
		},
	}

	md := RenderPlan(plan)

	if !strings.Contains(md, "#### Scenarios") {
		t.Error("missing Scenarios heading")
	}
	if !strings.Contains(md, "**Given** a registered user") {
		t.Error("missing Given")
	}
	if !strings.Contains(md, "**When** they submit") {
		t.Error("missing When")
	}
	if !strings.Contains(md, "- response status is 200") {
		t.Error("missing Then item as bullet")
	}
	if !strings.Contains(md, "- a JWT token is returned") {
		t.Error("missing second Then item")
	}
}

func TestRenderPlan_WithReviewHistory(t *testing.T) {
	plan := &workflow.Plan{
		Slug:                    "review-test",
		Title:                   "Test",
		Goal:                    "Test goal",
		ReviewIteration:         2,
		ReviewVerdict:           "needs_changes",
		ReviewSummary:           "Goal lacks specificity",
		ReviewFormattedFindings: "### Violations\n\n- **[ERROR]** Goal Clarity\n  - Issue: Goal is too vague\n",
	}

	md := RenderPlan(plan)

	if !strings.Contains(md, "## Review History") {
		t.Error("missing Review History section")
	}
	if !strings.Contains(md, "**Iteration:** 2") {
		t.Error("missing iteration count")
	}
	if !strings.Contains(md, "**Verdict:** needs_changes") {
		t.Error("missing verdict")
	}
	if !strings.Contains(md, "Goal lacks specificity") {
		t.Error("missing summary")
	}
	if !strings.Contains(md, "Goal is too vague") {
		t.Error("missing findings content")
	}
}

func TestRenderPlan_NoReviewHistory(t *testing.T) {
	plan := &workflow.Plan{
		Slug:            "no-review",
		Title:           "Clean Plan",
		Goal:            "Simple goal",
		ReviewIteration: 0,
	}

	md := RenderPlan(plan)

	if strings.Contains(md, "Review History") {
		t.Error("Review History should NOT be present when ReviewIteration=0")
	}
}

func TestRenderPlan_WithScope(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "scope-test",
		Title: "Scoped Plan",
		Goal:  "Modify API",
		Scope: workflow.Scope{
			Include:    []string{"api/", "lib/auth/"},
			Exclude:    []string{"vendor/"},
			DoNotTouch: []string{"config/production.yaml"},
		},
	}

	md := RenderPlan(plan)

	if !strings.Contains(md, "## Scope") {
		t.Error("missing Scope section")
	}
	if !strings.Contains(md, "`api/`") {
		t.Error("missing include path")
	}
	if !strings.Contains(md, "`vendor/`") {
		t.Error("missing exclude path")
	}
	if !strings.Contains(md, "`config/production.yaml`") {
		t.Error("missing do-not-touch path")
	}
}

func TestIsMilestoneStatus(t *testing.T) {
	tests := []struct {
		status workflow.Status
		want   bool
	}{
		{workflow.StatusDrafted, true},
		{workflow.StatusRequirementsGenerated, true},
		{workflow.StatusScenariosGenerated, true},
		{workflow.StatusReviewed, true},
		{workflow.StatusScenariosReviewed, true},
		{workflow.StatusReadyForExecution, true},
		{workflow.StatusComplete, true},
		// Non-milestones
		{workflow.StatusCreated, false},
		{workflow.StatusDrafting, false},
		{workflow.StatusReviewingDraft, false},
		{workflow.StatusApproved, false},
		{workflow.StatusGeneratingRequirements, false},
		{workflow.StatusGeneratingScenarios, false},
		{workflow.StatusReviewingScenarios, false},
		{workflow.StatusImplementing, false},
		{workflow.StatusReviewingRollup, false},
		{workflow.StatusRejected, false},
		{workflow.StatusArchived, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := isMilestoneStatus(tt.status); got != tt.want {
				t.Errorf("isMilestoneStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
