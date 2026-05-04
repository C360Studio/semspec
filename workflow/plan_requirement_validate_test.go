package workflow

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestValidatorSentinelContract pins the wire contract between the workflow
// validators and downstream callers (notably requirement-generator's
// handlePublishError, which routes validator-rejection errors back to the
// agent for retry instead of failing the plan terminally). Two guarantees:
//
//  1. Each validator wraps its sentinel via fmt.Errorf("%w: ...", sentinel)
//     so errors.Is works for in-process callers (the planner-side HTTP
//     handler that returns 422).
//  2. The sentinel text appears in err.Error() so cross-process callers
//     that only see the string (the requirement-generator goes through
//     plan-manager's MutationResponse.Error JSON field) can still match.
//
// If you change a sentinel, update both the constant AND any string-based
// matchers that depend on it.
func TestValidatorSentinelContract(t *testing.T) {
	t.Run("file ownership empty wraps ErrInvalidFileOwnership", func(t *testing.T) {
		err := ValidateFileOwnershipPartition([]Requirement{
			{ID: "a", FilesOwned: nil},
			{ID: "b", FilesOwned: []string{"main.go"}},
		})
		if err == nil {
			t.Fatal("expected error for empty FilesOwned")
		}
		if !errors.Is(err, ErrInvalidFileOwnership) {
			t.Errorf("expected errors.Is(ErrInvalidFileOwnership), got %v", err)
		}
		if !strings.Contains(err.Error(), ErrInvalidFileOwnership.Error()) {
			t.Errorf("err.Error() %q must contain sentinel text %q", err.Error(), ErrInvalidFileOwnership.Error())
		}
	})

	t.Run("file ownership overlap wraps ErrInvalidFileOwnership", func(t *testing.T) {
		err := ValidateFileOwnershipPartition([]Requirement{
			{ID: "a", FilesOwned: []string{"main.go"}},
			{ID: "b", FilesOwned: []string{"main.go"}},
		})
		if err == nil {
			t.Fatal("expected error for overlapping FilesOwned")
		}
		if !errors.Is(err, ErrInvalidFileOwnership) {
			t.Errorf("expected errors.Is(ErrInvalidFileOwnership), got %v", err)
		}
	})

	t.Run("DAG self-reference wraps ErrInvalidRequirementDAG", func(t *testing.T) {
		err := ValidateRequirementDAG([]Requirement{
			{ID: "a", DependsOn: []string{"a"}},
		})
		if err == nil {
			t.Fatal("expected error for self-reference")
		}
		if !errors.Is(err, ErrInvalidRequirementDAG) {
			t.Errorf("expected errors.Is(ErrInvalidRequirementDAG), got %v", err)
		}
		if !strings.Contains(err.Error(), ErrInvalidRequirementDAG.Error()) {
			t.Errorf("err.Error() %q must contain sentinel text", err.Error())
		}
	})

	t.Run("DAG cycle wraps ErrInvalidRequirementDAG", func(t *testing.T) {
		err := ValidateRequirementDAG([]Requirement{
			{ID: "a", DependsOn: []string{"b"}},
			{ID: "b", DependsOn: []string{"a"}},
		})
		if err == nil {
			t.Fatal("expected error for cycle")
		}
		if !errors.Is(err, ErrInvalidRequirementDAG) {
			t.Errorf("expected errors.Is(ErrInvalidRequirementDAG), got %v", err)
		}
	})
}

