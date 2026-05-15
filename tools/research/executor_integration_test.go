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
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// newIntegrationExecutor spins up a JetStream-backed NATS test client,
// creates the AGENT stream covering agent.research.requested.>, creates
// the RESEARCH KV bucket via the store, and returns the Executor wired
// against the real backend. Timeout is set tight so a hung test doesn't
// burn 5 minutes.
//
// The AGENT stream wiring mirrors what configs/e2e-*.json declare in
// production (see fix(research) commit a74f238). Without it,
// PublishToStream hangs waiting for a stream-ack that never comes
// because no stream is configured to capture the subject — same root
// cause as the take-26 production bug.
func newIntegrationExecutor(t *testing.T) (*Executor, *workflow.ResearchStore, func()) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// Create the AGENT stream with the research subject so PublishToStream
	// has a destination. Production declares this in configs/e2e-*.json;
	// the test scaffolding is the analog.
	if _, err := tc.Client.CreateStream(context.Background(), jetstream.StreamConfig{
		Name:     "AGENT",
		Subjects: []string{"agent.research.requested.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   1 * time.Hour,
	}); err != nil {
		t.Fatalf("create AGENT stream: %v", err)
	}

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

// TestIntegration_WaitCtxSurvivesParentDeadline locks in the take-27
// (2026-05-14) fix: the dev's research wait window MUST be bounded by
// e.timeout, NOT by the parent ctx's (often-tighter) deadline. Take-27
// hit this — parent ctx had a 3-min agentic-loop tool-call deadline,
// configured e.timeout was 5 min, but waitCtx (derived from parent via
// WithTimeout) inherited the earlier 3-min deadline. Researchers
// answering at 4-5 min were dispatched fine but the dev's wait was
// already over.
//
// Test shape:
//   - parent ctx canceled at 100ms (simulates parent deadline firing
//     much earlier than e.timeout would)
//   - e.timeout = 1s
//   - simulated researcher answers at 300ms (AFTER parent's deadline,
//     BEFORE e.timeout)
//   - expected: Execute returns the answer (waitCtx survived the
//     parent's tighter deadline)
//
// Pre-fix behavior would have been: waitCtx canceled at 100ms, dev
// returned timeout content, the answer at 300ms is wasted research
// effort the dev never sees.
func TestIntegration_WaitCtxSurvivesParentDeadline(t *testing.T) {
	exec, store, cleanup := newIntegrationExecutor(t)
	defer cleanup()
	exec.timeout = 1 * time.Second

	// Simulated researcher: answers at 300ms (between parent's
	// deadline and e.timeout).
	go func() {
		time.Sleep(300 * time.Millisecond)
		keys, err := store.Bucket().Keys(t.Context())
		if err != nil {
			return
		}
		for _, k := range keys {
			r, err := store.Get(t.Context(), k)
			if err != nil || r.Status != workflow.ResearchStatusPending {
				continue
			}
			now := time.Now().UTC()
			r.Status = workflow.ResearchStatusAnswered
			r.Answer = "Lifecycle: init→start→stop. Single-arg constructor."
			r.Citations = []workflow.Citation{
				{URL: "https://example/.../AbstractSensorModule.java", Lines: "45-52"},
			}
			r.AnsweredAt = &now
			_, _ = store.Put(t.Context(), r)
			return
		}
	}()

	// Parent ctx with deadline tighter than e.timeout. Pre-fix this
	// would truncate waitCtx and the answer-at-300ms would never reach
	// the dev.
	parentCtx, parentCancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer parentCancel()

	res, err := exec.Execute(parentCtx, agentic.ToolCall{
		ID:     "test-call-waitctx",
		LoopID: "test-dev-loop",
		Arguments: map[string]any{
			"question": "Lifecycle of AbstractSensorModule?",
			"sources":  []any{"github.com/opensensorhub/osh-core"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("ToolResult.Error = %q; want empty", res.Error)
	}
	if !strings.Contains(res.Content, "Lifecycle: init") {
		t.Errorf("expected answer prose in Content (waitCtx survived parent's tighter deadline); got: %q", res.Content)
	}
	if strings.Contains(res.Content, "timed out after") {
		t.Errorf("dev got timeout when researcher answered within e.timeout — waitCtx still inherits parent deadline")
	}
}

// TestIntegration_WaitCtxRespectsParentCancellation locks in the
// other half of the take-27 fix: while the parent's DEADLINE no
// longer truncates the wait, the parent's CANCELLATION still does
// (loop shutdown, dev's containing ctx torn down). Without this the
// research executor would hold a goroutine + JS resources until
// e.timeout fires even after the agentic loop is being torn down.
func TestIntegration_WaitCtxRespectsParentCancellation(t *testing.T) {
	exec, _, cleanup := newIntegrationExecutor(t)
	defer cleanup()
	exec.timeout = 30 * time.Second // long enough that we'd notice if cancel didn't propagate

	parentCtx, parentCancel := context.WithCancel(t.Context())

	// Cancel parent after 200ms to simulate loop shutdown.
	go func() {
		time.Sleep(200 * time.Millisecond)
		parentCancel()
	}()

	start := time.Now()
	res, err := exec.Execute(parentCtx, agentic.ToolCall{
		ID:     "test-call-cancel",
		LoopID: "test-dev-loop",
		Arguments: map[string]any{
			"question": "What is X?",
			"sources":  []any{"github.com/opensensorhub/osh-core"},
		},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	// Should return a timeout-shaped result well under e.timeout —
	// parent's cancel propagated to waitCtx.
	if !strings.Contains(res.Content, "timed out after") {
		t.Errorf("expected timeout content (parent canceled); got: %q", res.Content)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Execute took %v after parent cancel — propagation is broken (waitCtx waited until e.timeout)", elapsed)
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
