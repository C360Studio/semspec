package qareviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestIsValidQAVerdict(t *testing.T) {
	tests := []struct {
		verdict string
		want    bool
	}{
		{"approved", true},
		{"needs_changes", true},
		{"rejected", true},
		{"", false},
		{"unknown", false},
		{"Approved", false}, // case-sensitive
		{"NEEDS_CHANGES", false},
	}
	for _, tt := range tests {
		t.Run(tt.verdict, func(t *testing.T) {
			if got := isValidQAVerdict(tt.verdict); got != tt.want {
				t.Errorf("isValidQAVerdict(%q) = %v, want %v", tt.verdict, got, tt.want)
			}
		})
	}
}

func TestParseQAReviewResult(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantErr       bool
		wantErrSub    string
		wantVerdict   string
		wantSummary   string
		wantCPCount   int
		wantFulfilled string
	}{
		{
			name:       "empty result errors",
			input:      "",
			wantErr:    true,
			wantErrSub: "empty",
		},
		{
			name:        "direct JSON parse approved verdict",
			input:       `{"verdict":"approved","summary":"all good"}`,
			wantVerdict: "approved",
			wantSummary: "all good",
		},
		{
			name:          "direct JSON parse needs_changes with dimensions",
			input:         `{"verdict":"needs_changes","summary":"gap","dimensions":{"requirement_fulfillment":"2 of 3 satisfied"}}`,
			wantVerdict:   "needs_changes",
			wantSummary:   "gap",
			wantFulfilled: "2 of 3 satisfied",
		},
		{
			name:        "JSON embedded in preamble is extracted",
			input:       "Here is my verdict:\n" + `{"verdict":"rejected","summary":"critical issue"}`,
			wantVerdict: "rejected",
			wantSummary: "critical issue",
		},
		{
			name:        "JSON with change proposals array",
			input:       `{"verdict":"needs_changes","summary":"fix x","plan_decisions":[{"title":"fix","rationale":"r","affected_requirement_ids":["r-1"],"rejection_type":"fixable"}]}`,
			wantVerdict: "needs_changes",
			wantSummary: "fix x",
			wantCPCount: 1,
		},
		{
			name:       "invalid verdict enum errors",
			input:      `{"verdict":"maybe","summary":"..."}`,
			wantErr:    true,
			wantErrSub: "invalid qa verdict",
		},
		{
			name:       "no JSON object errors",
			input:      "hello world with no braces",
			wantErr:    true,
			wantErrSub: "no JSON found",
		},
		{
			name:       "malformed JSON after brace errors",
			input:      `{"verdict":"approved"`,
			wantErr:    true,
			wantErrSub: "malformed JSON",
		},
		{
			name:       "JSON-like fragment but invalid syntax errors",
			input:      `{"verdict":, "summary":"x"}`,
			wantErr:    true,
			wantErrSub: "parse qa-review JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseQAReviewResult(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%+v)", got)
				}
				if tt.wantErrSub != "" && !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Errorf("error %q missing %q", err.Error(), tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Verdict != tt.wantVerdict {
				t.Errorf("Verdict = %q, want %q", got.Verdict, tt.wantVerdict)
			}
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if len(got.PlanDecisions) != tt.wantCPCount {
				t.Errorf("PlanDecisions count = %d, want %d", len(got.PlanDecisions), tt.wantCPCount)
			}
			if tt.wantFulfilled != "" && got.Dimensions.RequirementFulfillment != tt.wantFulfilled {
				t.Errorf("RequirementFulfillment = %q, want %q",
					got.Dimensions.RequirementFulfillment, tt.wantFulfilled)
			}
		})
	}
}

