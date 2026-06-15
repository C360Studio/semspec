package qareviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
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

func TestBuildQAVerdictEvent_ExecutableQAFailClosed(t *testing.T) {
	result := &qaReviewOutput{Verdict: string(workflow.QAVerdictApproved), Summary: "looks fine"}

	t.Run("missing QARun rejects integration", func(t *testing.T) {
		plan := &workflow.Plan{Slug: "mavlink-hard", ID: "plan-1", QALevel: workflow.QALevelIntegration}
		got := buildQAVerdictEvent(plan.Slug, plan, result)
		if got.Verdict != workflow.QAVerdictRejected {
			t.Fatalf("Verdict = %q, want rejected", got.Verdict)
		}
		if !strings.Contains(got.Summary, "no QARun") {
			t.Errorf("Summary should explain missing QARun, got %q", got.Summary)
		}
	})

	t.Run("failing QARun cannot be approved", func(t *testing.T) {
		plan := &workflow.Plan{
			Slug:    "mavlink-hard",
			ID:      "plan-1",
			QALevel: workflow.QALevelIntegration,
			QARun: &workflow.QARun{
				Passed: false,
				Failures: []workflow.QAFailure{
					{JobName: "integration", Message: "gradle test failed"},
				},
			},
		}
		got := buildQAVerdictEvent(plan.Slug, plan, result)
		if got.Verdict != workflow.QAVerdictNeedsChanges {
			t.Fatalf("Verdict = %q, want needs_changes", got.Verdict)
		}
		if !strings.Contains(got.Summary, "gradle test failed") {
			t.Errorf("Summary should include failure evidence, got %q", got.Summary)
		}
	})

	t.Run("passing QARun allows reviewer verdict", func(t *testing.T) {
		plan := &workflow.Plan{
			Slug:    "mavlink-hard",
			ID:      "plan-1",
			QALevel: workflow.QALevelIntegration,
			QARun:   &workflow.QARun{Passed: true},
		}
		got := buildQAVerdictEvent(plan.Slug, plan, result)
		if got.Verdict != workflow.QAVerdictApproved {
			t.Fatalf("Verdict = %q, want approved", got.Verdict)
		}
		if got.Summary != "looks fine" {
			t.Errorf("Summary = %q, want model summary", got.Summary)
		}
	})
}

// TestBuildQAReviewContext_ADR044CapabilityEvidence pins the ADR-044
// release-readiness contract shift: QAReviewContext.Capabilities surfaces
// per-Capability covering Story join + shipped count. A Capability with
// zero CoveringStoryIDs is a coverage gap; a Capability with covering
// Stories but zero ShippedCount is an evidence gap. Both are blocking
// signals for the QA verdict.
func TestBuildQAReviewContext_ADR044CapabilityEvidence(t *testing.T) {
	plan := &workflow.Plan{
		Title: "Mavlink driver",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "mavsdk-lifecycle", Description: "Server boot"},
				{Name: "mavsdk-telemetry", Description: "Telemetry stream"},
				{Name: "uncovered-cap", Description: "No Story covers this"},
			},
		},
		Stories: []workflow.Story{
			{
				ID:              "story.mav.cohesive",
				ComponentName:   "mavsdk-driver",
				RequirementIDs:  []string{"r1", "r2"},
				CapabilityNames: []string{"mavsdk-lifecycle", "mavsdk-telemetry"},
				Title:           "Cohesive driver",
				Status:          workflow.StoryStatusComplete,
			},
		},
	}

	got := buildQAReviewContext(plan)
	if len(got.Capabilities) != 3 {
		t.Fatalf("Capabilities count = %d, want 3", len(got.Capabilities))
	}

	byName := make(map[string]prompt.QACapabilityEvidence)
	for _, c := range got.Capabilities {
		byName[c.Name] = c
	}

	life := byName["mavsdk-lifecycle"]
	if len(life.CoveringStoryIDs) != 1 || life.CoveringStoryIDs[0] != "story.mav.cohesive" {
		t.Errorf("mavsdk-lifecycle covering Stories = %v, want [story.mav.cohesive]", life.CoveringStoryIDs)
	}
	if life.ShippedCount != 1 {
		t.Errorf("mavsdk-lifecycle ShippedCount = %d, want 1 (Story is complete)", life.ShippedCount)
	}

	uncovered := byName["uncovered-cap"]
	if len(uncovered.CoveringStoryIDs) != 0 {
		t.Errorf("uncovered-cap should have 0 covering Stories, got %v", uncovered.CoveringStoryIDs)
	}
	if uncovered.ShippedCount != 0 {
		t.Errorf("uncovered-cap ShippedCount = %d, want 0", uncovered.ShippedCount)
	}

	// Story summary surfaces the M:N coverage join.
	if len(got.Stories) != 1 {
		t.Fatalf("Stories count = %d, want 1", len(got.Stories))
	}
	s := got.Stories[0]
	if len(s.RequirementIDs) != 2 || len(s.CapabilityNames) != 2 {
		t.Errorf("Story summary lost coverage joins: req=%v cap=%v", s.RequirementIDs, s.CapabilityNames)
	}
	if s.ComponentName != "mavsdk-driver" {
		t.Errorf("Story.ComponentName = %q, want mavsdk-driver", s.ComponentName)
	}
}

