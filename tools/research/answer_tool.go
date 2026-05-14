package research

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

const answerToolName = "answer_research"

// AnswerExecutor implements agentic.ToolExecutor for the answer_research
// terminal tool. The researcher sub-agent calls this when it has its
// answer; the call writes the answer to RESEARCH KV (unblocking the
// asking dev's research tool) and returns StopLoop=true to end the
// researcher's own loop.
//
// Validation enforces:
//   - research_id refers to an existing record in RESEARCH KV
//   - answer is non-empty AND ≤ workflow.MaxResearchAnswerBytes
//   - at least one citation, each with exactly one of {url, file}
//
// A submission that fails validation is rejected with a tool error
// (StopLoop=false on validation failure so the researcher can retry
// with a corrected payload; StopLoop=true only on accepted submit).
type AnswerExecutor struct {
	researchStore *workflow.ResearchStore
	logger        *slog.Logger
}

// NewAnswerExecutor constructs an answer_research executor. nil store
// means the executor returns a tool error rather than panicking — same
// defensive pattern as tools/question/answer_tool.go.
func NewAnswerExecutor(store *workflow.ResearchStore, logger *slog.Logger) *AnswerExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &AnswerExecutor{
		researchStore: store,
		logger:        logger,
	}
}

// Execute writes the researcher's answer to RESEARCH KV and signals loop
// completion. Validates the payload before persistence — a rejected
// submission does NOT end the loop, so the researcher gets to retry with
// a smaller answer / proper citations within its iter budget.
func (e *AnswerExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	researchID := stringArg(call.Arguments, "research_id")
	if researchID == "" {
		return errorResult(call, `missing required argument "research_id"`), nil
	}
	answer := stringArg(call.Arguments, "answer")
	if answer == "" {
		return errorResult(call, `missing required argument "answer"`), nil
	}
	citations, err := citationsArg(call.Arguments, "citations")
	if err != nil {
		return errorResult(call, fmt.Sprintf("invalid citations: %v", err)), nil
	}
	if len(citations) == 0 {
		return errorResult(call, "missing required argument \"citations\" (at least one citation required so the developer can verify or dig further)"), nil
	}

	// Validate the answered-shape BEFORE branching on store availability.
	// A malformed payload (oversize answer, bad citation shape) must be
	// rejected the same way whether or not a store is configured — so the
	// researcher gets the signal to retry with a corrected payload either
	// way. We build a transient Research record that satisfies the
	// pre-answered required fields, then let r.Validate() enforce all the
	// answered-state checks (size cap, citation rules).
	candidate := &workflow.Research{
		ID:           researchID,
		AskingLoopID: "validation-only", // placeholder, satisfies non-empty check
		AskingCallID: "validation-only", // placeholder, satisfies non-empty check
		Question:     "validation-only", // placeholder, satisfies non-empty check
		Status:       workflow.ResearchStatusAnswered,
		Answer:       answer,
		Citations:    citations,
	}
	if err := candidate.Validate(); err != nil {
		// Surface the validator's message directly — it already carries the
		// actionable shape (bytes vs cap for oversize, missing-field name
		// for omissions). Prepending "answer rejected by validator:" was
		// pure noise. Researcher's StopLoop stays false so it can retry
		// with a corrected payload within its iter budget.
		return errorResult(call, fmt.Sprintf("answer rejected: %v", err)), nil
	}

	if e.researchStore == nil {
		// No KV configured — still return StopLoop=true so the researcher
		// loop ends, but the answer won't reach the asking dev. Surfaces
		// in test fixtures that wire the executor without a real store.
		return agentic.ToolResult{
			CallID:   call.ID,
			Content:  answer,
			StopLoop: true,
		}, nil
	}

	r, err := e.researchStore.Get(ctx, researchID)
	if err != nil {
		// Record disappeared — common when the asking dev's loop already
		// timed out and the record TTL'd out. Return a soft error rather
		// than blocking the researcher's submit; the asking dev has
		// already moved on.
		e.logger.Warn("answer_research: research record not found, accepting answer anyway",
			slog.String("research_id", researchID),
			slog.Any("error", err))
		return agentic.ToolResult{
			CallID:   call.ID,
			Content:  fmt.Sprintf("Answer submitted but research record %s was not found in KV — the asking dev may have already timed out.", researchID),
			StopLoop: true,
		}, nil
	}

	now := time.Now().UTC()
	r.Status = workflow.ResearchStatusAnswered
	r.Answer = answer
	r.Citations = citations
	r.AnsweredAt = &now

	// Validate before persist. This is where MaxResearchAnswerBytes is
	// enforced — Validate rejects oversize answers with a message telling
	// the researcher to distill further. The researcher can retry within
	// its own iter budget.
	if err := r.Validate(); err != nil {
		return errorResult(call, fmt.Sprintf("answer rejected by validator: %v", err)), nil
	}

	// CAS-transition pending OR in_progress → answered. Accepting either
	// predecessor handles the race where the manager hasn't yet flipped
	// status to in_progress before the researcher finishes (fast model on
	// a small read) and the race where it has. ErrResearchStaleStatus
	// only fires if the record is ALREADY in a terminal state (answered/
	// timeout/error) — in which case another writer got there first and
	// we should preserve their result.
	if err := e.researchStore.TransitionStatus(ctx, r,
		workflow.ResearchStatusPending,
		workflow.ResearchStatusInProgress,
	); err != nil {
		// KV failure is a soft failure for the researcher — log loudly and
		// end the loop. The asking dev will time out on its KV watch.
		e.logger.Error("answer_research: failed to persist answer",
			slog.String("research_id", researchID),
			slog.Any("error", err))
		return agentic.ToolResult{
			CallID:   call.ID,
			Content:  fmt.Sprintf("Answer submitted but persistence failed: %v", err),
			StopLoop: true,
		}, nil
	}

	e.logger.Info("Research answered via answer_research tool",
		slog.String("research_id", researchID),
		slog.Int("answer_bytes", len(answer)),
		slog.Int("citation_count", len(citations)))

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  fmt.Sprintf("Answer recorded for %s.", researchID),
		StopLoop: true,
	}, nil
}

// ListTools returns the tool definition for answer_research that the
// researcher sub-agent sees in its function-definition list.
func (e *AnswerExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        answerToolName,
		Description: "Submit your distilled answer to the research request. This ends your loop and delivers the answer + citations to the waiting developer agent. You MUST call this tool when you have your answer.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"research_id", "answer", "citations"},
			"properties": map[string]any{
				"research_id": map[string]any{
					"type":        "string",
					"description": "The research ID from your task prompt.",
				},
				"answer": map[string]any{
					"type":        "string",
					"description": "Prose the developer can drop into their context as a reference. Include only what answers the specific question — names, signatures, calling conventions, lifecycle expectations. Leave out adjacent material the developer didn't ask about.",
				},
				"citations": map[string]any{
					"type":        "array",
					"description": "Pointers to the sources that back your answer. Each citation has exactly one of {url, file}, plus an optional lines hint. Required so the developer can verify or dig further if your answer leaves a gap.",
					"items": map[string]any{
						"type":     "object",
						"required": []string{},
						"properties": map[string]any{
							"url":   map[string]any{"type": "string", "description": "Canonical URL (github raw, docs site, etc.). Mutually exclusive with file."},
							"file":  map[string]any{"type": "string", "description": "Local path (worktree or /tmp extraction). Mutually exclusive with url."},
							"lines": map[string]any{"type": "string", "description": "Optional line range hint, e.g. '45-52' or '120'."},
						},
					},
				},
			},
		},
	}}
}
