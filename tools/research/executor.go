// Package research implements the research and answer_research tool executors.
//
// research is a non-terminal dev tool that delegates upstream-API-surface
// investigation to a researcher sub-agent. The dev's loop blocks (KV watch)
// until the researcher submits an answer via answer_research, or until the
// SLA expires. The synthesized ToolResult drops the distilled answer +
// citations into the dev's context, replacing what would otherwise be many
// raw-source reads accumulating in the dev's context.
//
// answer_research is the terminal tool used by the researcher sub-agent to
// write its answer to RESEARCH KV and end its loop. The terminal validates
// the answer size against workflow.MaxResearchAnswerBytes and the citation
// list shape before persisting — bad submissions are rejected so the
// researcher must distill further on retry.
//
// Mirrors the shape of tools/question (ask_question + answer_question)
// because the underlying mechanic — non-terminal dev tool blocks on KV,
// terminal sub-agent tool unblocks via KV write — is the same. The
// semantics differ (peer delegation vs upward escalation) but the
// machinery is identical.
package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	toolName = "research"

	// DefaultTimeout is the maximum wall-clock the dev waits for the
	// researcher to answer. Chosen to bound the dev cycle's exposure to a
	// stuck researcher — the researcher's own iter budget caps actual
	// runtime to well under this in normal operation.
	DefaultTimeout = 5 * time.Minute

	// SubjectResearchRequested is the JetStream subject the research
	// executor publishes to when a new research request is registered.
	// researcher-manager (R3) subscribes to the wildcard form
	// `agent.research.requested.>` to spawn researcher loops in response.
	// Hoisted to a package const so R3 can import the same symbol.
	SubjectResearchRequested = "agent.research.requested."
)

// Executor implements agentic.ToolExecutor for the research dev tool.
// Construction takes the workflow.ResearchStore so tests can inject an
// in-memory implementation; production wiring creates the store from the
// shared NATS client during component bootstrap.
type Executor struct {
	natsClient    *natsclient.Client
	researchStore *workflow.ResearchStore
	timeout       time.Duration
	logger        *slog.Logger
}

// NewExecutor constructs a research Executor. nil store/nats means the
// executor returns a "research backend unavailable" tool error rather than
// panicking — same defensive shape as tools/question/executor.go.
func NewExecutor(nc *natsclient.Client, store *workflow.ResearchStore, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		natsClient:    nc,
		researchStore: store,
		timeout:       DefaultTimeout,
		logger:        logger,
	}
}

// WithTimeout overrides the default wait timeout. Used by tests that need
// to verify the timeout path without sitting 5 minutes.
func (e *Executor) WithTimeout(d time.Duration) *Executor {
	e.timeout = d
	return e
}

