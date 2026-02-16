package aggregation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/processor/workflow/aggregation"

	"github.com/c360studio/semspec/workflow/prompts"
)

func TestReviewAggregatorName(t *testing.T) {
	agg := &ReviewAggregator{}
	if got := agg.Name(); got != "review" {
		t.Errorf("Name() = %q, want %q", got, "review")
	}
}

func TestDetermineVerdict(t *testing.T) {
	tests := []struct {
		name         string
		hasCritical  bool
		anyFailed    bool
		findingCount int
		want         string
	}{
		{"all passed no findings", false, false, 0, VerdictApproved},
		{"has critical findings", true, true, 1, VerdictRejected},
		{"some failed no critical", false, true, 3, VerdictNeedsChanges},
		{"all passed with non-critical findings", false, false, 2, VerdictNeedsChanges},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineVerdict(tt.hasCritical, tt.anyFailed, tt.findingCount)
			if got != tt.want {
				t.Errorf("determineVerdict() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{prompts.SeverityCritical, 4},
		{prompts.SeverityHigh, 3},
		{prompts.SeverityMedium, 2},
		{prompts.SeverityLow, 1},
		{"unknown", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			got := severityRank(tt.severity)
			if got != tt.want {
				t.Errorf("severityRank(%q) = %v, want %v", tt.severity, got, tt.want)
			}
		})
	}
}

func TestDeduplicateFindings(t *testing.T) {
	tests := []struct {
		name     string
		findings []prompts.ReviewFinding
		wantLen  int
	}{
		{"empty", nil, 0},
		{
			"no duplicates",
			[]prompts.ReviewFinding{
				{File: "a.go", Line: 10, Issue: "issue 1", Severity: "high"},
				{File: "b.go", Line: 20, Issue: "issue 2", Severity: "medium"},
			},
			2,
		},
		{
			"duplicate same file line issue",
			[]prompts.ReviewFinding{
				{File: "a.go", Line: 10, Issue: "issue 1", Severity: "medium", Role: "sop_reviewer"},
				{File: "a.go", Line: 10, Issue: "issue 1", Severity: "high", Role: "security_reviewer"},
			},
			1,
		},
		{
			"same file different line",
			[]prompts.ReviewFinding{
				{File: "a.go", Line: 10, Issue: "issue 1", Severity: "high"},
				{File: "a.go", Line: 20, Issue: "issue 2", Severity: "medium"},
			},
			2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicateFindings(tt.findings)
			if len(got) != tt.wantLen {
				t.Errorf("deduplicateFindings() returned %d findings, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSortBySeverity(t *testing.T) {
	findings := []prompts.ReviewFinding{
		{File: "c.go", Line: 30, Issue: "low issue", Severity: "low"},
		{File: "a.go", Line: 10, Issue: "critical issue", Severity: "critical"},
		{File: "b.go", Line: 20, Issue: "high issue", Severity: "high"},
		{File: "d.go", Line: 40, Issue: "medium issue", Severity: "medium"},
	}

	sortBySeverity(findings)

	expectedOrder := []string{"critical", "high", "medium", "low"}
	for i, f := range findings {
		if f.Severity != expectedOrder[i] {
			t.Errorf("Position %d: got severity %q, want %q", i, f.Severity, expectedOrder[i])
		}
	}
}

func TestAggregate(t *testing.T) {
	outputs := []prompts.ReviewOutput{
		{
			Role:     "spec_reviewer",
			Passed:   true,
			Summary:  "Spec compliant",
			Findings: []prompts.ReviewFinding{},
		},
		{
			Role:    "sop_reviewer",
			Passed:  false,
			Summary: "SOP violations found",
			Findings: []prompts.ReviewFinding{
				{Role: "sop_reviewer", File: "main.go", Line: 10, Issue: "Missing error handling", Severity: "high"},
			},
		},
		{
			Role:     "style_reviewer",
			Passed:   true,
			Summary:  "Style OK",
			Findings: []prompts.ReviewFinding{},
		},
		{
			Role:     "security_reviewer",
			Passed:   true,
			Summary:  "No security issues",
			Findings: []prompts.ReviewFinding{},
		},
	}

	result := aggregate(outputs)

	if result.Verdict != VerdictNeedsChanges {
		t.Errorf("Verdict = %q, want %q", result.Verdict, VerdictNeedsChanges)
	}
	if result.Passed {
		t.Error("Passed should be false")
	}
	if len(result.Findings) != 1 {
		t.Errorf("len(Findings) = %d, want 1", len(result.Findings))
	}
	if len(result.Reviewers) != 4 {
		t.Errorf("len(Reviewers) = %d, want 4", len(result.Reviewers))
	}
	if result.Stats.ReviewersTotal != 4 {
		t.Errorf("ReviewersTotal = %d, want 4", result.Stats.ReviewersTotal)
	}
	if result.Stats.ReviewersPassed != 3 {
		t.Errorf("ReviewersPassed = %d, want 3", result.Stats.ReviewersPassed)
	}
}

func TestReviewAggregatorAggregate(t *testing.T) {
	agg := &ReviewAggregator{}

	// Create agent results with ReviewOutput payloads
	sopOutput := prompts.ReviewOutput{
		Role:    "sop_reviewer",
		Passed:  true,
		Summary: "SOP OK",
		Findings: []prompts.ReviewFinding{
			{File: "main.go", Line: 10, Issue: "Minor issue", Severity: "low"},
		},
	}
	sopBytes, _ := json.Marshal(sopOutput)

	styleOutput := prompts.ReviewOutput{
		Role:     "style_reviewer",
		Passed:   true,
		Summary:  "Style OK",
		Findings: []prompts.ReviewFinding{},
	}
	styleBytes, _ := json.Marshal(styleOutput)

	results := []aggregation.AgentResult{
		{StepName: "sop_reviewer", Status: "success", Output: sopBytes},
		{StepName: "style_reviewer", Status: "success", Output: styleBytes},
	}

	ctx := context.Background()
	aggResult, err := agg.Aggregate(ctx, results)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}

	if aggResult.AggregatorUsed != "review" {
		t.Errorf("AggregatorUsed = %q, want %q", aggResult.AggregatorUsed, "review")
	}
	if aggResult.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", aggResult.SuccessCount)
	}

	// Parse the output
	var synthesis SynthesisResult
	if err := json.Unmarshal(aggResult.Output, &synthesis); err != nil {
		t.Fatalf("Failed to unmarshal output: %v", err)
	}

	if synthesis.Verdict != VerdictNeedsChanges {
		t.Errorf("Verdict = %q, want %q", synthesis.Verdict, VerdictNeedsChanges)
	}
	if len(synthesis.Findings) != 1 {
		t.Errorf("len(Findings) = %d, want 1", len(synthesis.Findings))
	}
}

func TestReviewAggregatorWithSpecOutput(t *testing.T) {
	agg := &ReviewAggregator{}

	// Create a SpecReviewOutput (should be converted)
	specOutput := prompts.SpecReviewOutput{
		Role:     "spec_reviewer",
		Verdict:  prompts.VerdictCompliant,
		Passed:   true,
		Summary:  "Spec compliant",
		Findings: []prompts.SpecFinding{},
	}
	specBytes, _ := json.Marshal(specOutput)

	results := []aggregation.AgentResult{
		{StepName: "spec_reviewer", Status: "success", Output: specBytes},
	}

	ctx := context.Background()
	aggResult, err := agg.Aggregate(ctx, results)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}

	var synthesis SynthesisResult
	if err := json.Unmarshal(aggResult.Output, &synthesis); err != nil {
		t.Fatalf("Failed to unmarshal output: %v", err)
	}

	if synthesis.Verdict != VerdictApproved {
		t.Errorf("Verdict = %q, want %q", synthesis.Verdict, VerdictApproved)
	}
	if !synthesis.Passed {
		t.Error("Passed should be true")
	}
}

func TestGenerateSummary(t *testing.T) {
	tests := []struct {
		name            string
		totalReviewers  int
		passedReviewers int
		findings        []prompts.ReviewFinding
		bySeverity      map[string]int
		wantContains    string
	}{
		{
			name:            "all passed no findings",
			totalReviewers:  4,
			passedReviewers: 4,
			findings:        []prompts.ReviewFinding{},
			bySeverity:      map[string]int{},
			wantContains:    "all 4 reviewers passed",
		},
		{
			name:            "some failed with findings",
			totalReviewers:  4,
			passedReviewers: 2,
			findings:        []prompts.ReviewFinding{{}, {}},
			bySeverity:      map[string]int{"high": 1, "medium": 1},
			wantContains:    "2/4 reviewers passed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateSummary(tt.totalReviewers, tt.passedReviewers, tt.findings, tt.bySeverity)
			if !contains(got, tt.wantContains) {
				t.Errorf("generateSummary() = %q, want to contain %q", got, tt.wantContains)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
