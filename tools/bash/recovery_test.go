package bash

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestClassifyPathMiss(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   string
	}{
		{"ls quoted", "ls: cannot access 'src/main/java/foo': No such file or directory", "src/main/java/foo"},
		{"ls double-quoted", `ls: cannot access "src/main/java/foo": No such file or directory`, "src/main/java/foo"},
		{"ls unquoted", "ls: cannot access src/main/java/foo: No such file or directory", "src/main/java/foo"},
		{"cat generic", "cat: missing/file.txt: No such file or directory", "missing/file.txt"},
		{"cd", "/bin/sh: line 1: cd: bad/dir: No such file or directory", "bad/dir"},
		{"head with quotes", "head: cannot open 'a/b.txt' for reading: No such file or directory", "a/b.txt"},
		{"permission denied", "ls: cannot access 'foo': Permission denied", ""},
		{"empty", "", ""},
		{"unrelated stderr", "warning: foo bar baz", ""},
		{"compile error not a path-miss", "main.go:5:2: undefined: foo", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyPathMiss(tc.stderr)
			if got != tc.want {
				t.Errorf("classifyPathMiss(%q) = %q, want %q", tc.stderr, got, tc.want)
			}
		})
	}
}

func TestPathMissDetector_FirstMissNoHint(t *testing.T) {
	d := NewPathMissDetector()
	hint := d.Inspect(context.Background(), CallContext{},"task-1", "ls bad/path", 2, "ls: cannot access 'bad/path': No such file or directory")
	if hint != "" {
		t.Errorf("first miss should not hint, got %q", hint)
	}
}

func TestPathMissDetector_RepeatHints(t *testing.T) {
	d := NewPathMissDetector()
	cmd := "ls bad/path"
	stderr := "ls: cannot access 'bad/path': No such file or directory"
	_ = d.Inspect(context.Background(), CallContext{},"task-1", cmd, 2, stderr)
	hint := d.Inspect(context.Background(), CallContext{},"task-1", cmd, 2, stderr)
	if hint == "" {
		t.Fatal("repeat miss should hint, got empty")
	}
	if !strings.Contains(hint, "RETRY HINT:") {
		t.Errorf("hint missing prefix: %q", hint)
	}
	if !strings.Contains(hint, "bad/path") {
		t.Errorf("hint missing path: %q", hint)
	}
	if !strings.Contains(hint, "git ls-files") {
		t.Errorf("hint missing git ls-files: %q", hint)
	}
	if !strings.Contains(hint, "find . -type f -name") {
		t.Errorf("hint missing find: %q", hint)
	}
	if !strings.Contains(hint, `"path"`) {
		t.Errorf("hint missing quoted basename: %q", hint)
	}
}

func TestPathMissDetector_HintRepeatsOnThirdCall(t *testing.T) {
	d := NewPathMissDetector()
	cmd := "ls bad/path"
	stderr := "ls: cannot access 'bad/path': No such file or directory"
	_ = d.Inspect(context.Background(), CallContext{},"task-1", cmd, 2, stderr)
	_ = d.Inspect(context.Background(), CallContext{},"task-1", cmd, 2, stderr)
	hint := d.Inspect(context.Background(), CallContext{},"task-1", cmd, 2, stderr)
	if hint == "" {
		t.Error("third repeat should still hint (model may need it more than once)")
	}
}

func TestPathMissDetector_DifferentCommandResets(t *testing.T) {
	d := NewPathMissDetector()
	_ = d.Inspect(context.Background(), CallContext{},"task-1", "ls a/b", 2, "ls: cannot access 'a/b': No such file or directory")
	_ = d.Inspect(context.Background(), CallContext{},"task-1", "ls c/d", 2, "ls: cannot access 'c/d': No such file or directory")
	hint := d.Inspect(context.Background(), CallContext{},"task-1", "ls a/b", 2, "ls: cannot access 'a/b': No such file or directory")
	if hint != "" {
		t.Errorf("after a different miss, repeating earlier command should not hint immediately, got %q", hint)
	}
}

func TestPathMissDetector_NonPathMissNoHint(t *testing.T) {
	d := NewPathMissDetector()
	_ = d.Inspect(context.Background(), CallContext{},"task-1", "cat secret", 1, "cat: secret: Permission denied")
	hint := d.Inspect(context.Background(), CallContext{},"task-1", "cat secret", 1, "cat: secret: Permission denied")
	if hint != "" {
		t.Errorf("non-path-miss class should not hint, got %q", hint)
	}
}

func TestPathMissDetector_ParallelTasks(t *testing.T) {
	d := NewPathMissDetector()
	_ = d.Inspect(context.Background(), CallContext{},"task-A", "ls a/b", 2, "ls: cannot access 'a/b': No such file or directory")
	hint := d.Inspect(context.Background(), CallContext{},"task-B", "ls a/b", 2, "ls: cannot access 'a/b': No such file or directory")
	if hint != "" {
		t.Errorf("task B should not see task A's state, got %q", hint)
	}
}

func TestPathMissDetector_ConcurrentSafe(t *testing.T) {
	d := NewPathMissDetector()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_ = d.Inspect(context.Background(), CallContext{},"task-X", "ls foo", 2, "ls: cannot access 'foo': No such file or directory")
		})
	}
	wg.Wait()
}

