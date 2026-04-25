package dispatchretry

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// Config governs a State's retry policy.
type Config struct {
	// MaxRetries caps the number of re-dispatches after the initial attempt.
	// Total dispatches per key is at most MaxRetries+1. Must be >= 0; a value
	// of 0 disables retry (Tick always returns false on the first call).
	MaxRetries int

	// BackoffMs is the floor of the jittered delay applied inside Tick before
	// it returns. Effective delay is in [BackoffMs, 2*BackoffMs). Set to 0
	// to disable the sleep — useful in tests and when the caller wants
	// instantaneous retries (legacy behaviour).
	BackoffMs int
}

// Entry is the per-key retry state observed by callers. Fields are read-only
// from outside the package — mutate only via State methods.
type Entry struct {
	// Count is the number of retry attempts that have already executed.
	// Initial dispatch is attempt 0; the first Tick() bumps to 1.
	Count int

	// Payload is the component-supplied context needed to re-dispatch
	// (typically *workflow.Plan or a per-component dispatch context).
	// The package never reads it.
	Payload any

	// activeLoopID is the most recent loop/task ID recorded via
	// SetActiveLoop. Used by IsStaleLoop to discard completion events
	// for older dispatches that race the current one.
	activeLoopID string
}

// State is the per-key retry registry. Concurrent-safe: a single State can
// be shared by all goroutines of one component. The zero value is not
// usable — construct via New.
type State struct {
	cfg Config

	mu      sync.Mutex
	entries map[string]*Entry

	// sleepFn is overridable for tests. nil means time.After.
	sleepFn func(context.Context, time.Duration) error
}

// New returns a State with the given config. MaxRetries < 0 is clamped to
// 0; BackoffMs < 0 is clamped to 0.
func New(cfg Config) *State {
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.BackoffMs < 0 {
		cfg.BackoffMs = 0
	}
	return &State{
		cfg:     cfg,
		entries: make(map[string]*Entry),
	}
}

// Track returns the current entry for key, creating it with payload if
// absent. The boolean is true when the entry was newly created (the caller
// is the initial dispatcher) and false when an entry already existed
// (deduplicate — drop the trigger). When false, payload is ignored and
// the existing payload is preserved.
//
// Callers use this to atomically claim "I am the dispatcher for this key,"
// matching the LoadOrStore pattern that sync.Map-backed components used
// pre-migration.
func (s *State) Track(key string, payload any) (*Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[key]; ok {
		return existing, false
	}
	entry := &Entry{Payload: payload}
	s.entries[key] = entry
	return entry, true
}

// SetActiveLoop records the current dispatch's loop/task ID so a later
// IsStaleLoop check can reject completions from earlier dispatches that
// raced the new one. No-op if no entry exists for key (e.g. the entry was
// concurrently cleared).
func (s *State) SetActiveLoop(key, loopID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[key]; ok {
		entry.activeLoopID = loopID
	}
}

// IsStaleLoop reports whether a completing loop should be discarded
// because a newer dispatch is in flight for the same key. Returns true
// only when an entry exists, an active loop ID has been recorded, AND
// the recorded ID disagrees with completedLoopID. Returns false when no
// entry or no recorded ID — callers shouldn't drop the event in that
// case (it's the initial dispatch, not a stale one).
func (s *State) IsStaleLoop(key, completedLoopID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return false
	}
	if entry.activeLoopID == "" {
		return false
	}
	return entry.activeLoopID != completedLoopID
}

// Tick attempts another retry: increments the count, sleeps the backoff
// (cancellable on ctx), and reports whether a re-dispatch should fire.
//
// Returns (entry, true) when count <= MaxRetries and the caller should
// proceed with re-dispatch. The returned entry carries the original
// payload so the caller can rebuild the dispatch.
//
// Returns (entry, false) when the cap is hit. The entry is automatically
// removed from the registry — callers should publish their fail-closed
// verdict using entry.Payload, then return.
//
// Returns (nil, false) when no entry exists for key (the caller has
// already lost retry context — fail-closed without payload) or when
// ctx is canceled during the backoff sleep (graceful shutdown).
func (s *State) Tick(ctx context.Context, key string) (*Entry, bool) {
	s.mu.Lock()
	entry, ok := s.entries[key]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	entry.Count++
	if entry.Count > s.cfg.MaxRetries {
		delete(s.entries, key)
		s.mu.Unlock()
		return entry, false
	}
	s.mu.Unlock()

	if err := s.sleep(ctx, s.backoff()); err != nil {
		return nil, false
	}
	return entry, true
}

// Clear removes the entry for key. Idempotent — no-op if absent. Call on
// terminal success and on fail-closed paths that didn't go through Tick.
func (s *State) Clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}

// Snapshot returns a defensive copy of the entry for key. Useful for
// observability and tests; production callers usually use Track or Tick.
// Returns (nil, false) when no entry exists.
func (s *State) Snapshot(key string) (*Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	return &Entry{
		Count:        entry.Count,
		Payload:      entry.Payload,
		activeLoopID: entry.activeLoopID,
	}, true
}

// Len returns the number of tracked keys. Test/observability hook.
func (s *State) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// backoff returns a single jittered delay drawn from [BackoffMs, 2*BackoffMs).
func (s *State) backoff() time.Duration {
	if s.cfg.BackoffMs <= 0 {
		return 0
	}
	base := time.Duration(s.cfg.BackoffMs) * time.Millisecond
	jitter := time.Duration(rand.Int64N(int64(base)))
	return base + jitter
}

// sleep waits for d, returning early with ctx.Err() on cancellation.
// d == 0 returns immediately. The sleepFn override allows tests to
// observe and control timing without actually sleeping.
func (s *State) sleep(ctx context.Context, d time.Duration) error {
	if s.sleepFn != nil {
		return s.sleepFn(ctx, d)
	}
	if d == 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
