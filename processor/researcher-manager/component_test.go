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

// TestSubjectCovers locks in the boot-time stream-coverage check. Take-26
// (2026-05-14) hit a 7-min wedge because the AGENT stream was missing
// agent.research.requested.> and the consumer was created on a stream
// that would never deliver — every research() call's publish hung until
// timeout, costing 5 min + tokens per call. The Start() validation now
// uses subjectCovers to fail-loud at boot when the stream config is
// missing the subject. These cases lock the coverage rules.
func TestSubjectCovers(t *testing.T) {
	cases := []struct {
		name   string
		parent string
		child  string
		want   bool
	}{
		{
			name:   "exact match",
			parent: "agent.research.requested.>",
			child:  "agent.research.requested.>",
			want:   true,
		},
		{
			name:   "parent > wildcard covers child",
			parent: "agent.>",
			child:  "agent.research.requested.>",
			want:   true,
		},
		{
			name:   "root > covers everything",
			parent: ">",
			child:  "agent.research.requested.>",
			want:   true,
		},
		{
			name:   "sibling subject does not cover",
			parent: "agent.task.>",
			child:  "agent.research.requested.>",
			want:   false,
		},
		{
			name:   "exact-without-> does not cover .>",
			parent: "agent.research.requested",
			child:  "agent.research.requested.>",
			want:   false,
		},
		{
			name:   "deeper parent prefix does not cover shorter child",
			parent: "agent.research.requested.foo.>",
			child:  "agent.research.requested.>",
			want:   false,
		},
		{
			name:   "case-sensitive (NATS convention)",
			parent: "Agent.>",
			child:  "agent.research.requested.>",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subjectCovers(tc.parent, tc.child)
			if got != tc.want {
				t.Errorf("subjectCovers(%q, %q) = %v; want %v", tc.parent, tc.child, got, tc.want)
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
