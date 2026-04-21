package question

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

func testExecutor() *Executor {
	return &Executor{
		timeout: DefaultTimeout,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestNormalizeQuestion(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{"Hello world", "hello world"},
		{"  Hello   world  ", "hello world"},
		{"HELLO\n\tWorld", "hello world"},
		{"", ""},
		{"   ", ""},
		{"Is there an existing implementation?", "is there an existing implementation?"},
		{"is there   an existing\nimplementation?", "is there an existing implementation?"},
	}
	for _, tt := range tests {
		got := normalizeQuestion(tt.in)
		if got != tt.out {
			t.Errorf("normalizeQuestion(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestHandleDuplicate_Answered(t *testing.T) {
	e := testExecutor()
	call := agentic.ToolCall{ID: "call-1", LoopID: "loop-1"}
	dup := &workflow.Question{
		ID:        "q-abc",
		FromAgent: "loop-1",
		Question:  "Is there an existing implementation?",
		Status:    workflow.QuestionStatusAnswered,
		Answer:    "No, this is a greenfield project. Start from scratch.",
		CreatedAt: time.Now().Add(-2 * time.Minute),
	}

	res := e.handleDuplicate(call, dup, dup.Question)
	if res.Error != "" {
		t.Errorf("unexpected error: %s", res.Error)
	}
	if !strings.Contains(res.Content, "q-abc") {
		t.Errorf("content should reference the prior question ID, got %q", res.Content)
	}
	if !strings.Contains(res.Content, dup.Answer) {
		t.Errorf("content should surface the prior answer verbatim, got %q", res.Content)
	}
}

func TestHandleDuplicate_Timeout(t *testing.T) {
	e := testExecutor()
	call := agentic.ToolCall{ID: "call-2", LoopID: "loop-1"}
	dup := &workflow.Question{
		ID:        "q-xyz",
		FromAgent: "loop-1",
		Question:  "Is there an existing implementation?",
		Status:    workflow.QuestionStatusTimeout,
		CreatedAt: time.Now().Add(-6 * time.Minute),
	}

	res := e.handleDuplicate(call, dup, dup.Question)
	if res.Error != "" {
		t.Errorf("unexpected error: %s", res.Error)
	}
	// Message must tell the agent to stop re-asking — the mortgage-calc log
	// shows the same question asked every ~1min after timeout until the
	// requirement-level deadline. That's what this guard is for.
	if !strings.Contains(strings.ToLower(res.Content), "do not ask it again") {
		t.Errorf("content should instruct agent not to re-ask, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "q-xyz") {
		t.Errorf("content should reference the prior question ID, got %q", res.Content)
	}
}

func TestHandleDuplicate_Pending(t *testing.T) {
	e := testExecutor()
	call := agentic.ToolCall{ID: "call-3", LoopID: "loop-1"}
	dup := &workflow.Question{
		ID:        "q-pending",
		FromAgent: "loop-1",
		Question:  "Is there an existing implementation?",
		Status:    workflow.QuestionStatusPending,
		CreatedAt: time.Now().Add(-1 * time.Minute),
	}

	res := e.handleDuplicate(call, dup, dup.Question)
	if !strings.Contains(strings.ToLower(res.Content), "still pending") {
		t.Errorf("content should indicate still pending, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "q-pending") {
		t.Errorf("content should reference the prior question ID, got %q", res.Content)
	}
}
