package executionmanager

import (
	"strings"
	"testing"
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

// ---------------------------------------------------------------------------
// LessonThreshold defaults
// ---------------------------------------------------------------------------

func TestConfig_WithDefaults_LessonThreshold(t *testing.T) {
	cfg := Config{}
	cfg = cfg.withDefaults()
	if cfg.LessonThreshold != DefaultLessonThreshold {
		t.Errorf("LessonThreshold = %d, want %d", cfg.LessonThreshold, DefaultLessonThreshold)
	}
}
