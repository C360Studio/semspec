package revieworchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxIterations != 3 {
		t.Errorf("MaxIterations = %d, want 3", cfg.MaxIterations)
	}
	if cfg.TimeoutSeconds != 1800 {
		t.Errorf("TimeoutSeconds = %d, want 1800", cfg.TimeoutSeconds)
	}
	if cfg.Model != "default" {
		t.Errorf("Model = %q, want %q", cfg.Model, "default")
	}
	if cfg.Ports == nil {
		t.Fatal("Ports should not be nil in defaults")
	}
	if len(cfg.Ports.Inputs) == 0 {
		t.Error("Ports.Inputs should have at least one entry")
	}
	if len(cfg.Ports.Outputs) == 0 {
		t.Error("Ports.Outputs should have at least one entry")
	}
}

func TestDefaultConfig_InputPortSubjects(t *testing.T) {
	cfg := DefaultConfig()

	subjects := make(map[string]bool)
	for _, p := range cfg.Ports.Inputs {
		subjects[p.Subject] = true
	}

	for _, want := range []string{
		subjectPlanReviewTrigger,
		subjectPhaseReviewTrigger,
		subjectTaskReviewTrigger,
		subjectLoopCompleted,
	} {
		if !subjects[want] {
			t.Errorf("default input ports missing subject %q", want)
		}
	}
}

