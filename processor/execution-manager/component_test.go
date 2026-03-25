package executionmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
)

// testEntityID computes the entity ID from slug+taskID using the same hash as the component.
func testEntityID(slug, taskID string) string {
	h := sha256.Sum256([]byte(slug + "-" + taskID))
	return fmt.Sprintf("%s.exec.task.run.%s", workflow.EntityPrefix(), hex.EncodeToString(h[:8]))
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxIterations != 3 {
		t.Errorf("MaxIterations: want 3, got %d", cfg.MaxIterations)
	}
	if cfg.TimeoutSeconds != 1800 {
		t.Errorf("TimeoutSeconds: want 1800, got %d", cfg.TimeoutSeconds)
	}
	if cfg.Model != "default" {
		t.Errorf("Model: want \"default\", got %q", cfg.Model)
	}
	if cfg.Ports == nil {
		t.Fatal("Ports must not be nil")
	}
	if len(cfg.Ports.Inputs) != 2 {
		t.Errorf("Ports.Inputs: want 2, got %d", len(cfg.Ports.Inputs))
	}
	if len(cfg.Ports.Outputs) != 2 {
		t.Errorf("Ports.Outputs: want 2, got %d", len(cfg.Ports.Outputs))
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config to pass, got: %v", err)
	}
}

func TestConfigValidate_ZeroMaxIterations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxIterations = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for MaxIterations=0, got nil")
	}
}

func TestConfigValidate_NegativeMaxIterations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxIterations = -1
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for MaxIterations=-1, got nil")
	}
}

func TestConfigValidate_ZeroTimeoutSeconds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TimeoutSeconds = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for TimeoutSeconds=0, got nil")
	}
}

func TestConfigValidate_NegativeTimeoutSeconds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TimeoutSeconds = -5
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for TimeoutSeconds=-5, got nil")
	}
}

func TestConfigGetTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TimeoutSeconds = 120

	got := cfg.GetTimeout()
	want := 120 * time.Second
	if got != want {
		t.Errorf("GetTimeout: want %v, got %v", want, got)
	}
}

func TestConfigGetTimeout_ZeroFallback(t *testing.T) {
	cfg := Config{TimeoutSeconds: 0}
	got := cfg.GetTimeout()
	want := 30 * time.Minute
	if got != want {
		t.Errorf("GetTimeout with zero: want %v, got %v", want, got)
	}
}

// ---------------------------------------------------------------------------
// withDefaults tests
// ---------------------------------------------------------------------------

func TestConfigWithDefaults_AllZeroAppliesDefaults(t *testing.T) {
	// An empty config should get all defaults filled in.
	empty := Config{}
	got := empty.withDefaults()

	if got.MaxIterations != 3 {
		t.Errorf("MaxIterations: want 3, got %d", got.MaxIterations)
	}
	if got.TimeoutSeconds != 1800 {
		t.Errorf("TimeoutSeconds: want 1800, got %d", got.TimeoutSeconds)
	}
	if got.Model != "default" {
		t.Errorf("Model: want \"default\", got %q", got.Model)
	}
	if got.Ports == nil {
		t.Error("Ports should not be nil after withDefaults")
	}
}

func TestConfigWithDefaults_ExplicitValuesPreserved(t *testing.T) {
	cfg := Config{
		MaxIterations:  5,
		TimeoutSeconds: 600,
		Model:          "gpt-4o",
	}
	got := cfg.withDefaults()

	if got.MaxIterations != 5 {
		t.Errorf("MaxIterations: want 5, got %d", got.MaxIterations)
	}
	if got.TimeoutSeconds != 600 {
		t.Errorf("TimeoutSeconds: want 600, got %d", got.TimeoutSeconds)
	}
	if got.Model != "gpt-4o" {
		t.Errorf("Model: want \"gpt-4o\", got %q", got.Model)
	}
}

// ---------------------------------------------------------------------------
// NewComponent construction tests
// ---------------------------------------------------------------------------

func TestNewComponent_Defaults(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{})
	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent with empty config: unexpected error: %v", err)
	}
	if comp == nil {
		t.Fatal("NewComponent returned nil component")
	}
}

func TestNewComponent_WithExplicitConfig(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{
		"max_iterations":  5,
		"timeout_seconds": 300,
		"model":           "claude-3-5-sonnet",
	})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comp == nil {
		t.Fatal("returned nil component")
	}
}

