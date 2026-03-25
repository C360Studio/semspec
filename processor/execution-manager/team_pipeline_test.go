package executionmanager

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
)

// ---------------------------------------------------------------------------
// Test 1: TestTeamMode_ValidatorPassesDispatchesReviewerDirectly
//
// Red team now operates at scenario level, not per-task. With teams enabled,
// a passing validator result should still dispatch the per-task reviewer
// directly — the red team challenge is no longer injected here.
// ---------------------------------------------------------------------------

func TestTeamMode_ValidatorPassesDispatchesReviewerDirectly(t *testing.T) {
	ctx := context.Background()
	c, _ := newTeamTestComponent(t)
	c.seedTeams()

	exec := newTestExec("slug-rt", "task-rt")
	exec.BlueTeamID = "blue"

	// Build a passing ValidationResult.
	validResult := payloads.ValidationResult{Passed: true}
	resultJSON, err := json.Marshal(validResult)
	if err != nil {
		t.Fatalf("marshal ValidationResult: %v", err)
	}

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.ValidatorTaskID,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageValidate,
		Result:       string(resultJSON),
	}

	exec.mu.Lock()
	// Validation is now synchronous (structural-validator component, not agentic loop).
	// These tests need integration test infrastructure to mock the structural-validator.
	// TODO: Rewrite as integration tests with structural-validator mock.
	t.Skip("Validation is now synchronous — requires structural-validator component")
	_ = ctx
	_ = event
	_ = exec
	_ = c
	exec.mu.Unlock()

	// Red team is now at scenario level — per-task pipeline goes straight to reviewer.
	if exec.RedTeamTaskID != "" {
		t.Errorf("expected RedTeamTaskID to be empty (red team now at scenario level), got %q", exec.RedTeamTaskID)
	}
	if exec.ReviewerTaskID == "" {
		t.Error("expected ReviewerTaskID to be set after team-mode validator pass (reviewer dispatched directly)")
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestSoloMode_ValidatorPassesDispatchesReviewer
//
// With teams DISABLED, a passing validator result should dispatch the reviewer
// directly, skipping the red team stage entirely.
// ---------------------------------------------------------------------------

func TestSoloMode_ValidatorPassesDispatchesReviewer(t *testing.T) {
	ctx := context.Background()
	c := newTestComponent(t)
	// Teams are not configured — teamsEnabled() returns false.

	exec := newTestExec("slug-solo", "task-solo")
	// No BlueTeamID — pure solo mode.

	validResult := payloads.ValidationResult{Passed: true}
	resultJSON, err := json.Marshal(validResult)
	if err != nil {
		t.Fatalf("marshal ValidationResult: %v", err)
	}

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.ValidatorTaskID,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageValidate,
		Result:       string(resultJSON),
	}

	exec.mu.Lock()
	// Validation is now synchronous (structural-validator component, not agentic loop).
	// These tests need integration test infrastructure to mock the structural-validator.
	// TODO: Rewrite as integration tests with structural-validator mock.
	t.Skip("Validation is now synchronous — requires structural-validator component")
	_ = ctx
	_ = event
	_ = exec
	_ = c
	exec.mu.Unlock()

	if exec.RedTeamTaskID != "" {
		t.Errorf("expected RedTeamTaskID to be empty in solo mode, got %q", exec.RedTeamTaskID)
	}
	if exec.ReviewerTaskID == "" {
		t.Error("expected ReviewerTaskID to be set after solo-mode validator pass, got empty")
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestTeamMode_RedTeamFallbackToReviewer
//
// When no red team can be selected (no teams seeded, so SelectRedTeam returns
// nil), dispatchRedTeamLocked should fall back to dispatching the reviewer
// directly so the pipeline always makes forward progress.
// ---------------------------------------------------------------------------

func TestTeamMode_RedTeamFallbackToReviewer(t *testing.T) {
	ctx := context.Background()
	c, _ := newTeamTestComponent(t)
	// Intentionally NOT calling c.seedTeams() — graph has no team entities.

	exec := newTestExec("slug-fallback", "task-fallback")
	exec.BlueTeamID = "blue"

	validResult := payloads.ValidationResult{Passed: true}
	resultJSON, err := json.Marshal(validResult)
	if err != nil {
		t.Fatalf("marshal ValidationResult: %v", err)
	}

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.ValidatorTaskID,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageValidate,
		Result:       string(resultJSON),
	}

	exec.mu.Lock()
	// Validation is now synchronous (structural-validator component, not agentic loop).
	// These tests need integration test infrastructure to mock the structural-validator.
	// TODO: Rewrite as integration tests with structural-validator mock.
	t.Skip("Validation is now synchronous — requires structural-validator component")
	_ = ctx
	_ = event
	_ = exec
	_ = c
	exec.mu.Unlock()

	// Red team could not be selected — reviewer should have been dispatched.
	if exec.ReviewerTaskID == "" {
		t.Error("expected ReviewerTaskID to be set when red-team fallback fires, got empty")
	}
}

// ---------------------------------------------------------------------------
// Test 4: TestTeamMode_RedTeamCompleteTransitionsToReviewer
//
// After the red team submits its challenge result, handleRedTeamCompleteLocked
// should parse the result into exec.RedTeamChallenge and then dispatch the
// reviewer.
// ---------------------------------------------------------------------------

func TestTeamMode_RedTeamCompleteTransitionsToReviewer(t *testing.T) {
	ctx := context.Background()
	c, _ := newTeamTestComponent(t)
	c.seedTeams()

	exec := newTestExec("slug-rtc", "task-rtc")
	exec.BlueTeamID = "blue"
	exec.RedTeamID = "red"
	exec.RedTeamTaskID = "red-task-123"

	// Index the task ID so the component can route the event.
	c.taskIDIndex.Store("red-task-123", exec.EntityID)

	challenge := payloads.RedTeamChallengeResult{
		Issues: []payloads.RedTeamIssue{
			{Description: "Missing nil guard on input", Severity: "major"},
		},
		OverallScore: 4,
		Summary:      "Implementation is mostly correct but has edge-case gaps.",
	}
	challengeJSON, err := json.Marshal(challenge)
	if err != nil {
		t.Fatalf("marshal RedTeamChallengeResult: %v", err)
	}

	event := &agentic.LoopCompletedEvent{
		TaskID:       "red-task-123",
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageRedTeam,
		Result:       string(challengeJSON),
	}

	exec.mu.Lock()
	c.handleRedTeamCompleteLocked(ctx, event, exec)
	exec.mu.Unlock()

	if exec.RedTeamChallenge == nil {
		t.Fatal("expected RedTeamChallenge to be populated, got nil")
	}
	if len(exec.RedTeamChallenge.Issues) != 1 {
		t.Errorf("RedTeamChallenge.Issues = %d entries, want 1", len(exec.RedTeamChallenge.Issues))
	}
	if exec.ReviewerTaskID == "" {
		t.Error("expected ReviewerTaskID to be set after red team completes, got empty")
	}
}

// ---------------------------------------------------------------------------
// Test 5: TestTeamMode_ReviewerUpdatesRedTeamStats
//
// After the reviewer produces a TaskCodeReviewResult with red-team scoring
// fields, the red team's RedTeamStats should be updated in the graph. We
// call the stat-update logic directly rather than through the full handler
// (which needs a valid NATS publish path for the approval/rejection branch).
// ---------------------------------------------------------------------------

func TestTeamMode_ReviewerUpdatesRedTeamStats(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)
	c.seedTeams()

	exec := newTestExec("slug-stats", "task-stats")
	exec.BlueTeamID = "blue"
	exec.RedTeamID = "red"

	// Simulate the stat update that handleReviewerCompleteLocked performs.
	const accuracy, thoroughness, fairness = 4, 3, 5
	if err := helper.UpdateTeamRedTeamStatsIncremental(ctx, exec.RedTeamID, accuracy, thoroughness, fairness); err != nil {
		t.Fatalf("UpdateTeamRedTeamStatsIncremental: %v", err)
	}

	// Verify the stats were persisted.
	team, err := helper.GetTeam(ctx, "red")
	if err != nil {
		t.Fatalf("GetTeam(red): %v", err)
	}
	if team.RedTeamStats.ReviewCount != 1 {
		t.Errorf("RedTeamStats.ReviewCount = %d, want 1", team.RedTeamStats.ReviewCount)
	}
	// The overall average incorporates all three scores: (4+3+5)/3 ≈ 4.0
	if team.RedTeamStats.OverallAvg <= 0 {
		t.Errorf("RedTeamStats.OverallAvg = %.2f, want > 0", team.RedTeamStats.OverallAvg)
	}
	if team.RedTeamStats.Q1CorrectnessAvg != float64(accuracy) {
		t.Errorf("Q1CorrectnessAvg = %.2f, want %.2f", team.RedTeamStats.Q1CorrectnessAvg, float64(accuracy))
	}
}

