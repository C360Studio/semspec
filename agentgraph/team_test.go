package agentgraph_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
)

// -- CreateTeam + GetTeam round-trip --

func TestHelper_CreateTeam_GetTeam_RoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	team := &workflow.Team{
		ID:        "alpha",
		Name:      "Team Alpha",
		Status:    workflow.TeamActive,
		MemberIDs: []string{"agent-1", "agent-2"},
		SharedKnowledge: []workflow.TeamInsight{
			{
				ID:          "ins-1",
				Source:      "reviewer-feedback",
				ScenarioID:  "scen-abc",
				Summary:     "Always write tests first.",
				CategoryIDs: []string{"missing_tests"},
				Skill:       "builder",
				CreatedAt:   now,
			},
		},
		TeamStats: workflow.ReviewStats{
			Q1CorrectnessAvg:  8.0,
			Q2QualityAvg:      7.5,
			Q3CompletenessAvg: 9.0,
			OverallAvg:        8.17,
			ReviewCount:       3,
		},
		RedTeamStats: workflow.ReviewStats{
			Q1CorrectnessAvg:  6.0,
			Q2QualityAvg:      6.5,
			Q3CompletenessAvg: 7.0,
			OverallAvg:        6.5,
			ReviewCount:       2,
		},
		ErrorCounts: map[workflow.ErrorCategory]int{"missing_tests": 1},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	got, err := h.GetTeam(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}

	if got.ID != "alpha" {
		t.Errorf("ID = %q, want %q", got.ID, "alpha")
	}
	if got.Name != "Team Alpha" {
		t.Errorf("Name = %q, want %q", got.Name, "Team Alpha")
	}
	if got.Status != workflow.TeamActive {
		t.Errorf("Status = %q, want %q", got.Status, workflow.TeamActive)
	}

	// MemberIDs — order not guaranteed so check by count and set membership.
	if len(got.MemberIDs) != 2 {
		t.Errorf("len(MemberIDs) = %d, want 2", len(got.MemberIDs))
	}
	memberSet := map[string]bool{}
	for _, m := range got.MemberIDs {
		memberSet[m] = true
	}
	for _, want := range []string{"agent-1", "agent-2"} {
		if !memberSet[want] {
			t.Errorf("MemberIDs missing %q", want)
		}
	}

	// SharedKnowledge round-trip.
	if len(got.SharedKnowledge) != 1 {
		t.Fatalf("len(SharedKnowledge) = %d, want 1", len(got.SharedKnowledge))
	}
	ins := got.SharedKnowledge[0]
	if ins.ID != "ins-1" {
		t.Errorf("insight ID = %q, want %q", ins.ID, "ins-1")
	}
	if ins.Summary != "Always write tests first." {
		t.Errorf("insight Summary = %q", ins.Summary)
	}
	if ins.Skill != "builder" {
		t.Errorf("insight Skill = %q, want %q", ins.Skill, "builder")
	}

	// TeamStats are stored as zero-init then separately updated in CreateTeam —
	// CreateTeam writes zero values for stats regardless of input stats, so we
	// only verify that ReviewCount is accessible as int (zero in fresh entity).
	// NOTE: CreateTeam zeroes stats; callers use UpdateTeamStats to set real values.
	// So the round-trip for stats goes through UpdateTeamStats, not CreateTeam.

	// CreatedAt round-trip (RFC3339 truncates sub-second).
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
}

func TestHelper_CreateTeam_SetsTimestampsWhenZero(t *testing.T) {
	t.Parallel()

	before := time.Now()
	team := &workflow.Team{
		ID:     "bravo",
		Name:   "Team Bravo",
		Status: workflow.TeamActive,
	}

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	// CreateTeam should have mutated the team's timestamps.
	if team.CreatedAt.Before(before) {
		t.Errorf("CreatedAt not set: %v", team.CreatedAt)
	}
	if team.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt not set: %v", team.UpdatedAt)
	}
}

func TestHelper_GetTeam_NotFound(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	_, err := h.GetTeam(context.Background(), "ghost")
	if err == nil {
		t.Fatal("GetTeam() expected error for missing team, got nil")
	}
}

