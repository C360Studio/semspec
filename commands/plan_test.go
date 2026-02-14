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
		wantUseLLM  bool
		wantHelp    bool
	}{
		{
			name:        "simple title",
			input:       "implement caching layer",
			wantTitle:   "implement caching layer",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "title with --llm flag",
			input:       "implement caching layer --llm",
			wantTitle:   "implement caching layer",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "title with -l short flag",
			input:       "implement caching layer -l",
			wantTitle:   "implement caching layer",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "llm flag at beginning",
			input:       "--llm implement caching layer",
			wantTitle:   "implement caching layer",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "llm flag in middle",
			input:       "implement --llm caching layer",
			wantTitle:   "implement caching layer",
			wantUseLLM:  true,
			wantHelp:    false,
		},
		{
			name:        "help flag long",
			input:       "--help",
			wantTitle:   "",
			wantUseLLM:  false,
			wantHelp:    true,
		},
		{
			name:        "help flag short",
			input:       "-h",
			wantTitle:   "",
			wantUseLLM:  false,
			wantHelp:    true,
		},
		{
			name:        "help with title",
			input:       "some title --help",
			wantTitle:   "some title",
			wantUseLLM:  false,
			wantHelp:    true,
		},
		{
			name:        "empty input",
			input:       "",
			wantTitle:   "",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "only whitespace",
			input:       "   ",
			wantTitle:   "",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "single word title",
			input:       "refactor",
			wantTitle:   "refactor",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "unknown flag is ignored",
			input:       "title --unknown-flag",
			wantTitle:   "title",
			wantUseLLM:  false,
			wantHelp:    false,
		},
		{
			name:        "multiple flags",
			input:       "title --llm --help",
			wantTitle:   "title",
			wantUseLLM:  true,
			wantHelp:    true,
		},
		{
			name:        "title with hyphen",
			input:       "api-gateway implementation",
			wantTitle:   "api-gateway implementation",
			wantUseLLM:  false,
			wantHelp:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotUseLLM, gotHelp := parsePlanArgs(tt.input)

			if gotTitle != tt.wantTitle {
				t.Errorf("parsePlanArgs() title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotUseLLM != tt.wantUseLLM {
				t.Errorf("parsePlanArgs() useLLM = %v, want %v", gotUseLLM, tt.wantUseLLM)
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

	// Should mention the command
	requiredContent := []string{
		"/plan",
		"--llm",
		"title",
	}

	for _, content := range requiredContent {
		if !strings.Contains(help, content) {
			t.Errorf("planHelpText() should contain %q", content)
		}
	}
}
