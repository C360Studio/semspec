package executionmanager

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semspec/workflow/payloads"
)

const testCategoriesJSON = `{
	"categories": [
		{
			"id": "missing_tests",
			"label": "Missing Tests",
			"description": "No tests submitted with implementation.",
			"signals": ["no test file", "FAIL: TestMissing"],
			"guidance": "Create test files."
		},
		{
			"id": "incomplete_implementation",
			"label": "Incomplete Implementation",
			"description": "Missing required components.",
			"signals": ["undefined:", "undeclared name"],
			"guidance": "All criteria must be fully addressed."
		}
	]
}`

func loadTestCategories(t *testing.T) *workflow.ErrorCategoryRegistry {
	t.Helper()
	reg, err := workflow.LoadErrorCategoriesFromBytes([]byte(testCategoriesJSON))
	if err != nil {
		t.Fatalf("LoadErrorCategoriesFromBytes: %v", err)
	}
	return reg
}

func TestBuildStructuralLessons_OneLessonPerFailedRequiredCheck(t *testing.T) {
	checks := []payloads.CheckResult{
		{Name: "go-build", Required: true, Passed: false, Stderr: "main.go:5: undefined: foo"},
		{Name: "go-test", Required: true, Passed: false, Stdout: "FAIL: TestMissing\nno test file for path X"},
		{Name: "lint", Required: true, Passed: true, Stdout: "ok"},
	}

	reg := loadTestCategories(t)
	lessons := buildStructuralLessons("task-1", checks, reg)

	if len(lessons) != 2 {
		t.Fatalf("expected 2 lessons (one per failed required check), got %d", len(lessons))
	}
	for _, l := range lessons {
		if l.Source != "structural-validation" {
			t.Errorf("Source = %q, want %q", l.Source, "structural-validation")
		}
		if l.Role != "developer" {
			t.Errorf("Role = %q, want developer", l.Role)
		}
		if l.ScenarioID != "task-1" {
			t.Errorf("ScenarioID = %q, want task-1", l.ScenarioID)
		}
		if l.ID == "" {
			t.Error("ID should be generated, got empty")
		}
		if l.CreatedAt.IsZero() {
			t.Error("CreatedAt should be set")
		}
	}
}

func TestBuildStructuralLessons_NonRequiredFailuresIgnored(t *testing.T) {
	checks := []payloads.CheckResult{
		{Name: "optional-bench", Required: false, Passed: false, Stderr: "bench failed"},
		{Name: "optional-fmt", Required: false, Passed: false, Stdout: "format diff"},
	}
	got := buildStructuralLessons("task-x", checks, loadTestCategories(t))
	if len(got) != 0 {
		t.Errorf("non-required failures must produce no lessons, got %d", len(got))
	}
}

func TestBuildStructuralLessons_PassedChecksIgnored(t *testing.T) {
	checks := []payloads.CheckResult{
		{Name: "go-build", Required: true, Passed: true, Stdout: "ok"},
		{Name: "go-test", Required: true, Passed: true, Stdout: "PASS"},
	}
	got := buildStructuralLessons("task-y", checks, loadTestCategories(t))
	if len(got) != 0 {
		t.Errorf("all-passing input must produce no lessons, got %d", len(got))
	}
}

func TestBuildStructuralLessons_CategoriesMatchedFromStderrAndStdout(t *testing.T) {
	checks := []payloads.CheckResult{
		// Stderr signal → incomplete_implementation via "undefined:".
		{Name: "go-build", Required: true, Passed: false, Stderr: "main.go:7: undefined: foo"},
		// Stdout signal → missing_tests via "FAIL: TestMissing".
		{Name: "go-test", Required: true, Passed: false, Stdout: "FAIL: TestMissing in TestSuite"},
		// Name + Stderr signal → both? Just incomplete via undeclared name.
		{Name: "vet", Required: true, Passed: false, Stderr: "undeclared name: bar"},
	}
	got := buildStructuralLessons("task-z", checks, loadTestCategories(t))
	if len(got) != 3 {
		t.Fatalf("expected 3 lessons, got %d", len(got))
	}
	want := map[string][]string{
		"go-build": {"incomplete_implementation"},
		"go-test":  {"missing_tests"},
		"vet":      {"incomplete_implementation"},
	}
	for _, l := range got {
		// Each lesson summary starts with check name; use it to key into want.
		var name string
		for n := range want {
			if strings.HasPrefix(l.Summary, n) {
				name = n
				break
			}
		}
		if name == "" {
			t.Errorf("could not match lesson %q to a check name", l.Summary)
			continue
		}
		expectedIDs := want[name]
		if len(l.CategoryIDs) != len(expectedIDs) {
			t.Errorf("check %q: CategoryIDs = %v, want %v", name, l.CategoryIDs, expectedIDs)
			continue
		}
		for i, id := range expectedIDs {
			if l.CategoryIDs[i] != id {
				t.Errorf("check %q: CategoryIDs[%d] = %q, want %q", name, i, l.CategoryIDs[i], id)
			}
		}
	}
}