func TestNewComponent_InvalidJSON(t *testing.T) {
	rawCfg := json.RawMessage(`{not valid json}`)
	deps := component.Dependencies{}

	_, err := NewComponent(rawCfg, deps)
	if err == nil {
		t.Error("expected error for malformed JSON config, got nil")
	}
}

func TestNewComponent_ZeroMaxIterations_IsReplacedByDefault(t *testing.T) {
	// withDefaults replaces any value <= 0 with the default (3), so a
	// JSON-supplied 0 results in a valid component — it silently becomes 3.
	// This test documents that deliberate behavior.
	rawCfg, _ := json.Marshal(map[string]any{
		"max_iterations":  0,
		"timeout_seconds": 300,
	})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Errorf("zero max_iterations should be silently defaulted, got error: %v", err)
	}
	if comp == nil {
		t.Fatal("expected a valid component, got nil")
	}
}

func TestNewComponent_ZeroTimeoutSeconds_IsReplacedByDefault(t *testing.T) {
	// Same rationale as above: withDefaults replaces 0 with 1800.
	rawCfg, _ := json.Marshal(map[string]any{
		"max_iterations":  3,
		"timeout_seconds": 0,
	})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Errorf("zero timeout_seconds should be silently defaulted, got error: %v", err)
	}
	if comp == nil {
		t.Fatal("expected a valid component, got nil")
	}
}

// ---------------------------------------------------------------------------
// Meta / Health / Ports
// ---------------------------------------------------------------------------

func TestMeta(t *testing.T) {
	c := newTestComponent(t)

	meta := c.Meta()
	if meta.Name != componentName {
		t.Errorf("Meta.Name: want %q, got %q", componentName, meta.Name)
	}
	if meta.Version != componentVersion {
		t.Errorf("Meta.Version: want %q, got %q", componentVersion, meta.Version)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type: want \"processor\", got %q", meta.Type)
	}
	if meta.Description == "" {
		t.Error("Meta.Description must not be empty")
	}
}

func TestHealth_NotRunning(t *testing.T) {
	c := newTestComponent(t)

	h := c.Health()
	if h.Healthy {
		t.Error("stopped component should not report Healthy=true")
	}
	if h.Status != "stopped" {
		t.Errorf("Health.Status: want \"stopped\", got %q", h.Status)
	}
}

func TestInitialize_Noop(t *testing.T) {
	c := newTestComponent(t)
	if err := c.Initialize(); err != nil {
		t.Errorf("Initialize should be a no-op, got error: %v", err)
	}
}

func TestInputPorts(t *testing.T) {
	c := newTestComponent(t)
	ports := c.InputPorts()

	// Default config has two input ports: execution-trigger + loop-completions.
	if len(ports) != 2 {
		t.Errorf("InputPorts: want 2, got %d", len(ports))
	}
	for _, p := range ports {
		if p.Direction != component.DirectionInput {
			t.Errorf("port %q has wrong direction: want input, got %v", p.Name, p.Direction)
		}
	}
}

func TestOutputPorts(t *testing.T) {
	c := newTestComponent(t)
	ports := c.OutputPorts()

	// Default config has two output ports: entity-triples + agent-tasks.
	if len(ports) != 2 {
		t.Errorf("OutputPorts: want 2, got %d", len(ports))
	}
	for _, p := range ports {
		if p.Direction != component.DirectionOutput {
			t.Errorf("port %q has wrong direction: want output, got %v", p.Name, p.Direction)
		}
	}
}

func TestDataFlow_ZeroBeforeAnyActivity(t *testing.T) {
	c := newTestComponent(t)
	flow := c.DataFlow()
	if !flow.LastActivity.IsZero() {
		t.Errorf("DataFlow.LastActivity should be zero before any messages, got %v", flow.LastActivity)
	}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_NilRegistry(t *testing.T) {
	if err := Register(nil); err == nil {
		t.Error("Register(nil) should return an error")
	}
}

func TestRegister_ValidRegistry(t *testing.T) {
	reg := &stubRegistry{}
	if err := Register(reg); err != nil {
		t.Errorf("Register with valid registry: unexpected error: %v", err)
	}
	if !reg.called {
		t.Error("expected RegisterWithConfig to be called on the registry")
	}
	if reg.cfg.Name != componentName {
		t.Errorf("registered name: want %q, got %q", componentName, reg.cfg.Name)
	}
}

// ---------------------------------------------------------------------------
// handleTrigger — parse/validate logic (via exported method path)
//
// We exercise the parsing branch directly by constructing raw NATS message
// bytes. The component's natsClient is nil so any write/publish side effects
// silently no-op, letting us focus on the parse and state-machine branches.
// ---------------------------------------------------------------------------

func TestHandleTrigger_MalformedJSON(t *testing.T) {
	c := newTestComponent(t)

	before := c.errors.Load()
	c.handleTrigger(testCtx(t), makeNATSMsg(t, []byte(`{bad json`)))

	if c.errors.Load() <= before {
		t.Error("malformed JSON trigger should increment error counter")
	}
	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed: want 1, got %d", c.triggersProcessed.Load())
	}
}

