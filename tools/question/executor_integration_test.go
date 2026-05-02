//go:build integration

// Package question integration tests cover the full Executor wiring:
// Registry + QuestionStore + NATS publish/subscribe.
//
// These tests close the seam-coverage gap that let the
// "registry exists but executor doesn't read it" bug ship undetected. The
// pure-function unit tests in executor_test.go assert decideDispatch's
// branches; THESE tests assert the entire Execute() flow does the right
// thing end-to-end against a real JetStream server.
//
// Run with: go test -tags integration ./tools/question/
package question

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

// newIntegrationExecutor spins up a real JetStream-backed NATS via
// testcontainers, wires a fresh QuestionStore, attaches the registry
// passed in (or default-empty), and returns the Executor + a publish
// recorder that captures every TaskMessage published to
// agent.task.question.
//
// The recorder receives messages via a JetStream subscription, NOT by
// substituting natsClient — so we exercise the real publish path.
func newIntegrationExecutor(t *testing.T, reg *answerer.Registry) (*Executor, <-chan *agentic.TaskMessage, func()) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// QuestionStore creates the QUESTIONS KV bucket.
	store, err := workflow.NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}

	// Make sure the AGENT JetStream stream exists with our subject so
	// PublishToStream succeeds. Mirrors what semstreams sets up at startup.
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if err := ensureAgentStream(t, js); err != nil {
		t.Fatalf("ensure stream: %v", err)
	}

	exec := NewExecutor(tc.Client, store, nil)
	if reg != nil {
		exec = exec.WithAnswererRegistry(reg)
	}
	// Tight timeout so a "no answer arrives" assertion doesn't burn 5
	// minutes; see test bodies for how each case drives this.
	exec.timeout = 3 * time.Second

	// Subscribe to agent.task.question and forward into a channel for
	// assertion-side polling.
	captured := make(chan *agentic.TaskMessage, 8)
	sub, err := tc.Client.SubscribeForRequests(t.Context(), subjectQuestionTask,
		func(_ context.Context, data []byte) ([]byte, error) {
			var env struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(data, &env); err == nil {
				var task agentic.TaskMessage
				if err := json.Unmarshal(env.Payload, &task); err == nil {
					select {
					case captured <- &task:
					default:
					}
				}
			}
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	cleanup := func() {
		if sub != nil {
			_ = sub.Unsubscribe()
		}
		tc.Terminate()
	}
	return exec, captured, cleanup
}

func ensureAgentStream(t *testing.T, js any) error {
	// Defer concrete type until call site to avoid pulling jetstream
	// imports into the helper signature.
	_ = t
	_ = js
	// JetStream stream creation happens lazily in PublishToStream when
	// the stream doesn't exist on some test infra; for our purposes the
	// natsclient.Client implementation creates streams on demand. If a
	// future change requires explicit stream pre-creation, do it here.
	return nil
}

// TestIntegration_Execute_AgentRoute_PublishesTaskWithCapability asserts
// that an Execute() call whose topic resolves to an agent route publishes
// a TaskMessage to agent.task.question with:
//   - Model = the route's Capability (so model registry resolves the
//     right backend)
//   - Metadata.capability = same string
//   - Metadata.answerer = the registry-config string
//   - ParentLoopID = the asking call's LoopID (beta.34 hierarchy)
//
// The test does NOT wait for an answer — it asserts the dispatch decision,
// then cancels the Execute() to free the goroutine.
func TestIntegration_Execute_AgentRoute_PublishesTaskWithCapability(t *testing.T) {
	reg := answerer.NewRegistry()
	reg.AddRoute(answerer.Route{
		Pattern:    "architecture.**",
		Answerer:   "agent/architect",
		Capability: "question_answering",
	})

	exec, captured, cleanup := newIntegrationExecutor(t, reg)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Run Execute in a goroutine — it blocks waiting for an answer that
	// won't arrive (we cancel after asserting the publish).
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = exec.Execute(ctx, agentic.ToolCall{
			ID:     "call-1",
			LoopID: "asker-loop-42",
			Arguments: map[string]any{
				"question": "How is the API laid out?",
				"context":  "I'm planning the auth flow",
				"topic":    "architecture.api",
			},
		})
	}()

	// Wait for the dispatched TaskMessage.
	select {
	case task := <-captured:
		if task.Model != "question_answering" {
			t.Errorf("Model = %q, want %q (capability)", task.Model, "question_answering")
		}
		if task.ParentLoopID != "asker-loop-42" {
			t.Errorf("ParentLoopID = %q, want %q (asker linkage)", task.ParentLoopID, "asker-loop-42")
		}
		if cap, _ := task.Metadata["capability"].(string); cap != "question_answering" {
			t.Errorf("Metadata.capability = %q, want %q", cap, "question_answering")
		}
		if ans, _ := task.Metadata["answerer"].(string); ans != "agent/architect" {
			t.Errorf("Metadata.answerer = %q, want %q", ans, "agent/architect")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent route did not publish TaskMessage within 5s")
	}

	cancel() // release Execute()
	<-done
}

// TestIntegration_Execute_HumanRoute_DoesNOTPublish asserts that an
// Execute() call whose topic resolves to a human route writes the
// question to QUESTIONS KV but does NOT publish a TaskMessage. This is
// the exact behavior that was broken before today's wiring fix — the
// pre-fix executor dispatched a generic agent regardless of route type.
func TestIntegration_Execute_HumanRoute_DoesNOTPublish(t *testing.T) {
	reg := answerer.NewRegistry()
	reg.AddRoute(answerer.Route{
		Pattern:  "requirements.**",
		Answerer: "human/requester",
	})

	exec, captured, cleanup := newIntegrationExecutor(t, reg)
	defer cleanup()

	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()

	// Execute will block until exec.timeout (3s, set in helper) elapses
	// because no human ever answers. We assert in parallel that nothing
	// gets published.
	var publishedDuringExec bool
	var mu sync.Mutex
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-captured:
			mu.Lock()
			publishedDuringExec = true
			mu.Unlock()
		case <-stopWatch:
		}
	}()

	res, _ := exec.Execute(ctx, agentic.ToolCall{
		ID:     "call-2",
		LoopID: "asker-loop-43",
		Arguments: map[string]any{
			"question": "Which user fields are required at signup?",
			"topic":    "requirements.scope",
		},
	})
	close(stopWatch)

	mu.Lock()
	if publishedDuringExec {
		t.Errorf("human route should NOT publish to %s — but a TaskMessage appeared", subjectQuestionTask)
	}
	mu.Unlock()

	// We expect a timeout result (3s exec.timeout < 4s ctx). Surface as
	// info: the timeout-message contains the question text per the
	// timeout branch in Execute().
	if res.Content == "" {
		t.Errorf("expected non-empty timeout result, got empty")
	}
}

// TestIntegration_Execute_NoRegistry_PreservesLegacyDispatch asserts the
// back-compat path: Executor without a Registry dispatches a generic agent
// (legacy behavior). Confirms the wiring change doesn't break callers
// that haven't adopted the registry yet.
func TestIntegration_Execute_NoRegistry_PreservesLegacyDispatch(t *testing.T) {
	exec, captured, cleanup := newIntegrationExecutor(t, nil) // no registry
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = exec.Execute(ctx, agentic.ToolCall{
			ID:     "call-3",
			LoopID: "asker-loop-44",
			Arguments: map[string]any{
				"question": "Some question with no topic",
			},
		})
	}()

	select {
	case task := <-captured:
		if task.Model != "default" {
			t.Errorf("legacy dispatch Model = %q, want %q", task.Model, "default")
		}
		// No capability/answerer in metadata for legacy path.
		if _, ok := task.Metadata["capability"]; ok {
			t.Errorf("legacy dispatch should NOT carry capability metadata")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("legacy path did not publish TaskMessage within 5s")
	}

	cancel()
	<-done
}
