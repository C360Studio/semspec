package commands

import (
	"testing"
)

func TestParseExploreArgs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantTopic   string
		wantUseLLM  bool
		wantHelp    bool
	}{
		{
			name:        "simple topic",
			input:       "authentication system",
			wantTopic:   "authentication system",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "topic with --llm flag",
			input:       "authentication system --llm",
			wantTopic:   "authentication system",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "topic with -l short flag",
			input:       "authentication system -l",
			wantTopic:   "authentication system",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "llm flag at beginning",
			input:       "--llm authentication system",
			wantTopic:   "authentication system",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "llm flag in middle",
			input:       "authentication --llm system",
			wantTopic:   "authentication system",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "help flag long",
			input:       "--help",
			wantTopic:   "",
			wantUseLLM:  false,
			wantHelp:    true,
		},
		{
			name:        "help flag short",
			input:       "-h",
			wantTopic:   "",
			wantUseLLM:  false,
			wantHelp:    true,
		},
		{
			name:        "help with topic",
			input:       "some topic --help",
			wantTopic:   "some topic",
			wantUseLLM:  false,
			wantHelp:    true,
		},
		{
			name:        "empty input",
			input:       "",
			wantTopic:   "",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "only whitespace",
			input:       "   ",
			wantTopic:   "",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "single word topic",
			input:       "auth",
			wantTopic:   "auth",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "unknown flag is ignored",
			input:       "topic --unknown-flag",
			wantTopic:   "topic",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "multiple flags",
			input:       "topic --llm --help",
			wantTopic:   "topic",
			wantUseLLM:  true,
			wantHelp:    true,
		},
		{
			name:        "topic with hyphen",
			input:       "api-gateway setup",
			wantTopic:   "api-gateway setup",
			wantUseLLM:  false,
			wantHelp:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTopic, gotUseLLM, gotHelp := parseExploreArgs(tt.input)

			if gotTopic != tt.wantTopic {
				t.Errorf("parseExploreArgs() topic = %q, want %q", gotTopic, tt.wantTopic)
			}
			if gotUseLLM != tt.wantUseLLM {
				t.Errorf("parseExploreArgs() useLLM = %v, want %v", gotUseLLM, tt.wantUseLLM)
			}
			if gotHelp != tt.wantHelp {
				t.Errorf("parseExploreArgs() help = %v, want %v", gotHelp, tt.wantHelp)
			}
		})
	}
}

func TestExploreHelpText(t *testing.T) {
	help := exploreHelpText()

	// Should contain usage information
	if help == "" {
		t.Error("exploreHelpText() should return non-empty help text")
	}

	// Should mention the command
	requiredContent := []string{
		"/explore",
		"--llm",
		"-l",
		"topic",
	}

	for _, content := range requiredContent {
		if !contains(help, content) {
			t.Errorf("exploreHelpText() should contain %q", content)
		}
	}
}

// Helper function for case-insensitive contains
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