func TestBuildQAReviewContext(t *testing.T) {
	plan := &workflow.Plan{
		Title:   "Test Plan",
		Goal:    "Add goodbye endpoint",
		QALevel: workflow.QALevelUnit,
		Requirements: []workflow.Requirement{
			{Title: "Active req", Status: workflow.RequirementStatusActive},
			{Title: "Deprecated req", Status: workflow.RequirementStatusDeprecated}, // filtered out
			{Title: "Another active", Status: workflow.RequirementStatusActive},
		},
		Architecture: &workflow.ArchitectureDocument{
			TestSurface: &workflow.TestSurface{
				IntegrationFlows: []workflow.IntegrationFlow{
					{Name: "goodbye-api", Description: "exercises /goodbye"},
				},
			},
		},
	}

	t.Run("with QARun populates all fields", func(t *testing.T) {
		plan.QARun = &workflow.QARun{
			Passed: false,
			Failures: []workflow.QAFailure{
				{JobName: "test", Message: "assertion failed"},
			},
			Artifacts: []workflow.QAArtifactRef{
				{Path: ".semspec/qa-artifacts/run-1/log", Type: "log"},
			},
			RunnerError: "",
		}
		got := buildQAReviewContext(plan)
		plan.QARun = nil

		if got.PlanTitle != "Test Plan" {
			t.Errorf("PlanTitle = %q", got.PlanTitle)
		}
		if got.PlanGoal != "Add goodbye endpoint" {
			t.Errorf("PlanGoal = %q", got.PlanGoal)
		}
		if got.QALevel != workflow.QALevelUnit {
			t.Errorf("QALevel = %q", got.QALevel)
		}
		if len(got.Requirements) != 2 {
			t.Errorf("Requirements count = %d, want 2 (deprecated filtered out)", len(got.Requirements))
		}
		if got.TestSurface == nil {
			t.Fatal("TestSurface should be populated from plan.Architecture")
		}
		if len(got.TestSurface.IntegrationFlows) != 1 {
			t.Errorf("IntegrationFlows count = %d, want 1", len(got.TestSurface.IntegrationFlows))
		}
		if got.Passed {
			t.Error("Passed should be false from QARun")
		}
		if len(got.Failures) != 1 {
			t.Errorf("Failures count = %d, want 1", len(got.Failures))
		}
		if len(got.Artifacts) != 1 {
			t.Errorf("Artifacts count = %d, want 1", len(got.Artifacts))
		}
	})

	t.Run("without QARun leaves runtime fields empty", func(t *testing.T) {
		got := buildQAReviewContext(plan)

		if got.Passed {
			t.Error("Passed should default to false when QARun is nil")
		}
		if len(got.Failures) != 0 {
			t.Errorf("Failures should be empty when QARun is nil, got %d", len(got.Failures))
		}
		if got.TestSurface == nil {
			t.Error("TestSurface should still come from plan.Architecture")
		}
	})

	t.Run("plan without architecture leaves TestSurface nil", func(t *testing.T) {
		bare := &workflow.Plan{Title: "bare", QALevel: workflow.QALevelSynthesis}
		got := buildQAReviewContext(bare)

		if got.TestSurface != nil {
			t.Error("TestSurface should be nil when plan has no Architecture")
		}
	})

	t.Run("runner error propagates", func(t *testing.T) {
		plan.QARun = &workflow.QARun{RunnerError: "act not in PATH"}
		got := buildQAReviewContext(plan)
		plan.QARun = nil

		if got.RunnerError != "act not in PATH" {
			t.Errorf("RunnerError = %q, want %q", got.RunnerError, "act not in PATH")
		}
	})
}

func TestBuildUserPrompt(t *testing.T) {
	plan := &workflow.Plan{
		Slug:    "test-plan",
		QALevel: workflow.QALevelUnit,
	}

	t.Run("passing QARun emits PASSED line", func(t *testing.T) {
		plan.QARun = &workflow.QARun{Passed: true}
		got := buildUserPrompt(plan)
		plan.QARun = nil

		if !strings.Contains(got, "test-plan") {
			t.Errorf("prompt missing slug: %q", got)
		}
		if !strings.Contains(got, "QA level: unit") {
			t.Errorf("prompt missing QA level: %q", got)
		}
		if !strings.Contains(got, "Test execution: PASSED") {
			t.Errorf("prompt missing PASSED line: %q", got)
		}
	})

	t.Run("failing QARun reports failure count", func(t *testing.T) {
		plan.QARun = &workflow.QARun{
			Passed:   false,
			Failures: make([]workflow.QAFailure, 3),
		}
		got := buildUserPrompt(plan)
		plan.QARun = nil
		if !strings.Contains(got, "FAILED (3 failures)") {
			t.Errorf("prompt missing failure count: %q", got)
		}
	})

	t.Run("runner error surfaces in prompt", func(t *testing.T) {
		plan.QARun = &workflow.QARun{Passed: false, RunnerError: "act not in PATH"}
		got := buildUserPrompt(plan)
		plan.QARun = nil
		if !strings.Contains(got, "Runner error: act not in PATH") {
			t.Errorf("prompt missing runner error: %q", got)
		}
	})

	t.Run("synthesis level with nil QARun omits warning", func(t *testing.T) {
		synthPlan := &workflow.Plan{Slug: "s", QALevel: workflow.QALevelSynthesis}
		got := buildUserPrompt(synthPlan)
		if strings.Contains(got, "Warning:") {
			t.Errorf("synthesis level should not emit unavailability warning: %q", got)
		}
	})

	t.Run("unit level with nil QARun emits warning", func(t *testing.T) {
		got := buildUserPrompt(plan)
		if !strings.Contains(got, "Warning") {
			t.Errorf("unit level with nil QARun should emit warning: %q", got)
		}
	})
}
