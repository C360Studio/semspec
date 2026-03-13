package workflow

import (
	"testing"
)

func TestUpdateStats_FirstReview(t *testing.T) {
	var stats ReviewStats

	stats.UpdateStats(8, 7, 9)

	if stats.ReviewCount != 1 {
		t.Errorf("ReviewCount = %d, want 1", stats.ReviewCount)
	}
	if stats.Q1CorrectnessAvg != 8.0 {
		t.Errorf("Q1CorrectnessAvg = %f, want 8.0", stats.Q1CorrectnessAvg)
	}
	if stats.Q2QualityAvg != 7.0 {
		t.Errorf("Q2QualityAvg = %f, want 7.0", stats.Q2QualityAvg)
	}
	if stats.Q3CompletenessAvg != 9.0 {
		t.Errorf("Q3CompletenessAvg = %f, want 9.0", stats.Q3CompletenessAvg)
	}
	wantOverall := (8.0 + 7.0 + 9.0) / 3
	if stats.OverallAvg != wantOverall {
		t.Errorf("OverallAvg = %f, want %f", stats.OverallAvg, wantOverall)
	}
}

func TestUpdateStats_RunningAverage(t *testing.T) {
	tests := []struct {
		name      string
		reviews   [][3]int
		wantQ1    float64
		wantQ2    float64
		wantQ3    float64
		wantCount int
	}{
		{
			name:      "two reviews",
			reviews:   [][3]int{{10, 10, 10}, {0, 0, 0}},
			wantQ1:    5.0,
			wantQ2:    5.0,
			wantQ3:    5.0,
			wantCount: 2,
		},
		{
			name:      "three reviews uniform",
			reviews:   [][3]int{{6, 6, 6}, {9, 9, 9}, {3, 3, 3}},
			wantQ1:    6.0,
			wantQ2:    6.0,
			wantQ3:    6.0,
			wantCount: 3,
		},
		{
			name:      "four reviews asymmetric",
			reviews:   [][3]int{{8, 4, 6}, {6, 8, 4}, {4, 6, 8}, {10, 10, 10}},
			wantQ1:    7.0,
			wantQ2:    7.0,
			wantQ3:    7.0,
			wantCount: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stats ReviewStats
			for _, r := range tc.reviews {
				stats.UpdateStats(r[0], r[1], r[2])
			}

			if stats.ReviewCount != tc.wantCount {
				t.Errorf("ReviewCount = %d, want %d", stats.ReviewCount, tc.wantCount)
			}

			const epsilon = 1e-9
			if diff := stats.Q1CorrectnessAvg - tc.wantQ1; diff < -epsilon || diff > epsilon {
				t.Errorf("Q1CorrectnessAvg = %f, want %f", stats.Q1CorrectnessAvg, tc.wantQ1)
			}
			if diff := stats.Q2QualityAvg - tc.wantQ2; diff < -epsilon || diff > epsilon {
				t.Errorf("Q2QualityAvg = %f, want %f", stats.Q2QualityAvg, tc.wantQ2)
			}
			if diff := stats.Q3CompletenessAvg - tc.wantQ3; diff < -epsilon || diff > epsilon {
				t.Errorf("Q3CompletenessAvg = %f, want %f", stats.Q3CompletenessAvg, tc.wantQ3)
			}

			wantOverall := (stats.Q1CorrectnessAvg + stats.Q2QualityAvg + stats.Q3CompletenessAvg) / 3
			if diff := stats.OverallAvg - wantOverall; diff < -epsilon || diff > epsilon {
				t.Errorf("OverallAvg = %f, want %f", stats.OverallAvg, wantOverall)
			}
		})
	}
}

func TestUpdateStats_RunningAverageFormula(t *testing.T) {
	// Explicitly verify the running average formula:
	// newAvg = (oldAvg * oldCount + newScore) / (oldCount + 1)
	var stats ReviewStats

	stats.UpdateStats(10, 10, 10) // count=1, all avgs = 10
	stats.UpdateStats(4, 4, 4)    // count=2, all avgs = (10*1 + 4) / 2 = 7

	const epsilon = 1e-9
	if diff := stats.Q1CorrectnessAvg - 7.0; diff < -epsilon || diff > epsilon {
		t.Errorf("after 2 reviews Q1 = %f, want 7.0", stats.Q1CorrectnessAvg)
	}

	stats.UpdateStats(1, 1, 1) // count=3, all avgs = (7*2 + 1) / 3 = 5
	if diff := stats.Q1CorrectnessAvg - 5.0; diff < -epsilon || diff > epsilon {
		t.Errorf("after 3 reviews Q1 = %f, want 5.0", stats.Q1CorrectnessAvg)
	}
}

func TestAgent_IncrementErrorCount(t *testing.T) {
	t.Run("initialises map on first call", func(t *testing.T) {
		agent := &Agent{}

		if agent.ErrorCounts != nil {
			t.Fatal("ErrorCounts should be nil before first increment")
		}

		agent.IncrementErrorCount("missing_tests")

		if agent.ErrorCounts == nil {
			t.Fatal("ErrorCounts should be non-nil after first increment")
		}
		if agent.ErrorCounts["missing_tests"] != 1 {
			t.Errorf("ErrorCounts[missing_tests] = %d, want 1", agent.ErrorCounts["missing_tests"])
		}
	})

	t.Run("increments existing category", func(t *testing.T) {
		agent := &Agent{}

		agent.IncrementErrorCount("sop_violation")
		agent.IncrementErrorCount("sop_violation")
		agent.IncrementErrorCount("sop_violation")

		if agent.ErrorCounts["sop_violation"] != 3 {
			t.Errorf("ErrorCounts[sop_violation] = %d, want 3", agent.ErrorCounts["sop_violation"])
		}
	})

	t.Run("tracks multiple categories independently", func(t *testing.T) {
		agent := &Agent{}

		agent.IncrementErrorCount("missing_tests")
		agent.IncrementErrorCount("wrong_pattern")
		agent.IncrementErrorCount("wrong_pattern")
		agent.IncrementErrorCount("scope_violation")

		if agent.ErrorCounts["missing_tests"] != 1 {
			t.Errorf("missing_tests = %d, want 1", agent.ErrorCounts["missing_tests"])
		}
		if agent.ErrorCounts["wrong_pattern"] != 2 {
			t.Errorf("wrong_pattern = %d, want 2", agent.ErrorCounts["wrong_pattern"])
		}
		if agent.ErrorCounts["scope_violation"] != 1 {
			t.Errorf("scope_violation = %d, want 1", agent.ErrorCounts["scope_violation"])
		}
		// Unset category should be zero value
		if agent.ErrorCounts["api_contract_mismatch"] != 0 {
			t.Errorf("api_contract_mismatch = %d, want 0", agent.ErrorCounts["api_contract_mismatch"])
		}
	})

	t.Run("uses existing map when already initialised", func(t *testing.T) {
		agent := &Agent{
			ErrorCounts: map[ErrorCategory]int{"edge_case_missed": 5},
		}

		agent.IncrementErrorCount("edge_case_missed")

		if agent.ErrorCounts["edge_case_missed"] != 6 {
			t.Errorf("edge_case_missed = %d, want 6", agent.ErrorCounts["edge_case_missed"])
		}
	})
}
