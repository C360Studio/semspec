package prompts

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// mdCodeFence is the markdown code fence delimiter.
const mdCodeFence = "```"

// Spec compliance reviewer for two-stage review workflow.
// This is the Stage 1 gate: "Did you build what was asked?"
// Must pass before quality reviewers (SOP, style, security) run.

// SpecReviewerParams contains parameters for spec compliance review.
type SpecReviewerParams struct {
	SpecContent string   // The task/feature specification
	FileDiffs   string   // Git diff showing what was actually built
	TaskScope   []string // Files that should have been modified
}

// SpecReviewerPrompt returns a prompt for spec compliance review.
// This is the Stage 1 gate in the two-stage review process.
// Context budget: ~500 tokens (system) + ~2K (spec) + ~2K (diffs) = ~4.5K total.
// Note: Actual budget depends on spec and diff sizes passed in params.
func SpecReviewerPrompt(params SpecReviewerParams) string {
	scopeStr := "Not specified - review all changed files"
	if len(params.TaskScope) > 0 {
		scopeStr = "- " + strings.Join(params.TaskScope, "\n- ")
	}

	return fmt.Sprintf(`You are a spec compliance reviewer. Your ONLY job is to verify that the implementation matches the specification exactly.

## Your Focus

Answer ONE question: "Did you build what was asked? Nothing more, nothing less?"

Check for:
1. **Over-building**: Features, abstractions, or code added that were NOT in the spec
2. **Under-building**: Requirements from the spec that are missing or incomplete
3. **Wrong scope**: Files modified that shouldn't have been, or expected files not touched

Do NOT evaluate code quality, style, security, or best practices - other reviewers handle those.

## The Specification

%s

## Expected Scope

Files that should be modified:
%s

## Actual Changes (Git Diff)

%s

## Output Format (REQUIRED)

Output ONLY valid JSON:

%sjson
{
  "role": "spec_reviewer",
  "verdict": "compliant | over_built | under_built | wrong_scope | multiple_issues",
  "findings": [
    {
      "type": "over_built | under_built | wrong_scope",
      "severity": "critical | high | medium",
      "description": "Clear description of the deviation",
      "file": "path/to/file.go (if applicable)",
      "lines": "45-67 (if applicable)",
      "spec_reference": "Section or requirement from spec that was violated/missed"
    }
  ],
  "passed": true | false,
  "summary": "Brief summary of spec compliance status"
}
%s

## Verdicts

- **compliant**: Implementation matches spec exactly
- **over_built**: Added features, abstractions, or code not in spec
- **under_built**: Missing requirements from spec
- **wrong_scope**: Modified files outside expected scope OR didn't modify expected files
- **multiple_issues**: Combination of above issues

## Rules

1. Be STRICT - any deviation from spec is a finding
2. Over-building is as bad as under-building (scope creep wastes effort)
3. "Nice to have" additions are over-building if not in spec
4. Refactoring unrelated code is wrong scope
5. Adding extra error handling beyond spec is over-building
6. Adding tests beyond what spec requires is over-building
7. If spec is ambiguous, note it but don't fail - flag for clarification
8. Compare against the FULL spec, not just headlines
`, params.SpecContent, scopeStr, params.FileDiffs, mdCodeFence, mdCodeFence)
}

// SpecFinding represents a single spec compliance finding.
type SpecFinding struct {
	Type          string `json:"type"`                     // over_built, under_built, wrong_scope
	Severity      string `json:"severity"`                 // critical, high, medium
	Description   string `json:"description"`              // What the issue is
	File          string `json:"file,omitempty"`           // Affected file
	Lines         string `json:"lines,omitempty"`          // Line range
	SpecReference string `json:"spec_reference,omitempty"` // Which spec section
}

// SpecReviewOutput represents the output from spec compliance review.
type SpecReviewOutput struct {
	Role     string        `json:"role"`     // Always "spec_reviewer"
	Verdict  string        `json:"verdict"`  // compliant, over_built, under_built, wrong_scope, multiple_issues
	Findings []SpecFinding `json:"findings"` // List of findings
	Passed   bool          `json:"passed"`   // Gate pass/fail
	Summary  string        `json:"summary"`  // Brief summary
}

// Verdict constants for spec review.
const (
	VerdictCompliant      = "compliant"
	VerdictOverBuilt      = "over_built"
	VerdictUnderBuilt     = "under_built"
	VerdictWrongScope     = "wrong_scope"
	VerdictMultipleIssues = "multiple_issues"
)

// Finding type constants.
const (
	FindingTypeOverBuilt  = "over_built"
	FindingTypeUnderBuilt = "under_built"
	FindingTypeWrongScope = "wrong_scope"
)

// Severity constants shared across all reviewers.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