// -- ListTeams --

func TestHelper_ListTeams(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	teams := []*workflow.Team{
		{ID: "alpha", Name: "Team Alpha", Status: workflow.TeamActive},
		{ID: "bravo", Name: "Team Bravo", Status: workflow.TeamBenched},
		{ID: "charlie", Name: "Team Charlie", Status: workflow.TeamActive},
	}

	for _, team := range teams {
		if err := h.CreateTeam(context.Background(), team); err != nil {
			t.Fatalf("CreateTeam(%q) error = %v", team.ID, err)
		}
	}

	got, err := h.ListTeams(context.Background())
	if err != nil {
		t.Fatalf("ListTeams() error = %v", err)
	}

	if len(got) != 3 {
		t.Errorf("ListTeams() count = %d, want 3", len(got))
	}

	ids := map[string]bool{}
	for _, t := range got {
		ids[t.ID] = true
	}
	for _, want := range []string{"alpha", "bravo", "charlie"} {
		if !ids[want] {
			t.Errorf("ListTeams() missing team %q", want)
		}
	}
}

func TestHelper_ListTeams_Empty(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	got, err := h.ListTeams(context.Background())
	if err != nil {
		t.Fatalf("ListTeams() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListTeams() on empty store = %d entries, want 0", len(got))
	}
}

// -- SelectBlueTeam ordering --

func TestHelper_SelectBlueTeam_LowestErrorsFirst(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// alpha: 5 errors — should lose to bravo
	// bravo: 1 error  — should win (fewest errors)
	// charlie: benched — excluded
	teams := []*workflow.Team{
		{ID: "alpha", Name: "Team Alpha", Status: workflow.TeamActive},
		{ID: "bravo", Name: "Team Bravo", Status: workflow.TeamActive},
		{ID: "charlie", Name: "Team Charlie", Status: workflow.TeamBenched},
	}
	for _, team := range teams {
		if err := h.CreateTeam(context.Background(), team); err != nil {
			t.Fatalf("CreateTeam(%q) error = %v", team.ID, err)
		}
	}

	// Accumulate errors via IncrementTeamErrorCounts so they persist in the graph.
	for range 5 {
		if err := h.IncrementTeamErrorCounts(context.Background(), "alpha",
			[]workflow.ErrorCategory{"missing_tests"}); err != nil {
			t.Fatalf("IncrementTeamErrorCounts(alpha) error = %v", err)
		}
	}
	if err := h.IncrementTeamErrorCounts(context.Background(), "bravo",
		[]workflow.ErrorCategory{"sop_violation"}); err != nil {
		t.Fatalf("IncrementTeamErrorCounts(bravo) error = %v", err)
	}

	got, err := h.SelectBlueTeam(context.Background())
	if err != nil {
		t.Fatalf("SelectBlueTeam() error = %v", err)
	}
	if got == nil {
		t.Fatal("SelectBlueTeam() returned nil, want bravo")
	}
	if got.ID != "bravo" {
		t.Errorf("SelectBlueTeam() = %q, want %q", got.ID, "bravo")
	}
}

func TestHelper_SelectBlueTeam_TieBreakByHighestScore(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// Both teams have 2 total errors; higher OverallAvg wins.
	teamA := &workflow.Team{
		ID:          "teamA",
		Name:        "Team A",
		Status:      workflow.TeamActive,
		ErrorCounts: map[workflow.ErrorCategory]int{"cat1": 2},
	}
	teamB := &workflow.Team{
		ID:          "teamB",
		Name:        "Team B",
		Status:      workflow.TeamActive,
		ErrorCounts: map[workflow.ErrorCategory]int{"cat1": 2},
	}

	for _, team := range []*workflow.Team{teamA, teamB} {
		if err := h.CreateTeam(context.Background(), team); err != nil {
			t.Fatalf("CreateTeam(%q) error = %v", team.ID, err)
		}
	}

	// Give teamB a higher OverallAvg via UpdateTeamStats.
	if err := h.UpdateTeamStats(context.Background(), "teamA", workflow.ReviewStats{OverallAvg: 6.0, ReviewCount: 1}); err != nil {
		t.Fatalf("UpdateTeamStats(teamA) error = %v", err)
	}
	if err := h.UpdateTeamStats(context.Background(), "teamB", workflow.ReviewStats{OverallAvg: 9.0, ReviewCount: 1}); err != nil {
		t.Fatalf("UpdateTeamStats(teamB) error = %v", err)
	}

	got, err := h.SelectBlueTeam(context.Background())
	if err != nil {
		t.Fatalf("SelectBlueTeam() error = %v", err)
	}
	if got == nil {
		t.Fatal("SelectBlueTeam() returned nil")
	}
	if got.ID != "teamB" {
		t.Errorf("SelectBlueTeam() = %q, want %q (higher OverallAvg)", got.ID, "teamB")
	}
}

