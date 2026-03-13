package workflow

import (
	"testing"
	"time"
)

// testCategoryJSON is a minimal error category registry for use in review tests.
// It defines two categories: "missing_tests" and "wrong_pattern".
const testCategoryJSON = `{
	"categories": [
		{
			"id": "missing_tests",
			"label": "Missing Tests",
			"description": "Implementation lacks adequate test coverage",
			"signals": ["no test file", "no assertions"],
			"guidance": "Add unit tests covering the acceptance criteria"
		},
		{
			"id": "wrong_pattern",
			"label": "Wrong Pattern",
			"description": "Implementation uses an incorrect architectural or idiomatic pattern",
			"signals": ["context not propagated", "mutex not deferred"],
			"guidance": "Follow the established patterns documented in the codebase CLAUDE.md"
		}
	]
}`

// mustRegistry builds an ErrorCategoryRegistry from the shared test JSON.
// Panics if the JSON fails to parse — test helper only.
func mustRegistry(t *testing.T) *ErrorCategoryRegistry {
	t.Helper()
	reg, err := LoadErrorCategoriesFromBytes([]byte(testCategoryJSON))
	if err != nil {
		t.Fatalf("mustRegistry: LoadErrorCategoriesFromBytes: %v", err)
	}
	return reg
}

// validAcceptedReview returns a fully-valid accepted review for use as a baseline.
func validAcceptedReview() *Review {
	return &Review{
		ID:              "review-001",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictAccepted,
		Q1Correctness:   4,
		Q2Quality:       4,
		Q3Completeness:  4,
		Timestamp:       time.Now(),
	}
}

func TestReview_Validate_AcceptedValid(t *testing.T) {
	reg := mustRegistry(t)
	r := validAcceptedReview()

	if err := r.Validate(reg); err != nil {
		t.Errorf("expected valid accepted review to pass, got: %v", err)
	}
}

func TestReview_Validate_RejectedWithErrors(t *testing.T) {
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-002",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   3,
		Q2Quality:       3,
		Q3Completeness:  3,
		Errors: []ReviewErrorRef{
			{CategoryID: "missing_tests"},
		},
		Timestamp: time.Now(),
	}

	if err := r.Validate(reg); err != nil {
		t.Errorf("expected rejected review with valid error ref to pass, got: %v", err)
	}
}

func TestReview_Validate_RejectedWithoutErrors(t *testing.T) {
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-003",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   3,
		Q2Quality:       3,
		Q3Completeness:  3,
		// Errors intentionally omitted
		Timestamp: time.Now(),
	}

	err := r.Validate(reg)
	if err == nil {
		t.Error("expected error for rejected review without error categories, got nil")
	}
}

func TestReview_Validate_InvalidCategoryID(t *testing.T) {
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-004",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   3,
		Q2Quality:       3,
		Q3Completeness:  3,
		Errors: []ReviewErrorRef{
			{CategoryID: "not_a_real_category"},
		},
		Timestamp: time.Now(),
	}

	err := r.Validate(reg)
	if err == nil {
		t.Error("expected error for unregistered category ID, got nil")
	}
}

func TestReview_Validate_ErrorRefWithRelatedEntityIDs(t *testing.T) {
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-005",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   3,
		Q2Quality:       3,
		Q3Completeness:  3,
		Errors: []ReviewErrorRef{
			{
				CategoryID:       "missing_tests",
				RelatedEntityIDs: []string{"sop.entity.123", "source.doc.456"},
			},
		},
		Timestamp: time.Now(),
	}

	if err := r.Validate(reg); err != nil {
		t.Errorf("expected review with related entity IDs to pass, got: %v", err)
	}
}

func TestReview_Validate_AllFivesAcceptedWithoutExplanation(t *testing.T) {
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-006",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictAccepted,
		Q1Correctness:   5,
		Q2Quality:       5,
		Q3Completeness:  5,
		// Explanation intentionally omitted — should fail anti-inflation guard
		Timestamp: time.Now(),
	}

	err := r.Validate(reg)
	if err == nil {
		t.Error("expected error for all-5s accepted without explanation, got nil")
	}
}

func TestReview_Validate_AllFivesAcceptedWithExplanation(t *testing.T) {
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-006b",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictAccepted,
		Q1Correctness:   5,
		Q2Quality:       5,
		Q3Completeness:  5,
		Explanation:     "Exceptional work: all acceptance criteria met, patterns followed, full test coverage.",
		Timestamp:       time.Now(),
	}

	if err := r.Validate(reg); err != nil {
		t.Errorf("expected all-5s accepted with explanation to pass, got: %v", err)
	}
}

