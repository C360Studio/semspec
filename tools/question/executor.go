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

	// Dedupe before creating a new question entry.
	if dup := e.findRecentDuplicate(ctx, call.LoopID, questionText); dup != nil {
		return e.handleDuplicate(call, dup, questionText), nil
	}

	// Create and store the question in QUESTIONS KV.
	q := workflow.NewQuestion(call.LoopID, "general", questionText, questionCtx)

	e.logger.Info("Agent asking question",
		"question_id", q.ID,
		"question", questionText,
		"from_agent", call.LoopID,
	)

	if e.questionStore != nil {
		if err := e.questionStore.Store(ctx, q); err != nil {
			e.logger.Warn("Failed to store question in KV", "error", err)
			// Continue — dispatch and wait still work, human answer path may not
		}
	}

	// Dispatch answerer agent via agentic-dispatch (non-blocking).
	e.dispatchAnswerer(ctx, q)

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

// dispatchAnswerer sends a TaskMessage to agentic-dispatch to spawn an answerer
// agent with bash + graph tools. The agent answers the question and calls
// answer_question (terminal tool) which writes directly to QUESTIONS KV.
func (e *Executor) dispatchAnswerer(ctx context.Context, q *workflow.Question) {
	if e.natsClient == nil {
		return
	}

	model := e.defaultModel
	if model == "" {
		model = "default" // agentic-model resolves via registry fallback
	}

	task := &agentic.TaskMessage{
		TaskID:       fmt.Sprintf("qa-%s-%s", q.ID, uuid.New().String()[:8]),
		Role:         agentic.RoleGeneral,
		Model:        model,
		Prompt:       fmt.Sprintf("Answer this question. Use bash and graph tools to look up relevant code if needed. When you have the answer, call answer_question with the question_id and your answer.\n\nQuestion ID: %s\n\nQuestion: %s\n\nContext: %s", q.ID, q.Question, q.Context),
		WorkflowSlug: "semspec-question",
		WorkflowStep: "answering",
		Metadata: map[string]any{
			"question_id": q.ID,
		},
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