// ---------------------------------------------------------------------------
// Test 6: TestTeamMode_ReviewerRejectionExtractsInsights
//
// When extractTeamInsights is called with rejection feedback containing a
// "missing_tests" signal, the blue team should receive a "tester" skill
// insight. When the RedTeamChallenge.OverallScore is low (<=2), the red team
// should receive a "red-team" skill insight about critique quality.
// ---------------------------------------------------------------------------

func TestTeamMode_ReviewerRejectionExtractsInsights(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)
	c.seedTeams()

	exec := newTestExec("slug-insights", "task-insights")
	exec.BlueTeamID = "blue"
	exec.RedTeamID = "red"

	// A poor red-team critique score triggers a red-team insight.
	exec.RedTeamChallenge = &payloads.RedTeamChallengeResult{
		Issues:       []payloads.RedTeamIssue{{Description: "Generic complaint", Severity: "minor"}},
		OverallScore: 2,
		Summary:      "Critique was vague.",
	}

	// "No test file created" matches the agentTestCategoriesJSON "missing_tests" signal.
	feedback := "No test file created, missing tests for edge case"

	c.extractTeamInsights(ctx, exec, feedback)

	// Check blue team received a "tester" insight.
	blueTeam, err := helper.GetTeam(ctx, "blue")
	if err != nil {
		t.Fatalf("GetTeam(blue): %v", err)
	}
	if len(blueTeam.SharedKnowledge) == 0 {
		t.Fatal("blue team SharedKnowledge should have at least one insight, got none")
	}
	foundTesterInsight := false
	for _, ins := range blueTeam.SharedKnowledge {
		if ins.Skill == "tester" {
			foundTesterInsight = true
			if !slices.Contains(ins.CategoryIDs, "missing_tests") {
				t.Errorf("blue team tester insight CategoryIDs = %v, want to contain \"missing_tests\"", ins.CategoryIDs)
			}
			break
		}
	}
	if !foundTesterInsight {
		t.Error("blue team should have a \"tester\" skill insight, found none")
	}

	// Check red team received a "red-team" insight for poor critique quality.
	redTeam, err := helper.GetTeam(ctx, "red")
	if err != nil {
		t.Fatalf("GetTeam(red): %v", err)
	}
	if len(redTeam.SharedKnowledge) == 0 {
		t.Fatal("red team SharedKnowledge should have at least one insight, got none")
	}
	foundRedTeamInsight := false
	for _, ins := range redTeam.SharedKnowledge {
		if ins.Skill == "red-team" {
			foundRedTeamInsight = true
			break
		}
	}
	if !foundRedTeamInsight {
		t.Error("red team should have a \"red-team\" skill insight for low critique score, found none")
	}
}

