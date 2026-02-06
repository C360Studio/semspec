package validation

import (
	"fmt"
	"sync"
	"time"
)

// RetryConfig holds retry configuration.
type RetryConfig struct {
	MaxAttempts       int           `json:"max_attempts"`
	BackoffBase       time.Duration `json:"backoff_base"`
	BackoffMultiplier float64       `json:"backoff_multiplier"`
}

// DefaultRetryConfig returns sensible retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       5 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// RetryState tracks retry attempts for a workflow step.
type RetryState struct {
	WorkflowSlug    string            `json:"workflow_slug"`
	WorkflowStep    string            `json:"workflow_step"`
	Attempts        int               `json:"attempts"`
	CreatedAt       time.Time         `json:"created_at"`
	LastAttempt     time.Time         `json:"last_attempt"`
	LastError       string            `json:"last_error,omitempty"`
	ValidationError *ValidationResult `json:"validation_error,omitempty"`
}

// DeepCopy returns a deep copy of the RetryState.
func (s *RetryState) DeepCopy() *RetryState {
	if s == nil {
		return nil
	}
	stateCopy := *s
	if s.ValidationError != nil {
		valCopy := *s.ValidationError
		// Deep copy slices
		if s.ValidationError.MissingSections != nil {
			valCopy.MissingSections = make([]string, len(s.ValidationError.MissingSections))
			copy(valCopy.MissingSections, s.ValidationError.MissingSections)
		}
		if s.ValidationError.Warnings != nil {
			valCopy.Warnings = make([]string, len(s.ValidationError.Warnings))
			copy(valCopy.Warnings, s.ValidationError.Warnings)
		}
		if s.ValidationError.SectionDetails != nil {
			valCopy.SectionDetails = make(map[string]string, len(s.ValidationError.SectionDetails))
			for k, v := range s.ValidationError.SectionDetails {
				valCopy.SectionDetails[k] = v
			}
		}
		stateCopy.ValidationError = &valCopy
	}
	return &stateCopy
}

// RetryManager tracks and manages retry attempts.
type RetryManager struct {
	config RetryConfig
	states map[string]*RetryState // key: slug:step
	mu     sync.RWMutex
}

// NewRetryManager creates a new retry manager.
func NewRetryManager(config RetryConfig) *RetryManager {
	return &RetryManager{
		config: config,
		states: make(map[string]*RetryState),
	}
}

// stateKey generates a unique key for a workflow step.
func stateKey(slug, step string) string {
	return fmt.Sprintf("%s:%s", slug, step)
}

// RecordAttempt records an attempt for a workflow step.
// Returns the current attempt number.
func (m *RetryManager) RecordAttempt(slug, step string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := stateKey(slug, step)
	state, exists := m.states[key]
	if !exists {
		now := time.Now()
		state = &RetryState{
			WorkflowSlug: slug,
			WorkflowStep: step,
			CreatedAt:    now,
		}
		m.states[key] = state
	}

	state.Attempts++
	state.LastAttempt = time.Now()
	return state.Attempts
}

// RecordFailure records a failed attempt with the error details.
func (m *RetryManager) RecordFailure(slug, step, errorMsg string, validation *ValidationResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := stateKey(slug, step)
	state, exists := m.states[key]
	if !exists {
		now := time.Now()
		state = &RetryState{
			WorkflowSlug: slug,
			WorkflowStep: step,
			CreatedAt:    now,
		}
		m.states[key] = state
	}

	state.LastError = errorMsg
	state.ValidationError = validation
}

// CanRetry checks if a retry is allowed.
func (m *RetryManager) CanRetry(slug, step string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := stateKey(slug, step)
	state, exists := m.states[key]
	if !exists {
		return true
	}

	return state.Attempts < m.config.MaxAttempts
}

// GetAttemptCount returns the current attempt count.
func (m *RetryManager) GetAttemptCount(slug, step string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := stateKey(slug, step)
	if state, exists := m.states[key]; exists {
		return state.Attempts
	}
	return 0
}

// GetState returns a deep copy of the retry state for a workflow step.
func (m *RetryManager) GetState(slug, step string) *RetryState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := stateKey(slug, step)
	if state, exists := m.states[key]; exists {
		return state.DeepCopy()
	}
	return nil
}

// GetBackoffDuration returns the backoff duration for the next retry.
func (m *RetryManager) GetBackoffDuration(slug, step string) time.Duration {
	attempts := m.GetAttemptCount(slug, step)
	if attempts == 0 {
		return 0
	}

	// Exponential backoff: base * multiplier^(attempts-1)
	multiplier := 1.0
	for i := 1; i < attempts; i++ {
		multiplier *= m.config.BackoffMultiplier
	}

	return time.Duration(float64(m.config.BackoffBase) * multiplier)
}

// ClearState clears the retry state for a workflow step (on success).
func (m *RetryManager) ClearState(slug, step string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, stateKey(slug, step))
}

// ClearWorkflow clears all retry states for a workflow.
func (m *RetryManager) ClearWorkflow(slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key := range m.states {
		if len(key) > len(slug)+1 && key[:len(slug)+1] == slug+":" {
			delete(m.states, key)
		}
	}
}

// GetMaxAttempts returns the maximum number of attempts allowed.
func (m *RetryManager) GetMaxAttempts() int {
	return m.config.MaxAttempts
}

// StateCount returns the number of tracked retry states.
func (m *RetryManager) StateCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.states)
}

// PruneOld removes retry states older than maxAge.
// Returns the number of states pruned.
func (m *RetryManager) PruneOld(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	threshold := time.Now().Add(-maxAge)
	pruned := 0
	for key, state := range m.states {
		if state.CreatedAt.Before(threshold) {
			delete(m.states, key)
			pruned++
		}
	}
	return pruned
}

// RetryDecision represents the decision on whether to retry.
type RetryDecision struct {
	ShouldRetry    bool
	AttemptNumber  int
	MaxAttempts    int
	BackoffSeconds float64
	Feedback       string
	IsFinalFailure bool
}

// ShouldRetry evaluates whether a retry should be attempted.
func (m *RetryManager) ShouldRetry(slug, step string, validation *ValidationResult) *RetryDecision {
	attempts := m.GetAttemptCount(slug, step)
	maxAttempts := m.config.MaxAttempts

	decision := &RetryDecision{
		AttemptNumber: attempts,
		MaxAttempts:   maxAttempts,
	}

	if validation.Valid {
		// No retry needed - success
		decision.ShouldRetry = false
		return decision
	}

	// Record the failure
	m.RecordFailure(slug, step, "Validation failed", validation)

	if attempts >= maxAttempts {
		// Max retries exceeded
		decision.ShouldRetry = false
		decision.IsFinalFailure = true
		decision.Feedback = fmt.Sprintf(
			"Maximum retry attempts (%d) exceeded for %s/%s. Manual intervention required.",
			maxAttempts, slug, step)
		return decision
	}

	// Calculate backoff
	backoff := m.GetBackoffDuration(slug, step)
	decision.ShouldRetry = true
	decision.BackoffSeconds = backoff.Seconds()
	decision.Feedback = validation.FormatFeedback()

	return decision
}
