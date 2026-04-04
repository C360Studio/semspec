package bash

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestFilterEnv_RemovesSensitiveVars(t *testing.T) {
	// Save and restore env.
	orig := os.Environ()
	defer func() {
		os.Clearenv()
		for _, e := range orig {
			k, v, _ := strings.Cut(e, "=")
			os.Setenv(k, v)
		}
	}()

	// Set up test env.
	os.Clearenv()
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("HOME", "/home/test")
	os.Setenv("GOPATH", "/go")
	os.Setenv("BRAVE_SEARCH_API_KEY", "secret-key")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	os.Setenv("OPENAI_API_KEY", "openai-key")
	os.Setenv("DATABASE_URL", "postgres://...")
	os.Setenv("MY_APP_TOKEN", "token-value")
	os.Setenv("ANTHROPIC_API_KEY", "claude-key")
	os.Setenv("NORMAL_VAR", "safe-value")

	filtered := filterEnv()

	allowed := make(map[string]bool)
	for _, e := range filtered {
		k, _, _ := strings.Cut(e, "=")
		allowed[k] = true
	}

	// These should be kept.
	for _, want := range []string{"PATH", "HOME", "GOPATH", "NORMAL_VAR"} {
		if !allowed[want] {
			t.Errorf("filterEnv() should keep %s", want)
		}
	}

	// These should be stripped.
	for _, blocked := range []string{
		"BRAVE_SEARCH_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"OPENAI_API_KEY",
		"DATABASE_URL",
		"MY_APP_TOKEN",
		"ANTHROPIC_API_KEY",
	} {
		if allowed[blocked] {
			t.Errorf("filterEnv() should strip %s", blocked)
		}
	}
}

func TestNewExecutor_DefaultTimeout(t *testing.T) {
	e := NewExecutor(t.TempDir(), "")
	if got := e.effectiveTimeout(); got != defaultTimeout {
		t.Errorf("effectiveTimeout() = %v, want %v", got, defaultTimeout)
	}
}

func TestNewExecutor_WithDefaultTimeout(t *testing.T) {
	e := NewExecutor(t.TempDir(), "", WithDefaultTimeout(10*time.Minute))
	if got := e.effectiveTimeout(); got != 10*time.Minute {
		t.Errorf("effectiveTimeout() = %v, want 10m", got)
	}
}

func TestNewExecutor_ZeroTimeoutUsesDefault(t *testing.T) {
	e := NewExecutor(t.TempDir(), "", WithDefaultTimeout(0))
	if got := e.effectiveTimeout(); got != defaultTimeout {
		t.Errorf("effectiveTimeout() with zero = %v, want %v", got, defaultTimeout)
	}
}
