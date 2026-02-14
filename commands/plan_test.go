package commands

import (
	"strings"
	"testing"
)

func TestParsePlanArgs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantTitle   string
		wantSkipLLM bool
		wantHelp    bool
	}{
		{
			name:        "simple title (LLM is default)",
			input:       "implement caching layer",
			wantTitle:   "implement caching layer",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "title with --manual flag",
			input:       "implement caching layer --manual",
			wantTitle:   "implement caching layer",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "title with -m short flag",
			input:       "implement caching layer -m",
			wantTitle:   "implement caching layer",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "manual flag at beginning",
			input:       "--manual implement caching layer",
			wantTitle:   "implement caching layer",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "manual flag in middle",
			input:       "implement --manual caching layer",
			wantTitle:   "implement caching layer",
			wantSkipLLM: true,
			wantHelp:    false,
		},
		{
			name:        "help flag long",
			input:       "--help",
			wantTitle:   "",
			wantSkipLLM: false,
			wantHelp:    true,
		},
		{
			name:        "help flag short",
			input:       "-h",
			wantTitle:   "",
			wantSkipLLM: false,
			wantHelp:    true,
		},
		{
			name:        "help with title",
			input:       "some title --help",
			wantTitle:   "some title",
			wantSkipLLM: false,
			wantHelp:    true,
		},
		{
			name:        "empty input",
			input:       "",
			wantTitle:   "",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "only whitespace",
			input:       "   ",
			wantTitle:   "",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "single word title",
			input:       "refactor",
			wantTitle:   "refactor",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "unknown flag is ignored",
			input:       "title --unknown-flag",
			wantTitle:   "title",
			wantSkipLLM: false,
			wantHelp:    false,
		},
		{
			name:        "multiple flags",
			input:       "title --manual --help",
			wantTitle:   "title",
			wantSkipLLM: true,
			wantHelp:    true,
		},
		{
			name:        "title with hyphen",
			input:       "api-gateway implementation",
			wantTitle:   "api-gateway implementation",
			wantSkipLLM: false,
			wantHelp:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotSkipLLM, gotHelp := parsePlanArgs(tt.input)

			if gotTitle != tt.wantTitle {
				t.Errorf("parsePlanArgs() title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotSkipLLM != tt.wantSkipLLM {
				t.Errorf("parsePlanArgs() skipLLM = %v, want %v", gotSkipLLM, tt.wantSkipLLM)
			}
			if gotHelp != tt.wantHelp {
				t.Errorf("parsePlanArgs() help = %v, want %v", gotHelp, tt.wantHelp)
			}
		})
	}
}

func TestPlanHelpText(t *testing.T) {
	help := planHelpText()

	// Should contain usage information
	if help == "" {
		t.Error("planHelpText() should return non-empty help text")
	}

	// Should mention the command and new flags
	requiredContent := []string{
		"/plan",
		"--manual",
		"-m",
		"title",
	}

	for _, content := range requiredContent {
		if !strings.Contains(help, content) {
			t.Errorf("planHelpText() should contain %q", content)
		}
	}
}