// TestBuildUserPrompt_ADR044CapabilityRollupSurfacesGaps pins the ADR-044
// QA prompt-surface contract: the rendered user prompt MUST include the
// "Capability evidence rollup" section + ❌ gap marker when a Capability
// has zero covering Stories. A regression that drops this render block
// would silently shift release-readiness back to the pre-ADR-044
// "all requirements complete" gate without operator notice.
func TestBuildUserPrompt_ADR044CapabilityRollupSurfacesGaps(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "demo",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "covered-cap"},
				{Name: "uncovered-cap"},
				{Name: "claimed-unshipped-cap"},
			},
		},
		Stories: []workflow.Story{
			{ID: "s1", CapabilityNames: []string{"covered-cap"}, Status: workflow.StoryStatusComplete},
			{ID: "s2", CapabilityNames: []string{"claimed-unshipped-cap"}, Status: workflow.StoryStatusExecuting},
		},
	}
	got := buildUserPrompt(plan)
	if !strings.Contains(got, "Capability evidence rollup") {
		t.Errorf("user prompt missing 'Capability evidence rollup' section — render regression:\n%s", got)
	}
	if !strings.Contains(got, "uncovered-cap") {
		t.Errorf("user prompt missing uncovered-cap name:\n%s", got)
	}
	if !strings.Contains(got, "❌") {
		t.Errorf("user prompt missing ❌ marker for uncovered Capability:\n%s", got)
	}
	if !strings.Contains(got, "⚠") {
		t.Errorf("user prompt missing ⚠ marker for claimed-but-unshipped Capability:\n%s", got)
	}
}

// TestBuildQAReviewContext_EvidenceGap_ClaimedButUnshipped pins the
// second blocking signal: Capability has covering Story but the Story
// has not reached terminal complete.
func TestBuildQAReviewContext_EvidenceGap_ClaimedButUnshipped(t *testing.T) {
	plan := &workflow.Plan{
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{{Name: "auth"}},
		},
		Stories: []workflow.Story{
			{ID: "s1", CapabilityNames: []string{"auth"}, Status: workflow.StoryStatusExecuting},
		},
	}
	got := buildQAReviewContext(plan)
	if len(got.Capabilities) != 1 {
		t.Fatalf("want 1 capability, got %d", len(got.Capabilities))
	}
	c := got.Capabilities[0]
	if len(c.CoveringStoryIDs) != 1 {
		t.Errorf("CoveringStoryIDs = %v, want [s1]", c.CoveringStoryIDs)
	}
	if c.ShippedCount != 0 {
		t.Errorf("ShippedCount = %d, want 0 (Story executing, not complete)", c.ShippedCount)
	}
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