// Execute creates a Research record in RESEARCH KV (status=pending),
// publishes a ResearchRequestPayload for the researcher-manager (R3) to
// pick up, and blocks until the KV flips to status=answered (or timeout/
// error). The synthesized ToolResult either returns the researcher's
// answer prose + citation list, or a timeout/error message so the dev
// can decide whether to retry, narrow the question, or proceed without.
//
// R2 scope: this method publishes the request but does NOT dispatch a
// researcher loop — that wiring lands in R3. Until R3 ships, calling
// this tool in production will timeout (no researcher answers). Tests
// drive the KV state directly to verify the unblock path.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	question := stringArg(call.Arguments, "question")
	if question == "" {
		return errorResult(call, `missing required argument "question"`), nil
	}

	sources := stringSliceArg(call.Arguments, "sources")
	if len(sources) == 0 {
		return errorResult(call, `missing required argument "sources" (list of URLs, repo refs, or maven coordinates the researcher should consult)`), nil
	}

	if e.researchStore == nil || e.natsClient == nil {
		return errorResult(call, "research backend not configured — falling back to direct reads"), nil
	}

	r := workflow.NewResearch(call.LoopID, call.ID, question, sources)

	e.logger.Info("Agent requesting research",
		slog.String("research_id", r.ID),
		slog.String("question", truncate(question, 200)),
		slog.String("asking_loop_id", call.LoopID),
		slog.Int("source_count", len(sources)),
	)

	// Open the KV watcher BEFORE writing the pending record so we never
	// miss an answered update that lands in the window between Put and
	// Watch. JetStream's Watch defaults emit the latest value first, so
	// even if the researcher answers extremely quickly the watcher will
	// pick it up on the initial replay. waitCtx bounds the watcher
	// lifetime to the executor's timeout so a stuck watcher can't leak
	// goroutines past the wall-clock deadline. Restructure from the
	// review of R2 — race tighter than the original Put-then-Watch
	// order, cheaper than testing the race empirically.
	//
	// waitCtx uses context.Background() rather than parent ctx so the
	// agentic-loop's tool-call deadline (often shorter than e.timeout)
	// can't truncate our wait window. Take-27 (2026-05-14) hit this:
	// configured 5-min timeout was effectively 3 min because the parent
	// ctx fired its own deadline at 3 min and waitCtx (derived from
	// parent via WithTimeout) inherited the earlier deadline. Researchers
	// that took 5-12 min to answer were dispatched fine but the dev's
	// wait was already over by the time the answer landed. Trace context
	// is carried over for distributed tracing; a goroutine forwards
	// parent cancellation so a real loop shutdown still aborts the wait.
	waitCtx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	if tc, ok := natsclient.TraceContextFromContext(ctx); ok {
		waitCtx = natsclient.ContextWithTrace(waitCtx, tc)
	}
	go func() {
		select {
		case <-ctx.Done():
			// Parent's Done channel fires for two reasons:
			//   - context.Canceled — real shutdown (loop tear-down,
			//     dev's containing ctx canceled). We MUST propagate
			//     so the watcher exits cleanly rather than holding
			//     resources until e.timeout fires.
			//   - context.DeadlineExceeded — parent's own deadline
			//     fired (LLM-call ctx, agentic-loop tool-call ctx).
			//     We MUST NOT propagate because that's exactly the
			//     truncation the take-27 fix is designed to prevent
			//     — researcher may answer between parent's deadline
			//     and our e.timeout, and the dev should see it.
			if errors.Is(ctx.Err(), context.Canceled) {
				cancel()
			}
		case <-waitCtx.Done():
			// Our timeout fired or we already canceled — nothing
			// to do, the deferred cancel cleaned up.
		}
	}()
	watcher, err := e.researchStore.Bucket().Watch(waitCtx, r.ID)
	if err != nil {
		return errorResult(call, fmt.Sprintf("failed to open research watcher: %v", err)), nil
	}
	defer watcher.Stop()

	if _, err := e.researchStore.Put(ctx, r); err != nil {
		return errorResult(call, fmt.Sprintf("failed to register research request: %v", err)), nil
	}

	// Notify researcher-manager (R3 subscribes to this subject and drives
	// researcher sub-agent dispatch). Now that R3 owns dispatch via the
	// subject subscription, a lost publish means the researcher-manager
	// never sees the request — the dev hangs on the KV watch until the
	// 5-minute timeout, wasting 5 min of LLM tokens per failed publish.
	// Take-25 surfaced this exact failure (2026-05-14): both research()
	// calls in the dev's first cycle hit "publish failed (best-effort)"
	// because the parent ctx had a tight LLM-call deadline. The dev then
	// timed out waiting for an answer that never came.
	//
	// Two fixes here:
	//   (1) Use a bounded background ctx for the publish so a tight
	//       parent deadline can't kill the JS publish in flight.
	//   (2) Hard-fail with errorResult instead of best-effort warn — if
	//       publish fails the dev finds out immediately and can decide
	//       (retry / narrow / proceed-without) rather than burning 5 min.
	if err := e.publishRequest(ctx, r.ID); err != nil {
		e.logger.Error("ResearchRequest publish failed — researcher-manager will never see this request",
			slog.String("research_id", r.ID), slog.Any("error", err))
		return errorResult(call, fmt.Sprintf(
			"failed to dispatch research request: %v. The researcher will not see this question. Retry, narrow your question, or proceed without research.",
			err)), nil
	}

	// Block on the already-open watcher until the researcher writes the
	// answer (or we time out).
	answered, err := e.consumeWatcher(waitCtx, watcher, r.ID)
	if err != nil {
		e.logger.Info("Research timed out",
			slog.String("research_id", r.ID),
			slog.Duration("timeout", e.timeout))
		return agentic.ToolResult{
			CallID: call.ID,
			Content: fmt.Sprintf(
				"Research request %s timed out after %s. No answer was received from the researcher. Proceed with your best judgment — the question may be too broad, or the researcher is unavailable. Your question was: %s",
				r.ID, e.timeout, truncate(question, 400),
			),
		}, nil
	}

	e.logger.Info("Research answered",
		slog.String("research_id", r.ID),
		slog.Int("answer_bytes", len(answered.Answer)),
		slog.Int("citation_count", len(answered.Citations)))

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: renderAnswer(answered),
	}, nil
}

