package commands

import (
	"strings"
	"testing"
)

func TestParsePlanArgs(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantTitle       string
		wantSkipLLM     bool
		wantAutoApprove bool
		wantHelp        bool
		wantParallel    int
		wantFocuses     []string
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
		{
			name:         "parallel flag with space",
			input:        "add auth -p 1",
			wantTitle:    "add auth",
			wantParallel: 1,
		},
		{
			name:         "parallel flag without space",
			input:        "add auth -p2",
			wantTitle:    "add auth",
			wantParallel: 2,
		},
		{
			name:         "parallel flag long form",
			input:        "add auth --parallel 3",
			wantTitle:    "add auth",
			wantParallel: 3,
		},
		{
			name:        "focus flag with comma-separated values",
			input:       "add auth --focus api,security",
			wantTitle:   "add auth",
			wantFocuses: []string{"api", "security"},
		},
		{
			name:        "focus flag with equals",
			input:       "add auth --focus=api,security,data",
			wantTitle:   "add auth",
			wantFocuses: []string{"api", "security", "data"},
		},
		{
			name:         "combined flags",
			input:        "add auth -p 2 --focus api,security",
			wantTitle:    "add auth",
			wantParallel: 2,
			wantFocuses:  []string{"api", "security"},
		},
		{
			name:            "auto flag long form",
			input:           "add auth --auto",
			wantTitle:       "add auth",
			wantAutoApprove: true,
		},
		{
			name:            "auto flag short form",
			input:           "add auth -a",
			wantTitle:       "add auth",
			wantAutoApprove: true,
		},
		{
			name:            "manual and auto flags combined",
			input:           "add auth --manual --auto",
			wantTitle:       "add auth",
			wantSkipLLM:     true,
			wantAutoApprove: true,
		},
		{
			name:            "auto flag at beginning",
			input:           "--auto implement caching",
			wantTitle:       "implement caching",
			wantAutoApprove: true,
		},
		{
			name:            "auto flag in middle",
			input:           "implement --auto caching",
			wantTitle:       "implement caching",
			wantAutoApprove: true,
		},
		{
			name:            "all flags combined",
			input:           "add auth -m -a -p 2 --focus api",
			wantTitle:       "add auth",
			wantSkipLLM:     true,
			wantAutoApprove: true,
			wantParallel:    2,
			wantFocuses:     []string{"api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := parsePlanArgs(tt.input)

			if opts.Title != tt.wantTitle {
				t.Errorf("parsePlanArgs() title = %q, want %q", opts.Title, tt.wantTitle)
			}
			if opts.SkipLLM != tt.wantSkipLLM {
				t.Errorf("parsePlanArgs() skipLLM = %v, want %v", opts.SkipLLM, tt.wantSkipLLM)
			}
			if opts.AutoApprove != tt.wantAutoApprove {
				t.Errorf("parsePlanArgs() autoApprove = %v, want %v", opts.AutoApprove, tt.wantAutoApprove)
			}
			if opts.ShowHelp != tt.wantHelp {
				t.Errorf("parsePlanArgs() help = %v, want %v", opts.ShowHelp, tt.wantHelp)
			}
			if opts.Parallel != tt.wantParallel {
				t.Errorf("parsePlanArgs() parallel = %d, want %d", opts.Parallel, tt.wantParallel)
			}
			if !equalStringSlices(opts.Focuses, tt.wantFocuses) {
				t.Errorf("parsePlanArgs() focuses = %v, want %v", opts.Focuses, tt.wantFocuses)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		"--auto",      // auto-approve flag
		"-a",          // short auto flag
		"title",
		"-p",          // parallel flag
		"--focus",     // focus flag
		"coordinator", // mentions coordinator mode
		"/approve",    // mentions approve command
		"draft",       // mentions draft workflow
	}

	for _, content := range requiredContent {
		if !strings.Contains(help, content) {
			t.Errorf("planHelpText() should contain %q", content)
		}
	}
}
