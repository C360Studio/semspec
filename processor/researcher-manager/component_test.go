package researchermanager

import (
	"testing"

	"github.com/c360studio/semspec/prompt"
)

func TestResolveProvider(t *testing.T) {
	cases := []struct {
		modelName string
		want      prompt.Provider
	}{
		{"claude-sonnet", prompt.ProviderAnthropic},
		{"claude-opus", prompt.ProviderAnthropic},
		{"gemini-flash", prompt.ProviderGoogle},
		{"gemini-pro", prompt.ProviderGoogle},
		{"gpt-4o", prompt.ProviderOpenAI},
		{"qwen3-32b", prompt.ProviderOllama},
		{"unknown-model", ""}, // empty provider — assembler tolerates
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.modelName, func(t *testing.T) {
			got := resolveProvider(tc.modelName)
			if got != tc.want {
				t.Errorf("resolveProvider(%q) = %q; want %q", tc.modelName, got, tc.want)
			}
		})
	}
}

func TestSubjectSuffix(t *testing.T) {
	cases := []struct {
		subject string
		prefix  string
		want    string
	}{
		{"agent.research.requested.research-abc123", "agent.research.requested.", "research-abc123"},
		{"agent.research.requested.research-abc", "agent.research.requested.", "research-abc"},
		{"agent.task.something", "agent.research.requested.", ""},     // wrong prefix
		{"agent.research.requested.", "agent.research.requested.", ""}, // prefix-only
		{"", "agent.research.requested.", ""},                          // empty subject
	}
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			got := subjectSuffix(tc.subject, tc.prefix)
			if got != tc.want {
				t.Errorf("subjectSuffix(%q, %q) = %q; want %q", tc.subject, tc.prefix, got, tc.want)
			}
		})
	}
}

func TestResearcherAvailableToolNames(t *testing.T) {
	got := researcherAvailableToolNames()

	want := map[string]bool{
		"bash":            true,
		"http_request":    true,
		"web_search":      true,
		"answer_research": true,
	}

	if len(got) != len(want) {
		t.Errorf("got %d tools, want %d (%v)", len(got), len(want), got)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected tool in researcher palette: %q", name)
		}
	}

	// Critical guardrails — these must NEVER be in the researcher palette.
	forbidden := []string{"research", "submit_work", "write_todos", "scratchpad", "decompose_task"}
	for _, name := range got {
		for _, bad := range forbidden {
			if name == bad {
				t.Errorf("forbidden tool %q is in researcher palette — would allow recursion, code submission, or scope creep", name)
			}
		}
	}
}
