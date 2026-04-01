package llm_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeRegistry is a test helper that creates a registry with the given endpoint configs.
func makeRegistry(endpoints map[string]*model.EndpointConfig, globalLimit int) *model.Registry {
	caps := map[model.Capability]*model.CapabilityConfig{
		model.CapabilityFast: {
			Preferred: endpointNames(endpoints),
		},
	}
	r := model.NewRegistry(caps, endpoints)
	if globalLimit > 0 {
		defaults := r.GetDefaults()
		if defaults == nil {
			r.SetDefault("default")
			defaults = r.GetDefaults()
		}
		defaults.MaxConcurrentGlobal = globalLimit
	}
	return r
}

// endpointNames returns sorted keys of the endpoints map.
func endpointNames(endpoints map[string]*model.EndpointConfig) []string {
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		names = append(names, name)
	}
	return names
}

func TestGovernor_NilPassthrough(t *testing.T) {
	// A nil governor must not panic and must return a no-op release.
	var g *llm.ConcurrencyGovernor

	release, err := g.Acquire(context.Background(), "any-endpoint")
	require.NoError(t, err)
	require.NotNil(t, release)

	// Calling release multiple times must not panic.
	release()
	release()
}

func TestGovernor_ConcurrencyLimit(t *testing.T) {
	const maxConcurrent = 2
	const goroutines = 5

	registry := makeRegistry(map[string]*model.EndpointConfig{
		"ep": {
			Provider:      "ollama",
			URL:           "http://localhost:11434",
			Model:         "test",
			MaxConcurrent: maxConcurrent,
		},
	}, 0)

	g := llm.NewConcurrencyGovernor(registry, nil)
	require.NotNil(t, g)

	var (
		current atomic.Int32
		peak    atomic.Int32
		wg      sync.WaitGroup
	)

	ctx := context.Background()

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			release, err := g.Acquire(ctx, "ep")
			require.NoError(t, err)
			defer release()

			// Track concurrency.
			c := current.Add(1)
			for {
				p := peak.Load()
				if c <= p || peak.CompareAndSwap(p, c) {
					break
				}
			}

			// Simulate work so other goroutines actually wait.
			time.Sleep(20 * time.Millisecond)
			current.Add(-1)
		}()
	}

	wg.Wait()

	assert.LessOrEqual(t, peak.Load(), int32(maxConcurrent),
		"peak concurrent (%d) exceeded max_concurrent (%d)", peak.Load(), maxConcurrent)
}

func TestGovernor_RateLimit(t *testing.T) {
	// Use 1 request/minute so the initial bucket starts with 1 token.
	// After the first immediate Acquire, the bucket is empty and the second
	// must wait ~1 second (1/60 of a minute) for a token to refill.
	// We verify the inter-call spacing, not absolute wall time.
	const rpm = 1 // 1 per minute = 1 per 60 seconds; but refillRate test is too slow.
	// Instead: 120 RPM = 2 per second. Drain the 120-token bucket first, then
	// measure the spacing between two back-to-back calls.
	// We use RPM=2 (1 per 30s) which is too slow for a unit test.
	//
	// Practical approach: use 120 RPM (2/s), drain all 120 tokens immediately,
	// then verify the next call takes ~500ms.
	registry := makeRegistry(map[string]*model.EndpointConfig{
		"ep": {
			Provider:          "ollama",
			URL:               "http://localhost:11434",
			Model:             "test",
			RequestsPerMinute: 120, // 2 per second
		},
	}, 0)

	g := llm.NewConcurrencyGovernor(registry, nil)
	require.NotNil(t, g)

	ctx := context.Background()

	// Drain all 120 tokens — these should all return immediately.
	for range 120 {
		release, err := g.Acquire(ctx, "ep")
		require.NoError(t, err)
		release()
	}

	// Now the bucket is empty. The next call must wait for a refill (~500ms at 2/s).
	start := time.Now()
	release, err := g.Acquire(ctx, "ep")
	require.NoError(t, err)
	release()
	waited := time.Since(start)

	// At 120 RPM = 2/s, one token takes 500ms to refill.
	// Allow generous tolerance: at least 400ms, under 3s.
	assert.GreaterOrEqual(t, waited, 400*time.Millisecond,
		"call after empty bucket should wait for refill (got %v)", waited)
	assert.Less(t, waited, 3*time.Second,
		"rate limit wait was unreasonably long (got %v)", waited)
}

