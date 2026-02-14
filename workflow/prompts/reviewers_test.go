package prompts

import (
	"strings"
	"testing"
)

func TestSOPReviewerPrompt(t *testing.T) {
	params := SOPReviewerParams{
		SOPContent: "## Error Handling\nAll errors must be wrapped with context using fmt.Errorf.",
		FileDiffs:  "diff --git a/api/handler.go\n+func Handle() error {\n+  return err\n+}",
		FileList:   []string{"api/handler.go", "api/routes.go"},
	}

	prompt := SOPReviewerPrompt(params)

	// Check that prompt contains key elements
	tests := []struct {
		name     string
		contains string
	}{
		{"role identifier", "SOP compliance reviewer"},
		{"focus section", "## Your Focus"},
		{"sop content", params.SOPContent},
		{"file list", "api/handler.go"},
		{"diffs", "diff --git"},
		{"output format", `"role": "sop_reviewer"`},
		{"json format", "Output ONLY valid JSON"},
		{"sop_id field", `"sop_id"`},
		{"severity field", `"severity"`},
		{"line field", `"line"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("prompt should contain %q", tt.contains)
			}
		})
	}

	// Check prompt is not too long (system prompt should be ~500 tokens)
	// Rough estimate: 1 token ~= 4 chars for English
	systemPromptOnly := strings.Split(prompt, params.SOPContent)[0]
	estimatedTokens := len(systemPromptOnly) / 4
	if estimatedTokens > 600 {
		t.Errorf("system prompt too long: ~%d tokens (target: <500)", estimatedTokens)
	}
}

func TestStyleReviewerPrompt(t *testing.T) {
	params := StyleReviewerParams{
		StyleGuide: "## Naming\nUse camelCase for variables, PascalCase for exports.",
		FileDiffs:  "diff --git a/api/handler.go\n+func do_thing() {",
		FileList:   []string{"api/handler.go"},
	}

	prompt := StyleReviewerPrompt(params)

	tests := []struct {
		name     string
		contains string
	}{
		{"role identifier", "code style reviewer"},
		{"focus section", "## Your Focus"},
		{"style guide", params.StyleGuide},
		{"output format", `"role": "style_reviewer"`},
		{"category field", `"category"`},
		{"no critical severity", "never \"critical\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("prompt should contain %q", tt.contains)
			}
		})
	}

	// Verify it excludes other concerns
	if strings.Contains(prompt, "security") && !strings.Contains(prompt, "Do NOT check") {
		t.Error("style prompt should explicitly exclude security checks")
	}
}

func TestSecurityReviewerPrompt(t *testing.T) {
	params := SecurityReviewerParams{
		SecurityChecklist: "## SQL Injection\nAlways use parameterized queries.",
		FileDiffs:         "diff --git a/db/query.go\n+query := \"SELECT * FROM users WHERE id=\" + userID",
		FileList:          []string{"db/query.go"},
	}

	prompt := SecurityReviewerPrompt(params)

	tests := []struct {
		name     string
		contains string
	}{
		{"role identifier", "security reviewer"},
		{"focus section", "## Your Focus"},
		{"checklist", params.SecurityChecklist},
		{"output format", `"role": "security_reviewer"`},
		{"cwe field", `"cwe"`},
		{"injection category", "injection"},
		{"critical severity allowed", "CAN be \"critical\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("prompt should contain %q", tt.contains)
			}
		})
	}
}

func TestReviewerPromptsAreFocused(t *testing.T) {
	// Each reviewer should explicitly state what they do NOT check
	t.Run("SOP excludes other concerns", func(t *testing.T) {
		prompt := SOPReviewerPrompt(SOPReviewerParams{})
		if !strings.Contains(prompt, "Do NOT check style, security") {
			t.Error("SOP reviewer should explicitly exclude style and security")
		}
	})

	t.Run("Style excludes other concerns", func(t *testing.T) {
		prompt := StyleReviewerPrompt(StyleReviewerParams{})
		if !strings.Contains(prompt, "Do NOT check SOP compliance, security") {
			t.Error("Style reviewer should explicitly exclude SOP and security")
		}
	})

	t.Run("Security excludes other concerns", func(t *testing.T) {
		prompt := SecurityReviewerPrompt(SecurityReviewerParams{})
		if !strings.Contains(prompt, "Do NOT check style, SOP compliance") {
			t.Error("Security reviewer should explicitly exclude style and SOP")
		}
	})
}

func TestReviewOutputStructure(t *testing.T) {
	// Verify the output types are properly defined
	output := ReviewOutput{
		Role: "sop_reviewer",
		Findings: []ReviewFinding{
			{
				Severity:   "high",
				File:       "api/handler.go",
				Line:       45,
				Issue:      "Error not wrapped",
				Suggestion: "Use fmt.Errorf",
			},
		},
		Passed:  false,
		Summary: "1 violation found",
	}

	if output.Role != "sop_reviewer" {
		t.Errorf("Role = %q, want sop_reviewer", output.Role)
	}
	if len(output.Findings) != 1 {
		t.Errorf("Findings count = %d, want 1", len(output.Findings))
	}
	if output.Findings[0].Line != 45 {
		t.Errorf("Finding line = %d, want 45", output.Findings[0].Line)
	}
}

func TestMultipleFilesInReview(t *testing.T) {
	params := SOPReviewerParams{
		FileList: []string{
			"api/handler.go",
			"api/routes.go",
			"internal/auth/token.go",
			"internal/auth/middleware.go",
		},
	}

	prompt := SOPReviewerPrompt(params)

	for _, file := range params.FileList {
		if !strings.Contains(prompt, file) {
			t.Errorf("prompt should contain file %q", file)
		}
	}
}