func TestHelper_SelectBlueTeam_NoActiveTeams(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	team := &workflow.Team{ID: "benched", Name: "Benched Team", Status: workflow.TeamBenched}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	got, err := h.SelectBlueTeam(context.Background())
	if err != nil {
		t.Fatalf("SelectBlueTeam() error = %v", err)
	}
	if got != nil {
		t.Errorf("SelectBlueTeam() = %+v, want nil", got)
	}
}

// -- SelectRedTeam --

func TestHelper_SelectRedTeam_ExcludesBlueTeam(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	teams := []*workflow.Team{
		{ID: "blue", Name: "Blue Team", Status: workflow.TeamActive},
		{ID: "red", Name: "Red Team", Status: workflow.TeamActive},
	}
	for _, team := range teams {
		if err := h.CreateTeam(context.Background(), team); err != nil {
			t.Fatalf("CreateTeam(%q) error = %v", team.ID, err)
		}
	}

	got, err := h.SelectRedTeam(context.Background(), "blue")
	if err != nil {
		t.Fatalf("SelectRedTeam() error = %v", err)
	}
	if got == nil {
		t.Fatal("SelectRedTeam() returned nil, want red")
	}
	if got.ID == "blue" {
		t.Error("SelectRedTeam() returned the blue team — it must be excluded")
	}
	if got.ID != "red" {
		t.Errorf("SelectRedTeam() = %q, want %q", got.ID, "red")
	}
}

func TestHelper_SelectRedTeam_SortsByHighestRedTeamScore(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// Two candidate red teams; the one with higher RedTeamStats.OverallAvg should win.
	teams := []*workflow.Team{
		{ID: "blue", Name: "Blue", Status: workflow.TeamActive},
		{ID: "crit-low", Name: "Critic Low", Status: workflow.TeamActive},
		{ID: "crit-high", Name: "Critic High", Status: workflow.TeamActive},
	}
	for _, team := range teams {
		if err := h.CreateTeam(context.Background(), team); err != nil {
			t.Fatalf("CreateTeam(%q) error = %v", team.ID, err)
		}
	}

	if err := h.UpdateTeamRedTeamStats(context.Background(), "crit-low", workflow.ReviewStats{OverallAvg: 5.0, ReviewCount: 1}); err != nil {
		t.Fatalf("UpdateTeamRedTeamStats(crit-low) error = %v", err)
	}
	if err := h.UpdateTeamRedTeamStats(context.Background(), "crit-high", workflow.ReviewStats{OverallAvg: 9.0, ReviewCount: 1}); err != nil {
		t.Fatalf("UpdateTeamRedTeamStats(crit-high) error = %v", err)
	}

	got, err := h.SelectRedTeam(context.Background(), "blue")
	if err != nil {
		t.Fatalf("SelectRedTeam() error = %v", err)
	}
	if got == nil {
		t.Fatal("SelectRedTeam() returned nil")
	}
	if got.ID != "crit-high" {
		t.Errorf("SelectRedTeam() = %q, want %q (highest RedTeamStats.OverallAvg)", got.ID, "crit-high")
	}
}