func TestReview_Validate_AllFivesRejectedPasses(t *testing.T) {
	// All-5s rejected is unusual but valid — requires errors, no explanation needed.
	reg := mustRegistry(t)
	r := &Review{
		ID:              "review-007",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   5,
		Q2Quality:       5,
		Q3Completeness:  5,
		Errors: []ReviewErrorRef{
			{CategoryID: "wrong_pattern"},
		},
		Timestamp: time.Now(),
	}

	if err := r.Validate(reg); err != nil {
		t.Errorf("expected all-5s rejected with errors to pass, got: %v", err)
	}
}

func TestReview_Validate_RatingOutOfRange(t *testing.T) {
	reg := mustRegistry(t)

	tests := []struct {
		name string
		q1   int
		q2   int
		q3   int
	}{
		{"q1 zero", 0, 3, 3},
		{"q1 six", 6, 3, 3},
		{"q2 zero", 3, 0, 3},
		{"q2 six", 3, 6, 3},
		{"q3 zero", 3, 3, 0},
		{"q3 six", 3, 3, 6},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Review{
				ID:              "review-rating-test",
				ScenarioID:      "scenario-001",
				AgentID:         "agent-001",
				ReviewerAgentID: "reviewer-001",
				Verdict:         VerdictAccepted,
				Q1Correctness:   tc.q1,
				Q2Quality:       tc.q2,
				Q3Completeness:  tc.q3,
				Timestamp:       time.Now(),
			}
			if err := r.Validate(reg); err == nil {
				t.Errorf("expected error for out-of-range rating (%d, %d, %d), got nil", tc.q1, tc.q2, tc.q3)
			}
		})
	}
}

func TestReview_Validate_BelowThreeRejectedWithoutExplanation(t *testing.T) {
	reg := mustRegistry(t)
	// Average: (1 + 2 + 3) / 3 = 2.0 — below 3, requires explanation.
	r := &Review{
		ID:              "review-008",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   1,
		Q2Quality:       2,
		Q3Completeness:  3,
		Errors: []ReviewErrorRef{
			{CategoryID: "missing_tests"},
		},
		// Explanation intentionally omitted
		Timestamp: time.Now(),
	}

	err := r.Validate(reg)
	if err == nil {
		t.Error("expected error for below-3 average rejected without explanation, got nil")
	}
}

func TestReview_Validate_BelowThreeRejectedWithExplanation(t *testing.T) {
	reg := mustRegistry(t)
	// Average: (1 + 2 + 3) / 3 = 2.0 — below 3, but explanation provided.
	r := &Review{
		ID:              "review-008b",
		ScenarioID:      "scenario-001",
		AgentID:         "agent-001",
		ReviewerAgentID: "reviewer-001",
		Verdict:         VerdictRejected,
		Q1Correctness:   1,
		Q2Quality:       2,
		Q3Completeness:  3,
		Errors: []ReviewErrorRef{
			{CategoryID: "missing_tests"},
		},
		Explanation: "The implementation is missing all tests and does not satisfy any acceptance criteria.",
		Timestamp:   time.Now(),
	}

	if err := r.Validate(reg); err != nil {
		t.Errorf("expected below-3 rejected with explanation to pass, got: %v", err)
	}
}

func TestReview_Average(t *testing.T) {
	tests := []struct {
		name string
		q1   int
		q2   int
		q3   int
		want float64
	}{
		{"all equal", 3, 3, 3, 3.0},
		{"ascending", 1, 2, 3, 2.0},
		{"all max", 5, 5, 5, 5.0},
		{"all min", 1, 1, 1, 1.0},
		{"mixed", 4, 2, 3, 3.0},
		{"fractional result", 5, 4, 3, 4.0},
		{"non-integer average", 5, 5, 4, 14.0 / 3.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Review{
				Q1Correctness:  tc.q1,
				Q2Quality:      tc.q2,
				Q3Completeness: tc.q3,
			}
			got := r.Average()
			const epsilon = 1e-9
			if diff := got - tc.want; diff < -epsilon || diff > epsilon {
				t.Errorf("Average() = %f, want %f", got, tc.want)
			}
		})
	}
}
