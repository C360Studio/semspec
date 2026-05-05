package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func makeLoop(t *testing.T, key string, metadata map[string]any) KVEntry {
	t.Helper()
	val, err := json.Marshal(map[string]any{
		"id":       key,
		"metadata": metadata,
		"state":    "running",
	})
	if err != nil {
		t.Fatalf("marshal loop value: %v", err)
	}
	return KVEntry{Key: key, Revision: 1, Value: val}
}

func TestFetchTraceMessages_HappyPath(t *testing.T) {
	const fakeBody = `{"entries":[{"subject":"agent.task.planner","timestamp":"x"}]}`
	var mu sync.Mutex
	var hitTraces []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /message-logger/trace/<id>
		parts := strings.Split(r.URL.Path, "/")
		mu.Lock()
		hitTraces = append(hitTraces, parts[len(parts)-1])
		mu.Unlock()
		_, _ = w.Write([]byte(fakeBody))
	}))
	defer srv.Close()

	loops := []KVEntry{
		makeLoop(t, "loop-A", map[string]any{"trace_id": "trace-aaa"}),
		makeLoop(t, "loop-B", map[string]any{"trace_id": "trace-bbb"}),
	}
	collector := newErrCollector()
	out := FetchTraceMessages(context.Background(), srv.Client(), srv.URL, loops, collector)

	if len(out) != 2 {
		t.Fatalf("got %d trace dumps, want 2", len(out))
	}
	if out["loop-A"].TraceID != "trace-aaa" || out["loop-B"].TraceID != "trace-bbb" {
		t.Errorf("trace_ids not threaded: %+v", out)
	}
	if string(out["loop-A"].Body) != fakeBody {
		t.Errorf("body not preserved")
	}
	if len(collector.snapshot()) != 0 {
		t.Errorf("expected no errors, got %v", collector.snapshot())
	}
}

func TestFetchTraceMessages_DedupesSharedTraceID(t *testing.T) {
	// Two loops on the same trace (e.g. parent + spawned child). Hit
	// /message-logger/trace/X just once.
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	loops := []KVEntry{
		makeLoop(t, "loop-A", map[string]any{"trace_id": "shared"}),
		makeLoop(t, "loop-B", map[string]any{"trace_id": "shared"}),
	}
	out := FetchTraceMessages(context.Background(), srv.Client(), srv.URL, loops, newErrCollector())
	if hits != 1 {
		t.Errorf("trace fetched %d times, want 1 (dedup)", hits)
	}
	// Map output keyed by loop_id; only the first loop with that trace_id
	// gets an entry — second is silently dropped because dedup happens
	// before the map write.
	if len(out) != 1 {
		t.Errorf("got %d entries, want 1", len(out))
	}
}

func TestFetchTraceMessages_SkipsLoopsWithoutTraceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must NOT be hit when no loops carry trace_id")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	loops := []KVEntry{
		makeLoop(t, "loop-pre-beta43", map[string]any{"plan_slug": "x"}), // no trace_id
		makeLoop(t, "loop-no-metadata", nil),                             // no metadata at all
	}
	out := FetchTraceMessages(context.Background(), srv.Client(), srv.URL, loops, newErrCollector())
	if len(out) != 0 {
		t.Errorf("expected no fetches, got %d", len(out))
	}
}

func TestFetchTraceMessages_SkipsCompleteMarkerLoops(t *testing.T) {
	// COMPLETE_<id> entries are state markers, not live loops. Skipping
	// avoids double-fetching the same trace_id (live entry has it too).
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	loops := []KVEntry{
		makeLoop(t, "loop-A", map[string]any{"trace_id": "T"}),
		makeLoop(t, completeLoopMarkerPrefix+"loop-A", map[string]any{"trace_id": "T"}),
	}
	_ = FetchTraceMessages(context.Background(), srv.Client(), srv.URL, loops, newErrCollector())
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (COMPLETE_ prefix should skip)", hits)
	}
}

func TestFetchTraceMessages_PerTraceErrorRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	loops := []KVEntry{makeLoop(t, "loop-A", map[string]any{"trace_id": "trace-x"})}
	collector := newErrCollector()
	out := FetchTraceMessages(context.Background(), srv.Client(), srv.URL, loops, collector)

	if len(out) != 0 {
		t.Errorf("failed fetch should not appear in output, got %d entries", len(out))
	}
	errs := collector.snapshot()
	if len(errs) != 1 {
		t.Fatalf("expected 1 collector error, got %d", len(errs))
	}
	if !strings.HasPrefix(errs[0].Source, "trace:") {
		t.Errorf("error source = %q, want trace:* prefix", errs[0].Source)
	}
}