func TestHandleTrigger_MissingSlug(t *testing.T) {
	c := newTestComponent(t)

	// Valid BaseMessage wrapper but missing slug in the payload.
	payload := map[string]any{
		"task_id": "task-123",
		// slug intentionally absent
	}
	before := c.errors.Load()
	c.handleTrigger(testCtx(t), makeTriggerMsg(t, payload))

	if c.errors.Load() <= before {
		t.Error("trigger missing slug should increment error counter")
	}
}

func TestHandleTrigger_MissingTaskID(t *testing.T) {
	c := newTestComponent(t)

	payload := map[string]any{
		"slug": "my-plan",
		// task_id intentionally absent
	}
	before := c.errors.Load()
	c.handleTrigger(testCtx(t), makeTriggerMsg(t, payload))

	if c.errors.Load() <= before {
		t.Error("trigger missing task_id should increment error counter")
	}
}

func TestHandleTrigger_ValidTrigger_RegistersExecution(t *testing.T) {
	c := newTestComponent(t)

	payload := map[string]any{
		"slug":    "my-plan",
		"task_id": "task-abc",
		"title":   "Do something",
		"model":   "default",
	}
	c.handleTrigger(testCtx(t), makeTriggerMsg(t, payload))

	entityID := testEntityID("my-plan", "task-abc")
	if _, ok := c.activeExecutions.Load(entityID); !ok {
		t.Errorf("expected active execution to be registered for entity %q", entityID)
	}
}

func TestHandleTrigger_DuplicateTrigger_IsIdempotent(t *testing.T) {
	c := newTestComponent(t)

	payload := map[string]any{
		"slug":    "my-plan",
		"task_id": "task-dup",
	}
	msg := makeTriggerMsg(t, payload)

	c.handleTrigger(testCtx(t), msg)
	firstCount := c.triggersProcessed.Load()

	// A second trigger for the same entity should be silently dropped.
	c.handleTrigger(testCtx(t), msg)

	if c.triggersProcessed.Load() != firstCount+1 {
		t.Errorf("second trigger should still increment triggersProcessed counter")
	}

	// The execution must still be registered (not doubled or removed).
	entityID := testEntityID("my-plan", "task-dup")
	if _, ok := c.activeExecutions.Load(entityID); !ok {
		t.Error("execution should remain registered after duplicate trigger")
	}
}

// ---------------------------------------------------------------------------
// handleLoopCompleted — routing / guard logic
// ---------------------------------------------------------------------------

func TestHandleLoopCompleted_MalformedJSON(t *testing.T) {
	c := newTestComponent(t)

	before := c.errors.Load()
	c.handleLoopCompleted(testCtx(t), makeNATSMsg(t, []byte(`bad json`)))

	if c.errors.Load() <= before {
		t.Error("malformed JSON in loop-completed message should increment error counter")
	}
}

func TestHandleLoopCompleted_UnknownTaskID_Noop(t *testing.T) {
	c := newTestComponent(t)

	// A well-formed LoopCompletedEvent for the right workflow slug but an
	// unregistered TaskID should be quietly ignored (no panic, no state change).
	msg := makeLoopCompletedMsg(t, WorkflowSlugTaskExecution, "unknown-task-id", stageDevelop, `{}`)
	before := c.executionsCompleted.Load()
	c.handleLoopCompleted(testCtx(t), makeNATSMsg(t, msg))

	if c.executionsCompleted.Load() != before {
		t.Error("unknown task_id should not change executionsCompleted counter")
	}
}

func TestHandleLoopCompleted_WrongWorkflowSlug_Noop(t *testing.T) {
	c := newTestComponent(t)

	msg := makeLoopCompletedMsg(t, "some-other-workflow", "task-xyz", stageDevelop, `{}`)
	before := c.executionsCompleted.Load()
	c.handleLoopCompleted(testCtx(t), makeNATSMsg(t, msg))

	if c.executionsCompleted.Load() != before {
		t.Error("wrong workflow slug should be ignored without side effects")
	}
}

