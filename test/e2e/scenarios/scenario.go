// Package scenarios defines the interface for e2e test scenarios.
package scenarios

import (
	"context"
	"sync"
	"time"
)

// Scenario defines the interface for e2e test scenarios.
// Each scenario tests a specific workflow or feature end-to-end.
type Scenario interface {
	// Name returns the scenario name for identification and reporting.
	Name() string

	// Description provides a human-readable description of what the scenario tests.
	Description() string

	// Setup prepares the scenario environment before execution.
	// This may include creating test data, configuring components, etc.
	Setup(ctx context.Context) error

	// Execute runs the actual test scenario.
	// Returns detailed results including pass/fail status and diagnostics.
	Execute(ctx context.Context) (*Result, error)

	// Teardown cleans up after the scenario execution.
	// This should restore the system to its original state.
	Teardown(ctx context.Context) error
}

// Result contains the outcome of a scenario execution.
// All methods are thread-safe for concurrent access.
type Result struct {
	mu sync.Mutex `json:"-"`

	ScenarioName string        `json:"scenario_name"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`

	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	// Metrics contains timing and count metrics from the scenario.
	Metrics map[string]any `json:"metrics,omitempty"`

	// Details contains scenario-specific output data.
	Details map[string]any `json:"details,omitempty"`

	// Errors contains all errors encountered during execution.
	Errors []string `json:"errors,omitempty"`

	// Warnings contains non-fatal issues encountered.
	Warnings []string `json:"warnings,omitempty"`

	// Stages tracks completion of each stage in the scenario.
	Stages []StageResult `json:"stages,omitempty"`
}

// StageResult represents the outcome of a single stage in a scenario.
type StageResult struct {
	Name     string         `json:"name"`
	Success  bool           `json:"success"`
	Duration time.Duration  `json:"duration"`
	Error    string         `json:"error,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

// NewResult creates a new Result initialized for the given scenario.
func NewResult(scenarioName string) *Result {
	return &Result{
		ScenarioName: scenarioName,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
		Stages:       []StageResult{},
	}
}

// Complete marks the result as complete, setting end time and duration.
func (r *Result) Complete() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.EndTime = time.Now()
	r.Duration = r.EndTime.Sub(r.StartTime)
}

// AddError adds an error to the result.
func (r *Result) AddError(err string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Errors = append(r.Errors, err)
}

// AddWarning adds a warning to the result.
func (r *Result) AddWarning(warning string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Warnings = append(r.Warnings, warning)
}

// AddStage adds a completed stage to the result.
func (r *Result) AddStage(name string, success bool, duration time.Duration, err string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Stages = append(r.Stages, StageResult{
		Name:     name,
		Success:  success,
		Duration: duration,
		Error:    err,
	})
}

// SetMetric sets a metric value.
func (r *Result) SetMetric(key string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Metrics[key] = value
}

// SetDetail sets a detail value.
func (r *Result) SetDetail(key string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Details[key] = value
}

// GetDetail retrieves a detail value safely.
func (r *Result) GetDetail(key string) (any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	val, ok := r.Details[key]
	return val, ok
}

// GetDetailString retrieves a string detail value safely.
func (r *Result) GetDetailString(key string) (string, bool) {
	val, ok := r.GetDetail(key)
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetDetailBool retrieves a bool detail value safely.
func (r *Result) GetDetailBool(key string) (bool, bool) {
	val, ok := r.GetDetail(key)
	if !ok {
		return false, false
	}
	b, ok := val.(bool)
	return b, ok
}
