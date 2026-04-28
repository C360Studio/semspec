package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSpecReviewOutputStructure(t *testing.T) {
	output := SpecReviewOutput{
		Role:    "spec_reviewer",
		Verdict: VerdictOverBuilt,
		Findings: []SpecFinding{
			{
				Type:          FindingTypeOverBuilt,
				Severity:      "high",
				Description:   "Added caching feature not in spec",
				File:          "api/handler.go",
				Lines:         "45-67",
				SpecReference: "No caching requirement in spec",
			},
		},
		Passed:  false,
		Summary: "Implementation adds features not in specification",
	}

	if output.Role != "spec_reviewer" {
		t.Errorf("Role = %q, want spec_reviewer", output.Role)
	}
	if output.Verdict != VerdictOverBuilt {
		t.Errorf("Verdict = %q, want %q", output.Verdict, VerdictOverBuilt)
	}
	if len(output.Findings) != 1 {
		t.Errorf("Findings count = %d, want 1", len(output.Findings))
	}
	if output.Findings[0].Type != FindingTypeOverBuilt {
		t.Errorf("Finding type = %q, want %q", output.Findings[0].Type, FindingTypeOverBuilt)
	}
	if output.Passed {
		t.Error("Passed should be false when there are findings")
	}
}

func TestSpecFindingTypes(t *testing.T) {
	tests := []struct {
		findingType string
		expected    string
	}{
		{FindingTypeOverBuilt, "over_built"},
		{FindingTypeUnderBuilt, "under_built"},
		{FindingTypeWrongScope, "wrong_scope"},
	}
	for _, tt := range tests {
		if tt.findingType != tt.expected {
			t.Errorf("FindingType constant %q != %q", tt.findingType, tt.expected)
		}
	}
}

func TestVerdictConstants(t *testing.T) {
	tests := []struct {
		verdict  string
		expected string
	}{
		{VerdictCompliant, "compliant"},
		{VerdictOverBuilt, "over_built"},
		{VerdictUnderBuilt, "under_built"},
		{VerdictWrongScope, "wrong_scope"},
		{VerdictMultipleIssues, "multiple_issues"},
	}
	for _, tt := range tests {
		if tt.verdict != tt.expected {
			t.Errorf("Verdict constant %q != %q", tt.verdict, tt.expected)
		}
	}
}

func TestSeverityConstants(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{SeverityCritical, "critical"},
		{SeverityHigh, "high"},
		{SeverityMedium, "medium"},
		{SeverityLow, "low"},
	}
	for _, tt := range tests {
		if tt.severity != tt.expected {
			t.Errorf("Severity constant %q != %q", tt.severity, tt.expected)
		}
	}
}

