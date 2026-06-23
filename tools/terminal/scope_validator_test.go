package terminal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semspec/vocabulary/observability"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
)

func TestExtractScopeIncludePaths(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"missing scope", map[string]any{"goal": "x"}, nil},
		{"scope not object", map[string]any{"scope": "string"}, nil},
		{"include missing", map[string]any{"scope": map[string]any{"create": []any{"a"}}}, nil},
		{"include empty", map[string]any{"scope": map[string]any{"include": []any{}}}, nil},
		{"include valid", map[string]any{"scope": map[string]any{"include": []any{"a.go", "b.go"}}}, []string{"a.go", "b.go"}},
		{"include filters non-strings", map[string]any{"scope": map[string]any{"include": []any{"a.go", 42, "b.go", ""}}}, []string{"a.go", "b.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractScopeIncludePaths(tc.args)
			if !equalStringSlice(got, tc.want) {
				t.Errorf("extractScopeIncludePaths = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFindMissingScopePaths_AllExist(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"existing.go", "subdir/nested.go"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	missing := findMissingScopePaths(dir, []string{"existing.go", "subdir/nested.go"})
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

func TestFindMissingScopePaths_SomeMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := findMissingScopePaths(dir, []string{"real.go", "ghost.go", "also-missing.go"})
	if len(missing) != 2 || missing[0] != "ghost.go" || missing[1] != "also-missing.go" {
		t.Errorf("expected [ghost.go also-missing.go], got %v", missing)
	}
}

func TestFindMissingScopePaths_EmptyWorkDirSkipsCheck(t *testing.T) {
	missing := findMissingScopePaths("", []string{"anything.go"})
	if missing != nil {
		t.Errorf("empty workDir should disable check, got %v", missing)
	}
}

func TestValidatePlanScope_NoIncludeReturnsEmptyAndNoCounter(t *testing.T) {
	dir := t.TempDir()
	hint := validatePlanScope(context.Background(), dir, nil, CallContext{}, map[string]any{"goal": "x"})
	if hint != "" {
		t.Errorf("missing scope should produce no hint, got %q", hint)
	}
}

func TestValidatePlanScope_AllPathsExistReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{
		"scope": map[string]any{"include": []any{"main.go"}},
	}
	hint := validatePlanScope(context.Background(), dir, nil, CallContext{}, args)
	if hint != "" {
		t.Errorf("all-exist should produce no hint, got %q", hint)
	}
}

func TestValidatePlanScope_MissingPathsReturnsHint(t *testing.T) {
	dir := t.TempDir()
	args := map[string]any{
		"scope": map[string]any{"include": []any{"new1.go", "new2.go"}},
	}
	hint := validatePlanScope(context.Background(), dir, nil, CallContext{}, args)
	if hint == "" {
		t.Fatal("expected directive hint for missing paths, got empty")
	}
	for _, want := range []string{"RETRY HINT:", "scope.include", "scope.create", "new1.go", "new2.go"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint missing %q, got: %q", want, hint)
		}
	}
}

// recordingTripleWriter satisfies scopeValidatorTripleEmitter for
// asserting on triple emission shape.
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

func (r *recordingTripleWriter) UpsertEntity(_ context.Context, _ message.Type, _ string, triples []message.Triple) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Flatten upserted triples into the same recording slice so existing
	// triple-value assertions continue working after the WriteTriple→UpsertEntity
	// migration (issue #154 slice #2).
	for _, t := range triples {
		r.triples = append(r.triples, recordedTriple{t.Subject, t.Predicate, t.Object})
	}
	return nil
}