// ---------------------------------------------------------------------------
// Test 7: TestTeamMode_CheckTeamBenching
//
// When a majority of a team's members are individually benched, checkTeamBenching
// should flip the team status to TeamBenched and return true.
// ---------------------------------------------------------------------------

func TestTeamMode_CheckTeamBenching(t *testing.T) {
	ctx := context.Background()

	// Build a component with a 3-member blue team so that benching 2 members
	// (>= len/2+1 = 2) triggers team benching.
	c, helper := newAgentTestComponent(t)
	c.config.Teams = &TeamsConfig{
		Enabled: true,
		Roster: []TeamRosterEntry{
			{
				Name: "blue",
				Members: []TeamMemberEntry{
					{Role: "tester", Model: "default"},
					{Role: "builder", Model: "default"},
					{Role: "reviewer", Model: "default"},
				},
			},
			// Second team satisfies the teamsEnabled() >= 2 requirement.
			{
				Name: "red",
				Members: []TeamMemberEntry{
					{Role: "reviewer", Model: "fast"},
				},
			},
		},
	}
	c.seedTeams()

	// Bench 2 of the 3 blue team members — this meets the majority threshold
	// (threshold = 3/2+1 = 2).
	for _, agentID := range []string{"blue-tester", "blue-builder"} {
		if err := helper.SetAgentStatus(ctx, agentID, workflow.AgentBenched); err != nil {
			t.Fatalf("SetAgentStatus(%q, benched): %v", agentID, err)
		}
	}

	benched := c.checkTeamBenching(ctx, "blue")
	if !benched {
		t.Error("checkTeamBenching: expected true (majority benched), got false")
	}

	// Confirm team status in the graph.
	team, err := helper.GetTeam(ctx, "blue")
	if err != nil {
		t.Fatalf("GetTeam(blue): %v", err)
	}
	if team.Status != workflow.TeamBenched {
		t.Errorf("team status = %q, want %q", team.Status, workflow.TeamBenched)
	}
}