func TestSpecReviewOutputValidate(t *testing.T) {
	tests := []struct {
		name    string
		output  SpecReviewOutput
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid compliant",
			output: SpecReviewOutput{
				Role:     "spec_reviewer",
				Verdict:  VerdictCompliant,
				Findings: []SpecFinding{},
				Passed:   true,
				Summary:  "All good",
			},
			wantErr: false,
		},
		{
			name: "valid compliant with nil findings",
			output: SpecReviewOutput{
				Role:     "spec_reviewer",
				Verdict:  VerdictCompliant,
				Findings: nil,
				Passed:   true,
				Summary:  "All good",
			},
			wantErr: false,
		},
		{
			name: "valid with findings",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictOverBuilt,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: "test"},
				},
				Passed:  false,
				Summary: "Over-built",
			},
			wantErr: false,
		},
		{
			name: "valid multiple_issues verdict",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictMultipleIssues,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: "over"},
					{Type: FindingTypeUnderBuilt, Severity: SeverityMedium, Description: "under"},
				},
				Passed:  false,
				Summary: "Multiple issues",
			},
			wantErr: false,
		},
		{
			name: "invalid role",
			output: SpecReviewOutput{
				Role:    "wrong_role",
				Verdict: VerdictCompliant,
				Passed:  true,
			},
			wantErr: true,
			errMsg:  "invalid role",
		},
		{
			name: "invalid verdict",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: "invalid",
				Passed:  false,
			},
			wantErr: true,
			errMsg:  "invalid verdict",
		},
		{
			name: "passed but not compliant",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictOverBuilt,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: "test"},
				},
				Passed: true,
			},
			wantErr: true,
			errMsg:  "passed=true but verdict is not compliant",
		},
		{
			name: "compliant but not passed",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictCompliant,
				Passed:  false,
			},
			wantErr: true,
			errMsg:  "verdict=compliant but passed=false",
		},
		{
			name: "compliant but has findings",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictCompliant,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: "test"},
				},
				Passed: true,
			},
			wantErr: true,
			errMsg:  "verdict=compliant but findings are present",
		},
		{
			name: "non-compliant but no findings",
			output: SpecReviewOutput{
				Role:     "spec_reviewer",
				Verdict:  VerdictOverBuilt,
				Findings: []SpecFinding{},
				Passed:   false,
			},
			wantErr: true,
			errMsg:  "no findings present",
		},
		{
			name: "verdict type mismatch",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictOverBuilt,
				Findings: []SpecFinding{
					{Type: FindingTypeUnderBuilt, Severity: SeverityHigh, Description: "wrong type"},
				},
				Passed: false,
			},
			wantErr: true,
			errMsg:  "no findings of that type",
		},
		{
			name: "invalid finding type",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictMultipleIssues,
				Findings: []SpecFinding{
					{Type: "invalid_type", Severity: SeverityHigh, Description: "test"},
				},
				Passed: false,
			},
			wantErr: true,
			errMsg:  "invalid type",
		},
		{
			name: "invalid finding severity",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictMultipleIssues,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: "invalid", Description: "test"},
				},
				Passed: false,
			},
			wantErr: true,
			errMsg:  "invalid severity",
		},
		{
			name: "empty description",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictOverBuilt,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: ""},
				},
				Passed: false,
			},
			wantErr: true,
			errMsg:  "description is required",
		},
		{
			name: "whitespace only description",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictOverBuilt,
				Findings: []SpecFinding{
					{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: "   "},
				},
				Passed: false,
			},
			wantErr: true,
			errMsg:  "description is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.output.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseSpecReviewOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "valid json",
			input: `{
				"role": "spec_reviewer",
				"verdict": "compliant",
				"findings": [],
				"passed": true,
				"summary": "All good"
			}`,
			wantErr: false,
		},
		{
			name: "with markdown fences",
			input: "```json\n" + `{
				"role": "spec_reviewer",
				"verdict": "compliant",
				"findings": [],
				"passed": true,
				"summary": "All good"
			}` + "\n```",
			wantErr: false,
		},
		{
			name: "with uppercase JSON fence",
			input: "```JSON\n" + `{
				"role": "spec_reviewer",
				"verdict": "compliant",
				"findings": [],
				"passed": true,
				"summary": "All good"
			}` + "\n```",
			wantErr: false,
		},
		{
			name: "with plain fence no language",
			input: "```\n" + `{
				"role": "spec_reviewer",
				"verdict": "compliant",
				"findings": [],
				"passed": true,
				"summary": "All good"
			}` + "\n```",
			wantErr: false,
		},
		{
			name: "whitespace surrounded valid json",
			input: `
	{
		"role": "spec_reviewer",
		"verdict": "compliant",
		"findings": [],
		"passed": true,
		"summary": "ok"
	}
	`,
			wantErr: false,
		},
		{
			name:    "whitespace only",
			input:   "   \n\t  ",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{not valid json}`,
			wantErr: true,
		},
		{
			name: "valid json but invalid output",
			input: `{
				"role": "wrong_role",
				"verdict": "compliant",
				"passed": true
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ParseSpecReviewOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if output == nil {
					t.Error("expected output, got nil")
				}
			}
		})
	}
}

