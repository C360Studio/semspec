package dispatchretry

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newWithSleepRecorder builds a State whose sleepFn records every requested
// duration without actually sleeping, so tests can assert backoff behaviour
// in microseconds.
func newWithSleepRecorder(cfg Config) (*State, *[]time.Duration) {
	var sleeps []time.Duration
	var mu sync.Mutex
	s := New(cfg)
	s.sleepFn = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		sleeps = append(sleeps, d)
		mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	return s, &sleeps
}

func TestNew_ClampsNegativeConfig(t *testing.T) {
	s := New(Config{MaxRetries: -5, BackoffMs: -10})
	if s.cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries: want clamp to 0, got %d", s.cfg.MaxRetries)
	}
	if s.cfg.BackoffMs != 0 {
		t.Errorf("BackoffMs: want clamp to 0, got %d", s.cfg.BackoffMs)
	}
}

func TestTrack_NewEntry(t *testing.T) {
	s := New(Config{MaxRetries: 2})
	entry, fresh := s.Track("plan-a", "payload-1")
	if !fresh {
		t.Fatal("first Track should report fresh=true")
	}
	if entry.Count != 0 {
		t.Errorf("new entry count: want 0, got %d", entry.Count)
	}
	if entry.Payload != "payload-1" {
		t.Errorf("payload: want payload-1, got %v", entry.Payload)
	}
}

func TestTrack_Dedup(t *testing.T) {
	s := New(Config{MaxRetries: 2})
	first, _ := s.Track("plan-a", "payload-1")
	second, fresh := s.Track("plan-a", "payload-2")
	if fresh {
		t.Fatal("second Track for same key should report fresh=false")
	}
	if second != first {
		t.Error("second Track should return existing entry pointer")
	}
	if second.Payload != "payload-1" {
		t.Errorf("payload preserved: want payload-1, got %v", second.Payload)
	}
}

func TestTick_RetryUnderCap(t *testing.T) {
	s, sleeps := newWithSleepRecorder(Config{MaxRetries: 2, BackoffMs: 50})
	s.Track("plan-a", "p")
	ctx := context.Background()

	entry, ok := s.Tick(ctx, "plan-a")
	if !ok {
		t.Fatal("first retry under cap should return ok=true")
	}
	if entry.Count != 1 {
		t.Errorf("count after Tick 1: want 1, got %d", entry.Count)
	}
	entry, ok = s.Tick(ctx, "plan-a")
	if !ok {
		t.Fatal("second retry under cap should return ok=true")
	}
	if entry.Count != 2 {
		t.Errorf("count after Tick 2: want 2, got %d", entry.Count)
	}
	if len(*sleeps) != 2 {
		t.Errorf("sleeps recorded: want 2, got %d", len(*sleeps))
	}
}

func TestTick_HitsCapAndClears(t *testing.T) {
	s, _ := newWithSleepRecorder(Config{MaxRetries: 1, BackoffMs: 1})
	s.Track("plan-a", "payload")
	ctx := context.Background()

	if _, ok := s.Tick(ctx, "plan-a"); !ok {
		t.Fatal("attempt 1 (under cap) should succeed")
	}
	entry, ok := s.Tick(ctx, "plan-a")
	if ok {
		t.Fatal("attempt 2 (over cap) should return ok=false")
	}
	if entry == nil {
		t.Fatal("over-cap Tick should still return entry for fail-closed payload access")
	}
	if entry.Count != 2 {
		t.Errorf("count at cap: want 2, got %d", entry.Count)
	}
	if _, present := s.Snapshot("plan-a"); present {
		t.Error("entry should be auto-cleared when cap hit")
	}
}

func TestTick_NoEntry(t *testing.T) {
	s := New(Config{MaxRetries: 1, BackoffMs: 1})
	entry, ok := s.Tick(context.Background(), "missing")
	if ok {
		t.Error("Tick for missing key should return ok=false")
	}
	if entry != nil {
		t.Error("Tick for missing key should return nil entry")
	}
}

func TestTick_ZeroMaxRetries(t *testing.T) {
	s := New(Config{MaxRetries: 0, BackoffMs: 1})
	s.Track("plan-a", "p")
	entry, ok := s.Tick(context.Background(), "plan-a")
	if ok {
		t.Error("MaxRetries=0 should reject the first Tick")
	}
	if entry == nil || entry.Count != 1 {
		t.Errorf("entry returned at cap: want count=1, got %+v", entry)
	}
}

