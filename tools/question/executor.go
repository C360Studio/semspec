// Package question implements the ask_question and answer_question tool executors.
//
// ask_question writes a question to the QUESTIONS KV bucket, dispatches an
// answerer agent via agentic-dispatch, and blocks (KV watch) until the question
// is answered — by the agent's answer_question tool call or a human via HTTP.
//
// answer_question is a terminal tool used by the answerer agent to write the
// answer directly to QUESTIONS KV and signal loop completion (StopLoop=true).
package question

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	toolName = "ask_question"

	// DefaultTimeout is the maximum time to wait for an answer.
	DefaultTimeout = 5 * time.Minute

	// subjectQuestionTask is the NATS subject for Q&A agent tasks.
	subjectQuestionTask = "agent.task.question"

	// dedupeWindow is the lookback window for treating the same agent asking
	// the same question as a duplicate. Chosen empirically: small-LLM agents
	// tend to re-ask verbatim within minutes of a timeout; legitimate repeat
	// questions about genuinely new situations rarely land on identical
	// normalized text inside this window.
	dedupeWindow = 10 * time.Minute
)

// normalizeQuestion produces a stable lowercase, whitespace-collapsed form
// of the question text for duplicate detection. It tolerates the minor
// formatting drift a model produces across retries ("is there..." vs "Is
// there..." vs "  Is there...   ") while still catching verbatim loops.
func normalizeQuestion(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// Executor implements agentic.ToolExecutor for the ask_question tool.
type Executor struct {
	natsClient    *natsclient.Client
	questionStore *workflow.QuestionStore
	registry      *answerer.Registry
	timeout       time.Duration
	defaultModel  string
	logger        *slog.Logger
}

// NewExecutor constructs an ask_question Executor.
func NewExecutor(nc *natsclient.Client, store *workflow.QuestionStore, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		natsClient:    nc,
		questionStore: store,
		timeout:       DefaultTimeout,
		logger:        logger,
	}
}

// WithDefaultModel sets the model used for dispatching answerer agents.
func (e *Executor) WithDefaultModel(model string) *Executor {
	e.defaultModel = model
	return e
}

// WithAnswererRegistry attaches a route registry. When set, dispatchAnswerer
// matches the question's topic to a route and either dispatches an agent with
// the route's capability (model resolution flows through the model registry),
// skips dispatch entirely for human/team routes (HTTP API answers), or logs a
// TODO for tool routes. Without a registry the executor falls back to the
// pre-existing "always dispatch a generic agent with default model" behavior.
func (e *Executor) WithAnswererRegistry(r *answerer.Registry) *Executor {
	e.registry = r
	return e
}

// Execute publishes a question to QUESTIONS KV, dispatches an answerer agent,
// and blocks until the answer arrives or the timeout expires.
//
// Dedupes circular-question loops: if the same agent (LoopID) just asked
// the same question text inside dedupeWindow, we do NOT enqueue a second
// copy. Small-LLM agents sometimes re-ask verbatim after a timeout or
// cancellation, burning 5 minutes of wall clock and a TDD cycle for a
// question that already had a definitive outcome. Returning that prior
// outcome immediately breaks the loop.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	questionText := stringArg(call.Arguments, "question")
	if questionText == "" {
		return errorResult(call, `missing required argument "question"`), nil
	}

	questionCtx := stringArg(call.Arguments, "context")
	topic := stringArg(call.Arguments, "topic")
	if topic == "" {
		topic = "general"
	}

	// Dedupe before creating a new question entry.
	if dup := e.findRecentDuplicate(ctx, call.LoopID, questionText); dup != nil {
		return e.handleDuplicate(call, dup, questionText), nil
	}

	// Resolve route from registry (nil-safe — falls through to legacy
	// always-dispatch-agent path when no registry is wired).
	route := e.resolveRoute(topic)

	// Create and store the question in QUESTIONS KV.
	q := workflow.NewQuestion(call.LoopID, topic, questionText, questionCtx)

	e.logger.Info("Agent asking question",
		"question_id", q.ID,
		"question", questionText,
		"from_agent", call.LoopID,
		"topic", topic,
		"answerer", routeAnswerer(route),
		"route_type", routeType(route),
	)

	if e.questionStore != nil {
		if err := e.questionStore.Store(ctx, q); err != nil {
			e.logger.Warn("Failed to store question in KV", "error", err)
			// Continue — dispatch and wait still work, human answer path may not
		}
	}

	// Branch on route type. Agent → dispatch with capability; human/team →
	// skip dispatch and wait for HTTP answer; tool → log TODO and skip;
	// nil route → legacy generic-agent dispatch.
	e.routeQuestion(ctx, q, route)

	// Watch QUESTIONS KV for answer (blocks until answered or timeout).
	answer, err := e.waitForAnswer(ctx, q.ID)
	if err != nil {
		e.logger.Info("Question timed out",
			"question_id", q.ID,
			"timeout", e.timeout,
		)
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("Question timed out after %s. No answer was received. Please proceed with your best judgment or try a different approach. Your question was: %s", e.timeout, questionText),
		}, nil
	}

	e.logger.Info("Question answered", "question_id", q.ID)

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: answer,
	}, nil
}

