package commands

import (
	"testing"
)

func TestParseExploreArgs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantTopic   string
		wantSkipLLM bool
		wantHelp    bool
	}{
		{
			name:        "simple topic (LLM is default)",
			input:       "authentication system",
			wantTopic:   "authentication system",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "topic with --manual flag",
			input:       "authentication system --manual",
			wantTopic:   "authentication system",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "topic with -m short flag",
			input:       "authentication system -m",
			wantTopic:   "authentication system",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "manual flag at beginning",
			input:       "--manual authentication system",
			wantTopic:   "authentication system",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "manual flag in middle",
			input:       "authentication --manual system",
			wantTopic:   "authentication system",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "help flag long",
			input:       "--help",
			wantTopic:   "",
			wantSkipLLM: false,
			wantHelp:    true,
		},
		{
			name:        "help flag short",
			input:       "-h",
			wantTopic:   "",
			wantSkipLLM: false,
			wantHelp:    true,
		},
		{
			name:        "help with topic",
			input:       "some topic --help",
			wantTopic:   "some topic",
			wantSkipLLM: false,
			wantHelp:    true,
		},
		{
			name:        "empty input",
			input:       "",
			wantTopic:   "",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "only whitespace",
			input:       "   ",
			wantTopic:   "",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "single word topic",
			input:       "auth",
			wantTopic:   "auth",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "unknown flag is ignored",
			input:       "topic --unknown-flag",
			wantTopic:   "topic",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "multiple flags",
			input:       "topic --manual --help",
			wantTopic:   "topic",
			wantSkipLLM: true,
			wantHelp:    true,
		},
		{
			name:        "topic with hyphen",
			input:       "api-gateway setup",
			wantTopic:   "api-gateway setup",
			wantSkipLLM: false,
			wantHelp:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTopic, gotSkipLLM, gotHelp := parseExploreArgs(tt.input)

			if gotTopic != tt.wantTopic {
				t.Errorf("parseExploreArgs() topic = %q, want %q", gotTopic, tt.wantTopic)
			}
			if gotSkipLLM != tt.wantSkipLLM {
				t.Errorf("parseExploreArgs() skipLLM = %v, want %v", gotSkipLLM, tt.wantSkipLLM)
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

	// Should mention the command and new flags
	requiredContent := []string{
		"/explore",
		"--manual",
		"-m",
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
