package prompts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSpecReviewerPrompt(t *testing.T) {
	params := SpecReviewerParams{
		SpecContent: "## Requirements\n1. Add login endpoint at POST /api/login\n2. Return JWT token on success\n3. Return 401 on invalid credentials",
		FileDiffs:   "diff --git a/api/auth.go\n+func Login(w http.ResponseWriter, r *http.Request) {\n+  // login logic\n+}",
		TaskScope:   []string{"api/auth.go", "api/routes.go"},
	}

	prompt := SpecReviewerPrompt(params)

	tests := []struct {
		name     string
		contains string
	}{
		{"role identifier", "spec compliance reviewer"},
		{"focus question", "Did you build what was asked"},
		{"over-building check", "Over-building"},
		{"under-building check", "Under-building"},
		{"wrong scope check", "Wrong scope"},
		{"spec content", params.SpecContent},
		{"scope files", "api/auth.go"},
		{"diffs", "diff --git"},
		{"output format", `"role": "spec_reviewer"`},
		{"verdict field", `"verdict"`},
		{"type field", `"type"`},
		{"spec_reference field", `"spec_reference"`},
		{"json format", "Output ONLY valid JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("prompt should contain %q", tt.contains)
			}
		})
	}
}

func TestSpecReviewerPromptTokenBudget(t *testing.T) {
	params := SpecReviewerParams{
		SpecContent: "Short spec",
		FileDiffs:   "Short diff",
		TaskScope:   []string{"file.go"},
	}

	prompt := SpecReviewerPrompt(params)

	// Extract system prompt only (before spec content)
	systemPromptOnly := strings.Split(prompt, params.SpecContent)[0]
	estimatedTokens := len(systemPromptOnly) / 4

	if estimatedTokens > 600 {
		t.Errorf("system prompt too long: ~%d tokens (target: <500)", estimatedTokens)
	}
}

func TestSpecReviewerPromptVerdicts(t *testing.T) {
	prompt := SpecReviewerPrompt(SpecReviewerParams{})

	verdicts := []string{
		VerdictCompliant,
		VerdictOverBuilt,
		VerdictUnderBuilt,
		VerdictWrongScope,
		VerdictMultipleIssues,
	}

	for _, verdict := range verdicts {
		if !strings.Contains(prompt, verdict) {
			t.Errorf("prompt should document verdict %q", verdict)
		}
	}
}

func TestSpecReviewerPromptFindingTypes(t *testing.T) {
	prompt := SpecReviewerPrompt(SpecReviewerParams{})

	findingTypes := []string{
		FindingTypeOverBuilt,
		FindingTypeUnderBuilt,
		FindingTypeWrongScope,
	}

	for _, findingType := range findingTypes {
		if !strings.Contains(prompt, findingType) {
			t.Errorf("prompt should document finding type %q", findingType)
		}
	}
}

func TestSpecReviewerPromptExcludesOtherConcerns(t *testing.T) {
	prompt := SpecReviewerPrompt(SpecReviewerParams{})

	// Should explicitly exclude quality concerns
	if !strings.Contains(prompt, "Do NOT evaluate code quality, style, security") {
		t.Error("spec reviewer should explicitly exclude quality concerns")
	}
}

func TestSpecReviewerPromptStrictRules(t *testing.T) {
	prompt := SpecReviewerPrompt(SpecReviewerParams{})

	strictRules := []string{
		"Over-building is as bad as under-building",
		"Nice to have",
		"Refactoring unrelated code",
	}

	for _, rule := range strictRules {
		if !strings.Contains(prompt, rule) {
			t.Errorf("prompt should include strict rule about %q", rule)
		}
	}
}

func TestSpecReviewerPromptNoScope(t *testing.T) {
	// When no scope is provided, should indicate that
	params := SpecReviewerParams{
		SpecContent: "Some spec",
		FileDiffs:   "Some diff",
		TaskScope:   nil, // No scope provided
	}

	prompt := SpecReviewerPrompt(params)

	if !strings.Contains(prompt, "Not specified") {
		t.Error("prompt should indicate when scope is not specified")
	}
}

func TestSpecReviewerPromptMultipleScope(t *testing.T) {
	params := SpecReviewerParams{
		TaskScope: []string{
			"api/auth.go",
			"api/routes.go",
			"internal/token/jwt.go",
			"config/auth.go",
		},
	}

	prompt := SpecReviewerPrompt(params)

	for _, file := range params.TaskScope {
		if !strings.Contains(prompt, file) {
			t.Errorf("prompt should contain scope file %q", file)
		}
	}
}

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
	// Test all finding types are properly defined
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