// Validate checks that the SpecReviewOutput is internally consistent.
func (o *SpecReviewOutput) Validate() error {
	if o.Role != "spec_reviewer" {
		return fmt.Errorf("invalid role: %q, expected spec_reviewer", o.Role)
	}

	validVerdicts := map[string]bool{
		VerdictCompliant:      true,
		VerdictOverBuilt:      true,
		VerdictUnderBuilt:     true,
		VerdictWrongScope:     true,
		VerdictMultipleIssues: true,
	}
	if !validVerdicts[o.Verdict] {
		return fmt.Errorf("invalid verdict: %q", o.Verdict)
	}

	// Invariant: passed should match verdict
	if o.Passed && o.Verdict != VerdictCompliant {
		return errors.New("passed=true but verdict is not compliant")
	}
	if !o.Passed && o.Verdict == VerdictCompliant {
		return errors.New("verdict=compliant but passed=false")
	}

	// Compliant verdict should have no findings
	if o.Verdict == VerdictCompliant && len(o.Findings) > 0 {
		return errors.New("verdict=compliant but findings are present")
	}

	// Non-compliant verdict should have findings
	if o.Verdict != VerdictCompliant && len(o.Findings) == 0 {
		return fmt.Errorf("verdict=%s but no findings present", o.Verdict)
	}

	// For single-issue verdicts, verify at least one matching finding type
	singleIssueVerdicts := map[string]string{
		VerdictOverBuilt:  FindingTypeOverBuilt,
		VerdictUnderBuilt: FindingTypeUnderBuilt,
		VerdictWrongScope: FindingTypeWrongScope,
	}
	if expectedType, ok := singleIssueVerdicts[o.Verdict]; ok {
		hasMatchingType := false
		for _, f := range o.Findings {
			if f.Type == expectedType {
				hasMatchingType = true
				break
			}
		}
		if !hasMatchingType {
			return fmt.Errorf("verdict=%s but no findings of that type", o.Verdict)
		}
	}

	// Validate individual findings
	validTypes := map[string]bool{
		FindingTypeOverBuilt:  true,
		FindingTypeUnderBuilt: true,
		FindingTypeWrongScope: true,
	}
	validSeverities := map[string]bool{
		SeverityCritical: true,
		SeverityHigh:     true,
		SeverityMedium:   true,
		SeverityLow:      true,
	}
	for i, f := range o.Findings {
		if !validTypes[f.Type] {
			return fmt.Errorf("finding %d: invalid type: %q", i, f.Type)
		}
		if !validSeverities[f.Severity] {
			return fmt.Errorf("finding %d: invalid severity: %q", i, f.Severity)
		}
		if strings.TrimSpace(f.Description) == "" {
			return fmt.Errorf("finding %d: description is required", i)
		}
	}

	return nil
}

// ParseSpecReviewOutput parses LLM JSON output into a SpecReviewOutput.
// It handles common formatting issues like markdown code fences.
func ParseSpecReviewOutput(jsonStr string) (*SpecReviewOutput, error) {
	jsonStr = strings.TrimSpace(jsonStr)

	// Strip markdown code fences with any language tag (```json, ```JSON, ```, etc.)
	if strings.HasPrefix(jsonStr, mdCodeFence) {
		// Find end of first line (the opening fence with optional language tag)
		if idx := strings.Index(jsonStr, "\n"); idx != -1 {
			jsonStr = jsonStr[idx+1:]
		}
	}
	jsonStr = strings.TrimSuffix(strings.TrimSpace(jsonStr), mdCodeFence)
	jsonStr = strings.TrimSpace(jsonStr)

	var output SpecReviewOutput
	if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
		return nil, fmt.Errorf("parsing spec review output: %w", err)
	}

	if err := output.Validate(); err != nil {
		return nil, fmt.Errorf("validating spec review output: %w", err)
	}

	return &output, nil
}

// ToReviewFinding converts a SpecFinding to the common ReviewFinding type.
// This enables unified aggregation across all reviewer types.
func (sf SpecFinding) ToReviewFinding() ReviewFinding {
	// Parse line number from range (e.g., "45-67" -> 45)
	line := 0
	if sf.Lines != "" {
		lineStr := sf.Lines
		if dashIdx := strings.Index(sf.Lines, "-"); dashIdx > 0 {
			lineStr = sf.Lines[:dashIdx]
		}
		if n, err := strconv.Atoi(lineStr); err == nil {
			line = n
		}
	}

	// Handle empty spec reference
	suggestion := ""
	if sf.SpecReference != "" {
		suggestion = fmt.Sprintf("See spec reference: %s", sf.SpecReference)
	}

	return ReviewFinding{
		Role:       "spec_reviewer",
		Category:   sf.Type,
		Severity:   sf.Severity,
		File:       sf.File,
		Line:       line,
		Issue:      sf.Description,
		Suggestion: suggestion,
	}
}

// ToReviewOutput converts a SpecReviewOutput to the common ReviewOutput type.
func (o *SpecReviewOutput) ToReviewOutput() ReviewOutput {
	findings := make([]ReviewFinding, len(o.Findings))
	for i, f := range o.Findings {
		findings[i] = f.ToReviewFinding()
	}
	return ReviewOutput{
		Role:     o.Role,
		Findings: findings,
		Passed:   o.Passed,
		Summary:  o.Summary,
	}
}