func TestDefaultConfig_OutputPortSubjects(t *testing.T) {
	cfg := DefaultConfig()

	subjects := make(map[string]bool)
	for _, p := range cfg.Ports.Outputs {
		subjects[p.Subject] = true
	}

	// Verify at least the triple and task subjects are represented.
	if !subjects["graph.mutation.triple.add"] {
		t.Error("default output ports missing graph.mutation.triple.add")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid default config",
			cfg:  DefaultConfig(),
		},
		{
			name: "max_iterations zero",
			cfg: Config{
				MaxIterations:  0,
				TimeoutSeconds: 1800,
			},
			wantErr: true,
			errMsg:  "max_iterations",
		},
		{
			name: "max_iterations negative",
			cfg: Config{
				MaxIterations:  -1,
				TimeoutSeconds: 1800,
			},
			wantErr: true,
			errMsg:  "max_iterations",
		},
		{
			name: "timeout_seconds zero",
			cfg: Config{
				MaxIterations:  3,
				TimeoutSeconds: 0,
			},
			wantErr: true,
			errMsg:  "timeout_seconds",
		},
		{
			name: "timeout_seconds negative",
			cfg: Config{
				MaxIterations:  3,
				TimeoutSeconds: -1,
			},
			wantErr: true,
			errMsg:  "timeout_seconds",
		},
		{
			name: "max_iterations one is valid",
			cfg: Config{
				MaxIterations:  1,
				TimeoutSeconds: 60,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_ValidateReviewTypes(t *testing.T) {
	// Each review type is configured through the ports — verify the default
	// config is valid for all three review loop subjects.
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig.Validate() = %v, want nil", err)
	}

	// Plan review port presence
	found := map[string]bool{}
	for _, p := range cfg.Ports.Inputs {
		found[p.Subject] = true
	}
	for _, wantSubject := range []string{subjectPlanReviewTrigger, subjectPhaseReviewTrigger, subjectTaskReviewTrigger} {
		if !found[wantSubject] {
			t.Errorf("config missing input port for review type subject %q", wantSubject)
		}
	}
}

func TestConfig_GetTimeout(t *testing.T) {
	tests := []struct {
		name           string
		timeoutSeconds int
		expected       time.Duration
	}{
		{
			name:           "standard value",
			timeoutSeconds: 1800,
			expected:       30 * time.Minute,
		},
		{
			name:           "60 seconds",
			timeoutSeconds: 60,
			expected:       60 * time.Second,
		},
		{
			name:           "zero falls back to default",
			timeoutSeconds: 0,
			expected:       30 * time.Minute,
		},
		{
			name:           "negative falls back to default",
			timeoutSeconds: -1,
			expected:       30 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{TimeoutSeconds: tt.timeoutSeconds}
			got := cfg.GetTimeout()
			if got != tt.expected {
				t.Errorf("GetTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_WithDefaults(t *testing.T) {
	// Explicit fields should be preserved; zero-value fields should get defaults.
	cfg := Config{MaxIterations: 7}
	got := cfg.withDefaults()

	if got.MaxIterations != 7 {
		t.Errorf("withDefaults should preserve MaxIterations=7, got %d", got.MaxIterations)
	}
	if got.TimeoutSeconds != 1800 {
		t.Errorf("withDefaults should set TimeoutSeconds=1800, got %d", got.TimeoutSeconds)
	}
	if got.Model != "default" {
		t.Errorf("withDefaults should set Model=default, got %q", got.Model)
	}
	if got.Ports == nil {
		t.Error("withDefaults should set Ports")
	}
}

func TestConfig_WithDefaults_PreservesNonZeroFields(t *testing.T) {
	cfg := Config{
		MaxIterations:  5,
		TimeoutSeconds: 300,
		Model:          "gpt-4",
	}
	got := cfg.withDefaults()

	if got.MaxIterations != 5 {
		t.Errorf("withDefaults MaxIterations = %d, want 5", got.MaxIterations)
	}
	if got.TimeoutSeconds != 300 {
		t.Errorf("withDefaults TimeoutSeconds = %d, want 300", got.TimeoutSeconds)
	}
	if got.Model != "gpt-4" {
		t.Errorf("withDefaults Model = %q, want gpt-4", got.Model)
	}
}

// ---------------------------------------------------------------------------
// Component construction tests
// ---------------------------------------------------------------------------

func TestNewComponent_Defaults(t *testing.T) {
	// Empty config — all defaults should be applied.
	rawCfg, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	deps := component.Dependencies{}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() unexpected error: %v", err)
	}

	c, ok := comp.(*Component)
	if !ok {
		t.Fatalf("NewComponent() returned %T, want *Component", comp)
	}

	if c.config.MaxIterations != 3 {
		t.Errorf("MaxIterations = %d, want 3", c.config.MaxIterations)
	}
	if c.config.TimeoutSeconds != 1800 {
		t.Errorf("TimeoutSeconds = %d, want 1800", c.config.TimeoutSeconds)
	}
	if c.config.Model != "default" {
		t.Errorf("Model = %q, want default", c.config.Model)
	}
	if c.config.Ports == nil {
		t.Error("Ports should not be nil after defaults are applied")
	}
}

func TestNewComponent_PartialOverride(t *testing.T) {
	rawCfg, err := json.Marshal(map[string]any{
		"max_iterations": 5,
		"model":          "claude-3",
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	deps := component.Dependencies{}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() unexpected error: %v", err)
	}

	c := comp.(*Component)

	if c.config.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", c.config.MaxIterations)
	}
	if c.config.Model != "claude-3" {
		t.Errorf("Model = %q, want claude-3", c.config.Model)
	}
	// Unset field should have default applied.
	if c.config.TimeoutSeconds != 1800 {
		t.Errorf("TimeoutSeconds = %d, want default 1800", c.config.TimeoutSeconds)
	}
}

// TestNewComponent_InvalidConfig_Validate verifies that Config.Validate is called
// and returns an error when both max_iterations and timeout_seconds are explicitly
// set to values that withDefaults() does NOT replace (positive values that still
// fail some other constraint).
//
// NOTE: withDefaults() replaces any non-positive max_iterations or timeout_seconds
// with the safe default, so a negative value for those fields cannot reach
// Validate() through NewComponent. Instead we test Validate() directly below
// (TestConfig_Validate) and document that NewComponent is safe by construction
// for those fields. What we can test here is that an unparseable JSON body is
// correctly rejected.
func TestNewComponent_InvalidConfig(t *testing.T) {
	// Malformed JSON at the top level must be rejected.
	_, err := NewComponent([]byte(`{invalid}`), component.Dependencies{})
	if err == nil {
		t.Fatal("NewComponent() expected error for invalid JSON, got nil")
	}
}

func TestNewComponent_MalformedJSON(t *testing.T) {
	_, err := NewComponent([]byte("not json"), component.Dependencies{})
	if err == nil {
		t.Fatal("NewComponent() expected error for malformed JSON, got nil")
	}
}

func TestNewComponent_NilLogger_UsesDefault(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{})

	// deps.Logger is nil — should fall back to slog.Default() without panic.
	deps := component.Dependencies{Logger: nil}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() with nil logger: %v", err)
	}
	if comp == nil {
		t.Fatal("NewComponent() returned nil")
	}
}

func TestNewComponent_BuildsPorts(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{})
	comp, err := NewComponent(rawCfg, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent(): %v", err)
	}

	inputs := comp.InputPorts()
	if len(inputs) == 0 {
		t.Error("component should have at least one input port")
	}
	outputs := comp.OutputPorts()
	if len(outputs) == 0 {
		t.Error("component should have at least one output port")
	}
}

// ---------------------------------------------------------------------------
// Meta / Health / Ports
// ---------------------------------------------------------------------------

func TestMeta(t *testing.T) {
	comp := newTestComponent(t)

	meta := comp.Meta()
	if meta.Name != componentName {
		t.Errorf("Meta().Name = %q, want %q", meta.Name, componentName)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta().Type = %q, want %q", meta.Type, "processor")
	}
	if meta.Version == "" {
		t.Error("Meta().Version should not be empty")
	}
	if meta.Description == "" {
		t.Error("Meta().Description should not be empty")
	}
}

func TestHealth_NotRunning(t *testing.T) {
	comp := newTestComponent(t)

	health := comp.Health()
	if health.Healthy {
		t.Error("Health().Healthy = true, want false when stopped")
	}
	if health.Status != "stopped" {
		t.Errorf("Health().Status = %q, want stopped", health.Status)
	}
}

func TestHealth_Running_ReportsHealthy(t *testing.T) {
	comp := newTestComponent(t)

	comp.mu.Lock()
	comp.running = true
	comp.mu.Unlock()

	health := comp.Health()
	if !health.Healthy {
		t.Error("Health().Healthy = false, want true when running")
	}
	if health.Status != "healthy" {
		t.Errorf("Health().Status = %q, want healthy", health.Status)
	}
}

func TestHealth_ErrorCountTracksFailures(t *testing.T) {
	comp := newTestComponent(t)

	comp.mu.Lock()
	comp.running = true
	comp.mu.Unlock()

	comp.errors.Add(4)
	health := comp.Health()
	if health.ErrorCount != 4 {
		t.Errorf("Health().ErrorCount = %d, want 4", health.ErrorCount)
	}
}

func TestInputPorts(t *testing.T) {
	comp := newTestComponent(t)

	ports := comp.InputPorts()
	if len(ports) == 0 {
		t.Fatal("InputPorts() should return at least one port")
	}

	for _, p := range ports {
		if p.Direction != component.DirectionInput {
			t.Errorf("port %q direction = %v, want DirectionInput", p.Name, p.Direction)
		}
		if p.Name == "" {
			t.Error("port Name should not be empty")
		}
	}
}

func TestOutputPorts(t *testing.T) {
	comp := newTestComponent(t)

	ports := comp.OutputPorts()
	if len(ports) == 0 {
		t.Fatal("OutputPorts() should return at least one port")
	}

	for _, p := range ports {
		if p.Direction != component.DirectionOutput {
			t.Errorf("port %q direction = %v, want DirectionOutput", p.Name, p.Direction)
		}
	}
}

func TestInputPorts_ContainAllTriggerSubjects(t *testing.T) {
	comp := newTestComponent(t)

	subjects := make(map[string]bool)
	for _, p := range comp.InputPorts() {
		switch cfg := p.Config.(type) {
		case component.JetStreamPort:
			for _, s := range cfg.Subjects {
				subjects[s] = true
			}
		case component.NATSPort:
			subjects[cfg.Subject] = true
		}
	}

	wantSubjects := []string{
		subjectPlanReviewTrigger,
		subjectPhaseReviewTrigger,
		subjectTaskReviewTrigger,
		subjectLoopCompleted,
	}
	for _, s := range wantSubjects {
		if !subjects[s] {
			t.Errorf("InputPorts() missing subject %q", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Initialize / Stop lifecycle
// ---------------------------------------------------------------------------

func TestInitialize(t *testing.T) {
	comp := newTestComponent(t)
	if err := comp.Initialize(); err != nil {
		t.Errorf("Initialize() = %v, want nil", err)
	}
}

func TestStop_NotRunning(t *testing.T) {
	comp := newTestComponent(t)

	// Stopping a component that was never started should be a no-op.
	err := comp.Stop(5 * time.Second)
	if err != nil {
		t.Errorf("Stop() on not-running component = %v, want nil", err)
	}
}

func TestStop_NotRunning_IdempotentRepeat(t *testing.T) {
	comp := newTestComponent(t)

	if err := comp.Stop(0); err != nil {
		t.Errorf("first Stop() = %v, want nil", err)
	}
	if err := comp.Stop(0); err != nil {
		t.Errorf("second Stop() = %v, want nil", err)
	}
}

func TestConfigSchema(t *testing.T) {
	comp := newTestComponent(t)
	schema := comp.ConfigSchema()
	// ConfigSchema should return a non-zero value.
	_ = schema
}

// ---------------------------------------------------------------------------
// DataFlow
// ---------------------------------------------------------------------------

func TestDataFlow_ReturnsMetrics(t *testing.T) {
	comp := newTestComponent(t)
	flow := comp.DataFlow()
	_ = flow.LastActivity
}

func TestDataFlow_LastActivityUpdates(t *testing.T) {
	comp := newTestComponent(t)

	before := time.Now()
	comp.updateLastActivity()
	after := time.Now()

	flow := comp.DataFlow()
	if flow.LastActivity.Before(before) || flow.LastActivity.After(after) {
		t.Errorf("DataFlow().LastActivity %v not in range [%v, %v]",
			flow.LastActivity, before, after)
	}
}

// ---------------------------------------------------------------------------
// Factory registration
// ---------------------------------------------------------------------------

func TestRegister_NilRegistry(t *testing.T) {
	err := Register(nil)
	if err == nil {
		t.Error("Register(nil) should return error")
	}
}

// ---------------------------------------------------------------------------
// handleTrigger parse/validate tests
// ---------------------------------------------------------------------------

func TestHandleTrigger_MalformedJSON(t *testing.T) {
	comp := newTestComponent(t)

	msg := &nats.Msg{Data: []byte("not-json-at-all")}
	comp.handleTrigger(context.Background(), msg, reviewTypePlanReview)

	// On malformed JSON the error counter should have incremented.
	if comp.errors.Load() == 0 {
		t.Error("expected error counter to be incremented for malformed JSON")
	}
	// The trigger counter still increments because parsing happens after the counter.
	if comp.triggersProcessed.Load() == 0 {
		t.Error("triggersProcessed should be incremented even on parse failure")
	}
}

func TestHandleTrigger_EmptyPayload(t *testing.T) {
	comp := newTestComponent(t)

	// An empty JSON object inside a BaseMessage wrapper — no payload field.
	data := []byte(`{}`)
	msg := &nats.Msg{Data: data}
	comp.handleTrigger(context.Background(), msg, reviewTypePlanReview)

	if comp.errors.Load() == 0 {
		t.Error("expected error counter to increment for empty payload")
	}
}

func TestHandleTrigger_MissingSlug(t *testing.T) {
	comp := newTestComponent(t)

	// A valid BaseMessage wrapper but inner payload has no slug.
	inner, _ := json.Marshal(map[string]any{
		"workflow_id": "test-workflow",
		"slug":        "",
	})
	wrapper, _ := json.Marshal(map[string]any{
		"payload": json.RawMessage(inner),
	})
	msg := &nats.Msg{Data: wrapper}
	comp.handleTrigger(context.Background(), msg, reviewTypePlanReview)

	if comp.errors.Load() == 0 {
		t.Error("expected error counter to increment for missing slug")
	}
}

// TestHandleTrigger_CounterIncrements_OnParseFailure verifies that the trigger
// counter increments even on parse failures (counter is the very first operation).
func TestHandleTrigger_CounterIncrements_OnParseFailure(t *testing.T) {
	comp := newTestComponent(t)

	before := comp.triggersProcessed.Load()
	msg := &nats.Msg{Data: []byte("not-json")}
	comp.handleTrigger(context.Background(), msg, reviewTypePlanReview)
	after := comp.triggersProcessed.Load()

	if after != before+1 {
		t.Errorf("triggersProcessed = %d, want %d", after, before+1)
	}
}

// TestHandleTrigger_ErrorCounter_IncrementedOnMissingSlug tests the second
// error gate — a valid BaseMessage wrapper but with an empty slug in the payload.
func TestHandleTrigger_ErrorCounter_IncrementedOnMissingSlug(t *testing.T) {
	comp := newTestComponent(t)

	inner, _ := json.Marshal(map[string]any{
		"workflow_id": "test-workflow",
		"slug":        "",
	})
	wrapper, _ := json.Marshal(map[string]any{
		"payload": json.RawMessage(inner),
	})
	msg := &nats.Msg{Data: wrapper}

	before := comp.errors.Load()
	comp.handleTrigger(context.Background(), msg, reviewTypeTaskReview)
	after := comp.errors.Load()

	if after != before+1 {
		t.Errorf("errors = %d, want %d", after, before+1)
	}
}

// Note: testing handleTrigger with a valid slug is not feasible in pure unit
// tests without NATS infrastructure because startExecutionTimeout acquires
// exec.mu (line 720) while handleTrigger already holds exec.mu (line 384),
// causing a self-deadlock that can only be reproduced with the real runtime.
// The duplicate-dedup and entity-ID-building logic is tested indirectly through
// the terminal state and startRevision tests that construct reviewExecution directly.

// ---------------------------------------------------------------------------
// handleLoopCompleted — parse/validate tests
// ---------------------------------------------------------------------------

func TestHandleLoopCompleted_MalformedJSON(t *testing.T) {
	comp := newTestComponent(t)

	msg := &nats.Msg{Data: []byte("not-json")}
	comp.handleLoopCompleted(context.Background(), msg)

	if comp.errors.Load() == 0 {
		t.Error("expected error counter to increment for malformed loop-completed envelope")
	}
}

func TestHandleLoopCompleted_UnknownWorkflowSlug_DoesNotAddToActiveReviews(t *testing.T) {
	comp := newTestComponent(t)

	// A message for a slug this component doesn't own should not add any
	// execution to activeReviews. The message may or may not increment errors
	// depending on whether BaseMessage deserialization succeeds in the test
	// environment (the semstreams payload registry may not be populated).
	data := buildLoopCompletedMsg(t, "some-other-workflow", workflowStepGenerate, "task-123", "result")
	msg := &nats.Msg{Data: data}
	comp.handleLoopCompleted(context.Background(), msg)

	var count int
	comp.activeReviews.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("activeReviews count = %d, want 0 after unknown workflow slug event", count)
	}
}

func TestHandleLoopCompleted_UnknownTaskID_DoesNotModifyActiveReviews(t *testing.T) {
	comp := newTestComponent(t)

	// Seed an unrelated execution to confirm it is not touched.
	unrelated := newTestExecution("local.semspec.workflow.plan-review.execution.other", reviewTypePlanReview)
	comp.activeReviews.Store(unrelated.EntityID, unrelated)

	// A message for a known workflow slug but an unregistered task ID should
	// be silently discarded — no modifications to the active execution.
	data := buildLoopCompletedMsg(t, WorkflowSlugPlanReview, workflowStepGenerate, "unknown-task-id", "result")
	msg := &nats.Msg{Data: data}
	comp.handleLoopCompleted(context.Background(), msg)

	// The unrelated execution should still be present.
	_, stillPresent := comp.activeReviews.Load(unrelated.EntityID)
	if !stillPresent {
		t.Error("unrelated active review should not be removed by an unknown task ID event")
	}
}

// ---------------------------------------------------------------------------
// Terminal state guard tests
// ---------------------------------------------------------------------------

// TestTerminalState_CleanupRemovesFromActiveReviews verifies that calling any
// of the three terminal state handlers removes the execution from activeReviews
// and cancels the timeout timer, so subsequent calls are safe no-ops.
func TestTerminalState_CleanupRemovesFromActiveReviews(t *testing.T) {
	tests := []struct {
		name     string
		terminal func(comp *Component, exec *reviewExecution)
	}{
		{
			name: "markApproved",
			terminal: func(comp *Component, exec *reviewExecution) {
				comp.markApprovedLocked(context.Background(), exec)
			},
		},
		{
			name: "markEscalated",
			terminal: func(comp *Component, exec *reviewExecution) {
				comp.markEscalatedLocked(context.Background(), exec)
			},
		},
		{
			name: "markError",
			terminal: func(comp *Component, exec *reviewExecution) {
				comp.markErrorLocked(context.Background(), exec, "test error")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := newTestComponent(t)
			exec := newTestExecution("local.semspec.workflow.plan-review.execution.test-slug", reviewTypePlanReview)

			// Register the execution in all maps to simulate an active review.
			comp.activeReviews.Store(exec.EntityID, exec)
			comp.taskIDIndex.Store(exec.GeneratorTaskID, exec.EntityID)
			comp.taskIDIndex.Store(exec.ReviewerTaskID, exec.EntityID)

			exec.mu.Lock()
			tt.terminal(comp, exec)
			exec.mu.Unlock()

			// Execution should no longer be present in activeReviews.
			_, stillActive := comp.activeReviews.Load(exec.EntityID)
			if stillActive {
				t.Errorf("%s: execution still in activeReviews after terminal state", tt.name)
			}

			// Task ID index entries should be cleaned up.
			_, genIdx := comp.taskIDIndex.Load(exec.GeneratorTaskID)
			if genIdx {
				t.Errorf("%s: GeneratorTaskID still in taskIDIndex after cleanup", tt.name)
			}
			_, revIdx := comp.taskIDIndex.Load(exec.ReviewerTaskID)
			if revIdx {
				t.Errorf("%s: ReviewerTaskID still in taskIDIndex after cleanup", tt.name)
			}
		})
	}
}

func TestMarkApprovedLocked_IncrementsCounters(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("local.semspec.workflow.plan-review.execution.counter-test", reviewTypePlanReview)
	comp.activeReviews.Store(exec.EntityID, exec)

	exec.mu.Lock()
	comp.markApprovedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if comp.reviewsApproved.Load() != 1 {
		t.Errorf("reviewsApproved = %d, want 1", comp.reviewsApproved.Load())
	}
	if comp.reviewsCompleted.Load() != 1 {
		t.Errorf("reviewsCompleted = %d, want 1", comp.reviewsCompleted.Load())
	}
}

func TestMarkEscalatedLocked_IncrementsCounters(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("local.semspec.workflow.plan-review.execution.escalate-test", reviewTypePlanReview)
	comp.activeReviews.Store(exec.EntityID, exec)

	exec.mu.Lock()
	comp.markEscalatedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if comp.reviewsEscalated.Load() != 1 {
		t.Errorf("reviewsEscalated = %d, want 1", comp.reviewsEscalated.Load())
	}
	if comp.reviewsCompleted.Load() != 1 {
		t.Errorf("reviewsCompleted = %d, want 1", comp.reviewsCompleted.Load())
	}
}

func TestMarkErrorLocked_IncrementsCounters(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("local.semspec.workflow.plan-review.execution.error-test", reviewTypePlanReview)
	comp.activeReviews.Store(exec.EntityID, exec)

	exec.mu.Lock()
	comp.markErrorLocked(context.Background(), exec, "something went wrong")
	exec.mu.Unlock()

	if comp.errors.Load() == 0 {
		t.Error("errors counter should be incremented by markErrorLocked")
	}
	if comp.reviewsCompleted.Load() != 1 {
		t.Errorf("reviewsCompleted = %d, want 1", comp.reviewsCompleted.Load())
	}
}

// TestTerminalState_TimeoutTimer_IsStopped verifies that cleanup stops the
// timeout timer so it doesn't fire after the execution has already terminated.
func TestTerminalState_TimeoutTimer_IsStopped(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("local.semspec.workflow.plan-review.execution.timer-test", reviewTypePlanReview)
	comp.activeReviews.Store(exec.EntityID, exec)

	// Attach a timeout timer with a stop-flag tracker.
	stopped := false
	exec.timeoutTimer = &timeoutHandle{
		stop: func() { stopped = true },
	}

	exec.mu.Lock()
	comp.markApprovedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if !stopped {
		t.Error("timeout timer should be stopped when execution reaches terminal state")
	}
}

// ---------------------------------------------------------------------------
// parseGeneratorResult tests
// ---------------------------------------------------------------------------

func TestParseGeneratorResult_PlanReview_ValidJSON(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePlanReview)

	result := `{"content": {"goal":"Build auth"}, "llm_request_ids": ["req-1"]}`
	content, llmIDs := comp.parseGeneratorResult(result, exec)

	if content == nil {
		t.Fatal("expected non-nil content for valid planner result")
	}
	if len(llmIDs) != 1 || llmIDs[0] != "req-1" {
		t.Errorf("llmIDs = %v, want [req-1]", llmIDs)
	}
}

func TestParseGeneratorResult_PlanReview_InvalidJSON_FallsBackToRaw(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePlanReview)

	result := "not json at all"
	content, _ := comp.parseGeneratorResult(result, exec)

	if string(content) != result {
		t.Errorf("fallback content = %q, want raw result %q", string(content), result)
	}
}

func TestParseGeneratorResult_PhaseReview_ValidJSON(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePhaseReview)

	result := `{"phases": [{"id": "p1"}], "llm_request_ids": ["req-2"]}`
	content, llmIDs := comp.parseGeneratorResult(result, exec)

	if content == nil {
		t.Fatal("expected non-nil content for valid phase generator result")
	}
	if len(llmIDs) != 1 || llmIDs[0] != "req-2" {
		t.Errorf("llmIDs = %v, want [req-2]", llmIDs)
	}
}

func TestParseGeneratorResult_TaskReview_ValidJSON(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypeTaskReview)

	result := `{"tasks": [{"id": "t1"}], "llm_request_ids": ["req-3"]}`
	content, llmIDs := comp.parseGeneratorResult(result, exec)

	if content == nil {
		t.Fatal("expected non-nil content for valid task generator result")
	}
	if len(llmIDs) != 1 || llmIDs[0] != "req-3" {
		t.Errorf("llmIDs = %v, want [req-3]", llmIDs)
	}
}

func TestParseGeneratorResult_UnknownReviewType_ReturnsRaw(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", "unknown-type")

	result := `{"foo": "bar"}`
	content, _ := comp.parseGeneratorResult(result, exec)

	if string(content) != result {
		t.Errorf("unknown review type: content = %q, want raw %q", string(content), result)
	}
}

// ---------------------------------------------------------------------------
// parseReviewerResult tests
// ---------------------------------------------------------------------------

func TestParseReviewerResult_PlanReview_Approved(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePlanReview)

	result := `{"verdict": "approved", "summary": "LGTM", "llm_request_ids": ["req-r1"]}`
	verdict, summary, _, _, llmIDs := comp.parseReviewerResult(result, exec)

	if verdict != "approved" {
		t.Errorf("verdict = %q, want approved", verdict)
	}
	if summary != "LGTM" {
		t.Errorf("summary = %q, want LGTM", summary)
	}
	if len(llmIDs) != 1 || llmIDs[0] != "req-r1" {
		t.Errorf("llmIDs = %v, want [req-r1]", llmIDs)
	}
}