// findRecentDuplicate returns the newest question with the same normalized
// text asked inside dedupeWindow, or nil if none. When fromAgent is
// non-empty we narrow the match to the same agent (the tight case we care
// most about); when empty — which happens for some reviewer tool calls whose
// ToolCall.LoopID isn't populated — we fall back to matching on normalized
// text alone. Two anonymous askers producing identical text within 10 minutes
// are almost certainly the same stuck agent re-asking; returning the prior
// outcome is safer than letting the loop re-create its own past.
//
// QUESTIONS bucket reads are best-effort: any error is logged and treated
// as "no duplicate found" so a single bad read can never block a legitimate
// question.
func (e *Executor) findRecentDuplicate(ctx context.Context, fromAgent, text string) *workflow.Question {
	if e.questionStore == nil {
		return nil
	}
	norm := normalizeQuestion(text)
	if norm == "" {
		return nil
	}

	all, err := e.questionStore.List(ctx, "") // empty status = all
	if err != nil {
		e.logger.Debug("Dedupe scan failed, proceeding without dedupe", "error", err)
		return nil
	}

	cutoff := time.Now().Add(-dedupeWindow)
	var newest *workflow.Question
	for _, q := range all {
		// When we have an agent id, require it to match. Otherwise fall back
		// to text-only matching across all agents — see func comment.
		if fromAgent != "" && q.FromAgent != fromAgent {
			continue
		}
		if q.CreatedAt.Before(cutoff) {
			continue
		}
		if normalizeQuestion(q.Question) != norm {
			continue
		}
		if newest == nil || q.CreatedAt.After(newest.CreatedAt) {
			newest = q
		}
	}
	return newest
}

// handleDuplicate builds a tool result that short-circuits the ask: the
// agent sees its prior outcome (pending, answered, or timed out) without
// paying the 5-minute wait again. The message is phrased to discourage
// another re-ask and nudge the agent to move on.
func (e *Executor) handleDuplicate(call agentic.ToolCall, dup *workflow.Question, originalText string) agentic.ToolResult {
	e.logger.Info("Duplicate question detected — skipping new ask",
		"existing_question_id", dup.ID,
		"from_agent", call.LoopID,
		"status", dup.Status,
		"age", time.Since(dup.CreatedAt).Round(time.Second),
	)

	var content string
	switch dup.Status {
	case workflow.QuestionStatusAnswered:
		content = fmt.Sprintf("You already asked this question (%s) and it was answered:\n\n%s", dup.ID, dup.Answer)
	case workflow.QuestionStatusTimeout:
		content = fmt.Sprintf("You already asked this question (%s, %s ago) and no answer arrived. Do NOT ask it again. Proceed with your best judgment — make a reasonable assumption from the plan/scenarios, use bash/graph tools to look up context, and continue.", dup.ID, time.Since(dup.CreatedAt).Round(time.Second))
	case workflow.QuestionStatusPending:
		content = fmt.Sprintf("You already asked this question (%s) and it is still pending. Do NOT ask it again. Proceed with your best judgment while you wait for an answer.", dup.ID)
	default:
		content = fmt.Sprintf("You already asked this question (%s, status=%s). Do NOT ask it again. Proceed with your best judgment.", dup.ID, dup.Status)
	}

	_ = originalText // retained for symmetry; not surfaced to the agent to avoid prompt bloat
	return agentic.ToolResult{CallID: call.ID, Content: content}
}

// resolveRoute returns the registry route for a topic, or nil when no
// registry is wired. nil signals "fall back to legacy generic-agent
// dispatch" — preserves behavior for callers that haven't adopted the
// registry yet.
func (e *Executor) resolveRoute(topic string) *answerer.Route {
	if e.registry == nil {
		return nil
	}
	return e.registry.Match(topic)
}