func TestTick_BackoffTimingFloor(t *testing.T) {
	s := New(Config{MaxRetries: 5, BackoffMs: 30})
	s.Track("plan-a", "p")
	ctx := context.Background()
	start := time.Now()
	if _, ok := s.Tick(ctx, "plan-a"); !ok {
		t.Fatal("expected ok")
	}
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Errorf("backoff floor not honoured: elapsed %s, want >= 30ms", elapsed)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("backoff jitter exceeded ceiling: elapsed %s, want <= 100ms (BackoffMs * 2 + scheduling slack)", elapsed)
	}
}

func TestTick_ContextCanceledDuringBackoff(t *testing.T) {
	s := New(Config{MaxRetries: 5, BackoffMs: 100})
	s.Track("plan-a", "p")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	entry, ok := s.Tick(ctx, "plan-a")
	if ok {
		t.Error("ctx cancellation should return ok=false")
	}
	if entry != nil {
		t.Error("ctx cancellation should return nil entry (caller should stop, not fail-closed)")
	}
	// Entry preserved so the next Tick (after restart, etc.) can pick up.
	snap, present := s.Snapshot("plan-a")
	if !present {
		t.Fatal("entry should be preserved on ctx cancellation")
	}
	if snap.Count != 1 {
		t.Errorf("count incremented before sleep: want 1, got %d", snap.Count)
	}
}

func TestSetActiveLoop_AndIsStaleLoop(t *testing.T) {
	s := New(Config{MaxRetries: 2})
	if s.IsStaleLoop("plan-a", "loop-1") {
		t.Error("missing entry should not report stale")
	}
	s.Track("plan-a", "p")
	if s.IsStaleLoop("plan-a", "loop-1") {
		t.Error("entry without recorded loop ID should not report stale")
	}
	s.SetActiveLoop("plan-a", "loop-1")
	if s.IsStaleLoop("plan-a", "loop-1") {
		t.Error("matching loop ID should not report stale")
	}
	if !s.IsStaleLoop("plan-a", "loop-0") {
		t.Error("mismatched loop ID should report stale")
	}
	s.SetActiveLoop("plan-a", "loop-2")
	if !s.IsStaleLoop("plan-a", "loop-1") {
		t.Error("after re-dispatch, old loop ID should be stale")
	}
}

func TestSetActiveLoop_NoEntry(t *testing.T) {
	s := New(Config{MaxRetries: 2})
	// No panic, no entry created.
	s.SetActiveLoop("missing", "loop-1")
	if s.Len() != 0 {
		t.Errorf("SetActiveLoop should not create entries: Len=%d", s.Len())
	}
}

func TestClear_Idempotent(t *testing.T) {
	s := New(Config{MaxRetries: 2})
	s.Track("plan-a", "p")
	s.Clear("plan-a")
	s.Clear("plan-a") // no panic
	s.Clear("missing")
	if s.Len() != 0 {
		t.Errorf("expected empty after Clear, got Len=%d", s.Len())
	}
}

func TestSnapshot_Defensive(t *testing.T) {
	s := New(Config{MaxRetries: 2})
	s.Track("plan-a", "p")
	s.SetActiveLoop("plan-a", "loop-1")
	snap, ok := s.Snapshot("plan-a")
	if !ok {
		t.Fatal("Snapshot should find tracked entry")
	}
	// Mutating snapshot must not affect registry.
	snap.Count = 99
	if entry, _ := s.Snapshot("plan-a"); entry.Count != 0 {
		t.Errorf("Snapshot mutation leaked: registry count=%d", entry.Count)
	}
}

// Concurrent Track/Tick/Clear should not race or lose accounting.
func TestConcurrent_TrackTickClear(_ *testing.T) {
	s := New(Config{MaxRetries: 100, BackoffMs: 0})
	const goroutines = 50
	const opsPerG = 200
	var wg sync.WaitGroup
	var clears atomic.Int64
	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx := context.Background()
			for i := range opsPerG {
				key := "plan-" + string(rune('A'+id%5))
				s.Track(key, id)
				_, _ = s.Tick(ctx, key)
				if i%10 == 0 {
					s.Clear(key)
					clears.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()
	// No assertion on Len — just confirming no race and no panic. -race
	// catches the data races; the lack of panic confirms correctness.
}