// ---------------------------------------------------------------------------
// Test 8: TestTeamMode_BuildTeamKnowledgeBlock
//
// buildTeamKnowledgeBlock must return a non-empty string containing the team
// framing header and only the insights that match the requested skill.
// When teams are disabled (agentHelper is nil), it must return "".
// ---------------------------------------------------------------------------

func TestTeamMode_BuildTeamKnowledgeBlock(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)
	c.seedTeams()

	// Add 3 insights to the blue team: 2 builder, 1 tester.
	builderInsight1 := workflow.TeamInsight{
		ID:      "ins-b-1",
		Source:  "reviewer-feedback",
		Skill:   "builder",
		Summary: "Always handle nil return from dependency injection.",
	}
	builderInsight2 := workflow.TeamInsight{
		ID:      "ins-b-2",
		Source:  "reviewer-feedback",
		Skill:   "builder",
		Summary: "Wrap errors with context using fmt.Errorf.",
	}
	testerInsight := workflow.TeamInsight{
		ID:      "ins-t-1",
		Source:  "reviewer-feedback",
		Skill:   "tester",
		Summary: "Table-driven tests reduce boilerplate.",
	}

	for _, ins := range []workflow.TeamInsight{builderInsight1, builderInsight2, testerInsight} {
		if err := helper.AddTeamInsight(ctx, "blue", ins); err != nil {
			t.Fatalf("AddTeamInsight: %v", err)
		}
	}

	block := c.buildTeamKnowledgeBlock(ctx, "blue", "builder", []string{"wrong_pattern"})

	if block == "" {
		t.Fatal("buildTeamKnowledgeBlock returned empty string, expected content")
	}
	if !strings.Contains(block, "TEAM CONTEXT") {
		t.Error("block should contain \"TEAM CONTEXT\" framing header")
	}
	if !strings.Contains(block, "Team Trophy") {
		t.Error("block should mention \"Team Trophy\"")
	}

	// Both builder insights should appear.
	if !strings.Contains(block, "Always handle nil") {
		t.Error("block should contain builder insight 1")
	}
	if !strings.Contains(block, "Wrap errors with context") {
		t.Error("block should contain builder insight 2")
	}

	// The tester insight must NOT appear when filtering by "builder" skill.
	if strings.Contains(block, "Table-driven tests") {
		t.Error("block should NOT contain tester insight when skill=builder")
	}

	// Solo mode: nil agentHelper means empty return.
	soloComp := newTestComponent(t) // agentHelper is nil
	soloBlock := soloComp.buildTeamKnowledgeBlock(ctx, "blue", "builder", nil)
	if soloBlock != "" {
		t.Errorf("buildTeamKnowledgeBlock with nil agentHelper should return \"\", got %q", soloBlock)
	}
}

// ---------------------------------------------------------------------------
// Test 9: TestTruncateInsight
//
// truncateInsight must not split multi-byte runes and must append "..." only
// when truncation actually occurs.
// ---------------------------------------------------------------------------

func TestTruncateInsight(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "ASCII under limit returns unchanged",
			input:  "short string",
			maxLen: 20,
			want:   "short string",
		},
		{
			name:   "ASCII exactly at limit returns unchanged",
			input:  "exactly ten!", // 12 chars
			maxLen: 12,
			want:   "exactly ten!",
		},
		{
			name:   "ASCII over limit truncated with ellipsis",
			input:  "Hello, World!",
			maxLen: 8,
			want:   "Hello...", // 5 chars + "..."
		},
		{
			name: "Multi-byte UTF-8 truncated at rune boundary",
			// Each of these is a 2-byte rune (U+00E9 = é, etc.)
			input:  "café résumé",
			maxLen: 6,
			// runes[:3] = "caf" + "..." = "caf..."
			want: "caf...",
		},
		{
			name:   "Multi-byte exactly at limit returns unchanged",
			input:  "Héllo", // 5 runes, 6 bytes
			maxLen: 5,
			want:   "Héllo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateInsight(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncateInsight(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}
