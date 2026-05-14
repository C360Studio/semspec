//go:build integration

// Integration test for the research tool's end-to-end KV-watch flow.
//
// Pure unit tests in executor_test.go cover arg validation, helpers,
// renderAnswer, and ListTools. They do NOT cover the load-bearing seam
// that R2 introduces: opening a KV watcher, writing a pending record,
// and seeing a subsequent answered write unblock the caller.
//
// This test stands up a real JetStream-backed NATS via testcontainers
// (same pattern as tools/question/executor_integration_test.go) and
// exercises the full Execute() → watcher → KV → ToolResult path against
// a simulated researcher that writes the answered state after a brief
// delay. Catches the put-before-watch race shape the R2 review flagged.
//
// Run with: go test -tags integration ./tools/research/
package research

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

// newIntegrationExecutor spins up a JetStream-backed NATS test client,
// creates the RESEARCH KV bucket via the store, and returns the Executor
// wired against the real backend. Timeout is set tight so a hung test
// doesn't burn 5 minutes.
func newIntegrationExecutor(t *testing.T) (*Executor, *workflow.ResearchStore, func()) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	store, err := workflow.NewResearchStore(tc.Client)
	if err != nil {
		t.Fatalf("NewResearchStore: %v", err)
	}

	exec := NewExecutor(tc.Client, store, nil)
	exec.timeout = 3 * time.Second

	cleanup := func() { /* testcontainers cleanup happens via t.Cleanup inside NewTestClient */ }
	return exec, store, cleanup
}

// TestIntegration_ResearchHappyPath validates the full delegation cycle:
// dev calls research → executor writes pending + opens watcher → simulated
// researcher writes answered after a delay → executor receives the answered
// state via watcher and returns the rendered tool result.
//
// The simulated researcher writes ~50ms after Execute starts, well inside
// the watch window. If R2's restructure (open watcher BEFORE Put) had a
// regression, this test would intermittently timeout.
func TestIntegration_ResearchHappyPath(t *testing.T) {
	exec, store, cleanup := newIntegrationExecutor(t)
	defer cleanup()

	// Run the simulated researcher in a goroutine: scan RESEARCH for a
	// pending record matching our asking loop, then flip it to answered.
	// Polling rather than watching keeps the test simple — production
	// uses watching via researcher-manager (R3).
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			// Find the pending record. We don't know the ID up front
			// because NewResearch generates it, so scan the bucket.
			keys, err := store.Bucket().Keys(t.Context())
			if err != nil {
				continue
			}
			for _, k := range keys {
				r, err := store.Get(t.Context(), k)
				if err != nil || r.Status != workflow.ResearchStatusPending {
					continue
				}
				if r.AskingLoopID != "test-dev-loop" {
					continue
				}
				now := time.Now().UTC()
				r.Status = workflow.ResearchStatusAnswered
				r.Answer = "AbstractSensorModule constructor: protected, takes no args. Lifecycle: init(config) → start() → stop()."
				r.Citations = []workflow.Citation{
					{URL: "https://raw.githubusercontent.com/opensensorhub/osh-core/master/sensorhub-core/src/main/java/.../AbstractSensorModule.java", Lines: "45-52"},
				}
				r.AnsweredAt = &now
				if _, err := store.Put(t.Context(), r); err == nil {
					return
				}
			}
		}
	}()

	res, err := exec.Execute(t.Context(), agentic.ToolCall{
		ID:     "test-call-1",
		LoopID: "test-dev-loop",
		Arguments: map[string]any{
			"question": "What is the constructor signature for AbstractSensorModule and what's the lifecycle?",
			"sources":  []any{"github.com/opensensorhub/osh-core"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("ToolResult.Error = %q; want empty", res.Error)
	}
	if !strings.Contains(res.Content, "AbstractSensorModule constructor") {
		t.Errorf("Content missing answer prose: %q", res.Content)
	}
	if !strings.Contains(res.Content, "Citations:") {
		t.Errorf("Content missing citations section: %q", res.Content)
	}
}

// TestIntegration_ResearchTimeout validates the wall-clock timeout path:
// no simulated researcher answers, executor returns a synthetic timeout
// tool result so the dev can proceed with its best judgment.
func TestIntegration_ResearchTimeout(t *testing.T) {
	exec, _, cleanup := newIntegrationExecutor(t)
	defer cleanup()
	exec.timeout = 500 * time.Millisecond // make the test fast

	res, err := exec.Execute(t.Context(), agentic.ToolCall{
		ID:     "test-call-2",
		LoopID: "test-dev-loop",
		Arguments: map[string]any{
			"question": "What is the constructor signature for AbstractSensorModule?",
			"sources":  []any{"github.com/opensensorhub/osh-core"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	// Timeout path returns a synthetic ToolResult with the timeout
	// message in Content, no Error field (so the dev's next turn can
	// continue rather than being short-circuited as a tool failure).
	if !strings.Contains(res.Content, "timed out after") {
		t.Errorf("Content missing timeout phrase: %q", res.Content)
	}
}

// TestIntegration_AnswerOversizeRejected validates that the answer-size
// cap reaches the wire path: the researcher writes an oversize answer
// directly to RESEARCH KV (bypassing answer_research) and the executor's
// watcher sees the answered state but the asking dev never gets garbage
// content past the cap. This is the structural-pressure guarantee the
// executor-layer enforcement provides.
//
// NB: this exercises a path that production wouldn't normally hit — the
// answer_research executor validates BEFORE writing, so an oversize
// answer never reaches KV via the tool. We test direct-KV-write here to
// confirm the watcher reads what's there as-is. The validate cap is
// enforced at WRITE time by ResearchStore.Put (workflow/research.go),
// so an oversize direct write also bounces.
func TestIntegration_AnswerOversizeBouncedByStore(t *testing.T) {
	_, store, cleanup := newIntegrationExecutor(t)
	defer cleanup()

	r := workflow.NewResearch("loop-1", "call-1", "Q?", []string{"x"})
	r.Status = workflow.ResearchStatusAnswered
	r.Answer = strings.Repeat("x", workflow.MaxResearchAnswerBytes+1)
	r.Citations = []workflow.Citation{{URL: "https://a"}}

	_, err := store.Put(t.Context(), r)
	if err == nil {
		t.Errorf("oversize Put should reject; got nil error")
	} else if !strings.Contains(err.Error(), "distill further") {
		t.Errorf("oversize Put err = %v; want substring 'distill further'", err)
	}
}