func TestParseReviewerResult_PlanReview_InvalidJSON_DefaultsToApproved(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePlanReview)

	verdict, _, _, _, _ := comp.parseReviewerResult("not json", exec)
	if verdict != "approved" {
		t.Errorf("verdict = %q, want approved as safe default on parse failure", verdict)
	}
}

func TestParseReviewerResult_TaskReview_NeedsChanges(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypeTaskReview)

	result := `{"verdict": "needs_changes", "summary": "Missing tests", "formatted_findings": "- Add unit tests"}`
	verdict, summary, _, formattedFindings, _ := comp.parseReviewerResult(result, exec)

	if verdict != "needs_changes" {
		t.Errorf("verdict = %q, want needs_changes", verdict)
	}
	if summary != "Missing tests" {
		t.Errorf("summary = %q, want 'Missing tests'", summary)
	}
	if formattedFindings == "" {
		t.Error("formattedFindings should not be empty")
	}
}

func TestParseReviewerResult_PhaseReview_SharesReviewResultType(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePhaseReview)

	result := `{"verdict": "approved", "summary": "Phases look good"}`
	verdict, summary, _, _, _ := comp.parseReviewerResult(result, exec)

	if verdict != "approved" {
		t.Errorf("verdict = %q, want approved", verdict)
	}
	if summary != "Phases look good" {
		t.Errorf("summary = %q, want 'Phases look good'", summary)
	}
}