func TestValidateFileOwnershipPartition(t *testing.T) {
	req := func(id string, deps []string, files []string) Requirement {
		return Requirement{ID: id, DependsOn: deps, FilesOwned: files}
	}

	tests := []struct {
		name        string
		reqs        []Requirement
		wantErr     bool
		errContains string
	}{
		{
			name: "empty slice passes",
			reqs: nil,
		},
		{
			name: "single requirement passes",
			reqs: []Requirement{req("a", nil, []string{"main.go"})},
		},
		{
			name: "disjoint files pass",
			reqs: []Requirement{
				req("a", nil, []string{"api/handler.go"}),
				req("b", nil, []string{"ui/form.go"}),
			},
		},
		{
			// Was previously back-compat amnesty; flipped 2026-04-29 after
			// Gemini @easy stalled at reviewing_qa because the validator
			// silently accepted requirements with files_owned=null. Empty
			// files_owned is now rejected when there's >1 requirement so
			// the prompt-side promise of enforcement is real.
			name: "no files_owned anywhere rejects",
			reqs: []Requirement{
				req("a", nil, nil),
				req("b", nil, nil),
			},
			wantErr:     true,
			errContains: "empty files_owned",
		},
		{
			// Same flip — partial coverage is a worse failure mode (the
			// declared req appears partitioned but the silent one could
			// touch anything), so any empty in a multi-req plan rejects.
			name: "one side missing files_owned rejects",
			reqs: []Requirement{
				req("a", nil, []string{"main.go"}),
				req("b", nil, nil),
			},
			wantErr:     true,
			errContains: "empty files_owned",
		},
		{
			// Single-req plans skip the whole check — no possible overlap.
			name: "single requirement with empty files_owned passes",
			reqs: []Requirement{req("solo", nil, nil)},
		},
		{
			// The exact bug from 2026-04-28 Gemini @easy: two parallel reqs
			// both rewriting main.go + main_test.go with no edge between them.
			// The plan-level merge can't reconcile two independent rewrites.
			name: "parallel reqs claiming same files reject",
			reqs: []Requirement{
				req("req.health.1", nil, []string{"main.go", "main_test.go"}),
				req("req.health.2", nil, []string{"main.go", "main_test.go"}),
			},
			wantErr:     true,
			errContains: "main.go",
		},
		{
			name: "overlap allowed when later depends on earlier (direct)",
			reqs: []Requirement{
				req("base", nil, []string{"main.go"}),
				req("extend", []string{"base"}, []string{"main.go"}),
			},
		},
		{
			name: "overlap allowed when later depends on earlier (transitive)",
			reqs: []Requirement{
				req("a", nil, []string{"main.go"}),
				req("b", []string{"a"}, []string{"helpers.go"}),
				req("c", []string{"b"}, []string{"main.go"}),
			},
		},
		{
			name: "partial overlap on one path with no edge rejects",
			reqs: []Requirement{
				req("a", nil, []string{"api/handler.go", "api/types.go"}),
				req("b", nil, []string{"api/types.go", "ui/form.go"}),
			},
			wantErr:     true,
			errContains: "api/types.go",
		},
		{
			name: "duplicate path within one requirement does not falsely conflict",
			reqs: []Requirement{
				req("a", nil, []string{"main.go", "main.go"}),
				req("b", nil, []string{"helpers.go"}),
			},
		},
		{
			name: "siblings under same parent still conflict if they overlap",
			reqs: []Requirement{
				req("parent", nil, []string{"setup.go"}),
				req("siblingA", []string{"parent"}, []string{"main.go"}),
				req("siblingB", []string{"parent"}, []string{"main.go"}),
			},
			wantErr:     true,
			errContains: "main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFileOwnershipPartition(tt.reqs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateFileOwnershipPartition() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want it to contain %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestTransitiveAncestors(t *testing.T) {
	reqs := []Requirement{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
		{ID: "d", DependsOn: []string{"a", "c"}},
	}
	got := transitiveAncestors(reqs)

	cases := []struct {
		id         string
		shouldHave []string
		shouldLack []string
	}{
		{"a", nil, []string{"b", "c", "d"}},
		{"b", []string{"a"}, []string{"c", "d"}},
		{"c", []string{"a", "b"}, []string{"d"}},
		{"d", []string{"a", "b", "c"}, nil},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			set := got[c.id]
			for _, want := range c.shouldHave {
				if !set[want] {
					t.Errorf("ancestors[%s] missing %s", c.id, want)
				}
			}
			for _, dont := range c.shouldLack {
				if set[dont] {
					t.Errorf("ancestors[%s] should not contain %s", c.id, dont)
				}
			}
		})
	}
}

// TestTransitiveAncestors_TolerantOfCycles pins the seen-set guard.
// ValidateRequirementDAG normally runs first and rejects cycles, but if a
// future caller forgets that, the closure walk must still terminate.
// Removing the seen-set check would hang this test instead of failing it
// silently in production.
func TestTransitiveAncestors_TolerantOfCycles(t *testing.T) {
	reqs := []Requirement{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		got := transitiveAncestors(reqs)
		if !got["a"]["b"] || !got["b"]["a"] {
			t.Errorf("expected each node to record the other as an ancestor; got %+v", got)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("transitiveAncestors did not terminate on cyclic input")
	}
}

func TestNormalizeFilePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"main.go", "main.go"},
		{"./main.go", "main.go"},
		{"  main.go  ", "main.go"},
		{"api//handler.go", "api/handler.go"},
		{"api/../main.go", "main.go"},
		{"a\\b\\c.go", "a/b/c.go"}, // Windows-style separators
		{"./", ""},                 // workspace root
		{".", ""},
		{"", ""},
		{"  ", ""},
		{"../escape.go", ""}, // out-of-workspace dropped
		{"..", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := NormalizeFilePath(c.in)
			if got != c.want {
				t.Errorf("NormalizeFilePath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Pin the load-bearing pieces of the directive worked-example hint
// added 2026-05-04. The 2026-05-02 fan-in prompt fix didn't stick on
// qwen3-moe — the rejection text now carries copy-pasteable templates
// for both valid resolutions so the model has a directive to follow
// rather than abstract guidance to reason about.
func TestValidateFileOwnershipPartition_HintIncludesWorkedExamples(t *testing.T) {
	reqs := []Requirement{
		{ID: "req.1", FilesOwned: []string{"internal/auth/health.go", "internal/auth/health_test.go"}},
		{ID: "req.2", FilesOwned: []string{"internal/auth/health.go", "internal/auth/health_test.go"}},
	}
	err := ValidateFileOwnershipPartition(reqs)
	if err == nil {
		t.Fatal("expected file-ownership conflict error")
	}
	msg := err.Error()
	mustContain := []string{
		"FIX: choose ONE",            // directive framing
		"(a) Consolidate",            // first valid resolution
		"(b) Keep two requirements",  // second valid resolution
		"depends_on",                 // hint pin from existing test
		"impl + its test",            // disambiguation cue
		"router/main wire-up",        // disambiguation cue
	}
	for _, s := range mustContain {
		if !strings.Contains(msg, s) {
			t.Errorf("rejection hint missing %q\nfull: %s", s, msg)
		}
	}
}

func TestValidateFileOwnershipPartition_PathNormalisation(t *testing.T) {
	// "./main.go" and "main.go" must collide, not pass as disjoint —
	// otherwise the partition validator's promise breaks at git-merge time
	// when both reqs write to the same physical file.
	reqs := []Requirement{
		{ID: "a", FilesOwned: []string{"./main.go"}},
		{ID: "b", FilesOwned: []string{"main.go"}},
	}
	err := ValidateFileOwnershipPartition(reqs)
	if err == nil {
		t.Fatal("expected overlap rejection across non-canonical paths, got nil")
	}
	if !strings.Contains(err.Error(), "main.go") {
		t.Errorf("error should name the conflicting path: %v", err)
	}
}
