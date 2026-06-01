package workflowdocuments

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func testComponent(t *testing.T) *Component {
	t.Helper()
	return &Component{
		logger:            slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		baseDir:           t.TempDir(),
		planStateBucket:   "PLAN_STATES",
		openSpecDebouncer: newOpenSpecDebouncer(),
	}
}

func samplePlanForWriter() *workflow.Plan {
	return &workflow.Plan{
		Slug:  "writer-test",
		Title: "Writer test",
		Goal:  "Test the OpenSpec writer.",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "core", Lifecycle: workflow.CapabilityNew, Description: "Core."},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "Core req", CapabilityName: "core"},
		},
		Architecture: &workflow.ArchitectureDocument{
			TechnologyChoices: []workflow.TechChoice{{Category: "lang", Choice: "Go", Rationale: "default"}},
		},
	}
}

func TestWriteOpenSpecArtifacts_HappyPath(t *testing.T) {
	c := testComponent(t)
	plan := samplePlanForWriter()
	planDir := filepath.Join(c.baseDir, ".semspec", "plans", plan.Slug)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}

	written := c.writeOpenSpecArtifacts(plan, planDir)

	expected := []string{
		filepath.Join("openspec", "proposal.md"),
		filepath.Join("openspec", "design.md"),
		filepath.Join("openspec", "tasks.md"),
		filepath.Join("openspec", "specs", "core", "spec.md"),
		"openspec/.openspec.yaml",
	}
	for _, want := range expected {
		found := false
		for _, w := range written {
			if w == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in written files, got %v", want, written)
		}
		fullPath := filepath.Join(planDir, want)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("expected file %q to exist: %v", fullPath, err)
		}
	}
}

func TestWriteOpenSpecArtifacts_LegacyPlanProducesNothing(t *testing.T) {
	c := testComponent(t)
	plan := &workflow.Plan{Slug: "legacy", Title: "Legacy"}
	planDir := filepath.Join(c.baseDir, ".semspec", "plans", plan.Slug)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}

	written := c.writeOpenSpecArtifacts(plan, planDir)
	if len(written) != 0 {
		t.Errorf("expected no files for legacy plan, got %v", written)
	}
	if _, err := os.Stat(filepath.Join(planDir, "openspec")); !os.IsNotExist(err) {
		t.Errorf("expected no openspec dir for legacy plan, got: %v", err)
	}
}

func TestWriteOpenSpecArtifacts_YAMLIsIdempotent(t *testing.T) {
	c := testComponent(t)
	plan := samplePlanForWriter()
	planDir := filepath.Join(c.baseDir, ".semspec", "plans", plan.Slug)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// First write — .openspec.yaml is created.
	c.writeOpenSpecArtifacts(plan, planDir)
	yamlPath := filepath.Join(planDir, "openspec", ".openspec.yaml")
	stat1, err := os.Stat(yamlPath)
	if err != nil {
		t.Fatalf("stat after first write: %v", err)
	}

	// Sleep one tick so mtime would differ if the file got rewritten.
	time.Sleep(10 * time.Millisecond)

	// Second write — yaml should NOT be rewritten.
	c.writeOpenSpecArtifacts(plan, planDir)
	stat2, err := os.Stat(yamlPath)
	if err != nil {
		t.Fatalf("stat after second write: %v", err)
	}
	if !stat2.ModTime().Equal(stat1.ModTime()) {
		t.Errorf("expected .openspec.yaml mtime unchanged (idempotent), got %v != %v",
			stat1.ModTime(), stat2.ModTime())
	}
}

func TestOpenSpecDebouncer_CoalescesBurst(t *testing.T) {
	d := newOpenSpecDebouncer()
	var fires atomic.Int32
	render := func() { fires.Add(1) }
	// 5 rapid schedules — should produce ONE render after debounce window.
	// Use sleep+assert (not WaitGroup) so a regression that fires twice
	// doesn't panic on negative counter — we want a clean "got 2, want 1".
	for i := 0; i < 5; i++ {
		d.schedule("slug-a", render)
		time.Sleep(50 * time.Millisecond)
	}
	// Wait through the debounce window + a buffer for the post-render check.
	time.Sleep(openSpecDebounceInterval + 300*time.Millisecond)
	if got := fires.Load(); got != 1 {
		t.Errorf("expected 1 render fire after burst, got %d", got)
	}
}

// TestOpenSpecDebouncer_KickDuringRenderTriggersRerun pins the go-reviewer
// PR 3 should-fix #1: a schedule that lands DURING a render must trigger
// another render rather than racing as a second parallel goroutine.
func TestOpenSpecDebouncer_KickDuringRenderTriggersRerun(t *testing.T) {
	d := newOpenSpecDebouncer()
	var fires atomic.Int32
	renderStarted := make(chan struct{})
	releaseRender := make(chan struct{})
	render := func() {
		fires.Add(1)
		select {
		case renderStarted <- struct{}{}:
		default:
		}
		<-releaseRender
	}

	d.schedule("slug-a", render)
	// Wait for first render to be in progress.
	select {
	case <-renderStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first render never started")
	}
	// Schedule during the render — this should re-arm the same goroutine.
	d.schedule("slug-a", render)
	// Release first render.
	releaseRender <- struct{}{}
	// Wait for second render to start.
	select {
	case <-renderStarted:
	case <-time.After(openSpecDebounceInterval + 1*time.Second):
		t.Fatal("second render never started — kick-during-render lost")
	}
	releaseRender <- struct{}{}
	time.Sleep(100 * time.Millisecond)
	if got := fires.Load(); got != 2 {
		t.Errorf("expected 2 fires (initial + during-render re-arm), got %d", got)
	}
}

func TestOpenSpecDebouncer_PerSlugIsolation(t *testing.T) {
	d := newOpenSpecDebouncer()
	var firesA, firesB atomic.Int32
	d.schedule("slug-a", func() { firesA.Add(1) })
	d.schedule("slug-b", func() { firesB.Add(1) })
	time.Sleep(openSpecDebounceInterval + 300*time.Millisecond)
	if firesA.Load() != 1 || firesB.Load() != 1 {
		t.Errorf("expected one fire per slug, got A=%d B=%d", firesA.Load(), firesB.Load())
	}
}

func TestOpenSpecDebouncer_SequentialSchedulesAfterFire(t *testing.T) {
	d := newOpenSpecDebouncer()
	var fires atomic.Int32
	render := func() { fires.Add(1) }
	d.schedule("slug-a", render)
	// Wait for first fire to land.
	time.Sleep(openSpecDebounceInterval + 200*time.Millisecond)
	d.schedule("slug-a", render)
	time.Sleep(openSpecDebounceInterval + 200*time.Millisecond)
	if got := fires.Load(); got != 2 {
		t.Errorf("expected 2 fires from sequential non-overlapping schedules, got %d", got)
	}
}