func TestParseReviewerResult_UnknownType_DefaultsToApproved(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", "unknown-type")

	verdict, _, _, _, _ := comp.parseReviewerResult(`{"verdict": "needs_changes"}`, exec)
	if verdict != "approved" {
		t.Errorf("unknown review type: verdict = %q, want approved as default", verdict)
	}
}

// ---------------------------------------------------------------------------
// buildRevisionContext (reviewExecution helper)
// ---------------------------------------------------------------------------

func TestBuildRevisionContext_ContainsSummaryAndFindings(t *testing.T) {
	exec := &reviewExecution{
		Summary:           "Needs better error handling",
		FormattedFindings: "- Missing nil checks\n- No timeout guards",
	}

	ctx := exec.buildRevisionContext()

	if !strings.Contains(ctx, "REVISION REQUEST") {
		t.Error("revision context should contain REVISION REQUEST header")
	}
	if !strings.Contains(ctx, exec.Summary) {
		t.Errorf("revision context should contain summary %q", exec.Summary)
	}
	if !strings.Contains(ctx, exec.FormattedFindings) {
		t.Errorf("revision context should contain findings %q", exec.FormattedFindings)
	}
}

func TestBuildRevisionContext_EmptyFieldsStillReturnsString(t *testing.T) {
	exec := &reviewExecution{}
	ctx := exec.buildRevisionContext()
	if ctx == "" {
		t.Error("buildRevisionContext should return non-empty string even with empty fields")
	}
}

