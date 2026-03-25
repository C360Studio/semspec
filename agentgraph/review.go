package agentgraph

import (
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// ReviewVerdict is the outcome of a peer review.
type ReviewVerdict string

// Review verdict constants.
const (
	VerdictAccepted ReviewVerdict = "accepted"
	VerdictRejected ReviewVerdict = "rejected"
)

// ReviewErrorRef is a reference to an error category in a review,
// with optional links to related entities (SOPs, files, etc).
type ReviewErrorRef struct {
	CategoryID       string   `json:"category_id"`
	RelatedEntityIDs []string `json:"related_entity_ids,omitempty"`
}

// Review represents a peer review of a scenario implementation.
// Reviews are submitted by a reviewer agent and evaluated against a
// three-question rubric: correctness, quality, and completeness.
type Review struct {
	// ID is the unique identifier for this review.
	ID string

	// ScenarioID is the scenario whose implementation is under review.
	ScenarioID string

	// AgentID is the persistent agent whose work is being reviewed.
	AgentID string

	// ReviewerAgentID is the persistent agent performing the review.
	ReviewerAgentID string

	// Verdict is the review outcome: accepted or rejected.
	Verdict ReviewVerdict

	// Q1Correctness rates whether acceptance criteria are met (1–5).
	Q1Correctness int

	// Q2Quality rates whether established patterns and SOPs are followed (1–5).
	Q2Quality int

	// Q3Completeness rates edge-case coverage, tests, and documentation (1–5).
	Q3Completeness int

	// Errors lists the error categories observed in this review.
	// Required on rejection. Each entry may link to related graph entities.
	Errors []ReviewErrorRef

	// Explanation provides human-readable context for the verdict.
	// Required when: (a) rejected with average below 3, or (b) accepted with all-5s.
	Explanation string

	// Timestamp is when the review was submitted.
	Timestamp time.Time
}

// Average returns the mean of the three rating dimensions.
func (r *Review) Average() float64 {
	return float64(r.Q1Correctness+r.Q2Quality+r.Q3Completeness) / 3.0
}

// Validate checks review validity against the error category registry.
// It enforces rating bounds, verdict constraints, category ID validity,
// and anti-inflation/low-score explanation requirements.
func (r *Review) Validate(registry *workflow.ErrorCategoryRegistry) error {
	// Rating bounds: each dimension must be in [1, 5].
	ratings := []struct {
		name string
		val  int
	}{
		{"q1_correctness", r.Q1Correctness},
		{"q2_quality", r.Q2Quality},
		{"q3_completeness", r.Q3Completeness},
	}
	for _, rating := range ratings {
		if rating.val < 1 || rating.val > 5 {
			return fmt.Errorf("%s must be between 1 and 5, got %d", rating.name, rating.val)
		}
	}

	// Verdict must be one of the known values.
	if r.Verdict != VerdictAccepted && r.Verdict != VerdictRejected {
		return fmt.Errorf("verdict must be 'accepted' or 'rejected', got %q", r.Verdict)
	}

	// Rejected reviews must cite at least one error category.
	if r.Verdict == VerdictRejected && len(r.Errors) == 0 {
		return fmt.Errorf("rejected review must include at least one error category")
	}

	// All cited category IDs must be registered.
	for i, errRef := range r.Errors {
		if !registry.IsValid(errRef.CategoryID) {
			return fmt.Errorf("errors[%d]: invalid category_id %q", i, errRef.CategoryID)
		}
	}

	// Anti-inflation guard: all-5s accepted review requires an explanation
	// to prevent rubber-stamping without genuine justification.
	if r.Verdict == VerdictAccepted &&
		r.Q1Correctness == 5 && r.Q2Quality == 5 && r.Q3Completeness == 5 &&
		r.Explanation == "" {
		return fmt.Errorf("all-5s accepted review requires an explanation (anti-inflation guard)")
	}

	// Low-score guard: rejected review with average below 3 requires an explanation
	// to ensure actionable feedback is provided to the reviewed agent.
	if r.Verdict == VerdictRejected && r.Average() < 3 && r.Explanation == "" {
		return fmt.Errorf("rejected review with average below 3 requires an explanation")
	}

	return nil
}

// ErrorTrend carries a resolved error category with its occurrence count.
// Used by the trend-based prompt injection system to surface recurring error patterns.
type ErrorTrend struct {
	// Category is the resolved category definition.
	Category *workflow.ErrorCategoryDef

	// Count is the number of times this category has been observed for the agent.
	Count int
}