// dispatchAction enumerates the routing decisions Executor makes after
// resolving a registry route. The split between "decide" (decideDispatch)
// and "act" (routeQuestion) exists so tests can exercise every branch
// without standing up a NATS publisher — the seam-coverage gap that let
// the registry-not-wired bug ship undetected.
type dispatchAction int

const (
	// dispatchSkip — no agent task should be published. The caller will
	// continue to wait on the QUESTIONS KV for an HTTP/UI-supplied answer.
	dispatchSkip dispatchAction = iota
	// dispatchAgent — publish a TaskMessage; populate from dispatchPlan.
	dispatchAgent
)

// dispatchPlan is the decision routeQuestion enacts. It contains
// everything the dispatch path needs (capability, answerer name, log
// reason) and nothing it doesn't — easy to assert in tests.
type dispatchPlan struct {
	Action     dispatchAction
	Capability string // empty when Action != dispatchAgent or fallback to default
	Answerer   string // registry-config string (e.g. "agent/architect"); "" for legacy
	Reason     string // human-readable explanation of why this branch was chosen
}

// decideDispatch is a PURE function: given a route, return the plan. No
// I/O, no logger, no clock. Every test in TestDecideDispatch_* covers a
// distinct branch — if you add a new route type you must extend this
// switch and the table-driven test together.
//
// Branch policy:
//
//	nil route        → dispatchAgent w/ no capability (legacy back-compat)
//	AnswererAgent    → dispatchAgent w/ route.Capability + route.Answerer
//	AnswererHuman    → dispatchSkip (HTTP API answers)
//	AnswererTeam     → dispatchSkip (HTTP API or external integration answers)
//	AnswererTool     → dispatchAgent (no impl yet; fall back to generic agent)
//	unknown type     → dispatchAgent (defensive fallback)
func decideDispatch(route *answerer.Route) dispatchPlan {
	if route == nil {
		return dispatchPlan{Action: dispatchAgent, Reason: "no registry — legacy generic-agent dispatch"}
	}
	switch route.Type {
	case answerer.AnswererAgent:
		return dispatchPlan{
			Action:     dispatchAgent,
			Capability: route.Capability,
			Answerer:   route.Answerer,
			Reason:     "agent route — dispatch with route capability",
		}
	case answerer.AnswererHuman, answerer.AnswererTeam:
		return dispatchPlan{
			Action:   dispatchSkip,
			Answerer: route.Answerer,
			Reason:   "human/team route — no dispatch; HTTP answer endpoint or external integration responds",
		}
	case answerer.AnswererTool:
		return dispatchPlan{
			Action:   dispatchAgent,
			Answerer: route.Answerer,
			Reason:   "tool route not yet implemented — falling back to generic agent",
		}
	default:
		return dispatchPlan{
			Action:   dispatchAgent,
			Answerer: route.Answerer,
			Reason:   fmt.Sprintf("unknown route type %q — falling back to generic agent", route.Type),
		}
	}
}

// routeQuestion enacts the plan from decideDispatch: logs the decision
// (so trajectories show what happened) and either publishes the agent
// task or skips and waits.
func (e *Executor) routeQuestion(ctx context.Context, q *workflow.Question, route *answerer.Route) {
	plan := decideDispatch(route)
	switch plan.Action {
	case dispatchSkip:
		sla := time.Duration(0)
		if route != nil {
			sla = time.Duration(route.SLA)
		}
		e.logger.Info("Question routed to human/team — awaiting HTTP answer",
			"question_id", q.ID,
			"answerer", plan.Answerer,
			"sla", sla,
			"reason", plan.Reason,
		)
	case dispatchAgent:
		e.dispatchAnswerer(ctx, q, plan.Capability, plan.Answerer)
	}
}

