package lessondecomposer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// stubRequester implements trajectoryRequester for unit tests.
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

func TestFetchTrajectory_Success(t *testing.T) {
	traj := agentic.Trajectory{
		LoopID:    "loop-abc",
		StartTime: time.Now(),
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "bash", ToolResult: "ok"},
		},
	}
	body, _ := json.Marshal(&traj)
	stub := &stubRequester{resp: body}

	got, err := fetchTrajectory(context.Background(), stub, "loop-abc", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LoopID != "loop-abc" || len(got.Steps) != 1 {
		t.Errorf("unexpected trajectory: %+v", got)
	}
	if stub.gotSubject != trajectorySubject {
		t.Errorf("subject = %q, want %q", stub.gotSubject, trajectorySubject)
	}
	if stub.gotTimeout != trajectoryRequestTimeout {
		t.Errorf("timeout = %v, want %v", stub.gotTimeout, trajectoryRequestTimeout)
	}
}

func TestFetchTrajectory_RequestEncoding(t *testing.T) {
	traj := agentic.Trajectory{LoopID: "x", Steps: []agentic.TrajectoryStep{}}
	body, _ := json.Marshal(&traj)
	stub := &stubRequester{resp: body}

	if _, err := fetchTrajectory(context.Background(), stub, "x", 25); err != nil {
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

func TestFetchTrajectory_NotFound(t *testing.T) {
	stub := &stubRequester{err: errors.New("trajectory not found: missing")}

	_, err := fetchTrajectory(context.Background(), stub, "loop-x", 0)
	if !errors.Is(err, ErrTrajectoryNotFound) {
		t.Errorf("expected ErrTrajectoryNotFound, got %v", err)
	}
}

func TestFetchTrajectory_TransportError(t *testing.T) {
	stub := &stubRequester{err: errors.New("nats: timeout")}

	_, err := fetchTrajectory(context.Background(), stub, "loop-x", 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrTrajectoryNotFound) {
		t.Error("transport error should not classify as ErrTrajectoryNotFound")
	}
}

func TestFetchTrajectory_NilClient(t *testing.T) {
	if _, err := fetchTrajectory(context.Background(), nil, "loop-x", 0); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestFetchTrajectory_EmptyLoopID(t *testing.T) {
	stub := &stubRequester{}
	if _, err := fetchTrajectory(context.Background(), stub, "", 0); err == nil {
		t.Fatal("expected error for empty loop id")
	}
}

func TestFetchTrajectory_EmptyResponseLoopID(t *testing.T) {
	// Defensive: agentic-loop responder is expected to return either an
	// error or a populated Trajectory. A blank Trajectory{} on the wire is
	// treated as a wire-format failure so the decomposer doesn't try to
	// build a lesson from nothing.
	stub := &stubRequester{resp: []byte(`{}`)}
	if _, err := fetchTrajectory(context.Background(), stub, "x", 0); err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestFetchTrajectory_MalformedResponse(t *testing.T) {
	stub := &stubRequester{resp: []byte(`not json`)}
	if _, err := fetchTrajectory(context.Background(), stub, "x", 0); err == nil {
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
	got := summarizeStep(step, 200)
	if !strContains(got, "tool_call(read_file)") {
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
	got := summarizeStep(step, 200)
	if !strContains(got, "FAILED") || !strContains(got, "exit code 2") {
		t.Errorf("expected FAILED + error in summary, got %q", got)
	}
}

func TestSummarizeStep_ModelCall(t *testing.T) {
	step := agentic.TrajectoryStep{StepType: "model_call", Model: "qwen3-coder:14b"}
	got := summarizeStep(step, 200)
	if !strContains(got, "model_call(qwen3-coder:14b)") {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestSummarizeStep_Compaction(t *testing.T) {
	step := agentic.TrajectoryStep{StepType: "context_compaction", Utilization: 0.85}
	got := summarizeStep(step, 200)
	if !strContains(got, "context_compaction") {
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
