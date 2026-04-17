package executionmanager

import (
	"context"
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
			{Name: "blue", Members: []TeamMemberEntry{{Role: "developer", Model: "default"}}},
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
				{Role: "developer", Model: "default"},
				{Role: "reviewer", Model: "default"},
			}},
			{Name: "red", Members: []TeamMemberEntry{
				{Role: "developer", Model: "fast"},
				{Role: "reviewer", Model: "fast"},
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

func TestTeamsEnabled_FalseWhenNoLessonWriter(t *testing.T) {
	c := newTestComponent(t)
	// lessonWriter is nil by default from newTestComponent.
	if c.teamsEnabled() {
		t.Error("teamsEnabled() should be false when lessonWriter is nil")
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

// ---------------------------------------------------------------------------
// Sandbox-required validation
// ---------------------------------------------------------------------------

func TestStart_FailsWithoutSandbox(t *testing.T) {
	c := newTestComponent(t) // SandboxURL is empty by default
	ctx := context.Background()

	err := c.Start(ctx)
	if err == nil {
		t.Fatal("Start() should fail when SandboxURL is not configured")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("error should mention sandbox, got: %q", err.Error())
	}
}

func TestSandboxFieldNonNil_WhenURLConfigured(t *testing.T) {
	// Verify that newWorktreeManager returns a non-nil sandbox when URL is set.
	mgr := newWorktreeManager("http://localhost:8090")
	if mgr == nil {
		t.Fatal("newWorktreeManager should return non-nil when URL is provided")
	}

	// And nil when empty.
	mgr = newWorktreeManager("")
	if mgr != nil {
		t.Fatal("newWorktreeManager should return nil when URL is empty")
	}
}
