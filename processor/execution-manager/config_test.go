package executionmanager

import (
	"context"
	"strings"
	"testing"
)

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

// ---------------------------------------------------------------------------
// requireDeveloperDiff defaults — closes a fixture-disable contract.
// ---------------------------------------------------------------------------

func TestRequireDeveloperDiff_DefaultsTrue(t *testing.T) {
	cfg := Config{}
	if !cfg.requireDeveloperDiff() {
		t.Error("requireDeveloperDiff: want true when unset, got false")
	}
}

func TestRequireDeveloperDiff_RespectsExplicitFalse(t *testing.T) {
	disabled := false
	cfg := Config{RequireDeveloperDiff: &disabled}
	if cfg.requireDeveloperDiff() {
		t.Error("requireDeveloperDiff: want false when *false, got true")
	}
}
