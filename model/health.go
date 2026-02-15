package model

import (
	"sync"
	"time"
)

// EndpointHealth tracks the health status of a model endpoint.
type EndpointHealth struct {
	// Available indicates if the endpoint is currently usable.
	Available bool `json:"available"`

	// LastSuccess is the time of the last successful request.
	LastSuccess time.Time `json:"last_success,omitempty"`

	// LastFailure is the time of the last failed request.
	LastFailure time.Time `json:"last_failure,omitempty"`

	// FailureCount is the number of consecutive failures.
	FailureCount int `json:"failure_count"`

	// CircuitOpen indicates if the circuit breaker has tripped.
	CircuitOpen bool `json:"circuit_open"`

	// CircuitOpenedAt is when the circuit was opened.
	CircuitOpenedAt time.Time `json:"circuit_opened_at,omitempty"`
}

// HealthConfig configures the health tracking behavior.
type HealthConfig struct {
	// FailureThreshold is the number of failures before opening the circuit.
	FailureThreshold int

	// RecoveryTimeout is how long to wait before trying a failed endpoint again.
	RecoveryTimeout time.Duration

	// HalfOpenRequests is how many test requests to allow when recovering.
	HalfOpenRequests int
}

// DefaultHealthConfig returns sensible defaults for health tracking.
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  30 * time.Second,
		HalfOpenRequests: 1,
	}
}

// healthState stores endpoint health information.
type healthState struct {
	mu       sync.RWMutex
	config   HealthConfig
	statuses map[string]*EndpointHealth
}

// newHealthState creates a new health state tracker.
func newHealthState(cfg HealthConfig) *healthState {
	return &healthState{
		config:   cfg,
		statuses: make(map[string]*EndpointHealth),
	}
}

// getOrCreate returns the health status for an endpoint, creating if needed.
func (h *healthState) getOrCreate(name string) *EndpointHealth {
	h.mu.Lock()
	defer h.mu.Unlock()

	if status, ok := h.statuses[name]; ok {
		return status
	}

	status := &EndpointHealth{Available: true}
	h.statuses[name] = status
	return status
}

// MarkEndpointSuccess records a successful request to an endpoint.
func (r *Registry) MarkEndpointSuccess(name string) {
	r.mu.Lock()
	if r.health == nil {
		r.health = newHealthState(DefaultHealthConfig())
	}
	r.mu.Unlock()

	status := r.health.getOrCreate(name)

	r.health.mu.Lock()
	defer r.health.mu.Unlock()

	status.LastSuccess = time.Now()
	status.FailureCount = 0
	status.Available = true
	status.CircuitOpen = false
}

// MarkEndpointFailure records a failed request to an endpoint.
func (r *Registry) MarkEndpointFailure(name string) {
	r.mu.Lock()
	if r.health == nil {
		r.health = newHealthState(DefaultHealthConfig())
	}
	r.mu.Unlock()

	status := r.health.getOrCreate(name)

	r.health.mu.Lock()
	defer r.health.mu.Unlock()

	status.LastFailure = time.Now()
	status.FailureCount++

	// Check if we should open the circuit
	if status.FailureCount >= r.health.config.FailureThreshold {
		status.CircuitOpen = true
		status.CircuitOpenedAt = time.Now()
		status.Available = false
	}
}

// IsEndpointAvailable checks if an endpoint is available for requests.
// Returns false if the circuit breaker is open and recovery timeout hasn't passed.
func (r *Registry) IsEndpointAvailable(name string) bool {
	r.mu.RLock()
	if r.health == nil {
		r.mu.RUnlock()
		return true // No health tracking = always available
	}
	r.mu.RUnlock()

	r.health.mu.RLock()
	status, ok := r.health.statuses[name]
	if !ok {
		r.health.mu.RUnlock()
		return true // Unknown endpoint = available
	}

	// Copy values to avoid holding lock during time comparison
	circuitOpen := status.CircuitOpen
	circuitOpenedAt := status.CircuitOpenedAt
	r.health.mu.RUnlock()

	if !circuitOpen {
		return true
	}

	// Check if recovery timeout has passed (half-open state)
	r.mu.RLock()
	recoveryTimeout := r.health.config.RecoveryTimeout
	r.mu.RUnlock()

	if time.Since(circuitOpenedAt) > recoveryTimeout {
		return true // Allow a test request (half-open)
	}

	return false
}

// GetEndpointHealth returns the health status for an endpoint.
// Returns nil if no health information is available.
func (r *Registry) GetEndpointHealth(name string) *EndpointHealth {
	r.mu.RLock()
	if r.health == nil {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()

	r.health.mu.RLock()
	defer r.health.mu.RUnlock()

	if status, ok := r.health.statuses[name]; ok {
		// Return a copy to avoid races
		return &EndpointHealth{
			Available:       status.Available,
			LastSuccess:     status.LastSuccess,
			LastFailure:     status.LastFailure,
			FailureCount:    status.FailureCount,
			CircuitOpen:     status.CircuitOpen,
			CircuitOpenedAt: status.CircuitOpenedAt,
		}
	}
	return nil
}

// GetAvailableFallbackChain returns the fallback chain filtered to only available endpoints.
// This allows the LLM client to skip unavailable endpoints during fallback iteration.
func (r *Registry) GetAvailableFallbackChain(cap Capability) []string {
	chain := r.GetFallbackChain(cap)
	available := make([]string, 0, len(chain))

	for _, name := range chain {
		if r.IsEndpointAvailable(name) {
			available = append(available, name)
		}
	}

	// If all endpoints are unavailable, return the full chain
	// (better to try something than nothing)
	if len(available) == 0 {
		return chain
	}

	return available
}

// SetHealthConfig updates the health tracking configuration.
func (r *Registry) SetHealthConfig(cfg HealthConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.health == nil {
		r.health = newHealthState(cfg)
	} else {
		r.health.config = cfg
	}
}

// ResetEndpointHealth clears the health status for an endpoint.
func (r *Registry) ResetEndpointHealth(name string) {
	r.mu.RLock()
	if r.health == nil {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	r.health.mu.Lock()
	defer r.health.mu.Unlock()

	delete(r.health.statuses, name)
}