// dispatchAnswerer sends a TaskMessage to agentic-dispatch to spawn an answerer
// agent with bash + graph tools. The agent answers the question and calls
// answer_question (terminal tool) which writes directly to QUESTIONS KV.
//
// capability, when non-empty, names a model-registry capability that
// agentic-model uses to resolve the backend endpoint. Empty capability →
// fall back to defaultModel (or the literal "default", which agentic-model
// then resolves through its own fallback chain).
//
// answererName, when non-empty, is the registry-config string (e.g.
// "agent/architect") and is stamped into TaskMessage.Metadata for
// trajectory/debug visibility.
//
// ParentLoopID is set to the asking agent's loop ID so beta.34's hierarchical
// agent tracking (Depth/Parent) can link the asker's trajectory to the
// answerer's.
func (e *Executor) dispatchAnswerer(ctx context.Context, q *workflow.Question, capability, answererName string) {
	if e.natsClient == nil {
		return
	}

	// Model resolution: capability wins (agentic-model maps via registry),
	// then explicit defaultModel, then literal "default" (registry fallback).
	model := capability
	if model == "" {
		model = e.defaultModel
	}
	if model == "" {
		model = "default"
	}

	metadata := map[string]any{
		"question_id": q.ID,
	}
	if capability != "" {
		metadata["capability"] = capability
	}
	if answererName != "" {
		metadata["answerer"] = answererName
	}

	task := &agentic.TaskMessage{
		TaskID:       fmt.Sprintf("qa-%s-%s", q.ID, uuid.New().String()[:8]),
		Role:         agentic.RoleGeneral,
		Model:        model,
		ParentLoopID: q.FromAgent,
		Prompt:       fmt.Sprintf("Answer this question. Use bash and graph tools to look up relevant code if needed. When you have the answer, call answer_question with the question_id and your answer.\n\nQuestion ID: %s\n\nQuestion: %s\n\nContext: %s", q.ID, q.Question, q.Context),
		WorkflowSlug: "semspec-question",
		WorkflowStep: "answering",
		Metadata:     metadata,
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "ask-question")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		e.logger.Warn("Failed to marshal Q&A task message", "error", err)
		return
	}

	if err := e.natsClient.PublishToStream(ctx, subjectQuestionTask, data); err != nil {
		e.logger.Warn("Failed to dispatch answerer agent", "error", err)
	}
}

// routeAnswerer + routeType return display strings for nil-safe logging.
func routeAnswerer(r *answerer.Route) string {
	if r == nil {
		return "(no registry)"
	}
	return r.Answerer
}

func routeType(r *answerer.Route) string {
	if r == nil {
		return "legacy"
	}
	return string(r.Type)
}

// waitForAnswer watches the QUESTIONS KV bucket for the question to be answered.
// Returns the answer text when status changes to "answered", or an error on timeout.
func (e *Executor) waitForAnswer(ctx context.Context, questionID string) (string, error) {
	if e.questionStore == nil || e.natsClient == nil {
		return "", fmt.Errorf("QUESTIONS KV not configured")
	}

	js, err := e.natsClient.JetStream()
	if err != nil {
		return "", fmt.Errorf("get jetstream: %w", err)
	}

	bucket, err := js.KeyValue(ctx, workflow.QuestionsBucket)
	if err != nil {
		return "", fmt.Errorf("get QUESTIONS bucket: %w", err)
	}

	watcher, err := bucket.Watch(ctx, questionID)
	if err != nil {
		return "", fmt.Errorf("watch QUESTIONS[%s]: %w", questionID, err)
	}
	defer watcher.Stop()

	waitCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	for {
		select {
		case entry, ok := <-watcher.Updates():
			if !ok {
				return "", fmt.Errorf("watcher closed")
			}
			if entry == nil {
				continue // end of initial replay
			}
			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}

			var q workflow.Question
			if err := json.Unmarshal(entry.Value(), &q); err != nil {
				continue
			}
			if q.Status == workflow.QuestionStatusAnswered && q.Answer != "" {
				return q.Answer, nil
			}

		case <-waitCtx.Done():
			return "", waitCtx.Err()
		}
	}
}

// ListTools returns the tool definition for ask_question.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        toolName,
		Description: "Ask a question when you are blocked and cannot proceed without an answer. The tool will wait for an answer from a human or automated responder. If no answer arrives within 5 minutes, you'll receive a timeout message and should proceed with your best judgment.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"question"},
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The question to ask",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Why you need this answered to proceed",
				},
				"topic": map[string]any{
					"type":        "string",
					"description": "Optional dotted topic for routing — e.g. \"architecture.api\", \"requirements.scope\", \"security.auth\". Default \"general\". The system uses this to route to the right answerer (architect/api-expert/human/etc.) per configs/answerers.json.",
				},
			},
		},
	}}
}

func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func errorResult(call agentic.ToolCall, msg string) agentic.ToolResult {
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: msg,
		Error:   msg,
	}
}
