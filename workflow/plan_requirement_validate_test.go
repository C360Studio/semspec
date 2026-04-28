package workflow

import (
	"strings"
	"testing"
	"time"
)

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
			name: "no files_owned anywhere passes (back-compat)",
			reqs: []Requirement{
				req("a", nil, nil),
				req("b", nil, nil),
			},
		},
		{
			name: "one side missing files_owned passes",
			reqs: []Requirement{
				req("a", nil, []string{"main.go"}),
				req("b", nil, nil),
			},
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
