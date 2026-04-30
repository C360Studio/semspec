package health

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// stubRequester emulates natsclient.Client.Request for trajectory tests.
type stubRequester struct {
	body []byte
	err  error
	got  struct {
		subject string
		data    []byte
		timeout time.Duration
	}
}

func (s *stubRequester) Request(_ context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	s.got.subject = subject
	s.got.data = data
	s.got.timeout = timeout
	return s.body, s.err
}

func TestFetchTrajectory_HappyPath(t *testing.T) {
	body := []byte(`{"loop_id":"loop-1","steps":[{"step_type":"model_call"},{"step_type":"tool_call"}],"outcome":"success"}`)
	req := &stubRequester{body: body}

	gotBytes, ref, err := FetchTrajectory(context.Background(), req, "loop-1")
	if err != nil {
		t.Fatalf("FetchTrajectory: %v", err)
	}
	if string(gotBytes) != string(body) {
		t.Errorf("body not preserved verbatim")
	}
	if ref.LoopID != "loop-1" || ref.Steps != 2 || ref.Outcome != "success" {
		t.Errorf("ref = %+v", ref)
	}
	if req.got.subject != trajectorySubject {
		t.Errorf("subject = %q", req.got.subject)
	}
	// Sanity-check the on-the-wire request shape.
	var sent struct {
		LoopID string `json:"loopId"`
	}
	if err := json.Unmarshal(req.got.data, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if sent.LoopID != "loop-1" {
		t.Errorf("LoopID in request = %q", sent.LoopID)
	}
}

func TestFetchTrajectory_NotFound(t *testing.T) {
	// agentic-loop returns this error string for missing loop IDs.
	req := &stubRequester{err: errors.New("trajectory not found: loop-x")}
	_, _, err := FetchTrajectory(context.Background(), req, "loop-x")
	if !errors.Is(err, errTrajectoryNotFound) {
		t.Errorf("expected errTrajectoryNotFound, got %v", err)
	}
}

func TestFetchTrajectory_EmptyResponseIsHardError(t *testing.T) {
	// A non-error responder returning `{}` (no loop_id) is a buggy
	// responder, not a benign not-found. Surface loudly so adopters
	// see the responder bug in the bundle's error list rather than
	// confusing it with "loop evicted from cache."
	req := &stubRequester{body: []byte(`{}`)}
	_, _, err := FetchTrajectory(context.Background(), req, "loop-y")
	if err == nil {
		t.Fatal("expected error for empty-response responder bug")
	}
	if errors.Is(err, errTrajectoryNotFound) {
		t.Errorf("empty response should NOT be classified as not-found: %v", err)
	}
}

func TestFetchTrajectory_RejectsEmptyLoopID(t *testing.T) {
	req := &stubRequester{}
	if _, _, err := FetchTrajectory(context.Background(), req, ""); err == nil {
		t.Error("expected error for empty loop id")
	}
}