func TestHelper_SelectRedTeam_NoEligibleTeams(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// Only the blue team exists.
	team := &workflow.Team{ID: "solo", Name: "Solo Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	got, err := h.SelectRedTeam(context.Background(), "solo")
	if err != nil {
		t.Fatalf("SelectRedTeam() error = %v", err)
	}
	if got != nil {
		t.Errorf("SelectRedTeam() = %+v, want nil", got)
	}
}

// -- SetTeamStatus --

func TestHelper_SetTeamStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		startState workflow.TeamStatus
		newState   workflow.TeamStatus
	}{
		{
			name:       "active to benched",
			startState: workflow.TeamActive,
			newState:   workflow.TeamBenched,
		},
		{
			name:       "benched to retired",
			startState: workflow.TeamBenched,
			newState:   workflow.TeamRetired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()
			h := agentgraph.NewHelper(kv)

			team := &workflow.Team{ID: "test-team", Name: "Test Team", Status: tc.startState}
			if err := h.CreateTeam(context.Background(), team); err != nil {
				t.Fatalf("CreateTeam() error = %v", err)
			}

			if err := h.SetTeamStatus(context.Background(), "test-team", tc.newState); err != nil {
				t.Fatalf("SetTeamStatus() error = %v", err)
			}

			got, err := h.GetTeam(context.Background(), "test-team")
			if err != nil {
				t.Fatalf("GetTeam() error = %v", err)
			}
			if got.Status != tc.newState {
				t.Errorf("Status = %q, want %q", got.Status, tc.newState)
			}
		})
	}
}

func TestHelper_SetTeamStatus_NotFound(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	err := h.SetTeamStatus(context.Background(), "ghost", workflow.TeamBenched)
	if err == nil {
		t.Fatal("SetTeamStatus() on missing team expected error, got nil")
	}
}

// -- UpdateTeamStats --

func TestHelper_UpdateTeamStats(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	team := &workflow.Team{ID: "stats-team", Name: "Stats Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	stats := workflow.ReviewStats{
		Q1CorrectnessAvg:  8.5,
		Q2QualityAvg:      7.0,
		Q3CompletenessAvg: 9.0,
		OverallAvg:        8.17,
		ReviewCount:       5,
	}
	if err := h.UpdateTeamStats(context.Background(), "stats-team", stats); err != nil {
		t.Fatalf("UpdateTeamStats() error = %v", err)
	}

	got, err := h.GetTeam(context.Background(), "stats-team")
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}

	if got.TeamStats.Q1CorrectnessAvg != 8.5 {
		t.Errorf("Q1CorrectnessAvg = %v, want 8.5", got.TeamStats.Q1CorrectnessAvg)
	}
	if got.TeamStats.Q2QualityAvg != 7.0 {
		t.Errorf("Q2QualityAvg = %v, want 7.0", got.TeamStats.Q2QualityAvg)
	}
	if got.TeamStats.Q3CompletenessAvg != 9.0 {
		t.Errorf("Q3CompletenessAvg = %v, want 9.0", got.TeamStats.Q3CompletenessAvg)
	}
	if got.TeamStats.OverallAvg != 8.17 {
		t.Errorf("OverallAvg = %v, want 8.17", got.TeamStats.OverallAvg)
	}
	if got.TeamStats.ReviewCount != 5 {
		t.Errorf("ReviewCount = %d, want 5", got.TeamStats.ReviewCount)
	}
}

// -- UpdateTeamRedTeamStats --

func TestHelper_UpdateTeamRedTeamStats(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	team := &workflow.Team{ID: "redstats-team", Name: "Red Stats Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	stats := workflow.ReviewStats{
		Q1CorrectnessAvg:  7.0,
		Q2QualityAvg:      8.0,
		Q3CompletenessAvg: 6.5,
		OverallAvg:        7.17,
		ReviewCount:       3,
	}
	if err := h.UpdateTeamRedTeamStats(context.Background(), "redstats-team", stats); err != nil {
		t.Fatalf("UpdateTeamRedTeamStats() error = %v", err)
	}

	got, err := h.GetTeam(context.Background(), "redstats-team")
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}

	if got.RedTeamStats.Q1CorrectnessAvg != 7.0 {
		t.Errorf("RedTeamStats.Q1CorrectnessAvg = %v, want 7.0", got.RedTeamStats.Q1CorrectnessAvg)
	}
	if got.RedTeamStats.OverallAvg != 7.17 {
		t.Errorf("RedTeamStats.OverallAvg = %v, want 7.17", got.RedTeamStats.OverallAvg)
	}
	if got.RedTeamStats.ReviewCount != 3 {
		t.Errorf("RedTeamStats.ReviewCount = %d, want 3", got.RedTeamStats.ReviewCount)
	}

	// TeamStats must remain zero — UpdateTeamRedTeamStats must not touch team.review.* predicates.
	if got.TeamStats.ReviewCount != 0 {
		t.Errorf("TeamStats.ReviewCount = %d after RedTeamStats update, want 0", got.TeamStats.ReviewCount)
	}
}