// ---------------------------------------------------------------------------
// Terminal state helpers (direct invocation)
// ---------------------------------------------------------------------------

func TestMarkApprovedLocked_IncrementsCounters(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-1")

	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if c.executionsApproved.Load() != 1 {
		t.Errorf("executionsApproved: want 1, got %d", c.executionsApproved.Load())
	}
	if c.executionsCompleted.Load() != 1 {
		t.Errorf("executionsCompleted: want 1, got %d", c.executionsCompleted.Load())
	}
	// Execution must be removed from the active map.
	if _, ok := c.activeExecutions.Load(exec.EntityID); ok {
		t.Error("execution should be removed from activeExecutions after approval")
	}
}

func TestMarkEscalatedLocked_IncrementsCounters(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-2")

	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.markEscalatedLocked(testCtx(t), exec, "test escalation reason")
	exec.mu.Unlock()

	if c.executionsEscalated.Load() != 1 {
		t.Errorf("executionsEscalated: want 1, got %d", c.executionsEscalated.Load())
	}
	if c.executionsCompleted.Load() != 1 {
		t.Errorf("executionsCompleted: want 1, got %d", c.executionsCompleted.Load())
	}
	if _, ok := c.activeExecutions.Load(exec.EntityID); ok {
		t.Error("execution should be removed from activeExecutions after escalation")
	}
}

func TestMarkErrorLocked_IncrementsErrorCounter(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-3")

	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	before := c.errors.Load()
	c.markErrorLocked(testCtx(t), exec, "something went wrong")
	exec.mu.Unlock()

	if c.errors.Load() <= before {
		t.Error("markErrorLocked should increment error counter")
	}
	if c.executionsCompleted.Load() != 1 {
		t.Errorf("executionsCompleted: want 1, got %d", c.executionsCompleted.Load())
	}
}

// ---------------------------------------------------------------------------
// startDeveloperRetryLocked — state reset
// ---------------------------------------------------------------------------

func TestStartDeveloperRetryLocked_IncrementsIteration(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-retry")
	exec.Iteration = 0
	exec.MaxIterations = 3

	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store(exec.DeveloperTaskID, exec.EntityID)

	exec.mu.Lock()
	c.startDeveloperRetryLocked(testCtx(t), exec, "reviewer said no")
	exec.mu.Unlock()

	if exec.Iteration != 1 {
		t.Errorf("Iteration after retry: want 1, got %d", exec.Iteration)
	}
	if exec.Feedback != "reviewer said no" {
		t.Errorf("Feedback: want %q, got %q", "reviewer said no", exec.Feedback)
	}
}

func TestStartDeveloperRetryLocked_ClearsPreviousOutputs(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-clear")
	exec.FilesModified = []string{"foo.go", "bar.go"}
	exec.DeveloperOutput = json.RawMessage(`{"key":"val"}`)
	exec.ValidationPassed = true
	exec.Verdict = "rejected"
	exec.RejectionType = "misscoped"
	exec.Iteration = 0
	exec.MaxIterations = 3

	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.startDeveloperRetryLocked(testCtx(t), exec, "some feedback")
	exec.mu.Unlock()

	if exec.FilesModified != nil {
		t.Error("FilesModified should be cleared on retry")
	}
	if exec.DeveloperOutput != nil {
		t.Error("DeveloperOutput should be cleared on retry")
	}
	if exec.ValidationPassed {
		t.Error("ValidationPassed should be reset to false on retry")
	}
	if exec.Verdict != "" {
		t.Error("Verdict should be cleared on retry")
	}
	if exec.RejectionType != "" {
		t.Error("RejectionType should be cleared on retry")
	}
}

// ---------------------------------------------------------------------------
// cleanupExecutionLocked
// ---------------------------------------------------------------------------

func TestCleanupExecutionLocked_RemovesIndexEntries(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-clean")
	exec.DeveloperTaskID = "dev-111"
	exec.ValidatorTaskID = "val-222"
	exec.ReviewerTaskID = "rev-333"

	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store(exec.DeveloperTaskID, exec.EntityID)
	c.taskIDIndex.Store(exec.ValidatorTaskID, exec.EntityID)
	c.taskIDIndex.Store(exec.ReviewerTaskID, exec.EntityID)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()

	for _, id := range []string{"dev-111", "val-222", "rev-333"} {
		if _, ok := c.taskIDIndex.Load(id); ok {
			t.Errorf("taskIDIndex should not contain %q after cleanup", id)
		}
	}
	if _, ok := c.activeExecutions.Load(exec.EntityID); ok {
		t.Error("activeExecutions should not contain entity after cleanup")
	}
}