func TestBuildStructuralLessons_NilRegistryProducesEmptyCategoryIDs(t *testing.T) {
	checks := []payloads.CheckResult{
		{Name: "go-build", Required: true, Passed: false, Stderr: "any"},
	}
	got := buildStructuralLessons("task-q", checks, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 lesson, got %d", len(got))
	}
	if len(got[0].CategoryIDs) != 0 {
		t.Errorf("nil registry must produce empty CategoryIDs, got %v", got[0].CategoryIDs)
	}
}

func TestBuildStructuralLessons_SummaryTruncatedTo200(t *testing.T) {
	long := strings.Repeat("x", 500)
	checks := []payloads.CheckResult{
		{Name: "go-test", Required: true, Passed: false, Stderr: long},
	}
	got := buildStructuralLessons("task-t", checks, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 lesson, got %d", len(got))
	}
	if rl := []rune(got[0].Summary); len(rl) > 200 {
		t.Errorf("Summary length = %d runes, want ≤ 200", len(rl))
	}
}

func TestExtractStructuralLessons_NilWriterIsNoOp(_ *testing.T) {
	c := &Component{
		lessonWriter: nil,
		logger:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	exec := &taskExecution{TaskExecution: &workflow.TaskExecution{TaskID: "t1"}}
	checks := []payloads.CheckResult{{Name: "x", Required: true, Passed: false, Stderr: "boom"}}
	c.extractStructuralLessons(context.Background(), exec, checks)
}

func TestShouldDispatchPositiveLesson(t *testing.T) {
	// ADR-033 Phase 6 gate contract:
	//   1. EnablePositiveLessons=false → never dispatch
	//   2. EnablePositiveLessons=true + first-try (TDDCycle=0,
	//      ReviewRetryCount=0) → dispatch
	//   3. EnablePositiveLessons=true + retry (TDDCycle>0) → no dispatch
	//   4. EnablePositiveLessons=true + parse-retry (ReviewRetryCount>0)
	//      → no dispatch
	//   5. nil exec → no dispatch (defensive)
	cases := []struct {
		name             string
		enablePositive   bool
		tddCycle         int
		reviewRetryCount int
		want             bool
	}{
		{"flag off + first try", false, 0, 0, false},
		{"flag on + first try", true, 0, 0, true},
		{"flag on + tdd retry", true, 1, 0, false},
		{"flag on + parse retry", true, 0, 1, false},
		{"flag on + both retry", true, 2, 3, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			comp := &Component{config: Config{EnablePositiveLessons: c.enablePositive}}
			exec := &taskExecution{
				TaskExecution: &workflow.TaskExecution{
					TaskID:           "t",
					TDDCycle:         c.tddCycle,
					ReviewRetryCount: c.reviewRetryCount,
				},
			}
			if got := comp.shouldDispatchPositiveLesson(exec); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}

	// Defensive: nil exec must not panic.
	comp := &Component{config: Config{EnablePositiveLessons: true}}
	if comp.shouldDispatchPositiveLesson(nil) {
		t.Error("nil exec must not dispatch")
	}
}

func TestExtractStructuralLessons_RecordsLessonsViaWriter(t *testing.T) {
	// Real Writer with nil-NATS TripleWriter — RecordLesson succeeds (no-op),
	// no panic. We can't verify what was written without mocks, so we just
	// confirm the orchestration path completes for a valid input.
	tw := &graphutil.TripleWriter{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	c := &Component{
		lessonWriter:    &lessons.Writer{TW: tw, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))},
		errorCategories: loadTestCategories(t),
		logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
		config:          Config{LessonThreshold: 2},
	}
	exec := &taskExecution{TaskExecution: &workflow.TaskExecution{TaskID: "t2"}}
	checks := []payloads.CheckResult{
		{Name: "go-test", Required: true, Passed: false, Stdout: "FAIL: TestMissing"},
	}
	c.extractStructuralLessons(context.Background(), exec, checks)
}