// publishRequest emits a ResearchRequestPayload-wrapped JetStream message
// on the agent.research.requested subject. The researcher-manager component
// (R3) subscribes and dispatches a researcher loop in response.
//
// Uses natsclient.Client.PublishToStream rather than the bare JetStream
// publish so we get the circuit-breaker check, trace-context propagation,
// and metrics that the rest of semspec relies on. The bare js.Publish that
// take-25 used was a regression — every other component in the project
// goes through the natsclient publish path. Migrated 2026-05-14 after
// take-25 hit two consecutive publish failures from a tight parent ctx.
//
// publishCtx is a fresh background context with an explicit short timeout.
// The parent ctx (the agentic loop's tool-call ctx) can have a deadline
// shorter than what JetStream needs to round-trip an ack — once that
// deadline fires the publish goroutine sees ctx.Err() and returns
// "context deadline exceeded" without ever reaching the broker. Decoupling
// the publish from the parent deadline means the dispatch doesn't drop
// just because the LLM-call clock is ticking down.
func (e *Executor) publishRequest(parent context.Context, researchID string) error {
	payload := workflow.ResearchRequestPayload{ResearchID: researchID}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request payload: %w", err)
	}

	publishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Carry the parent's trace context into the publish so the
	// researcher-manager's resulting work traces back to the dev call.
	if tc, ok := natsclient.TraceContextFromContext(parent); ok {
		publishCtx = natsclient.ContextWithTrace(publishCtx, tc)
	}

	subject := SubjectResearchRequested + researchID
	if err := e.natsClient.PublishToStream(publishCtx, subject, data); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// consumeWatcher reads from an ALREADY-OPEN KV watcher until the record
// transitions to a terminal state (answered/timeout/error) or waitCtx
// fires. The watcher must have been opened with waitCtx so its lifetime
// is bounded by the same deadline — this function does not call Stop on
// the watcher (the caller's defer handles that).
//
// Restructured from the previous waitForAnswer (which opened its own
// watcher) after the R2 review flagged a put-before-watch race window:
// opening the watcher before the Put is strictly safer, and lifting the
// watcher into Execute lets Execute control the open/close ordering.
func (e *Executor) consumeWatcher(waitCtx context.Context, watcher jetstream.KeyWatcher, researchID string) (*workflow.Research, error) {
	for {
		select {
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil, fmt.Errorf("watcher %s[%s] closed", workflow.ResearchBucket, researchID)
			}
			if entry == nil {
				continue // end of initial replay
			}
			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}
			var r workflow.Research
			if err := json.Unmarshal(entry.Value(), &r); err != nil {
				continue
			}
			switch r.Status {
			case workflow.ResearchStatusAnswered:
				return &r, nil
			case workflow.ResearchStatusTimeout, workflow.ResearchStatusError:
				// Terminal failure on the researcher side — surface as a
				// timeout result to the dev (we don't have a separate
				// "researcher failed" tool result shape; from the dev's
				// perspective the result is "no answer arrived").
				return nil, fmt.Errorf("research %s: %s", r.Status, r.Error)
			}
		case <-waitCtx.Done():
			return nil, waitCtx.Err()
		}
	}
}

// renderAnswer formats the researcher's distilled answer + citations into
// the prose the dev sees as its tool_result. Citations render after the
// answer so the dev's context shows pointers, not pasted content.
func renderAnswer(r *workflow.Research) string {
	if len(r.Citations) == 0 {
		return r.Answer
	}
	out := r.Answer + "\n\nCitations:"
	for _, c := range r.Citations {
		ref := c.URL
		if ref == "" {
			ref = c.File
		}
		if c.Lines != "" {
			ref += " (lines " + c.Lines + ")"
		}
		out += "\n- " + ref
	}
	return out
}

// ListTools returns the tool definition for research that the LLM sees in
// its function-definition list. The description is framed as the *value*
// the tool provides (context offload + concrete API surface) rather than
// a metric to optimize against ("be short", "be shallow"). The schema
// forces articulation: question and sources both required.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: toolName,
		Description: "Delegate a specific upstream-API-surface question to a researcher sub-agent. " +
			"Use this when you would otherwise read several files of external source/docs to learn the " +
			"names, signatures, or calling conventions you need — the researcher reads in its own context " +
			"window and returns a distilled summary plus citations, so the same answers cost ~1 of your " +
			"iters instead of many bash reads. " +
			"Use for: upstream library API surfaces, protocol/spec details, configuration formats from " +
			"external systems. " +
			"Do NOT use for: files in your worktree (read them directly with bash), to delegate writing " +
			"code (the researcher cannot write), or for general 'explore the codebase' tasks (be specific).",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"question", "sources"},
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The specific question to answer. State exactly what you need to know to write correct code (e.g. 'What is the constructor signature for AbstractSensorModule and what lifecycle methods does it require?'). Vague questions ('how does X work') return weaker answers.",
				},
				"sources": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "Starting points for the researcher: canonical repo URLs (github.com/owner/repo), maven coordinates (maven:group:artifact:version), or known doc URLs. The researcher may consult other sources if needed but starts here.",
				},
			},
		},
	}}
}