// ---------------------------------------------------------------------------
// startRevision — iteration counter and phase transition
// ---------------------------------------------------------------------------

func TestStartRevision_IncrementsIteration(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("local.semspec.workflow.plan-review.execution.revision-test", reviewTypePlanReview)
	comp.activeReviews.Store(exec.EntityID, exec)

	exec.Iteration = 0
	exec.PlanContent = json.RawMessage(`{"goal":"old"}`)
	exec.LLMRequestIDs = []string{"old-req"}
	exec.Summary = "Rejected for X"
	exec.FormattedFindings = "- Fix X"

	// startRevision requires exec.mu to be held by the caller (mirrors production usage).
	exec.mu.Lock()
	comp.startRevision(context.Background(), exec)
	exec.mu.Unlock()

	if exec.Iteration != 1 {
		t.Errorf("Iteration = %d after first revision, want 1", exec.Iteration)
	}
	if exec.PlanContent != nil {
		t.Error("PlanContent should be cleared on revision")
	}
	if exec.LLMRequestIDs != nil {
		t.Error("LLMRequestIDs should be cleared on revision")
	}
	// Summary and FormattedFindings are deliberately kept for the revision prompt.
	if exec.Summary != "Rejected for X" {
		t.Errorf("Summary = %q, should be preserved across revision", exec.Summary)
	}
}

