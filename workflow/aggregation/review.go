// Package aggregation provides custom aggregators for semspec workflows.
//
// These types were previously imported from semstreams processor/workflow/aggregation
// which was removed when the old workflow engine was replaced by the reactive engine.
package aggregation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semspec/workflow/prompts"
)

// AgentResult represents the result from a single agent step in a parallel workflow.
type AgentResult struct {
	StepName string          `json:"step_name"`
	Status   string          `json:"status"` // "success" or "failed"
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// AggregatedResult represents the combined output from all parallel agents.
type AggregatedResult struct {
	Success        bool            `json:"success"`
	Output         json.RawMessage `json:"output"`
	FailedSteps    []string        `json:"failed_steps,omitempty"`
	SuccessCount   int             `json:"success_count"`
	FailureCount   int             `json:"failure_count"`
	MergedErrors   string          `json:"merged_errors,omitempty"`
	AggregatorUsed string          `json:"aggregator_used"`
}

// Aggregator combines multiple agent results into a single result.
type Aggregator interface {
	Name() string
	Aggregate(ctx context.Context, results []AgentResult) (*AggregatedResult, error)
}

// Registry holds named aggregators.
type Registry struct {
	aggregators map[string]Aggregator
}

// NewRegistry creates a new aggregator registry.
func NewRegistry() *Registry {
	return &Registry{aggregators: make(map[string]Aggregator)}
}

// Register adds an aggregator to the registry.
func (r *Registry) Register(a Aggregator) {
	r.aggregators[a.Name()] = a
}

// DefaultRegistry is the global aggregator registry.
var DefaultRegistry = NewRegistry()

// Verdict constants for review results.
const (
	VerdictApproved     = "approved"
	VerdictRejected     = "rejected"
	VerdictNeedsChanges = "needs_changes"
)

// ReviewAggregator aggregates parallel reviewer outputs into a unified review result.
// It implements the semstreams aggregation.Aggregator interface.
type ReviewAggregator struct{}

// Name returns the aggregator name for registry lookup.
func (a *ReviewAggregator) Name() string {
	return "review"
}

// Aggregate combines multiple reviewer outputs into a single result.
func (a *ReviewAggregator) Aggregate(ctx context.Context, results []AgentResult) (*AggregatedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Parse reviewer outputs
	var outputs []prompts.ReviewOutput
	for _, r := range results {
		if r.Status != "success" {
			continue
		}

		var output prompts.ReviewOutput
		if err := json.Unmarshal(r.Output, &output); err != nil {
			// Try parsing as SpecReviewOutput and convert
			var specOutput prompts.SpecReviewOutput
			if specErr := json.Unmarshal(r.Output, &specOutput); specErr == nil {
				output = specOutput.ToReviewOutput()
			} else {
				continue // Skip unparseable outputs
			}
		}

		// Use step name as role if not set
		if output.Role == "" {
			output.Role = r.StepName
		}

		outputs = append(outputs, output)
	}

	// Aggregate the outputs
	synthesisResult := aggregate(outputs)

	// Count success/failure
	successCount, failureCount, failedSteps := countResults(results)

	// Marshal synthesis result
	outputBytes, err := json.Marshal(synthesisResult)
	if err != nil {
		return nil, fmt.Errorf("marshal synthesis result: %w", err)
	}

	return &AggregatedResult{
		Success:        synthesisResult.Passed,
		Output:         outputBytes,
		FailedSteps:    failedSteps,
		SuccessCount:   successCount,
		FailureCount:   failureCount,
		MergedErrors:   collectErrors(results),
		AggregatorUsed: "review",
	}, nil
}

// SynthesisResult is the aggregated output from all reviewers.
type SynthesisResult struct {
	Verdict   string                  `json:"verdict"`
	Passed    bool                    `json:"passed"`
	Findings  []prompts.ReviewFinding `json:"findings"`
	Reviewers []ReviewerSummary       `json:"reviewers"`
	Summary   string                  `json:"summary"`
	Stats     SynthesisStats          `json:"stats"`
}

// ReviewerSummary contains a summary from a single reviewer.
type ReviewerSummary struct {
	Role         string `json:"role"`
	Passed       bool   `json:"passed"`
	Summary      string `json:"summary"`
	FindingCount int    `json:"finding_count"`
}

// SynthesisStats contains aggregation statistics.
type SynthesisStats struct {
	TotalFindings   int            `json:"total_findings"`
	BySeverity      map[string]int `json:"by_severity"`
	ByReviewer      map[string]int `json:"by_reviewer"`
	ReviewersTotal  int            `json:"reviewers_total"`
	ReviewersPassed int            `json:"reviewers_passed"`
}

// aggregate combines multiple reviewer outputs into a synthesis result.
func aggregate(outputs []prompts.ReviewOutput) *SynthesisResult {
	var allFindings []prompts.ReviewFinding
	reviewerSummaries := make([]ReviewerSummary, 0, len(outputs))
	bySeverity := make(map[string]int)
	byReviewer := make(map[string]int)

	reviewersPassed := 0
	hasCritical := false
	anyFailed := false

	for _, output := range outputs {
		summary := ReviewerSummary{
			Role:         output.Role,
			Passed:       output.Passed,
			Summary:      output.Summary,
			FindingCount: len(output.Findings),
		}
		reviewerSummaries = append(reviewerSummaries, summary)

		if output.Passed {
			reviewersPassed++
		} else {
			anyFailed = true
		}

		for _, finding := range output.Findings {
			if finding.Role == "" {
				finding.Role = output.Role
			}
			allFindings = append(allFindings, finding)
			bySeverity[finding.Severity]++
			byReviewer[output.Role]++

			if finding.Severity == prompts.SeverityCritical {
				hasCritical = true
			}
		}
	}

	// Deduplicate and sort findings
	dedupedFindings := deduplicateFindings(allFindings)
	sortBySeverity(dedupedFindings)

	// Determine verdict
	verdict := determineVerdict(hasCritical, anyFailed, len(dedupedFindings))
	passed := verdict == VerdictApproved

	// Generate summary
	summaryText := generateSummary(len(outputs), reviewersPassed, dedupedFindings, bySeverity)

	return &SynthesisResult{
		Verdict:   verdict,
		Passed:    passed,
		Findings:  dedupedFindings,
		Reviewers: reviewerSummaries,
		Summary:   summaryText,
		Stats: SynthesisStats{
			TotalFindings:   len(dedupedFindings),
			BySeverity:      bySeverity,
			ByReviewer:      byReviewer,
			ReviewersTotal:  len(outputs),
			ReviewersPassed: reviewersPassed,
		},
	}
}

func determineVerdict(hasCritical, anyFailed bool, findingCount int) string {
	if hasCritical {
		return VerdictRejected
	}
	if anyFailed || findingCount > 0 {
		return VerdictNeedsChanges
	}
	return VerdictApproved
}

func deduplicateFindings(findings []prompts.ReviewFinding) []prompts.ReviewFinding {
	if len(findings) == 0 {
		return findings
	}

	type dedupEntry struct {
		finding prompts.ReviewFinding
		roles   []string
	}

	groups := make(map[string]*dedupEntry)

	for _, f := range findings {
		key := dedupKey(f)
		existing, ok := groups[key]
		if !ok {
			groups[key] = &dedupEntry{
				finding: f,
				roles:   []string{f.Role},
			}
		} else {
			if severityRank(f.Severity) > severityRank(existing.finding.Severity) {
				existing.finding.Severity = f.Severity
			}
			if f.Suggestion != "" && f.Suggestion != existing.finding.Suggestion {
				if existing.finding.Suggestion != "" {
					existing.finding.Suggestion += "; " + f.Suggestion
				} else {
					existing.finding.Suggestion = f.Suggestion
				}
			}
			existing.roles = append(existing.roles, f.Role)
		}
	}

	result := make([]prompts.ReviewFinding, 0, len(groups))
	for _, entry := range groups {
		if len(entry.roles) > 1 {
			entry.finding.Role = strings.Join(entry.roles, ", ")
		}
		result = append(result, entry.finding)
	}

	return result
}

func dedupKey(f prompts.ReviewFinding) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(f.Issue))))
	issueHash := fmt.Sprintf("%x", h[:8])
	return fmt.Sprintf("%s:%d:%s", f.File, f.Line, issueHash)
}