// -- AddTeamInsight --

func TestHelper_AddTeamInsight_AppendsAndCaps(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	team := &workflow.Team{ID: "insight-team", Name: "Insight Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	// Add 102 insights — cap is 100, so oldest 2 should be dropped (FIFO).
	for i := range 102 {
		insight := workflow.TeamInsight{
			ID:        fmt.Sprintf("ins-%d", i),
			Source:    "reviewer-feedback",
			Summary:   fmt.Sprintf("Lesson %d", i),
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := h.AddTeamInsight(context.Background(), "insight-team", insight); err != nil {
			t.Fatalf("AddTeamInsight(%d) error = %v", i, err)
		}
	}

	got, err := h.GetTeam(context.Background(), "insight-team")
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}

	if len(got.SharedKnowledge) != 100 {
		t.Errorf("len(SharedKnowledge) = %d, want 100 (capped)", len(got.SharedKnowledge))
	}

	// The oldest entries (ins-0, ins-1) should have been evicted; ins-2 is now first.
	firstID := got.SharedKnowledge[0].ID
	if firstID != "ins-2" {
		t.Errorf("SharedKnowledge[0].ID = %q, want %q (oldest two evicted)", firstID, "ins-2")
	}
	lastID := got.SharedKnowledge[99].ID
	if lastID != "ins-101" {
		t.Errorf("SharedKnowledge[99].ID = %q, want %q (newest)", lastID, "ins-101")
	}
}

func TestHelper_AddTeamInsight_SingleAppend(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	team := &workflow.Team{ID: "single-insight-team", Name: "Single Insight Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	insight := workflow.TeamInsight{
		ID:        "first-insight",
		Source:    "red-team-critique-feedback",
		Summary:   "Validate all edge cases.",
		Skill:     "tester",
		CreatedAt: time.Now(),
	}
	if err := h.AddTeamInsight(context.Background(), "single-insight-team", insight); err != nil {
		t.Fatalf("AddTeamInsight() error = %v", err)
	}

	got, err := h.GetTeam(context.Background(), "single-insight-team")
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}

	if len(got.SharedKnowledge) != 1 {
		t.Fatalf("len(SharedKnowledge) = %d, want 1", len(got.SharedKnowledge))
	}
	if got.SharedKnowledge[0].ID != "first-insight" {
		t.Errorf("insight ID = %q, want %q", got.SharedKnowledge[0].ID, "first-insight")
	}
	if got.SharedKnowledge[0].Skill != "tester" {
		t.Errorf("insight Skill = %q, want %q", got.SharedKnowledge[0].Skill, "tester")
	}
}

// -- IncrementTeamErrorCounts --

func TestHelper_IncrementTeamErrorCounts(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	team := &workflow.Team{ID: "errcount-team", Name: "Error Count Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	// First increment: two distinct categories.
	if err := h.IncrementTeamErrorCounts(context.Background(), "errcount-team",
		[]workflow.ErrorCategory{"missing_tests", "sop_violation"}); err != nil {
		t.Fatalf("IncrementTeamErrorCounts() first call error = %v", err)
	}

	// Second increment: one existing category, one new.
	if err := h.IncrementTeamErrorCounts(context.Background(), "errcount-team",
		[]workflow.ErrorCategory{"missing_tests", "logic_error"}); err != nil {
		t.Fatalf("IncrementTeamErrorCounts() second call error = %v", err)
	}

	got, err := h.GetTeam(context.Background(), "errcount-team")
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}

	if got.ErrorCounts["missing_tests"] != 2 {
		t.Errorf("ErrorCounts[missing_tests] = %d, want 2", got.ErrorCounts["missing_tests"])
	}
	if got.ErrorCounts["sop_violation"] != 1 {
		t.Errorf("ErrorCounts[sop_violation] = %d, want 1", got.ErrorCounts["sop_violation"])
	}
	if got.ErrorCounts["logic_error"] != 1 {
		t.Errorf("ErrorCounts[logic_error] = %d, want 1", got.ErrorCounts["logic_error"])
	}
}