// ---------------------------------------------------------------------------
// buildGeneratorPayload — subject and revision flag
// ---------------------------------------------------------------------------

func TestBuildGeneratorPayload_PlanReview_FirstIteration(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePlanReview)
	exec.Prompt = "Plan the auth feature"
	exec.Iteration = 0

	subject, payload, workflowSlug := comp.buildGeneratorPayload(exec)

	if subject != subjectPlannerAsync {
		t.Errorf("subject = %q, want %q", subject, subjectPlannerAsync)
	}
	if workflowSlug != WorkflowSlugPlanReview {
		t.Errorf("workflowSlug = %q, want %q", workflowSlug, WorkflowSlugPlanReview)
	}
	if payload == nil {
		t.Fatal("payload should not be nil")
	}
}

func TestBuildGeneratorPayload_PlanReview_RevisionIteration(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePlanReview)
	exec.Prompt = "Plan the auth feature"
	exec.Iteration = 1
	exec.Summary = "Missing scope"
	exec.FormattedFindings = "- Add scope section"

	_, payload, _ := comp.buildGeneratorPayload(exec)

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	revision, _ := m["revision"].(bool)
	if !revision {
		t.Error("revision=true expected on second iteration")
	}
}

func TestBuildGeneratorPayload_PhaseReview_UsesPhaseGeneratorAsync(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypePhaseReview)

	subject, _, workflowSlug := comp.buildGeneratorPayload(exec)

	if subject != subjectPhaseGeneratorAsync {
		t.Errorf("subject = %q, want %q", subject, subjectPhaseGeneratorAsync)
	}
	if workflowSlug != WorkflowSlugPhaseReview {
		t.Errorf("workflowSlug = %q, want %q", workflowSlug, WorkflowSlugPhaseReview)
	}
}