func TestGovernor_ContextCancellation(t *testing.T) {
	// Fill the semaphore so the next Acquire must block.
	const maxConcurrent = 1
	registry := makeRegistry(map[string]*model.EndpointConfig{
		"ep": {
			Provider:      "ollama",
			URL:           "http://localhost:11434",
			Model:         "test",
			MaxConcurrent: maxConcurrent,
		},
	}, 0)

	g := llm.NewConcurrencyGovernor(registry, nil)
	require.NotNil(t, g)

	ctx := context.Background()

	// Acquire the only slot and hold it.
	holderRelease, err := g.Acquire(ctx, "ep")
	require.NoError(t, err)
	defer holderRelease()

	// Cancel context before the second caller can get in.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // cancel immediately

	_, err = g.Acquire(cancelCtx, "ep")
	assert.ErrorIs(t, err, context.Canceled,
		"Acquire should return context.Canceled when ctx is cancelled")
}

func TestGovernor_GlobalLimit(t *testing.T) {
	const globalLimit = 3
	const perEndpointLimit = 5

	// Two endpoints each allowing 5 concurrent, but global capped at 3.
	registry := makeRegistry(map[string]*model.EndpointConfig{
		"ep1": {
			Provider:      "ollama",
			URL:           "http://localhost:11434",
			Model:         "test1",
			MaxConcurrent: perEndpointLimit,
		},
		"ep2": {
			Provider:      "ollama",
			URL:           "http://localhost:11434",
			Model:         "test2",
			MaxConcurrent: perEndpointLimit,
		},
	}, globalLimit)

	g := llm.NewConcurrencyGovernor(registry, nil)
	require.NotNil(t, g)

	var (
		current atomic.Int32
		peak    atomic.Int32
		wg      sync.WaitGroup
	)

	ctx := context.Background()

	// Launch goroutines alternating between the two endpoints.
	endpoints := []string{"ep1", "ep2"}
	for i := range 10 {
		ep := endpoints[i%2]
		wg.Add(1)
		go func(endpoint string) {
			defer wg.Done()

			release, err := g.Acquire(ctx, endpoint)
			require.NoError(t, err)
			defer release()

			c := current.Add(1)
			for {
				p := peak.Load()
				if c <= p || peak.CompareAndSwap(p, c) {
					break
				}
			}

			time.Sleep(30 * time.Millisecond)
			current.Add(-1)
		}(ep)
	}

	wg.Wait()

	assert.LessOrEqual(t, peak.Load(), int32(globalLimit),
		"peak concurrent (%d) exceeded global limit (%d)", peak.Load(), globalLimit)
}

func TestGovernor_NoLimitsEndpoint(t *testing.T) {
	// An endpoint with neither rate limit nor concurrency limit should pass through immediately.
	registry := makeRegistry(map[string]*model.EndpointConfig{
		"unlimited": {
			Provider: "ollama",
			URL:      "http://localhost:11434",
			Model:    "test",
			// MaxConcurrent and RequestsPerMinute both default to 0.
		},
	}, 0)

	g := llm.NewConcurrencyGovernor(registry, nil)
	require.NotNil(t, g)

	ctx := context.Background()

	// All 10 acquires should return immediately with no blocking.
	start := time.Now()
	releases := make([]func(), 10)
	for i := range 10 {
		release, err := g.Acquire(ctx, "unlimited")
		require.NoError(t, err)
		releases[i] = release
	}
	elapsed := time.Since(start)

	for _, r := range releases {
		r()
	}

	assert.Less(t, elapsed, 50*time.Millisecond,
		"unlimited endpoint should pass through instantly (got %v)", elapsed)
}

func TestGovernor_ReleaseIdempotent(t *testing.T) {
	// Calling release multiple times must not panic or double-return slots.
	registry := makeRegistry(map[string]*model.EndpointConfig{
		"ep": {
			Provider:      "ollama",
			URL:           "http://localhost:11434",
			Model:         "test",
			MaxConcurrent: 1,
		},
	}, 0)

	g := llm.NewConcurrencyGovernor(registry, nil)
	require.NotNil(t, g)

	ctx := context.Background()

	release, err := g.Acquire(ctx, "ep")
	require.NoError(t, err)

	// Call release three times — none should panic or overflow the semaphore.
	release()
	release()
	release()

	// Should be able to acquire immediately after (slot properly returned once).
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	release2, err := g.Acquire(timeoutCtx, "ep")
	require.NoError(t, err, "slot should be available after idempotent release")
	release2()
}
