package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// mdCodeFence is the markdown code fence delimiter used when stripping
// model-emitted JSON wrappers.
const mdCodeFence = "```"

// Severity constants shared across all reviewers.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

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

// ReviewFinding represents a single finding from any reviewer.
type ReviewFinding struct {
	Role       string `json:"role,omitempty"`
	Category   string `json:"category,omitempty"`
	SOPID      string `json:"sop_id,omitempty"`
	Status     string `json:"status,omitempty"`
	Severity   string `json:"severity"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Issue      string `json:"issue"`
	Suggestion string `json:"suggestion"`
	CWE        string `json:"cwe,omitempty"`
}

// ReviewOutput represents the output from any focused reviewer.
type ReviewOutput struct {
	Role     string          `json:"role"`
	Findings []ReviewFinding `json:"findings"`
	Passed   bool            `json:"passed"`
	Summary  string          `json:"summary"`
}

// SpecFinding represents a single spec compliance finding.
type SpecFinding struct {
	Type          string `json:"type"`
	Severity      string `json:"severity"`
	Description   string `json:"description"`
	File          string `json:"file,omitempty"`
	Lines         string `json:"lines,omitempty"`
	SpecReference string `json:"spec_reference,omitempty"`
}

// SpecReviewOutput represents the output from spec compliance review.
type SpecReviewOutput struct {
	Role     string        `json:"role"`
	Verdict  string        `json:"verdict"`
	Findings []SpecFinding `json:"findings"`
	Passed   bool          `json:"passed"`
	Summary  string        `json:"summary"`
}

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

	if o.Passed && o.Verdict != VerdictCompliant {
		return errors.New("passed=true but verdict is not compliant")
	}
	if !o.Passed && o.Verdict == VerdictCompliant {
		return errors.New("verdict=compliant but passed=false")
	}

	if o.Verdict == VerdictCompliant && len(o.Findings) > 0 {
		return errors.New("verdict=compliant but findings are present")
	}

	if o.Verdict != VerdictCompliant && len(o.Findings) == 0 {
		return fmt.Errorf("verdict=%s but no findings present", o.Verdict)
	}

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

	if strings.HasPrefix(jsonStr, mdCodeFence) {
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
