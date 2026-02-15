package model

import (
	"testing"
	"time"
)

func TestEndpointHealthTracking(t *testing.T) {
	r := NewDefaultRegistry()

	// Initially, all endpoints should be available
	if !r.IsEndpointAvailable("qwen") {
		t.Error("expected qwen to be available initially")
	}

	// No health info should exist yet
	health := r.GetEndpointHealth("qwen")
	if health != nil {
		t.Error("expected no health info before any requests")
	}

	// Record a success
	r.MarkEndpointSuccess("qwen")

	health = r.GetEndpointHealth("qwen")
	if health == nil {
		t.Fatal("expected health info after success")
	}
	if !health.Available {
		t.Error("expected endpoint to be available after success")
	}
	if health.FailureCount != 0 {
		t.Errorf("expected failure count 0, got %d", health.FailureCount)
	}
	if health.LastSuccess.IsZero() {
		t.Error("expected last success to be set")
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	r := NewDefaultRegistry()

	// Configure low threshold for testing
	r.SetHealthConfig(HealthConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  100 * time.Millisecond,
	})

	// First failure - still available
	r.MarkEndpointFailure("qwen")
	if !r.IsEndpointAvailable("qwen") {
		t.Error("expected qwen to be available after 1 failure")
	}

	// Second failure - circuit opens
	r.MarkEndpointFailure("qwen")
	if r.IsEndpointAvailable("qwen") {
		t.Error("expected qwen to be unavailable after circuit opens")
	}

	health := r.GetEndpointHealth("qwen")
	if health == nil {
		t.Fatal("expected health info")
	}
	if !health.CircuitOpen {
		t.Error("expected circuit to be open")
	}
	if health.FailureCount != 2 {
		t.Errorf("expected failure count 2, got %d", health.FailureCount)
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	r := NewDefaultRegistry()

	// Configure short recovery for testing
	r.SetHealthConfig(HealthConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  50 * time.Millisecond,
	})

	// Trip the circuit
	r.MarkEndpointFailure("qwen")
	if r.IsEndpointAvailable("qwen") {
		t.Error("expected qwen to be unavailable immediately after failure")
	}

	// Wait for recovery timeout
	time.Sleep(60 * time.Millisecond)

	// Should be available again (half-open)
	if !r.IsEndpointAvailable("qwen") {
		t.Error("expected qwen to be available after recovery timeout")
	}

	// Success should close the circuit
	r.MarkEndpointSuccess("qwen")
	health := r.GetEndpointHealth("qwen")
	if health == nil {
		t.Fatal("expected health info")
	}
	if health.CircuitOpen {
		t.Error("expected circuit to be closed after success")
	}
	if health.FailureCount != 0 {
		t.Errorf("expected failure count reset to 0, got %d", health.FailureCount)
	}
}

func TestGetAvailableFallbackChain(t *testing.T) {
	r := NewDefaultRegistry()

	// Configure quick circuit breaker
	r.SetHealthConfig(HealthConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  1 * time.Hour, // Long timeout so it stays open
	})

	// Trip qwen's circuit
	r.MarkEndpointFailure("qwen")

	// Get available chain for planning
	chain := r.GetAvailableFallbackChain(CapabilityPlanning)

	// qwen should not be in the chain
	for _, name := range chain {
		if name == "qwen" {
			t.Error("expected qwen to be excluded from available chain")
		}
	}

	// But qwen3 and llama3.2 should be
	hasQwen3 := false
	for _, name := range chain {
		if name == "qwen3" {
			hasQwen3 = true
			break
		}
	}
	if !hasQwen3 {
		t.Error("expected qwen3 to be in available chain")
	}
}

func TestGetAvailableFallbackChainAllUnavailable(t *testing.T) {
	r := NewDefaultRegistry()

	// Configure quick circuit breaker
	r.SetHealthConfig(HealthConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  1 * time.Hour,
	})

	// Trip all endpoints
	for _, name := range r.ListEndpoints() {
		r.MarkEndpointFailure(name)
	}

	// Should still return the full chain (better to try something)
	chain := r.GetAvailableFallbackChain(CapabilityPlanning)
	if len(chain) == 0 {
		t.Error("expected non-empty chain even when all unavailable")
	}
}

func TestResetEndpointHealth(t *testing.T) {
	r := NewDefaultRegistry()

	// Record some activity
	r.MarkEndpointSuccess("qwen")
	r.MarkEndpointFailure("qwen")

	health := r.GetEndpointHealth("qwen")
	if health == nil {
		t.Fatal("expected health info")
	}

	// Reset
	r.ResetEndpointHealth("qwen")

	health = r.GetEndpointHealth("qwen")
	if health != nil {
		t.Error("expected no health info after reset")
	}

	// Should be available again
	if !r.IsEndpointAvailable("qwen") {
		t.Error("expected qwen to be available after reset")
	}
}

func TestDefaultHealthConfig(t *testing.T) {
	cfg := DefaultHealthConfig()

	if cfg.FailureThreshold != 3 {
		t.Errorf("expected failure threshold 3, got %d", cfg.FailureThreshold)
	}
	if cfg.RecoveryTimeout != 30*time.Second {
		t.Errorf("expected recovery timeout 30s, got %v", cfg.RecoveryTimeout)
	}
}
