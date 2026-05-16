package research

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

// TestExecute_RejectsMissingArgs covers the cheap-branch validation that
// happens before any KV/NATS I/O. We construct the executor with nil
// dependencies — these branches must short-circuit before reaching them.
func TestExecute_RejectsMissingArgs(t *testing.T) {
	exec := NewExecutor(nil, nil, nil)

	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing question",
			args:    map[string]any{"sources": []any{"github.com/x/y"}},
			wantErr: `missing required argument "question"`,
		},
		{
			name:    "empty question",
			args:    map[string]any{"question": "", "sources": []any{"github.com/x/y"}},
			wantErr: `missing required argument "question"`,
		},
		{
			name:    "missing sources",
			args:    map[string]any{"question": "what?"},
			wantErr: `missing required argument "sources"`,
		},
		{
			name:    "empty sources",
			args:    map[string]any{"question": "what?", "sources": []any{}},
			wantErr: `missing required argument "sources"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := exec.Execute(context.Background(), agentic.ToolCall{
				ID:        "call-1",
				LoopID:    "loop-1",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("Execute returned err (want tool result): %v", err)
			}
			if !strings.Contains(res.Error, tc.wantErr) {
				t.Errorf("Error field = %q; want substring %q", res.Error, tc.wantErr)
			}
		})
	}
}

// TestExecute_BackendUnavailable verifies the executor returns a graceful
// tool error instead of panicking when wired without a store/NATS.
func TestExecute_BackendUnavailable(t *testing.T) {
	exec := NewExecutor(nil, nil, nil)
	res, err := exec.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-1",
		LoopID:    "loop-1",
		Arguments: map[string]any{"question": "Q?", "sources": []any{"x"}},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if !strings.Contains(res.Error, "research backend not configured") {
		t.Errorf("Error = %q; want 'research backend not configured'", res.Error)
	}
}

// TestRenderAnswer covers the dev-facing tool_result formatting. Citations
// must render as pointers (URL or file + lines), never as inline content.
func TestRenderAnswer(t *testing.T) {
	cases := []struct {
		name string
		r    *workflow.Research
		want string
	}{
		{
			name: "no citations",
			r:    &workflow.Research{Answer: "Plain answer."},
			want: "Plain answer.",
		},
		{
			name: "single url citation with lines",
			r: &workflow.Research{
				Answer:    "AbstractSensorModule has init/start/stop.",
				Citations: []workflow.Citation{{URL: "https://raw.example/x.java", Lines: "45-52"}},
			},
			want: "AbstractSensorModule has init/start/stop.\n\nCitations:\n- https://raw.example/x.java (lines 45-52)",
		},
		{
			name: "file citation no lines",
			r: &workflow.Research{
				Answer:    "see local",
				Citations: []workflow.Citation{{File: "/sources/foo.go"}},
			},
			want: "see local\n\nCitations:\n- /sources/foo.go",
		},
		{
			name: "multiple citations",
			r: &workflow.Research{
				Answer: "ans",
				Citations: []workflow.Citation{
					{URL: "https://a.test"},
					{File: "/b.go", Lines: "1"},
				},
			},
			want: "ans\n\nCitations:\n- https://a.test\n- /b.go (lines 1)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderAnswer(tc.r)
			if got != tc.want {
				t.Errorf("renderAnswer =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestStringSliceArg covers the JSON-decode coercion path. The agentic
// loop's tool args arrive as map[string]any so a sources: ["a","b"] slice
// shows up as []any{"a","b"}; the helper must coerce element-by-element.
func TestStringSliceArg(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"absent", map[string]any{}, nil},
		{"native []string", map[string]any{"k": []string{"a", "b"}}, []string{"a", "b"}},
		{"json []any", map[string]any{"k": []any{"a", "b"}}, []string{"a", "b"}},
		{"json mixed types skip non-string", map[string]any{"k": []any{"a", 5, "b"}}, []string{"a", "b"}},
		{"wrong type", map[string]any{"k": "scalar"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stringSliceArg(tc.args, "k")
			if !stringSlicesEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCitationsArg covers the JSON-decode path for citations payloads.
func TestCitationsArg(t *testing.T) {
	t.Run("absent returns nil nil", func(t *testing.T) {
		got, err := citationsArg(map[string]any{}, "citations")
		if err != nil || got != nil {
			t.Errorf("got (%v, %v); want (nil, nil)", got, err)
		}
	})

	t.Run("wrong type errors", func(t *testing.T) {
		_, err := citationsArg(map[string]any{"citations": "scalar"}, "citations")
		if err == nil || !strings.Contains(err.Error(), "must be an array") {
			t.Errorf("want array error; got %v", err)
		}
	})

	t.Run("non-object element errors", func(t *testing.T) {
		_, err := citationsArg(map[string]any{"citations": []any{"plain string"}}, "citations")
		if err == nil || !strings.Contains(err.Error(), "citations[0] must be an object") {
			t.Errorf("want object error; got %v", err)
		}
	})

	t.Run("valid url citation", func(t *testing.T) {
		got, err := citationsArg(map[string]any{
			"citations": []any{
				map[string]any{"url": "https://a", "lines": "1-5"},
			},
		}, "citations")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(got) != 1 || got[0].URL != "https://a" || got[0].Lines != "1-5" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("valid file citation", func(t *testing.T) {
		got, err := citationsArg(map[string]any{
			"citations": []any{
				map[string]any{"file": "/x.go"},
			},
		}, "citations")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(got) != 1 || got[0].File != "/x.go" {
			t.Errorf("got %+v", got)
		}
	})
}

// TestTruncate covers the log-line truncation helper.
func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"longer than the limit", 5, "longe…"},
	}
	for _, tc := range cases {
		got := truncate(tc.in, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q; want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

// TestListTools covers the tool definition wire shape — the LLM sees this
// in its function-definition list. We assert the required-args contract
// and that the description isn't goodhart-prone (no "be short" / "be
// shallow" framing that would optimize against a metric).
func TestListTools(t *testing.T) {
	exec := NewExecutor(nil, nil, nil)
	defs := exec.ListTools()
	if len(defs) != 1 {
		t.Fatalf("ListTools returned %d defs; want 1", len(defs))
	}
	d := defs[0]
	if d.Name != "research" {
		t.Errorf("Name = %q; want research", d.Name)
	}
	// Description must not contain length-anchored framing — those are
	// the goodhart shapes we explicitly rejected during design (see
	// project_research_tool_plan_2026_05_14).
	for _, banned := range []string{"be short", "be shallow", "≤ 4K", "MUST be brief"} {
		if strings.Contains(d.Description, banned) {
			t.Errorf("Description contains goodhart-prone phrase %q", banned)
		}
	}
	// Required args sanity. Parameters is map[string]any; "required" is
	// declared as []string in the literal so the type assertion is safe.
	req, _ := d.Parameters["required"].([]string)
	if !stringSlicesEqual(req, []string{"question", "sources"}) {
		t.Errorf("required = %v; want [question sources]", req)
	}
}

// stringSlicesEqual is a small helper since Go's slices.Equal lives in
// experimental in older versions. Same shape as standard slices.Equal.
func stringSlicesEqual(a, b []string) bool {
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