// ---------------------------------------------------------------------------
// Stop when not running
// ---------------------------------------------------------------------------

func TestStop_NotRunning_Noop(t *testing.T) {
	c := newTestComponent(t)
	if err := c.Stop(time.Second); err != nil {
		t.Errorf("Stop on non-running component should be a no-op, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// updateLastActivity / DataFlow
// ---------------------------------------------------------------------------

func TestUpdateLastActivity(t *testing.T) {
	c := newTestComponent(t)
	before := time.Now()

	c.updateLastActivity()

	activity := c.getLastActivity()
	if activity.Before(before) {
		t.Errorf("lastActivity (%v) should be >= start of test (%v)", activity, before)
	}
}

// ---------------------------------------------------------------------------
// Config — IndexingBudget validation
// ---------------------------------------------------------------------------

func TestConfigValidate_InvalidIndexingBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IndexingBudgetStr = "not-a-duration"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for IndexingBudgetStr=\"not-a-duration\", got nil")
	}
}

func TestConfigValidate_ValidIndexingBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IndexingBudgetStr = "90s"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with IndexingBudgetStr=\"90s\" to pass, got: %v", err)
	}
}

func TestConfigGetIndexingBudget_Empty(t *testing.T) {
	cfg := Config{}
	got := cfg.GetIndexingBudget()
	if got != 0 {
		t.Errorf("GetIndexingBudget with empty string: want 0, got %v", got)
	}
}

func TestConfigGetIndexingBudget_Valid(t *testing.T) {
	cfg := Config{IndexingBudgetStr: "90s"}
	got := cfg.GetIndexingBudget()
	want := 90 * time.Second
	if got != want {
		t.Errorf("GetIndexingBudget(\"90s\"): want %v, got %v", want, got)
	}
}

func TestConfigGetIndexingBudget_Invalid(t *testing.T) {
	cfg := Config{IndexingBudgetStr: "bad"}
	got := cfg.GetIndexingBudget()
	if got != 0 {
		t.Errorf("GetIndexingBudget(\"bad\"): want 0 (silent fallback), got %v", got)
	}
}

// ---------------------------------------------------------------------------
// NewComponent — indexingGate wiring
// ---------------------------------------------------------------------------

func TestNewComponent_WithGraphGatewayURL(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{
		"graph_gateway_url": "http://localhost:8082",
	})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent with graph_gateway_url: unexpected error: %v", err)
	}
	c := comp.(*Component)
	if c.indexingGate == nil {
		t.Error("expected indexingGate to be non-nil when graph_gateway_url is configured")
	}
}

func TestNewComponent_WithoutGraphGatewayURL(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent without graph_gateway_url: unexpected error: %v", err)
	}
	c := comp.(*Component)
	if c.indexingGate != nil {
		t.Error("expected indexingGate to be nil when graph_gateway_url is absent")
	}
}

func TestNewComponent_WithIndexingBudget(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{
		"indexing_budget": "90s",
	})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent with indexing_budget=\"90s\": unexpected error: %v", err)
	}
	c := comp.(*Component)
	want := 90 * time.Second
	got := c.config.GetIndexingBudget()
	if got != want {
		t.Errorf("GetIndexingBudget(): want %v, got %v", want, got)
	}
}

func TestNewComponent_InvalidIndexingBudget(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{
		"indexing_budget": "not-a-duration",
	})
	deps := component.Dependencies{}

	_, err := NewComponent(rawCfg, deps)
	if err == nil {
		t.Error("expected error for indexing_budget=\"not-a-duration\", got nil")
	}
}

// ---------------------------------------------------------------------------
// awaitIndexing — no-op path tests
// ---------------------------------------------------------------------------

func TestAwaitIndexing_NilGate_IsNoop(t *testing.T) {
	c := newTestComponent(t)
	// Default component has no graph_gateway_url, so indexingGate is nil.
	if c.indexingGate != nil {
		t.Skip("indexingGate is unexpectedly set; skipping nil-gate test")
	}
	// Must not panic and must return immediately.
	c.awaitIndexing("abc123def456", "task-1")
}

func TestAwaitIndexing_EmptyCommitSHA_IsNoop(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{
		"graph_gateway_url": "http://localhost:8082",
	})
	deps := component.Dependencies{}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := comp.(*Component)

	// An empty commitSHA triggers the early-return guard in awaitIndexing.
	// Must not panic and must return immediately even with a non-nil gate.
	c.awaitIndexing("", "task-1")
}