func TestPathMissDetector_NilSafe(t *testing.T) {
	var d *PathMissDetector
	hint := d.Inspect(context.Background(), CallContext{},"task-1", "ls", 2, "ls: cannot access 'x': No such file or directory")
	if hint != "" {
		t.Errorf("nil detector should return empty, got %q", hint)
	}
}

func TestPathMissDetector_SuccessClearsState(t *testing.T) {
	d := NewPathMissDetector()
	_ = d.Inspect(context.Background(), CallContext{},"task-1", "ls a/b", 2, "ls: cannot access 'a/b': No such file or directory")
	_ = d.Inspect(context.Background(), CallContext{},"task-1", "ls .", 0, "")
	hint := d.Inspect(context.Background(), CallContext{},"task-1", "ls a/b", 2, "ls: cannot access 'a/b': No such file or directory")
	if hint != "" {
		t.Errorf("success between misses should reset state, got %q", hint)
	}
}

func TestPathMissDetector_EvictionCap(t *testing.T) {
	d := NewPathMissDetector()
	for i := range maxTrackedTasks + 50 {
		taskID := strings.Repeat("x", 1) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
		d.Inspect(context.Background(), CallContext{},taskID, "ls foo", 2, "ls: cannot access 'foo': No such file or directory")
	}
	d.mu.Lock()
	n := len(d.entries)
	d.mu.Unlock()
	if n > maxTrackedTasks {
		t.Errorf("entries exceeded cap: got %d, want <= %d", n, maxTrackedTasks)
	}
}

// recordingTripleWriter satisfies the tripleEmitter interface for
// asserting on triple emission. Captures every WriteTriple call.
type recordingTripleWriter struct {
	mu      sync.Mutex
	triples []recordedTriple
}

type recordedTriple struct {
	subject   string
	predicate string
	object    any
}

func (r *recordingTripleWriter) WriteTriple(_ context.Context, subject, predicate string, object any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triples = append(r.triples, recordedTriple{subject, predicate, object})
	return nil
}

func TestPathMissDetector_EmitsIncidentOnSuggested(t *testing.T) {
	rw := &recordingTripleWriter{}
	d := NewPathMissDetector().WithTripleEmitter(rw)
	cc := CallContext{CallID: "loop-42", Role: "developer", Model: "gemini-flash"}
	cmd := "ls bad/path"
	stderr := "ls: cannot access 'bad/path': No such file or directory"
	_ = d.Inspect(context.Background(), cc, "task-1", cmd, 2, stderr)
	// First call: not_suggested triple set
	notSuggested := len(rw.triples)
	if notSuggested == 0 {
		t.Fatal("first occurrence should emit at least one not_suggested triple")
	}
	// Second call: suggested triple set with candidate
	_ = d.Inspect(context.Background(), cc, "task-1", cmd, 2, stderr)
	if len(rw.triples) <= notSuggested {
		t.Fatalf("second occurrence should add suggested triples; got %d (was %d)", len(rw.triples), notSuggested)
	}
	// Verify a suggested-outcome triple landed
	var sawSuggested, sawCandidate, sawTool bool
	for _, tr := range rw.triples {
		if s, ok := tr.object.(string); ok && s == "suggested" {
			sawSuggested = true
		}
		if s, ok := tr.object.(string); ok && s == "bad/path" {
			sawCandidate = true
		}
		if s, ok := tr.object.(string); ok && s == "bash" {
			sawTool = true
		}
	}
	if !sawSuggested {
		t.Error("expected at least one triple with object=suggested")
	}
	if !sawCandidate {
		t.Error("expected the missing path to be emitted as a candidate")
	}
	if !sawTool {
		t.Error("expected tool_name=bash triple")
	}
}

func TestPathMissDetector_NoEmitWhenCallIDEmpty(t *testing.T) {
	rw := &recordingTripleWriter{}
	d := NewPathMissDetector().WithTripleEmitter(rw)
	cc := CallContext{} // empty CallID
	cmd := "ls bad/path"
	stderr := "ls: cannot access 'bad/path': No such file or directory"
	_ = d.Inspect(context.Background(), cc, "task-1", cmd, 2, stderr)
	_ = d.Inspect(context.Background(), cc, "task-1", cmd, 2, stderr)
	if len(rw.triples) > 0 {
		t.Errorf("empty CallID should suppress emission; got %d triples", len(rw.triples))
	}
}

func TestFormatPathMissHint_BasenameExtraction(t *testing.T) {
	cases := []struct {
		path     string
		wantBase string
	}{
		{"src/main/java/foo/Bar.java", "Bar.java"},
		{"foo.txt", "foo.txt"},
		{"./relative/path", "path"},
		{"/absolute/path/file", "file"},
	}
	for _, tc := range cases {
		got := formatPathMissHint(tc.path)
		if !strings.Contains(got, "\""+tc.wantBase+"\"") {
			t.Errorf("formatPathMissHint(%q) missing quoted basename %q in: %q", tc.path, tc.wantBase, got)
		}
	}
}
