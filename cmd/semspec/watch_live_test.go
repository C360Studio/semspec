package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// emptyStopMessages is a JSON payload for /message-logger/entries
// that triggers EmptyStopAfterToolCalls — a tool-call response
// followed by an empty stop in the same loop.
const emptyStopMessages = `[
  {"sequence":2,"timestamp":"2026-04-30T13:59:00Z","subject":"agent.response.loop-A:req:r2","raw_data":{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}},
  {"sequence":1,"timestamp":"2026-04-30T13:58:00Z","subject":"agent.response.loop-A:req:r1","raw_data":{"payload":{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"t1"}]}}}}
]`

func newLiveHTTPServer(messagesBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("semspec_loop_active_loops 2\n"))
		case "/message-logger/entries":
			_, _ = w.Write([]byte(messagesBody))
		case "/message-logger/kv/PLAN_STATES",
			"/message-logger/kv/AGENT_LOOPS":
			_, _ = w.Write([]byte(`{"bucket":"x","entries":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestRunWatchLive_BailOnCriticalExits(t *testing.T) {
	srv := newLiveHTTPServer(emptyStopMessages)
	defer srv.Close()

	var out bytes.Buffer
	cfg := liveConfig{
		HTTPURL:     srv.URL,
		NATSURL:     "",
		Interval:    20 * time.Millisecond,
		BailOn:      "critical",
		SkipOllama:  true,
		MaxDuration: 5 * time.Second, // safety cap if bail logic is broken
		Out:         &out,
	}
	err := runWatchLive(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runWatchLive: %v", err)
	}
	dump := out.String()
	if !strings.Contains(dump, "ALERT:") {
		t.Errorf("expected ALERT line in output:\n%s", dump)
	}
	if !strings.Contains(dump, "BAIL: severity=critical") {
		t.Errorf("expected BAIL line in output:\n%s", dump)
	}
	if !strings.Contains(dump, "EmptyStopAfterToolCalls") {
		t.Errorf("expected EmptyStopAfterToolCalls in alert:\n%s", dump)
	}
}

func TestRunWatchLive_AlertsAreDeduped(t *testing.T) {
	// Same diagnosis on every tick should fire ALERT exactly once.
	srv := newLiveHTTPServer(emptyStopMessages)
	defer srv.Close()

	var out bytes.Buffer
	cfg := liveConfig{
		HTTPURL: srv.URL,
		// No --bail-on; rely on MaxDuration to terminate after a few ticks.
		Interval:    30 * time.Millisecond,
		SkipOllama:  true,
		MaxDuration: 200 * time.Millisecond, // ~5 ticks
		Out:         &out,
	}
	if err := runWatchLive(context.Background(), cfg); err != nil {
		t.Fatalf("runWatchLive: %v", err)
	}
	alertCount := strings.Count(out.String(), "ALERT:")
	if alertCount != 1 {
		t.Errorf("expected exactly 1 ALERT line across ticks, got %d:\n%s", alertCount, out.String())
	}
}

func TestRunWatchLive_NoDiagnosesEmitsHeartbeatOnly(t *testing.T) {
	// All-clean message log: no detectors fire, only the per-tick
	// state line is printed. ALERT/BAIL lines must be absent.
	cleanMessages := `[]`
	srv := newLiveHTTPServer(cleanMessages)
	defer srv.Close()

	var out bytes.Buffer
	cfg := liveConfig{
		HTTPURL:     srv.URL,
		Interval:    30 * time.Millisecond,
		SkipOllama:  true,
		MaxDuration: 100 * time.Millisecond,
		Out:         &out,
	}
	if err := runWatchLive(context.Background(), cfg); err != nil {
		t.Fatalf("runWatchLive: %v", err)
	}
	dump := out.String()
	if strings.Contains(dump, "ALERT:") {
		t.Errorf("clean run should not emit ALERT:\n%s", dump)
	}
	if strings.Contains(dump, "BAIL:") {
		t.Errorf("clean run should not emit BAIL:\n%s", dump)
	}
	if !strings.Contains(dump, "plans=") {
		t.Errorf("expected at least one heartbeat line:\n%s", dump)
	}
}

func TestRunWatchLive_ContextCancelExitsCleanly(t *testing.T) {
	srv := newLiveHTTPServer(emptyStopMessages)
	defer srv.Close()

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cfg := liveConfig{
		HTTPURL:    srv.URL,
		Interval:   30 * time.Millisecond,
		SkipOllama: true,
		Out:        &out,
	}
	done := make(chan error, 1)
	go func() {
		done <- runWatchLive(ctx, cfg)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("runWatchLive did not exit after ctx cancel")
	}
	if !strings.Contains(out.String(), "context done") {
		t.Errorf("expected 'context done' shutdown line:\n%s", out.String())
	}
}

func TestSeverityRank_OrderingPinnedForBailOn(t *testing.T) {
	// --bail-on uses severityRank to compare observed vs threshold.
	// If the ranking changes silently, a "warning" threshold could
	// become weaker than "info" and runs would never bail.
	if !(severityRank("critical") > severityRank("warning") &&
		severityRank("warning") > severityRank("info")) {
		t.Error("severityRank ordering wrong: critical > warning > info expected")
	}
	// Unknown severity must rank below info so a typo in --bail-on
	// never causes the loop to exit unexpectedly.
	if severityRank("badtypo") >= severityRank("info") {
		t.Error("unknown severity should rank below info")
	}
}
