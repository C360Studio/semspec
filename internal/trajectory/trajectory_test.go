package trajectory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// stubRequester implements Requester for unit tests.
type stubRequester struct {
	resp []byte
	err  error

	// Captured request for assertion.
	gotSubject string
	gotData    []byte
	gotTimeout time.Duration
}

func (s *stubRequester) Request(_ context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	s.gotSubject = subject
	s.gotData = data
	s.gotTimeout = timeout
	return s.resp, s.err
}

func TestFetch_Success(t *testing.T) {
	traj := agentic.Trajectory{
		LoopID:    "loop-abc",
		StartTime: time.Now(),
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "bash", ToolResult: "ok"},
		},
	}
	body, _ := json.Marshal(&traj)
	stub := &stubRequester{resp: body}

	got, err := Fetch(context.Background(), stub, "loop-abc", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LoopID != "loop-abc" || len(got.Steps) != 1 {
		t.Errorf("unexpected trajectory: %+v", got)
	}
	if stub.gotSubject != Subject {
		t.Errorf("subject = %q, want %q", stub.gotSubject, Subject)
	}
	if stub.gotTimeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", stub.gotTimeout, DefaultTimeout)
	}
}

func TestFetch_RequestEncoding(t *testing.T) {
	traj := agentic.Trajectory{LoopID: "x", Steps: []agentic.TrajectoryStep{}}
	body, _ := json.Marshal(&traj)
	stub := &stubRequester{resp: body}

	if _, err := Fetch(context.Background(), stub, "x", 25); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sent struct {
		LoopID string `json:"loopId"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(stub.gotData, &sent); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if sent.LoopID != "x" || sent.Limit != 25 {
		t.Errorf("unexpected request body: %+v", sent)
	}
}

func TestFetch_NotFound(t *testing.T) {
	stub := &stubRequester{err: errors.New("trajectory not found: missing")}

	_, err := Fetch(context.Background(), stub, "loop-x", 0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFetch_TransportError(t *testing.T) {
	stub := &stubRequester{err: errors.New("nats: timeout")}

	_, err := Fetch(context.Background(), stub, "loop-x", 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("transport error should not classify as ErrNotFound")
	}
}

func TestFetch_NilClient(t *testing.T) {
	if _, err := Fetch(context.Background(), nil, "loop-x", 0); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestFetch_EmptyLoopID(t *testing.T) {
	stub := &stubRequester{}
	if _, err := Fetch(context.Background(), stub, "", 0); err == nil {
		t.Fatal("expected error for empty loop id")
	}
}

func TestFetch_EmptyResponseLoopID(t *testing.T) {
	// Defensive: agentic-loop responder is expected to return either an
	// error or a populated Trajectory. A blank Trajectory{} on the wire is
	// treated as a wire-format failure so callers don't try to build
	// anything from nothing.
	stub := &stubRequester{resp: []byte(`{}`)}
	if _, err := Fetch(context.Background(), stub, "x", 0); err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestFetch_MalformedResponse(t *testing.T) {
	stub := &stubRequester{resp: []byte(`not json`)}
	if _, err := Fetch(context.Background(), stub, "x", 0); err == nil {
		t.Fatal("expected error for malformed response")
	}
}

func TestSummarizeStep_ToolCallSuccess(t *testing.T) {
	step := agentic.TrajectoryStep{
		StepType: "tool_call",
		ToolName: "read_file",
		ToolResult: "package main\nimport \"fmt\"\nfunc main(){\n" +
			"  fmt.Println(\"hello world this is a long result that should be clipped\")\n}\n",
	}
	got := SummarizeStep(step, 200)
	if !strings.Contains(got, "tool_call(read_file)") {
		t.Errorf("missing tool name in summary: %q", got)
	}
}

func TestSummarizeStep_ToolCallFailed(t *testing.T) {
	step := agentic.TrajectoryStep{
		StepType:     "tool_call",
		ToolName:     "bash",
		ToolStatus:   "failed",
		ErrorMessage: "exit code 2",
	}
	got := SummarizeStep(step, 200)
	if !strings.Contains(got, "FAILED") || !strings.Contains(got, "exit code 2") {
		t.Errorf("expected FAILED + error in summary, got %q", got)
	}
}

func TestSummarizeStep_ModelCall(t *testing.T) {
	step := agentic.TrajectoryStep{StepType: "model_call", Model: "qwen3-coder:14b"}
	got := SummarizeStep(step, 200)
	if !strings.Contains(got, "model_call(qwen3-coder:14b)") {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestSummarizeStep_Compaction(t *testing.T) {
	step := agentic.TrajectoryStep{StepType: "context_compaction", Utilization: 0.85}
	got := SummarizeStep(step, 200)
	if !strings.Contains(got, "context_compaction") {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestClip(t *testing.T) {
	if got := clip("hello", 100); got != "hello" {
		t.Errorf("clip short string changed it: %q", got)
	}
	got := clip("abcdefghij", 5)
	if len([]rune(got)) != 5 || got[len(got)-len("…"):] != "…" {
		t.Errorf("clip did not append ellipsis: %q", got)
	}
}

// debugLogger returns a slog.Logger that writes JSON to a buffer at DEBUG
// level — the trajectory log helper emits at DEBUG so the default INFO
// handler would silently drop everything.
func debugLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), &buf
}

func TestLogSummary_Success(t *testing.T) {
	traj := agentic.Trajectory{
		LoopID:    "loop-7",
		StartTime: time.Now(),
		Steps: []agentic.TrajectoryStep{
			{StepType: "tool_call", ToolName: "bash", ToolStatus: "failed", ErrorMessage: "exit 1"},
			{StepType: "model_call", Model: "qwen3:14b"},
		},
	}
	body, _ := json.Marshal(&traj)
	stub := &stubRequester{resp: body}
	logger, buf := debugLogger()

	LogSummary(context.Background(), logger, stub, "loop-7", "test-context", 0)

	out := buf.String()
	if !strings.Contains(out, `"msg":"Trajectory summary"`) {
		t.Errorf("expected success message, got %s", out)
	}
	if !strings.Contains(out, `"loop_id":"loop-7"`) {
		t.Errorf("expected loop_id field, got %s", out)
	}
	if !strings.Contains(out, `"context":"test-context"`) {
		t.Errorf("expected context field, got %s", out)
	}
	if !strings.Contains(out, `"step_count":2`) {
		t.Errorf("expected step_count=2, got %s", out)
	}
}

func TestLogSummary_NotFound(t *testing.T) {
	stub := &stubRequester{err: errors.New("trajectory not found: missing")}
	logger, buf := debugLogger()

	LogSummary(context.Background(), logger, stub, "loop-x", "test-context", 0)

	out := buf.String()
	if !strings.Contains(out, `"msg":"Trajectory not available"`) {
		t.Errorf("expected not-available message, got %s", out)
	}
	if !strings.Contains(out, `"loop_id":"loop-x"`) {
		t.Errorf("expected loop_id field, got %s", out)
	}
}

func TestLogSummary_TransportError(t *testing.T) {
	stub := &stubRequester{err: errors.New("nats: connection lost")}
	logger, buf := debugLogger()

	LogSummary(context.Background(), logger, stub, "loop-x", "test-context", 0)

	out := buf.String()
	if !strings.Contains(out, `"msg":"Trajectory fetch failed"`) {
		t.Errorf("expected fetch-failed message, got %s", out)
	}
	if !strings.Contains(out, "connection lost") {
		t.Errorf("expected error detail in log, got %s", out)
	}
}

func TestLogSummary_EmptyLoopID(t *testing.T) {
	// Empty loopID is a soft-no-op so callers don't need a guard.
	stub := &stubRequester{}
	logger, buf := debugLogger()

	LogSummary(context.Background(), logger, stub, "", "test-context", 0)

	if buf.Len() != 0 {
		t.Errorf("expected no log output for empty loopID, got %s", buf.String())
	}
	if stub.gotSubject != "" {
		t.Errorf("expected no NATS request for empty loopID, but got subject %q", stub.gotSubject)
	}
}

func TestLogSummary_NilLogger(t *testing.T) {
	// Nil logger is a soft-no-op so callers don't crash if they forgot to
	// thread the logger through.
	stub := &stubRequester{}
	LogSummary(context.Background(), nil, stub, "loop-x", "test-context", 0)
	if stub.gotSubject != "" {
		t.Errorf("expected no NATS request when logger nil, got subject %q", stub.gotSubject)
	}
}
