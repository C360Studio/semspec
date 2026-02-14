package prompts

import (
	"fmt"
	"strings"
)

// Focused reviewer prompts for parallel multi-agent review.
// These are designed for ~4K token context budgets on local LLMs.
// The existing ReviewerPrompt() is kept for single-agent mode.

// SOPReviewerParams contains parameters for SOP review.
type SOPReviewerParams struct {
	SOPContent string   // The applicable SOP content
	FileDiffs  string   // Git diff of changed files
	FileList   []string // List of files being reviewed
}

// SOPReviewerPrompt returns a focused prompt for SOP compliance review.
// Context budget: ~500 tokens (system) + ~1.5K (SOP) + ~2K (diffs) = ~4K total.
func SOPReviewerPrompt(params SOPReviewerParams) string {
	fileListStr := strings.Join(params.FileList, "\n- ")

	return fmt.Sprintf(`You are an SOP compliance reviewer. Your ONLY job is to verify code follows the provided Standard Operating Procedures.

## Your Focus

Check ONLY for SOP compliance:
- Required patterns and practices
- Error handling standards
- Logging requirements
- Testing requirements
- Documentation requirements

Do NOT check style, security, or other concerns - other reviewers handle those.

## SOP Document

%s

## Files Under Review

- %s

## Changes to Review

%s

## Output Format (REQUIRED)

Output ONLY valid JSON:

%sjson
{
  "role": "sop_reviewer",
  "findings": [
    {
      "sop_id": "string - which SOP section violated",
      "status": "violated | passed | not_applicable",
      "severity": "critical | high | medium | low",
      "file": "path/to/file.go",
      "line": 45,
      "issue": "Specific description of the violation",
      "suggestion": "How to fix it"
    }
  ],
  "passed": true | false,
  "summary": "Brief summary of SOP compliance status"
}
%s

## Rules

1. ONLY check against the provided SOP document
2. Every finding MUST reference a specific SOP section
3. Include line numbers for all violations
4. If no violations found, return empty findings array with passed=true
5. Be specific and actionable in suggestions
`, params.SOPContent, fileListStr, params.FileDiffs, "```", "```")
}

// StyleReviewerParams contains parameters for style review.
type StyleReviewerParams struct {
	StyleGuide string   // The style guide content
	FileDiffs  string   // Git diff of changed files
	FileList   []string // List of files being reviewed
}

// StyleReviewerPrompt returns a focused prompt for code style review.
// Context budget: ~500 tokens (system) + ~1.5K (guide) + ~2K (diffs) = ~4K total.
func StyleReviewerPrompt(params StyleReviewerParams) string {
	fileListStr := strings.Join(params.FileList, "\n- ")

	return fmt.Sprintf(`You are a code style reviewer. Your ONLY job is to verify code follows the provided style guide.

## Your Focus

Check ONLY for style compliance:
- Naming conventions (variables, functions, types)
- Code organization and structure
- Comment and documentation style
- Formatting consistency
- Import organization

Do NOT check SOP compliance, security, or logic - other reviewers handle those.

## Style Guide

%s

## Files Under Review

- %s

## Changes to Review

%s

## Output Format (REQUIRED)

Output ONLY valid JSON:

%sjson
{
  "role": "style_reviewer",
  "findings": [
    {
      "category": "naming | formatting | organization | documentation | imports",
      "severity": "high | medium | low",
      "file": "path/to/file.go",
      "line": 12,
      "issue": "Specific description of the style issue",
      "suggestion": "How to fix it"
    }
  ],
  "passed": true | false,
  "summary": "Brief summary of style compliance"
}
%s

## Rules

1. ONLY check against the provided style guide
2. Categorize each finding appropriately
3. Include line numbers for all issues
4. Style issues are never "critical" severity
5. If no issues found, return empty findings array with passed=true
6. Be specific about what naming/style convention was violated
`, params.StyleGuide, fileListStr, params.FileDiffs, "```", "```")
}

// SecurityReviewerParams contains parameters for security review.
type SecurityReviewerParams struct {
	SecurityChecklist string   // The security checklist content
	FileDiffs         string   // Git diff of changed files
	FileList          []string // List of files being reviewed
}

// SecurityReviewerPrompt returns a focused prompt for security review.
// Context budget: ~500 tokens (system) + ~1K (checklist) + ~2K (diffs) = ~3.5K total.
func SecurityReviewerPrompt(params SecurityReviewerParams) string {
	fileListStr := strings.Join(params.FileList, "\n- ")

	return fmt.Sprintf(`You are a security reviewer. Your ONLY job is to identify security vulnerabilities in code changes.

## Your Focus

Check ONLY for security issues:
- Input validation and sanitization
- SQL/command/code injection risks
- Authentication and authorization flaws
- Secrets and credential handling
- Cryptographic issues
- Path traversal risks
- Unsafe deserialization

Do NOT check style, SOP compliance, or general code quality - other reviewers handle those.

## Security Checklist

%s

## Files Under Review

- %s

## Changes to Review

%s

## Output Format (REQUIRED)

Output ONLY valid JSON:

%sjson
{
  "role": "security_reviewer",
  "findings": [
    {
      "category": "injection | auth | secrets | crypto | traversal | validation | other",
      "severity": "critical | high | medium | low",
      "file": "path/to/file.go",
      "line": 89,
      "issue": "Specific description of the security vulnerability",
      "suggestion": "How to fix it",
      "cwe": "CWE-89 (optional - if applicable)"
    }
  ],
  "passed": true | false,
  "summary": "Brief summary of security status"
}
%s

## Rules

1. Security issues CAN be "critical" - use appropriately
2. Always include line numbers
3. Reference CWE IDs when applicable
4. If no vulnerabilities found, return empty findings array with passed=true
5. Err on the side of caution - flag potential issues
6. Be specific about the attack vector and impact
`, params.SecurityChecklist, fileListStr, params.FileDiffs, "```", "```")
}

// ReviewFinding represents a single finding from any reviewer.
type ReviewFinding struct {
	Role       string `json:"role,omitempty"`        // Which reviewer found this
	Category   string `json:"category,omitempty"`    // Category of finding
	SOPID      string `json:"sop_id,omitempty"`      // For SOP findings
	Status     string `json:"status,omitempty"`      // violated, passed, not_applicable
	Severity   string `json:"severity"`              // critical, high, medium, low
	File       string `json:"file"`                  // File path
	Line       int    `json:"line"`                  // Line number
	Issue      string `json:"issue"`                 // Description
	Suggestion string `json:"suggestion"`            // How to fix
	CWE        string `json:"cwe,omitempty"`         // CWE ID for security
}

// ReviewOutput represents the output from any focused reviewer.
type ReviewOutput struct {
	Role     string          `json:"role"`
	Findings []ReviewFinding `json:"findings"`
	Passed   bool            `json:"passed"`
	Summary  string          `json:"summary"`
}