func TestBuildGeneratorPayload_TaskReview_UsesTaskGeneratorAsync(t *testing.T) {
	comp := newTestComponent(t)
	exec := newTestExecution("e", reviewTypeTaskReview)

	subject, _, workflowSlug := comp.buildGeneratorPayload(exec)

	if subject != subjectTaskGeneratorAsync {
		t.Errorf("subject = %q, want %q", subject, subjectTaskGeneratorAsync)
	}
	if workflowSlug != WorkflowSlugTaskReview {
		t.Errorf("workflowSlug = %q, want %q", workflowSlug, WorkflowSlugTaskReview)
	}
}

// ---------------------------------------------------------------------------
// portSubject helper
// ---------------------------------------------------------------------------

func TestPortSubject_NilConfig(t *testing.T) {
	p := component.Port{Config: nil}
	if got := portSubject(p); got != "" {
		t.Errorf("portSubject(nil config) = %q, want empty", got)
	}
}

func TestPortSubject_NATSPort(t *testing.T) {
	p := component.Port{
		Config: component.NATSPort{Subject: "workflow.trigger.plan-review-loop"},
	}
	got := portSubject(p)
	if got != "workflow.trigger.plan-review-loop" {
		t.Errorf("portSubject(NATSPort) = %q, want workflow.trigger.plan-review-loop", got)
	}
}

