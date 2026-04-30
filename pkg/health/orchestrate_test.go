package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestCapture_SkipsCOMPLETELoopMarkers(t *testing.T) {
	// AGENT_LOOPS contains marker rows like COMPLETE_<uuid> that
	// aren't fetchable trajectories. The orchestrator must skip
	// them up front; before this guard ~43% of trajectory pulls
	// failed on these markers in a healthy run.
	loopRow := KVEntry{Key: "real-uuid-1", Revision: 1, Value: json.RawMessage(`{}`)}
	markerRow := KVEntry{Key: "COMPLETE_real-uuid-1", Revision: 2, Value: json.RawMessage(`{}`)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte(""))
		case "/message-logger/entries":
			_, _ = w.Write([]byte("[]"))
		case "/message-logger/kv/AGENT_LOOPS":
			_ = json.NewEncoder(w).Encode(kvEntriesResponse{Bucket: "AGENT_LOOPS", Entries: []KVEntry{loopRow, markerRow}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var requested []string
	var requestedMu sync.Mutex
	traj := trajRequesterFunc(func(_ context.Context, _ string, data []byte, _ time.Duration) ([]byte, error) {
		var sent struct {
			LoopID string `json:"loopId"`
		}
		_ = json.Unmarshal(data, &sent)
		requestedMu.Lock()
		requested = append(requested, sent.LoopID)
		requestedMu.Unlock()
		return []byte(`{"loop_id":"` + sent.LoopID + `","steps":[],"outcome":"success"}`), nil
	})

	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), traj)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(requested) != 1 || requested[0] != "real-uuid-1" {
		t.Errorf("expected exactly one trajectory request for the real loop; got %v", requested)
	}
	for _, e := range res.Errors {
		if strings.HasPrefix(e.Source, "trajectory:COMPLETE_") {
			t.Errorf("orchestrator produced an error for a COMPLETE_ marker; got %v", e)
		}
	}
}

func TestCaptureError_NoDoublePrefix(t *testing.T) {
	// Regression: FetchTrajectory used to wrap its inner errors with
	// "trajectory:%s:" while orchestrator's CaptureError.Source did
	// the same, producing
	// "trajectory:X: trajectory:X: decode: invalid character 'e'..."
	// in the bundle's error list. Pin that the visible Error() form
	// has the prefix exactly once.
	traj := trajRequesterFunc(func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		return []byte(`not-json-body`), nil
	})
	loopRow := KVEntry{Key: "loop-X", Revision: 1, Value: json.RawMessage(`{}`)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/message-logger/kv/AGENT_LOOPS":
			_ = json.NewEncoder(w).Encode(kvEntriesResponse{Bucket: "AGENT_LOOPS", Entries: []KVEntry{loopRow}})
		case "/metrics":
			_, _ = w.Write([]byte(""))
		case "/message-logger/entries":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), traj)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	var trajErr *CaptureError
	for _, e := range res.Errors {
		if strings.HasPrefix(e.Source, "trajectory:") {
			trajErr = e
			break
		}
	}
	if trajErr == nil {
		t.Fatalf("expected a trajectory CaptureError; got %v", res.Errors)
	}
	full := trajErr.Error()
	// Counting "trajectory:" prefix occurrences. The Source provides
	// one; the inner err must not add a second.
	if got := strings.Count(full, "trajectory:"); got != 1 {
		t.Errorf("expected one 'trajectory:' prefix in error, got %d in %q", got, full)
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

func TestCapture_PullsEverySubjectAndDedupes(t *testing.T) {
	// Pin the behavior the P1 fix protects: orchestrator does one
	// HTTP pull per cfg.MessageSubjects (default: agent.* + tool.*),
	// and mergeMessages dedupes overlapping sequences.
	subjects := make(map[string]int)
	var subjectsMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte(""))
		case "/message-logger/entries":
			subj := r.URL.Query().Get("subject")
			subjectsMu.Lock()
			subjects[subj]++
			subjectsMu.Unlock()
			// Same payload (sequence 7) for both pulls so merge has
			// to dedupe.
			_, _ = w.Write([]byte(`[{"sequence":7,"timestamp":"2026-04-30T13:59:00Z","subject":"agent.response.foo","raw_data":{"finish_reason":"stop"}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := CaptureConfig{HTTPBaseURL: srv.URL, SkipOllama: true}
	res, err := Capture(context.Background(), cfg, srv.Client(), nil)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if subjects["agent.*"] != 1 || subjects["tool.*"] != 1 {
		t.Errorf("expected one pull per default subject; got %v", subjects)
	}
	if len(res.Bundle.Messages) != 1 {
		t.Errorf("dedupe failed: got %d messages, want 1 (same seq from two pulls)", len(res.Bundle.Messages))
	}
}

func TestMergeMessages_NewestFirst(t *testing.T) {
	// Bundle.Messages convention is newest-first; mergeMessages must
	// preserve that across multi-subject merges.
	in := map[string][]Message{
		"agent.*": {{Sequence: 5}, {Sequence: 3}},
		"tool.*":  {{Sequence: 4}, {Sequence: 1}},
	}
	got := mergeMessages(in)
	wantSeqs := []int64{5, 4, 3, 1}
	if len(got) != len(wantSeqs) {
		t.Fatalf("len = %d, want %d", len(got), len(wantSeqs))
	}
	for i, want := range wantSeqs {
		if got[i].Sequence != want {
			t.Errorf("got[%d].Sequence = %d, want %d", i, got[i].Sequence, want)
		}
	}
}

func TestMergeMessages_DedupesAcrossPatterns(t *testing.T) {
	// Sequence 5 appears in both pattern results — should land once.
	in := map[string][]Message{
		"agent.*": {{Sequence: 5, Subject: "agent.response.x"}},
		"tool.*":  {{Sequence: 5, Subject: "agent.response.x"}, {Sequence: 4, Subject: "tool.execute.y"}},
	}
	got := mergeMessages(in)
	if len(got) != 2 {
		t.Errorf("dedupe failed: got %d, want 2", len(got))
	}
}
