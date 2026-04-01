package llm

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semspec/model"
)

// ConcurrencyGovernor controls LLM request concurrency and rate per endpoint.
// It combines per-endpoint token-bucket rate limiting with concurrency semaphores,
// plus an optional global concurrency cap across all endpoints.
//
// A nil *ConcurrencyGovernor is valid and acts as a passthrough — all Acquire
// calls return immediately with a no-op release function.
type ConcurrencyGovernor struct {
	endpoints map[string]*endpointLimiter
	global    chan struct{} // nil if no global limit; buffered channel used as semaphore
	logger    *slog.Logger
}

// endpointLimiter mirrors the semstreams EndpointThrottle pattern:
// a token bucket for rate limiting plus a channel-based semaphore for concurrency.
type endpointLimiter struct {
	name string

	// semaphore caps concurrent in-flight requests (nil if MaxConcurrent == 0).
	semaphore chan struct{}

	// Token bucket fields (rate == 0 means rate limiting disabled).
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per nanosecond
	lastRefill time.Time
}

// NewConcurrencyGovernor creates a governor from the model registry.
// Endpoints without any limits (MaxConcurrent == 0 and RequestsPerMinute == 0)
// are not tracked — Acquire is a no-op for them.
// Returns nil if the registry is nil.
func NewConcurrencyGovernor(registry *model.Registry, logger *slog.Logger) *ConcurrencyGovernor {
	if registry == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	g := &ConcurrencyGovernor{
		endpoints: make(map[string]*endpointLimiter),
		logger:    logger,
	}

	for _, name := range registry.ListEndpoints() {
		ep := registry.GetEndpoint(name)
		if ep == nil {
			continue
		}
		if ep.MaxConcurrent == 0 && ep.RequestsPerMinute == 0 {
			continue // passthrough — no limiter needed
		}
		g.endpoints[name] = newEndpointLimiter(name, ep.RequestsPerMinute, ep.MaxConcurrent)
	}

	if defaults := registry.GetDefaults(); defaults != nil && defaults.MaxConcurrentGlobal > 0 {
		g.global = make(chan struct{}, defaults.MaxConcurrentGlobal)
		for range defaults.MaxConcurrentGlobal {
			g.global <- struct{}{}
		}
	}

	return g
}

// newEndpointLimiter creates a limiter for the given per-endpoint limits.
func newEndpointLimiter(name string, requestsPerMinute, maxConcurrent int) *endpointLimiter {
	l := &endpointLimiter{
		name:       name,
		lastRefill: time.Now(),
	}

	if requestsPerMinute > 0 {
		l.maxTokens = float64(requestsPerMinute)
		l.tokens = float64(requestsPerMinute)
		// Convert requests/minute → tokens/nanosecond for time.Duration arithmetic.
		l.refillRate = float64(requestsPerMinute) / float64(time.Minute)
	}

	if maxConcurrent > 0 {
		l.semaphore = make(chan struct{}, maxConcurrent)
		for range maxConcurrent {
			l.semaphore <- struct{}{}
		}
	}

	return l
}

// Acquire blocks until a request slot is available for the named endpoint,
// or the context is cancelled. Returns a release function that must be called
// when the request completes. The release function is safe to call multiple times.
//
// If the governor is nil or the endpoint has no configured limits, Acquire
// returns immediately with a no-op release and nil error.
func (g *ConcurrencyGovernor) Acquire(ctx context.Context, endpointName string) (release func(), err error) {
	noop := func() {}

	if g == nil {
		return noop, nil
	}

	limiter, ok := g.endpoints[endpointName]
	if !ok {
		return noop, nil // endpoint has no limits configured
	}

	start := time.Now()

	// Rate-limit: wait until a token is available.
	if limiter.refillRate > 0 {
		logged := false
		for {
			limiter.mu.Lock()
			limiter.refill()
			if limiter.tokens >= 1.0 {
				limiter.tokens--
				limiter.mu.Unlock()
				break
			}
			// Calculate how long until one token refills.
			need := (1.0 - limiter.tokens) / limiter.refillRate
			waitNs := time.Duration(need)
			limiter.mu.Unlock()

			if !logged {
				g.logger.Info("llm.governor: queued", "endpoint", endpointName)
				logged = true
			}

			select {
			case <-ctx.Done():
				return noop, ctx.Err()
			case <-time.After(waitNs):
				// Loop and recheck — another goroutine may have consumed the token.
			}
		}
	}

	// Concurrency limit: acquire endpoint semaphore slot.
	if limiter.semaphore != nil {
		select {
		case <-ctx.Done():
			return noop, ctx.Err()
		case <-limiter.semaphore:
			// Slot acquired.
		}
	}

	// Global concurrency limit: acquire global semaphore slot.
	if g.global != nil {
		select {
		case <-ctx.Done():
			// Return the endpoint slot we already acquired before failing.
			if limiter.semaphore != nil {
				limiter.semaphore <- struct{}{}
			}
			return noop, ctx.Err()
		case <-g.global:
			// Global slot acquired.
		}
	}

	waitMs := time.Since(start).Milliseconds()
	g.logger.Debug("llm.governor: admitted", "endpoint", endpointName, "wait_ms", waitMs)

	var once sync.Once
	release = func() {
		once.Do(func() {
			if limiter.semaphore != nil {
				limiter.semaphore <- struct{}{}
			}
			if g.global != nil {
				g.global <- struct{}{}
			}
		})
	}

	return release, nil
}

// refill adds tokens based on elapsed time since the last refill.
// Must be called with l.mu held.
func (l *endpointLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill)
	if elapsed <= 0 {
		return
	}
	l.tokens += float64(elapsed) * l.refillRate
	if l.tokens > l.maxTokens {
		l.tokens = l.maxTokens
	}
	l.lastRefill = now
}
