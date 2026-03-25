package executionmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
)

// ---------------------------------------------------------------------------
// TeamsConfig validation tests
// ---------------------------------------------------------------------------

func TestConfig_Validate_TeamsDisabled_NoRosterRequired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{}
	cfg.Teams.Enabled = false
	// No roster entries — must be valid because teams is off.
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with teams disabled should pass, got error: %v", err)
	}
}

func TestConfig_Validate_TeamsEnabled_RequiresAtLeastTwoTeams(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{}
	cfg.Teams.Enabled = true

	// Zero teams.
	if err := cfg.Validate(); err == nil {
		t.Error("Validate with teams enabled and 0 teams should fail, got nil")
	}

	// One team — still insufficient (need blue + red).
	cfg.Teams.Roster = []TeamRosterEntry{
		{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate with teams enabled and 1 team should fail, got nil")
	}

	// Two teams — meets the minimum.
	cfg.Teams.Roster = append(cfg.Teams.Roster,
		TeamRosterEntry{Name: "red", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
	)
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with teams enabled and 2 teams should pass, got error: %v", err)
	}
}

func TestConfig_Validate_TeamsEnabled_TeamWithNoMembersFails(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{}
	cfg.Teams.Enabled = true
	cfg.Teams.Roster = []TeamRosterEntry{
		{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
		{Name: "red", Members: nil}, // no members
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate with a team having 0 members should fail, got nil")
	}
	if !strings.Contains(err.Error(), "red") {
		t.Errorf("error should mention the offending team name %q, got: %v", "red", err)
	}
}

func TestConfig_Validate_TeamsEnabled_TwoTeamsMultipleMembers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{}
	cfg.Teams.Enabled = true
	cfg.Teams.Roster = []TeamRosterEntry{
		{
			Name: "blue",
			Members: []TeamMemberEntry{
				{Role: "tester", Model: "default"},
				{Role: "builder", Model: "default"},
				{Role: "reviewer", Model: "default"},
			},
		},
		{
			Name: "red",
			Members: []TeamMemberEntry{
				{Role: "tester", Model: "fast"},
				{Role: "builder", Model: "fast"},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with valid two-team roster should pass, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// teamsEnabled helper tests
// ---------------------------------------------------------------------------

func TestTeamsEnabled_FalseWhenDisabled(t *testing.T) {
	c := newTestComponent(t)
	c.config.Teams = &TeamsConfig{Enabled: false}
	c.config.Teams.Roster = []TeamRosterEntry{
		{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
		{Name: "red", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
	}
	if c.teamsEnabled() {
		t.Error("teamsEnabled() should be false when Enabled=false")
	}
}

func TestTeamsEnabled_FalseWhenFewerThanTwoTeams(t *testing.T) {
	c := newTestComponent(t)
	c.config.Teams = &TeamsConfig{Enabled: true}
	c.config.Teams.Roster = []TeamRosterEntry{
		{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
	}
	if c.teamsEnabled() {
		t.Error("teamsEnabled() should be false with only 1 roster entry")
	}
}

func TestTeamsEnabled_TrueWithTwoTeams(t *testing.T) {
	c := newTestComponent(t)
	c.config.Teams = &TeamsConfig{Enabled: true}
	c.config.Teams.Roster = []TeamRosterEntry{
		{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
		{Name: "red", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
	}
	if !c.teamsEnabled() {
		t.Error("teamsEnabled() should be true with Enabled=true and 2 teams")
	}
}

// ---------------------------------------------------------------------------
// seedTeams tests
// ---------------------------------------------------------------------------

// newTeamTestComponent builds a Component wired with a mock KV and two teams.
func newTeamTestComponent(t *testing.T) (*Component, *agentgraph.Helper) {
	t.Helper()
	c, helper := newAgentTestComponent(t)
	c.config.Teams = &TeamsConfig{
		Enabled: true,
		Roster: []TeamRosterEntry{
			{
				Name: "blue",
				Members: []TeamMemberEntry{
					{Role: "tester", Model: "default"},
					{Role: "builder", Model: "default"},
				},
			},
			{
				Name: "red",
				Members: []TeamMemberEntry{
					{Role: "reviewer", Model: "fast"},
				},
			},
		},
	}
	return c, helper
}

func TestSeedTeams_CreatesTeamsAndAgents(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)

	c.seedTeams()

	// Both teams must exist with correct MemberIDs.
	blueTeam, err := helper.GetTeam(ctx, "blue")
	if err != nil {
		t.Fatalf("GetTeam(blue): %v", err)
	}
	if blueTeam.Status != workflow.TeamActive {
		t.Errorf("blue team status = %q, want %q", blueTeam.Status, workflow.TeamActive)
	}
	if len(blueTeam.MemberIDs) != 2 {
		t.Errorf("blue team MemberIDs = %v, want 2 members", blueTeam.MemberIDs)
	}

	redTeam, err := helper.GetTeam(ctx, "red")
	if err != nil {
		t.Fatalf("GetTeam(red): %v", err)
	}
	if redTeam.Status != workflow.TeamActive {
		t.Errorf("red team status = %q, want %q", redTeam.Status, workflow.TeamActive)
	}
	if len(redTeam.MemberIDs) != 1 {
		t.Errorf("red team MemberIDs = %v, want 1 member", redTeam.MemberIDs)
	}

	// All agents must exist and be linked to their team.
	type agentCheck struct {
		id   string
		role string
		team string
	}
	checks := []agentCheck{
		{id: "blue-tester", role: "tester", team: "blue"},
		{id: "blue-builder", role: "builder", team: "blue"},
		{id: "red-reviewer", role: "reviewer", team: "red"},
	}
	for _, check := range checks {
		agent, err := helper.GetAgent(ctx, check.id)
		if err != nil {
			t.Fatalf("GetAgent(%q): %v", check.id, err)
		}
		if agent.Role != check.role {
			t.Errorf("agent %q role = %q, want %q", check.id, agent.Role, check.role)
		}
		// Verify the team linkage via GetTeamForAgent.
		teamID, err := helper.GetTeamForAgent(ctx, check.id)
		if err != nil {
			t.Fatalf("GetTeamForAgent(%q): %v", check.id, err)
		}
		if teamID != check.team {
			t.Errorf("agent %q teamID = %q, want %q", check.id, teamID, check.team)
		}
	}
}

func TestSeedTeams_NoOpWhenDisabled(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)
	c.config.Teams.Enabled = false // disable after newTeamTestComponent set it to true

	c.seedTeams()

	// No team entities should have been written.
	for _, teamName := range []string{"blue", "red"} {
		if _, err := helper.GetTeam(ctx, teamName); err == nil {
			t.Errorf("GetTeam(%q) should return error when seeding was skipped, got nil", teamName)
		}
	}
}

func TestSeedTeams_NoOpWhenAgentHelperNil(t *testing.T) {
	c := newTestComponent(t)
	c.config.Teams = &TeamsConfig{
		Enabled: true,
		Roster: []TeamRosterEntry{
			{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
			{Name: "red", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
		},
	}
	// agentHelper is nil — seedTeams must not panic.
	c.seedTeams()
}

func TestSeedTeams_Idempotent(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)

	// Call twice — second call must not return an error or corrupt state.
	c.seedTeams()
	c.seedTeams()

	// Team should still exist with the correct status.
	team, err := helper.GetTeam(ctx, "blue")
	if err != nil {
		t.Fatalf("GetTeam after idempotent seed: %v", err)
	}
	if team.Status != workflow.TeamActive {
		t.Errorf("team status after idempotent seed = %q, want %q", team.Status, workflow.TeamActive)
	}
}