func TestSpecReviewOutputJSONRoundtrip(t *testing.T) {
	original := SpecReviewOutput{
		Role:    "spec_reviewer",
		Verdict: VerdictOverBuilt,
		Findings: []SpecFinding{
			{
				Type:          FindingTypeOverBuilt,
				Severity:      SeverityHigh,
				Description:   "Added caching",
				File:          "api/handler.go",
				Lines:         "45-67",
				SpecReference: "Not in spec",
			},
		},
		Passed:  false,
		Summary: "Over-built",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed SpecReviewOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Verdict != original.Verdict {
		t.Errorf("verdict mismatch: got %q, want %q", parsed.Verdict, original.Verdict)
	}
	if len(parsed.Findings) != len(original.Findings) {
		t.Errorf("findings count mismatch: got %d, want %d", len(parsed.Findings), len(original.Findings))
	}
	if parsed.Findings[0].Type != original.Findings[0].Type {
		t.Errorf("finding type mismatch")
	}
}

func TestSpecFindingToReviewFinding(t *testing.T) {
	sf := SpecFinding{
		Type:          FindingTypeOverBuilt,
		Severity:      SeverityHigh,
		Description:   "Added caching feature",
		File:          "api/handler.go",
		Lines:         "45-67",
		SpecReference: "Caching was not specified",
	}

	rf := sf.ToReviewFinding()

	if rf.Role != "spec_reviewer" {
		t.Errorf("Role = %q, want spec_reviewer", rf.Role)
	}
	if rf.Category != FindingTypeOverBuilt {
		t.Errorf("Category = %q, want %q", rf.Category, FindingTypeOverBuilt)
	}
	if rf.Severity != SeverityHigh {
		t.Errorf("Severity = %q, want %q", rf.Severity, SeverityHigh)
	}
	if rf.File != "api/handler.go" {
		t.Errorf("File = %q, want api/handler.go", rf.File)
	}
	if rf.Line != 45 {
		t.Errorf("Line = %d, want 45 (parsed from '45-67')", rf.Line)
	}
	if rf.Issue != "Added caching feature" {
		t.Errorf("Issue = %q, want 'Added caching feature'", rf.Issue)
	}
	if !strings.Contains(rf.Suggestion, "Caching was not specified") {
		t.Errorf("Suggestion should contain spec reference")
	}
}

func TestSpecFindingToReviewFindingLineParsingEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		lines    string
		wantLine int
	}{
		{"range", "45-67", 45},
		{"single line", "123", 123},
		{"empty", "", 0},
		{"invalid", "abc", 0},
		{"range with invalid start", "abc-67", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sf := SpecFinding{
				Type:        FindingTypeOverBuilt,
				Severity:    SeverityHigh,
				Description: "test",
				Lines:       tt.lines,
			}
			rf := sf.ToReviewFinding()
			if rf.Line != tt.wantLine {
				t.Errorf("Line = %d, want %d for lines=%q", rf.Line, tt.wantLine, tt.lines)
			}
		})
	}
}

func TestSpecFindingToReviewFindingEmptySpecReference(t *testing.T) {
	sf := SpecFinding{
		Type:          FindingTypeOverBuilt,
		Severity:      SeverityHigh,
		Description:   "Added feature",
		SpecReference: "",
	}

	rf := sf.ToReviewFinding()

	if rf.Suggestion != "" {
		t.Errorf("Suggestion should be empty when SpecReference is empty, got %q", rf.Suggestion)
	}
}

func TestSpecReviewOutputToReviewOutput(t *testing.T) {
	spec := &SpecReviewOutput{
		Role:    "spec_reviewer",
		Verdict: VerdictOverBuilt,
		Findings: []SpecFinding{
			{Type: FindingTypeOverBuilt, Severity: SeverityHigh, Description: "test1"},
			{Type: FindingTypeUnderBuilt, Severity: SeverityCritical, Description: "test2"},
		},
		Passed:  false,
		Summary: "Multiple issues",
	}

	review := spec.ToReviewOutput()

	if review.Role != "spec_reviewer" {
		t.Errorf("Role = %q, want spec_reviewer", review.Role)
	}
	if len(review.Findings) != 2 {
		t.Errorf("Findings count = %d, want 2", len(review.Findings))
	}
	if review.Passed != false {
		t.Error("Passed should be false")
	}
	if review.Summary != "Multiple issues" {
		t.Errorf("Summary = %q, want 'Multiple issues'", review.Summary)
	}
}
