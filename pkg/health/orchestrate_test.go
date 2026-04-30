package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// trajRequesterFunc adapts a function to trajectoryRequester.
type trajRequesterFunc func(context.Context, string, []byte, time.Duration) ([]byte, error)

func (f trajRequesterFunc) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	return f(ctx, subject, data, timeout)
}

func newOrchestrateServer(t *testing.T) *httptest.Server {
	t.Helper()
	loopRow := KVEntry{
		Key:      "loop-1",
		Revision: 3,
		Created:  time.Date(2026, 4, 30, 13, 50, 0, 0, time.UTC),
		Value:    json.RawMessage(`{"id":"loop-1","state":"complete"}`),
	}
	planRow := KVEntry{
		Key:      "plan-abc",
		Revision: 7,
		Created:  time.Date(2026, 4, 30, 13, 51, 0, 0, time.UTC),
		Value:    json.RawMessage(`{"slug":"plan-abc","status":"complete"}`),
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("semstreams_agentic_loop_active_loops 4\n"))
		case "/message-logger/entries":
			_, _ = w.Write([]byte(`[{"sequence":1,"timestamp":"2026-04-30T13:59:00Z","subject":"agent.response.foo","message_type":"agentic.response.v1","raw_data":{"finish_reason":"stop"}}]`))
		case "/message-logger/kv/PLAN_STATES":
			_ = json.NewEncoder(w).Encode(kvEntriesResponse{Bucket: "PLAN_STATES", Entries: []KVEntry{planRow}})
		case "/message-logger/kv/AGENT_LOOPS":
			_ = json.NewEncoder(w).Encode(kvEntriesResponse{Bucket: "AGENT_LOOPS", Entries: []KVEntry{loopRow}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestCapture_AssemblesBundleFromAllSources(t *testing.T) {
	srv := newOrchestrateServer(t)
	defer srv.Close()

	trajBody := []byte(`{"loop_id":"loop-1","steps":[{}],"outcome":"success"}`)
	traj := trajRequesterFunc(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		if subject != trajectorySubject {
			t.Errorf("unexpected subject %q", subject)
		}
		return trajBody, nil
	})

	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true, CapturedBy: "semspec-test"}
	res, err := Capture(context.Background(), cfg, srv.Client(), traj)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if res.Bundle.Bundle.Format != BundleFormat {
		t.Errorf("Format = %q", res.Bundle.Bundle.Format)
	}
	if res.Bundle.Bundle.CapturedBy != "semspec-test" {
		t.Errorf("CapturedBy = %q", res.Bundle.Bundle.CapturedBy)
	}
	if res.Bundle.Bundle.CapturedAt.IsZero() {
		t.Error("Bundle.CapturedAt should be set")
	}
	if !res.Bundle.Metrics.CapturedAt.Equal(res.Bundle.Bundle.CapturedAt) {
		t.Errorf("Metrics.CapturedAt should match bundle instant: %v vs %v",
			res.Bundle.Metrics.CapturedAt, res.Bundle.Bundle.CapturedAt)
	}
	if res.Bundle.Metrics.LoopActiveLoops != 4 {
		t.Errorf("Metrics not parsed: %+v", res.Bundle.Metrics)
	}
	if len(res.Bundle.Plans) != 1 || res.Bundle.Plans[0].Key != "plan-abc" {
		t.Errorf("Plans missing: %+v", res.Bundle.Plans)
	}
	if len(res.Bundle.Loops) != 1 || res.Bundle.Loops[0].Key != "loop-1" {
		t.Errorf("Loops missing: %+v", res.Bundle.Loops)
	}
	if len(res.Bundle.Messages) != 1 {
		t.Errorf("Messages missing: %+v", res.Bundle.Messages)
	}
	if len(res.Bundle.TrajectoryRefs) != 1 || res.Bundle.TrajectoryRefs[0].LoopID != "loop-1" {
		t.Errorf("TrajectoryRefs: %+v", res.Bundle.TrajectoryRefs)
	}
	if got := res.Trajectories["loop-1"]; string(got) != string(trajBody) {
		t.Error("trajectory body not preserved verbatim")
	}
	if len(res.Errors) != 0 {
		t.Errorf("expected no errors, got %v", res.Errors)
	}
}

func TestCapture_PartialFailureLogsError(t *testing.T) {
	// Metrics endpoint 5xx but everything else healthy: bundle still
	// assembles, error is recorded, metrics section stays zero.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(http.StatusInternalServerError)
		case "/message-logger/entries":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound) // KV buckets 404 → empty section
		}
	}))
	defer srv.Close()

	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), nil)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if res.Bundle == nil {
		t.Fatal("Bundle should be non-nil even with partial failure")
	}
	if res.Bundle.Metrics.LoopActiveLoops != 0 {
		t.Errorf("Metrics should be zero on failure: %+v", res.Bundle.Metrics)
	}
	if len(res.Errors) != 1 || !strings.HasPrefix(res.Errors[0].Source, "metrics") {
		t.Errorf("expected one metrics error, got %v", res.Errors)
	}
}