// -- GetTeamForAgent + SetAgentTeam --

func TestHelper_SetAgentTeam_GetTeamForAgent_RoundTrip(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// Create an agent entity first.
	agent := workflow.Agent{
		ID:        "agent-123",
		Name:      "builder-agent-123",
		Role:      "builder",
		Model:     "gpt-4o",
		Status:    workflow.AgentAvailable,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	// Before SetAgentTeam, agent has no team.
	teamID, err := h.GetTeamForAgent(context.Background(), "agent-123")
	if err != nil {
		t.Fatalf("GetTeamForAgent() before set error = %v", err)
	}
	if teamID != "" {
		t.Errorf("GetTeamForAgent() before set = %q, want empty string", teamID)
	}

	// Set team.
	if err := h.SetAgentTeam(context.Background(), "agent-123", "alpha"); err != nil {
		t.Fatalf("SetAgentTeam() error = %v", err)
	}

	// After SetAgentTeam the value must be retrievable.
	teamID, err = h.GetTeamForAgent(context.Background(), "agent-123")
	if err != nil {
		t.Fatalf("GetTeamForAgent() after set error = %v", err)
	}
	if teamID != "alpha" {
		t.Errorf("GetTeamForAgent() after set = %q, want %q", teamID, "alpha")
	}
}

func TestHelper_SetAgentTeam_OverwritesPreviousTeam(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent := workflow.Agent{
		ID:        "transferable-agent",
		Name:      "builder-transferable-agent",
		Role:      "builder",
		Model:     "gpt-4o",
		Status:    workflow.AgentAvailable,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	if err := h.SetAgentTeam(context.Background(), "transferable-agent", "team-old"); err != nil {
		t.Fatalf("SetAgentTeam() first call error = %v", err)
	}
	if err := h.SetAgentTeam(context.Background(), "transferable-agent", "team-new"); err != nil {
		t.Fatalf("SetAgentTeam() second call error = %v", err)
	}

	teamID, err := h.GetTeamForAgent(context.Background(), "transferable-agent")
	if err != nil {
		t.Fatalf("GetTeamForAgent() error = %v", err)
	}
	if teamID != "team-new" {
		t.Errorf("GetTeamForAgent() = %q, want %q (overwritten)", teamID, "team-new")
	}
}

func TestHelper_GetTeamForAgent_NotFound(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	_, err := h.GetTeamForAgent(context.Background(), "nonexistent-agent")
	if err == nil {
		t.Fatal("GetTeamForAgent() on missing agent expected error, got nil")
	}
}

// -- ListTeams skips insight sub-entities --

func TestHelper_ListTeams_SkipsInsightSubEntities(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// Create a real team.
	team := &workflow.Team{ID: "real-team", Name: "Real Team", Status: workflow.TeamActive}
	if err := h.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	// Manually inject a fake team-insight key that shares the team prefix.
	insightKey := agentgraph.TeamInsightEntityID("real-team", "ins1")
	kv.data[insightKey] = []byte(`{"ID":"semspec.local.agent.team.insight.real-team-ins1"}`)

	got, err := h.ListTeams(context.Background())
	if err != nil {
		t.Fatalf("ListTeams() error = %v", err)
	}

	if len(got) != 1 {
		t.Errorf("ListTeams() count = %d, want 1 (insight sub-entity must be excluded)", len(got))
	}
	if got[0].ID != "real-team" {
		t.Errorf("ListTeams()[0].ID = %q, want %q", got[0].ID, "real-team")
	}
}
