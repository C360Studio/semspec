package executionmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
)

func boolPtr(b bool) *bool { return &b }

// ---------------------------------------------------------------------------
// TeamsConfig validation tests
// ---------------------------------------------------------------------------

func TestConfig_Validate_TeamsDisabled_NoRosterRequired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{Enabled: boolPtr(false)}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with teams disabled should pass, got error: %v", err)
	}
}

func TestConfig_Validate_EmptyRosterIsValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{}
	// Empty roster is valid — seedTeams auto-generates defaults.
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with empty roster should pass (default generation), got: %v", err)
	}
}

func TestConfig_Validate_TeamWithNoMembersFails(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{
		Roster: []TeamRosterEntry{
			{Name: "blue", Members: []TeamMemberEntry{{Role: "builder", Model: "default"}}},
			{Name: "red", Members: nil}, // no members
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate with a team having 0 members should fail, got nil")
	}
	if !strings.Contains(err.Error(), "red") {
		t.Errorf("error should mention the offending team name %q, got: %v", "red", err)
	}
}

func TestConfig_Validate_ExplicitRosterValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{
		Roster: []TeamRosterEntry{
			{Name: "blue", Members: []TeamMemberEntry{
				{Role: "tester", Model: "default"},
				{Role: "builder", Model: "default"},
				{Role: "reviewer", Model: "default"},
			}},
			{Name: "red", Members: []TeamMemberEntry{
				{Role: "tester", Model: "fast"},
				{Role: "builder", Model: "fast"},
			}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with valid roster should pass, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// teamsEnabled helper tests
// ---------------------------------------------------------------------------

func TestTeamsEnabled_FalseWhenKillSwitch(t *testing.T) {
	c := newTestComponent(t)
	c.config.Teams = &TeamsConfig{Enabled: boolPtr(false)}
	if c.teamsEnabled() {
		t.Error("teamsEnabled() should be false when Enabled=false (kill switch)")
	}
}

func TestTeamsEnabled_FalseWhenNoAgentHelper(t *testing.T) {
	c := newTestComponent(t)
	// agentHelper is nil by default from newTestComponent.
	if c.teamsEnabled() {
		t.Error("teamsEnabled() should be false when agentHelper is nil")
	}
}

func TestTeamsEnabled_TrueWithAgentHelper(t *testing.T) {
	c, _ := newAgentTestComponent(t)
	// agentHelper is set — teams should be on by default.
	if !c.teamsEnabled() {
		t.Error("teamsEnabled() should be true when agentHelper is available")
	}
}

func TestTeamsEnabled_TrueWithNilTeamsConfig(t *testing.T) {
	c, _ := newAgentTestComponent(t)
	c.config.Teams = nil // no teams config at all
	if !c.teamsEnabled() {
		t.Error("teamsEnabled() should be true with nil Teams config (default: always on)")
	}
}

func TestTeamsEnabled_TrueWithEmptyTeamsConfig(t *testing.T) {
	c, _ := newAgentTestComponent(t)
	c.config.Teams = &TeamsConfig{} // empty config, Enabled is nil
	if !c.teamsEnabled() {
		t.Error("teamsEnabled() should be true with empty Teams config (Enabled nil = on)")
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
	c.config.Teams.Enabled = boolPtr(false) // kill switch

	c.seedTeams()

	// No team entities should have been written.
	for _, teamName := range []string{"blue", "red"} {
		if _, err := helper.GetTeam(ctx, teamName); err == nil {
			t.Errorf("GetTeam(%q) should return error when seeding was skipped, got nil", teamName)
		}
	}
}

func TestSeedTeams_DefaultRosterWhenNoConfig(t *testing.T) {
	ctx := context.Background()
	c, helper := newAgentTestComponent(t)
	// No Teams config at all — should auto-generate alpha + bravo.
	c.config.Teams = nil

	c.seedTeams()

	for _, teamName := range []string{"alpha", "bravo"} {
		team, err := helper.GetTeam(ctx, teamName)
		if err != nil {
			t.Fatalf("GetTeam(%q): %v", teamName, err)
		}
		if len(team.MemberIDs) != 3 {
			t.Errorf("team %q MemberIDs = %v, want 3 members", teamName, team.MemberIDs)
		}
	}
}

func TestSeedTeams_NoOpWhenAgentHelperNil(t *testing.T) {
	c := newTestComponent(t)
	// agentHelper is nil — seedTeams must not panic.
	c.seedTeams()
}

func TestSeedTeams_Idempotent(t *testing.T) {
	ctx := context.Background()
	c, helper := newTeamTestComponent(t)

	c.seedTeams()
	c.seedTeams()

	team, err := helper.GetTeam(ctx, "blue")
	if err != nil {
		t.Fatalf("GetTeam after idempotent seed: %v", err)
	}
	if team.Status != workflow.TeamActive {
		t.Errorf("team status after idempotent seed = %q, want %q", team.Status, workflow.TeamActive)
	}
}