func TestCapture_NotFoundTrajectoryNotAnError(t *testing.T) {
	// Loop in AGENT_LOOPS, but the agentic-loop has evicted the
	// trajectory from cache. That's a benign skip — bundle should not
	// record a CaptureError for it.
	srv := newOrchestrateServer(t)
	defer srv.Close()

	traj := trajRequesterFunc(func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		return nil, errors.New("trajectory not found: loop-1")
	})
	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), traj)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(res.Bundle.TrajectoryRefs) != 0 {
		t.Errorf("expected no refs for not-found loop, got %+v", res.Bundle.TrajectoryRefs)
	}
	for _, e := range res.Errors {
		if strings.HasPrefix(e.Source, "trajectory:") {
			t.Errorf("not-found should not be a CaptureError: %v", e)
		}
	}
}

func TestCapture_NilNATSSkipsTrajectories(t *testing.T) {
	// Adopters running without a NATS connection (offline bundle replay)
	// should still get a bundle minus the trajectory section.
	srv := newOrchestrateServer(t)
	defer srv.Close()

	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), nil)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(res.Bundle.TrajectoryRefs) != 0 {
		t.Errorf("nil NATS should skip trajectories, got %+v", res.Bundle.TrajectoryRefs)
	}
}

func TestCapture_TrajectoriesSkippedWhenAgentLoopsUnavailable(t *testing.T) {
	// When AGENT_LOOPS fetch fails, the orchestrator must record a
	// causal "trajectories skipped" error so a reader sees the link
	// instead of silently empty TrajectoryRefs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("semstreams_agentic_loop_active_loops 1\n"))
		case "/message-logger/entries":
			_, _ = w.Write([]byte("[]"))
		case "/message-logger/kv/AGENT_LOOPS":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	traj := trajRequesterFunc(func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		t.Fatal("trajectory request should not happen when AGENT_LOOPS is unavailable")
		return nil, nil
	})
	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), traj)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	var sawKVErr, sawTrajErr bool
	for _, e := range res.Errors {
		if strings.HasPrefix(e.Source, "kv:AGENT_LOOPS") {
			sawKVErr = true
		}
		if e.Source == "trajectories" {
			sawTrajErr = true
		}
	}
	if !sawKVErr || !sawTrajErr {
		t.Errorf("expected kv:AGENT_LOOPS + trajectories errors; got %v", res.Errors)
	}
}

func TestCapture_DefaultCapturedBy(t *testing.T) {
	srv := newOrchestrateServer(t)
	defer srv.Close()
	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), nil)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if res.Bundle.Bundle.CapturedBy != "semspec-dev" {
		t.Errorf("default CapturedBy = %q, want semspec-dev", res.Bundle.Bundle.CapturedBy)
	}
}