func TestSpecReviewerCompliantExample(t *testing.T) {
	// Example of a compliant review output
	output := SpecReviewOutput{
		Role:     "spec_reviewer",
		Verdict:  VerdictCompliant,
		Findings: []SpecFinding{}, // Empty - no issues
		Passed:   true,
		Summary:  "Implementation matches specification exactly",
	}

	if !output.Passed {
		t.Error("Compliant review should pass")
	}
	if len(output.Findings) != 0 {
		t.Error("Compliant review should have no findings")
	}
}

func TestSpecReviewerUnderBuiltExample(t *testing.T) {
	output := SpecReviewOutput{
		Role:    "spec_reviewer",
		Verdict: VerdictUnderBuilt,
		Findings: []SpecFinding{
			{
				Type:          FindingTypeUnderBuilt,
				Severity:      "critical",
				Description:   "Missing error handling per spec section 3.2",
				SpecReference: "Section 3.2: All endpoints must return structured errors",
			},
		},
		Passed:  false,
		Summary: "Missing required error handling",
	}

	if output.Passed {
		t.Error("Under-built review should not pass")
	}
	if output.Verdict != VerdictUnderBuilt {
		t.Errorf("Expected verdict %q, got %q", VerdictUnderBuilt, output.Verdict)
	}
}

func TestSpecReviewerWrongScopeExample(t *testing.T) {
	output := SpecReviewOutput{
		Role:    "spec_reviewer",
		Verdict: VerdictWrongScope,
		Findings: []SpecFinding{
			{
				Type:          FindingTypeWrongScope,
				Severity:      "medium",
				Description:   "Modified unrelated configuration file",
				File:          "config/database.go",
				SpecReference: "Task only specified changes to api/auth.go",
			},
		},
		Passed:  false,
		Summary: "Changes made outside expected scope",
	}

	if output.Passed {
		t.Error("Wrong scope review should not pass")
	}
}

func TestSpecReviewerMultipleIssuesExample(t *testing.T) {
	output := SpecReviewOutput{
		Role:    "spec_reviewer",
		Verdict: VerdictMultipleIssues,
		Findings: []SpecFinding{
			{
				Type:        FindingTypeOverBuilt,
				Severity:    "high",
				Description: "Added rate limiting not in spec",
				File:        "api/middleware.go",
				Lines:       "1-50",
			},
			{
				Type:          FindingTypeUnderBuilt,
				Severity:      "critical",
				Description:   "Missing logout endpoint",
				SpecReference: "Requirement 4: POST /api/logout",
			},
		},
		Passed:  false,
		Summary: "Both over-building and under-building detected",
	}

	if output.Passed {
		t.Error("Multiple issues review should not pass")
	}
	if len(output.Findings) != 2 {
		t.Errorf("Expected 2 findings, got %d", len(output.Findings))
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
				Findings: nil, // Nil is ok for compliant
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
				Passed: true, // Should be false
			},
			wantErr: true,
			errMsg:  "passed=true but verdict is not compliant",
		},
		{
			name: "compliant but not passed",
			output: SpecReviewOutput{
				Role:    "spec_reviewer",
				Verdict: VerdictCompliant,
				Passed:  false, // Should be true
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
		SpecReference: "", // Empty
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

func TestSpecReviewerPromptEmptyInputs(t *testing.T) {
	params := SpecReviewerParams{} // All empty
	prompt := SpecReviewerPrompt(params)

	// Should still produce valid prompt
	if !strings.Contains(prompt, "spec compliance reviewer") {
		t.Error("empty params should still produce valid prompt")
	}
	if !strings.Contains(prompt, "Not specified") {
		t.Error("empty scope should show 'Not specified'")
	}
}

func TestSpecReviewerPromptScopeFormatting(t *testing.T) {
	params := SpecReviewerParams{
		TaskScope: []string{"a.go", "b.go", "c.go"},
	}
	prompt := SpecReviewerPrompt(params)

	// Each file should be prefixed with "- "
	if !strings.Contains(prompt, "- a.go") {
		t.Error("first file should be prefixed with '- '")
	}
	if !strings.Contains(prompt, "- b.go") {
		t.Error("middle file should be prefixed with '- '")
	}
	if !strings.Contains(prompt, "- c.go") {
		t.Error("last file should be prefixed with '- '")
	}
}