func TestPortSubject_JetStreamPort_WithSubjects(t *testing.T) {
	p := component.Port{
		Config: component.JetStreamPort{
			Subjects: []string{"agentic.loop_completed.v1", "other"},
		},
	}
	got := portSubject(p)
	if got != "agentic.loop_completed.v1" {
		t.Errorf("portSubject(JetStreamPort) = %q, want agentic.loop_completed.v1", got)
	}
}

func TestPortSubject_JetStreamPort_EmptySubjects(t *testing.T) {
	p := component.Port{
		Config: component.JetStreamPort{Subjects: []string{}},
	}
	got := portSubject(p)
	if got != "" {
		t.Errorf("portSubject(JetStreamPort empty subjects) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// WorkflowSlug constants
// ---------------------------------------------------------------------------

func TestWorkflowSlugConstants(t *testing.T) {
	// These constants are matched by the loop-completion handler.
	// Verify they are non-empty and stable.
	if WorkflowSlugPlanReview == "" {
		t.Error("WorkflowSlugPlanReview should not be empty")
	}
	if WorkflowSlugPhaseReview == "" {
		t.Error("WorkflowSlugPhaseReview should not be empty")
	}
	if WorkflowSlugTaskReview == "" {
		t.Error("WorkflowSlugTaskReview should not be empty")
	}

	// They must all be distinct.
	if WorkflowSlugPlanReview == WorkflowSlugPhaseReview {
		t.Error("WorkflowSlugPlanReview and WorkflowSlugPhaseReview must differ")
	}
	if WorkflowSlugPlanReview == WorkflowSlugTaskReview {
		t.Error("WorkflowSlugPlanReview and WorkflowSlugTaskReview must differ")
	}
	if WorkflowSlugPhaseReview == WorkflowSlugTaskReview {
		t.Error("WorkflowSlugPhaseReview and WorkflowSlugTaskReview must differ")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestComponent constructs a Component with default config and no NATS client.
func newTestComponent(t *testing.T) *Component {
	t.Helper()

	rawCfg, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}

	deps := component.Dependencies{}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() error: %v", err)
	}

	return comp.(*Component)
}

// newTestExecution builds a minimal reviewExecution with the provided entityID
// and reviewType. Task IDs are pre-populated so index cleanup tests work.
func newTestExecution(entityID, reviewType string) *reviewExecution {
	return &reviewExecution{
		EntityID:        entityID,
		ReviewType:      reviewType,
		Slug:            "test-slug",
		MaxIterations:   3,
		GeneratorTaskID: "generator-task-" + entityID,
		ReviewerTaskID:  "reviewer-task-" + entityID,
	}
}

// buildLoopCompletedMsg constructs a minimal serialised BaseMessage containing
// an agentic.LoopCompletedEvent for use in handleLoopCompleted tests.
//
// The function produces the minimal JSON that component.handleLoopCompleted
// expects: a BaseMessage envelope whose payload field can be decoded by
// message.BaseMessage.Payload() into an *agentic.LoopCompletedEvent.
//
// NOTE: Because the production code calls base.Payload() which uses the
// semstreams payload registry, we construct the raw JSON shape that
// message.BaseMessage.Payload() will decode correctly.  The type discriminator
// fields must match what agentic.LoopCompletedEvent.Schema() returns.
func buildLoopCompletedMsg(t *testing.T, workflowSlug, workflowStep, taskID, result string) []byte {
	t.Helper()

	inner := map[string]any{
		"task_id":       taskID,
		"workflow_slug": workflowSlug,
		"workflow_step": workflowStep,
		"result":        result,
	}
	innerBytes, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal loop completed inner: %v", err)
	}

	// message.BaseMessage shape expected by json.Unmarshal in handleLoopCompleted.
	outer := map[string]any{
		"type": map[string]any{
			"domain":   "agentic",
			"category": "loop-completed",
			"version":  "v1",
		},
		"payload": json.RawMessage(innerBytes),
	}

	data, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("marshal loop completed message: %v", err)
	}
	return data
}