func severityRank(severity string) int {
	switch severity {
	case prompts.SeverityCritical:
		return 4
	case prompts.SeverityHigh:
		return 3
	case prompts.SeverityMedium:
		return 2
	case prompts.SeverityLow:
		return 1
	default:
		return 0
	}
}

func sortBySeverity(findings []prompts.ReviewFinding) {
	sort.Slice(findings, func(i, j int) bool {
		ri := severityRank(findings[i].Severity)
		rj := severityRank(findings[j].Severity)
		if ri != rj {
			return ri > rj
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
}

func generateSummary(totalReviewers, passedReviewers int, findings []prompts.ReviewFinding, bySeverity map[string]int) string {
	var parts []string

	if passedReviewers == totalReviewers {
		parts = append(parts, fmt.Sprintf("Review complete: all %d reviewers passed.", totalReviewers))
	} else {
		parts = append(parts, fmt.Sprintf("Review complete: %d/%d reviewers passed.", passedReviewers, totalReviewers))
	}

	if len(findings) == 0 {
		parts = append(parts, "No issues found.")
	} else {
		var severityParts []string
		if n := bySeverity[prompts.SeverityCritical]; n > 0 {
			severityParts = append(severityParts, fmt.Sprintf("%d critical", n))
		}
		if n := bySeverity[prompts.SeverityHigh]; n > 0 {
			severityParts = append(severityParts, fmt.Sprintf("%d high", n))
		}
		if n := bySeverity[prompts.SeverityMedium]; n > 0 {
			severityParts = append(severityParts, fmt.Sprintf("%d medium", n))
		}
		if n := bySeverity[prompts.SeverityLow]; n > 0 {
			severityParts = append(severityParts, fmt.Sprintf("%d low", n))
		}
		parts = append(parts, fmt.Sprintf("Found %d issues (%s).", len(findings), strings.Join(severityParts, ", ")))
	}

	return strings.Join(parts, " ")
}

// Helper functions to match semstreams aggregation patterns
func countResults(results []AgentResult) (success, failure int, failed []string) {
	for _, r := range results {
		if r.Status == "success" {
			success++
		} else {
			failure++
			failed = append(failed, r.StepName)
		}
	}
	return
}

func collectErrors(results []AgentResult) string {
	var errors []string
	for _, r := range results {
		if r.Error != "" {
			errors = append(errors, fmt.Sprintf("%s: %s", r.StepName, r.Error))
		}
	}
	return strings.Join(errors, "; ")
}

// Register adds the review aggregator to the default registry.
// Call this from main or an init function.
func Register() {
	DefaultRegistry.Register(&ReviewAggregator{})
}