func TestValidatePlanScope_EmitsSuggestedTriple(t *testing.T) {
	dir := t.TempDir()
	rw := &recordingTripleWriter{}
	cc := CallContext{CallID: "loop-1", Role: "planner", Model: "gemini-pro"}
	args := map[string]any{
		"scope": map[string]any{"include": []any{"missing.go"}},
	}
	hint := validatePlanScope(context.Background(), dir, rw, cc, args)
	if hint == "" {
		t.Fatal("expected hint")
	}
	if len(rw.triples) == 0 {
		t.Fatal("expected SKG triples to be written")
	}
	var sawSuggested, sawMissing, sawTool bool
	for _, tr := range rw.triples {
		s, ok := tr.object.(string)
		if !ok {
			continue
		}
		switch s {
		case observability.ToolRecoveryOutcomeSuggested:
			sawSuggested = true
		case "missing.go":
			sawMissing = true
		case "submit_work":
			sawTool = true
		}
	}
	if !sawSuggested {
		t.Error("expected outcome=suggested triple")
	}
	if !sawMissing {
		t.Error("expected missing path emitted as candidate")
	}
	if !sawTool {
		t.Error("expected tool_name=submit_work triple")
	}
}

func TestValidatePlanScope_NoEmitWhenCallIDEmpty(t *testing.T) {
	dir := t.TempDir()
	rw := &recordingTripleWriter{}
	args := map[string]any{
		"scope": map[string]any{"include": []any{"missing.go"}},
	}
	_ = validatePlanScope(context.Background(), dir, rw, CallContext{}, args)
	if len(rw.triples) != 0 {
		t.Errorf("empty CallID should suppress emission; got %d triples", len(rw.triples))
	}
}

// End-to-end: submit_work with a plan deliverable referencing nonexistent
// files in scope.include returns the directive RETRY HINT instead of
// accepting the deliverable.
func TestSubmitWork_PlanScopeMissReturnsHintInsteadOfAccept(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := NewExecutor().WithWorkDir(dir)

	// Plan with one existing + one nonexistent path.
	res, err := exec.Execute(context.Background(), agentic.ToolCall{
		ID:     "call-1",
		LoopID: "loop-1",
		Name:   "submit_work",
		Arguments: map[string]any{
			"goal":    "demo",
			"context": "test fixture",
			"scope":   map[string]any{"include": []any{"real.go", "ghost.go"}},
		},
		Metadata: map[string]any{
			"deliverable_type": "plan",
			"role":             "planner",
			"model":            "gemini-pro",
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.StopLoop {
		t.Error("scope-miss submit_work should NOT signal StopLoop")
	}
	if res.Error == "" {
		t.Fatal("expected Error to carry the RETRY HINT")
	}
	for _, want := range []string{"RETRY HINT:", "ghost.go", "scope.create"} {
		if !strings.Contains(res.Error, want) {
			t.Errorf("Error missing %q: %q", want, res.Error)
		}
	}
}

func TestSubmitWork_PlanScopeAllExistAccepts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := NewExecutor().WithWorkDir(dir)

	res, err := exec.Execute(context.Background(), agentic.ToolCall{
		ID:     "call-1",
		LoopID: "loop-1",
		Name:   "submit_work",
		Arguments: map[string]any{
			"goal":    "demo",
			"context": "test fixture",
			"scope":   map[string]any{"include": []any{"real.go"}},
		},
		Metadata: map[string]any{
			"deliverable_type": "plan",
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.StopLoop {
		t.Errorf("clean plan should signal StopLoop=true; got %+v", res)
	}
	if res.Error != "" {
		t.Errorf("clean plan should have empty Error; got %q", res.Error)
	}
}

func TestSubmitWork_NonPlanDeliverableSkipsScopeCheck(t *testing.T) {
	dir := t.TempDir()
	exec := NewExecutor().WithWorkDir(dir)

	// requirements deliverable with a "scope" key that the planner
	// validator would reject — confirms we don't run the planner-only
	// check on other deliverable types.
	res, err := exec.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-1",
		Name: "submit_work",
		Arguments: map[string]any{
			"requirements": []any{
				map[string]any{"title": "x", "description": "y", "acceptance_criteria": []any{"x returns y"}},
			},
		},
		Metadata: map[string]any{
			"deliverable_type": "requirements",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.StopLoop {
		t.Errorf("non-plan deliverable should accept normally; got %+v", res)
	}
}

func equalStringSlice(a, b []string) bool {
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
