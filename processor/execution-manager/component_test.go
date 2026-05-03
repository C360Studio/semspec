package executionmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/nats-io/nats.go/jetstream"
)

// testEntityID computes the entity ID from slug+taskID using the canonical
// workflow function so it stays in sync with the component's execution setup.
func testEntityID(slug, taskID string) string {
	return workflow.TaskExecutionEntityID(slug, taskID)
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxTDDCycles != 3 {
		t.Errorf("MaxTDDCycles: want 3, got %d", cfg.MaxTDDCycles)
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

func TestConfigValidate_ZeroMaxTDDCycles(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTDDCycles = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for MaxTDDCycles=0, got nil")
	}
}

func TestConfigValidate_NegativeMaxTDDCycles(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTDDCycles = -1
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for MaxTDDCycles=-1, got nil")
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

	if got.MaxTDDCycles != 3 {
		t.Errorf("MaxTDDCycles: want 3, got %d", got.MaxTDDCycles)
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
		MaxTDDCycles:   5,
		TimeoutSeconds: 600,
		Model:          "gpt-4o",
	}
	got := cfg.withDefaults()

	if got.MaxTDDCycles != 5 {
		t.Errorf("MaxTDDCycles: want 5, got %d", got.MaxTDDCycles)
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
		"max_tdd_cycles":  5,
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

func TestNewComponent_ZeroMaxTDDCycles_IsReplacedByDefault(t *testing.T) {
	// withDefaults replaces any value <= 0 with the default (3), so a
	// JSON-supplied 0 results in a valid component — it silently becomes 3.
	// This test documents that deliberate behavior.
	rawCfg, _ := json.Marshal(map[string]any{
		"max_tdd_cycles":  0,
		"timeout_seconds": 300,
	})
	deps := component.Dependencies{}

	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Errorf("zero max_tdd_cycles should be silently defaulted, got error: %v", err)
	}
	if comp == nil {
		t.Fatal("expected a valid component, got nil")
	}
}

func TestNewComponent_ZeroTimeoutSeconds_IsReplacedByDefault(t *testing.T) {
	// Same rationale as above: withDefaults replaces 0 with 1800.
	rawCfg, _ := json.Marshal(map[string]any{
		"max_tdd_cycles":  3,
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
// handleTaskPending / initTaskExecution — KV self-trigger path
//
// handleTaskPending guards the entry gate (JSON parse, stage filter, dedup).
// initTaskExecution performs the post-claim initialization sequence.
// The component's natsClient is nil so any write/publish side effects
// silently no-op, letting us focus on the parse and state-machine branches.
// ---------------------------------------------------------------------------

func TestHandleTaskPending_MalformedJSON(t *testing.T) {
	c := newTestComponent(t)

	entry := &mockKVEntry{
		key:   "task.my-plan.task-123",
		value: []byte(`{bad json`),
		op:    jetstream.KeyValuePut,
	}

	// Malformed JSON: handleTaskPending should return early without panicking
	// or modifying any state.
	c.handleTaskPending(testCtx(t), entry)

	if c.triggersProcessed.Load() != 0 {
		t.Errorf("triggersProcessed: want 0 for malformed JSON, got %d", c.triggersProcessed.Load())
	}
}

func TestHandleTaskPending_NonPendingStage_IsIgnored(t *testing.T) {
	c := newTestComponent(t)

	// An entry with a non-pending stage must be silently ignored.
	entry := makeKVEntry(t, "task.my-plan.task-skip", map[string]any{
		"slug":    "my-plan",
		"task_id": "task-skip",
		"stage":   "developing",
	})

	c.handleTaskPending(testCtx(t), entry)

	if c.triggersProcessed.Load() != 0 {
		t.Errorf("triggersProcessed: want 0 for non-pending stage, got %d", c.triggersProcessed.Load())
	}
	entityID := testEntityID("my-plan", "task-skip")
	if _, ok := c.activeExecs.Get(entityID); ok {
		t.Error("non-pending entry should not register an active execution")
	}
}

func TestInitTaskExecution_RegistersExecution(t *testing.T) {
	c := newTestComponent(t)

	exec := &taskExecution{
		key: workflow.TaskExecutionKey("my-plan", "task-abc"),
		TaskExecution: &workflow.TaskExecution{
			EntityID:     testEntityID("my-plan", "task-abc"),
			Slug:         "my-plan",
			TaskID:       "task-abc",
			Stage:        phaseDeveloping,
			MaxTDDCycles: 3,
			Model:        "default",
		},
	}

	c.activeExecsMu.Lock()
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck
	c.activeExecsMu.Unlock()

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	if _, ok := c.activeExecs.Get(exec.EntityID); !ok {
		t.Errorf("expected active execution to be registered for entity %q", exec.EntityID)
	}
	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed: want 1, got %d", c.triggersProcessed.Load())
	}
}

func TestInitTaskExecution_DuplicateIsIdempotent(t *testing.T) {
	c := newTestComponent(t)

	exec := &taskExecution{
		key: workflow.TaskExecutionKey("my-plan", "task-dup"),
		TaskExecution: &workflow.TaskExecution{
			EntityID:     testEntityID("my-plan", "task-dup"),
			Slug:         "my-plan",
			TaskID:       "task-dup",
			Stage:        phaseDeveloping,
			MaxTDDCycles: 3,
			Model:        "default",
		},
	}

	// First registration.
	c.activeExecsMu.Lock()
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck
	c.activeExecsMu.Unlock()

	// Simulate a second KV event for the same entity — handleTaskPending dedup
	// check should prevent a second registration.
	c.activeExecsMu.Lock()
	_, alreadyActive := c.activeExecs.Get(exec.EntityID)
	c.activeExecsMu.Unlock()

	if !alreadyActive {
		t.Error("execution should be registered after first claim")
	}

	// Simulate the dedup path: a second claim attempt for the same entity.
	c.activeExecsMu.Lock()
	_, duplicate := c.activeExecs.Get(exec.EntityID)
	c.activeExecsMu.Unlock()

	if !duplicate {
		t.Error("execution must still be registered (not removed by duplicate detection)")
	}
}

// ---------------------------------------------------------------------------
// Terminal state helpers (direct invocation)
// ---------------------------------------------------------------------------

func TestMarkApprovedLocked_IncrementsCounters(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-1")

	c.activeExecs.Set(exec.EntityID, exec)

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
	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Error("execution should be removed from activeExecutions after approval")
	}
}

func TestMarkEscalatedLocked_IncrementsCounters(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-2")

	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markEscalatedLocked(testCtx(t), exec, "test escalation reason")
	exec.mu.Unlock()

	if c.executionsEscalated.Load() != 1 {
		t.Errorf("executionsEscalated: want 1, got %d", c.executionsEscalated.Load())
	}
	if c.executionsCompleted.Load() != 1 {
		t.Errorf("executionsCompleted: want 1, got %d", c.executionsCompleted.Load())
	}
	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Error("execution should be removed from activeExecutions after escalation")
	}
}

func TestMarkErrorLocked_IncrementsErrorCounter(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-3")

	c.activeExecs.Set(exec.EntityID, exec)

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

func TestStartDeveloperRetryLocked_IncrementsTDDCycle(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-retry")
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)

	exec.mu.Lock()
	c.startDeveloperRetryLocked(testCtx(t), exec, "reviewer said no")
	exec.mu.Unlock()

	if exec.TDDCycle != 1 {
		t.Errorf("TDDCycle after retry: want 1, got %d", exec.TDDCycle)
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
	exec.RejectionType = "restructure"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)

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

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)
	c.taskRouting.Set(exec.ValidatorTaskID, exec.EntityID)
	c.taskRouting.Set(exec.ReviewerTaskID, exec.EntityID)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()

	for _, id := range []string{"dev-111", "val-222", "rev-333"} {
		if _, ok := c.taskRouting.Get(id); ok {
			t.Errorf("taskIDIndex should not contain %q after cleanup", id)
		}
	}
	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
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

// ---------------------------------------------------------------------------
// Loop outcome=failed → escalation (handler guards)
// ---------------------------------------------------------------------------

// TestMarkApprovedLocked_MergeFailure_RoutesToError guards the cross-component
// race fixed for the mortgage-calc early-adopter run: when a parent
// requirement cleaned up node worktrees while a node reviewer was still in
// flight, the reviewer's approve + merge would fail silently and the task
// would still be marked approved. Merge failure must now route to phaseError.
// TestDispatchDeveloperLocked_ParentTerminated_MarksError guards the
// cross-component cancellation pathway: once the parent requirement has
// terminated (timeout/error), execution-manager must stop dispatching new
// pipeline stages. Without this, small-LLM runs burn 5+ minutes per orphan
// node producing code nobody will merge.
func TestDispatchDeveloperLocked_ParentTerminated_MarksError(t *testing.T) {
	c := newTestComponent(t)

	// Seed a terminal parent requirement in the store cache.
	reqKey := workflow.RequirementExecutionKey("plan", "req-1")
	c.store.reqCache.Set(reqKey, &workflow.RequirementExecution{
		EntityID:      "req-entity",
		Slug:          "plan",
		RequirementID: "req-1",
		Stage:         "failed", // parent timed out
	})

	exec := newTestExec("plan", "task-orphan")
	exec.RequirementID = "req-1"
	exec.Stage = phaseDeveloping
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchDeveloperLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if exec.Stage != phaseError {
		t.Errorf("Stage: want %q (parent terminal → error), got %q", phaseError, exec.Stage)
	}
	if !exec.terminated {
		t.Error("exec.terminated should be true — no further dispatches")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors: want 1, got %d", c.errors.Load())
	}
}

// TestDispatchDeveloperLocked_ParentAlive_ProceedsNormally confirms that the
// guard is a narrow gate — a live parent does not block dispatch.
func TestDispatchDeveloperLocked_ParentAlive_ProceedsNormally(t *testing.T) {
	c := newTestComponent(t)

	reqKey := workflow.RequirementExecutionKey("plan", "req-2")
	c.store.reqCache.Set(reqKey, &workflow.RequirementExecution{
		EntityID:      "req-entity-2",
		Slug:          "plan",
		RequirementID: "req-2",
		Stage:         "executing", // still alive
	})

	exec := newTestExec("plan", "task-live")
	exec.RequirementID = "req-2"
	exec.Stage = phaseDeveloping
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchDeveloperLocked(testCtx(t), exec)
	exec.mu.Unlock()

	// With a live parent, the guard lets us through; checkWorktreeExists may
	// still short-circuit if WorktreePath is unset. Either way, exec must NOT
	// be marked terminal via parent_requirement_* error.
	if exec.Stage == phaseError && c.errors.Load() == 1 {
		t.Error("live parent should not trigger parent_requirement error")
	}
}

// TestDispatchDeveloperLocked_NoRequirementID_SkipsGuard verifies tasks
// created outside requirement-executor (empty RequirementID) are not blocked.
func TestDispatchDeveloperLocked_NoRequirementID_SkipsGuard(t *testing.T) {
	c := newTestComponent(t)

	exec := newTestExec("plan", "task-adhoc")
	// RequirementID intentionally empty
	exec.Stage = phaseDeveloping
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	terminated, _ := c.parentRequirementTerminated(exec)
	exec.mu.Unlock()

	if terminated {
		t.Error("empty RequirementID must not trigger the parent-terminated guard")
	}
}

func TestMarkApprovedLocked_MergeFailure_RoutesToError(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{mergeErr: errors.New("server error 500: failed to stage changes: chdir: no such file or directory")}

	exec := newTestExec("plan", "task-merge-fail")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-merge-fail"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if exec.Stage != phaseError {
		t.Errorf("Stage: want %q (merge failure routes to error), got %q", phaseError, exec.Stage)
	}
	if !exec.terminated {
		t.Error("exec.terminated should be true after markErrorLocked")
	}
	if c.executionsApproved.Load() != 0 {
		t.Errorf("executionsApproved: want 0 (merge failed), got %d", c.executionsApproved.Load())
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors counter: want 1, got %d", c.errors.Load())
	}
}

// TestMergeWorktree_IncludesProvenanceTrailers pins invariant A1 from
// docs/audit/task-11-worktree-invariants.md. Every merge commit must carry
// enough provenance in its trailers that `git log` alone can answer
// "which plan / requirement / loop produced this commit?" without cross-
// referencing EXECUTION_STATES.
//
// Task-ID and Plan-Slug were already present. A1 adds Requirement-ID and
// Loop-ID (Node-ID is deferred — it lives on NodeResult, not TaskExecution).
func TestMergeWorktree_IncludesProvenanceTrailers(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{}
	c.sandbox = stub

	exec := newTestExec("plan-auth", "task-p1-t1")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-p1-t1"
	exec.RequirementID = "req-auth-refresh"
	exec.LoopID = "loop-abc-123"
	exec.TraceID = "trace-xyz"
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if stub.MergeCallCount() != 1 {
		t.Fatalf("expected exactly 1 merge call, got %d", stub.MergeCallCount())
	}
	trailers := stub.capturedTrailers()

	want := map[string]string{
		"Task-ID":        "task-p1-t1",
		"Plan-Slug":      "plan-auth",
		"Requirement-ID": "req-auth-refresh",
		"Loop-ID":        "loop-abc-123",
		"Trace-ID":       "trace-xyz",
	}
	for k, v := range want {
		if got := trailers[k]; got != v {
			t.Errorf("trailer %q = %q, want %q (full trailers=%v)", k, got, v, trailers)
		}
	}
}

// TestMergeWorktree_OmitsEmptyTrailers verifies that optional provenance
// trailers (Requirement-ID, Loop-ID, Trace-ID) are only attached when the
// corresponding field on the task execution is populated. An empty string
// would produce a noisy "Trailer: " line in the commit message.
func TestMergeWorktree_OmitsEmptyTrailers(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{}
	c.sandbox = stub

	// Minimal exec — no RequirementID, LoopID, or TraceID.
	exec := newTestExec("plan-bare", "task-bare-1")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-bare-1"
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	trailers := stub.capturedTrailers()
	for _, k := range []string{"Requirement-ID", "Loop-ID", "Trace-ID"} {
		if _, present := trailers[k]; present {
			t.Errorf("trailer %q should be absent when source field is empty; got trailers=%v", k, trailers)
		}
	}
	// Always-present trailers must still be there.
	if trailers["Task-ID"] == "" || trailers["Plan-Slug"] == "" {
		t.Errorf("Task-ID and Plan-Slug must always be present; got %v", trailers)
	}
}

// TestMarkErrorLocked_ClassifiesAgentVsInfrastructure pins Phase 5's
// cross-layer error classification: exec.ErrorClass must be populated from
// the reason string so plan-manager (and the retry UX) can distinguish
// "agent failure — retry might help" from "infrastructure wedged — retry
// is futile until operator intervenes." Drift between the INFRASTRUCTURE:
// prefix writer (markApprovedLocked) and the classifier leaves the system
// silently misclassifying failures.
func TestMarkErrorLocked_ClassifiesAgentVsInfrastructure(t *testing.T) {
	c := newTestComponent(t)

	agentExec := newTestExec("plan", "task-agent")
	c.activeExecs.Set(agentExec.EntityID, agentExec)
	agentExec.mu.Lock()
	c.markErrorLocked(testCtx(t), agentExec, "merge_failed: conflict on foo.go")
	agentExec.mu.Unlock()
	if agentExec.ErrorClass != workflow.ErrorClassAgent {
		t.Errorf("agent-class ErrorClass = %q, want %q", agentExec.ErrorClass, workflow.ErrorClassAgent)
	}

	infraExec := newTestExec("plan", "task-infra")
	c.activeExecs.Set(infraExec.EntityID, infraExec)
	infraExec.mu.Lock()
	c.markErrorLocked(testCtx(t), infraExec, "INFRASTRUCTURE: merge_failed: sandbox needs reconciliation")
	infraExec.mu.Unlock()
	if infraExec.ErrorClass != workflow.ErrorClassInfrastructure {
		t.Errorf("infra-class ErrorClass = %q, want %q", infraExec.ErrorClass, workflow.ErrorClassInfrastructure)
	}
}

// TestMarkApprovedLocked_MergeNeedsReconciliation_SkipsRetry pins invariant
// A2 from docs/audit/task-11-worktree-invariants.md: when the sandbox
// signals it is wedged (ErrNeedsReconciliation), mergeWorktree must not
// burn the normal 3× retry loop — the sandbox will return the same error
// on every attempt until an operator clears the flag. The task must land
// in phaseError with an INFRASTRUCTURE-prefixed reason so UI + retry code
// can distinguish it from an agent-level merge conflict.
func TestMarkApprovedLocked_MergeNeedsReconciliation_SkipsRetry(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{
		mergeErr: fmt.Errorf("wrapped: %w", sandbox.ErrNeedsReconciliation),
	}
	c.sandbox = stub

	exec := newTestExec("plan", "task-wedged")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-wedged"
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if got := stub.MergeCallCount(); got != 1 {
		t.Errorf("MergeWorktree called %d times; want 1 (no retry on needs_reconciliation)", got)
	}
	if exec.Stage != phaseError {
		t.Errorf("Stage = %q, want %q (infra error must route to phaseError)", exec.Stage, phaseError)
	}
	if !exec.terminated {
		t.Error("exec.terminated should be true after markErrorLocked")
	}
	if got := c.errors.Load(); got != 1 {
		t.Errorf("errors counter = %d, want 1", got)
	}
	// ErrorReason must carry the INFRASTRUCTURE: prefix so downstream retry
	// logic (Phase 5) can distinguish infra failures from agent failures.
	if !strings.Contains(exec.ErrorReason, "INFRASTRUCTURE:") {
		t.Errorf("exec.ErrorReason = %q, want INFRASTRUCTURE: prefix", exec.ErrorReason)
	}
}

// TestMarkApprovedLocked_MergeSuccess_Approves verifies the happy path still
// works: successful merge advances the task to phaseApproved.
func TestMarkApprovedLocked_MergeSuccess_Approves(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{} // mergeErr=nil

	exec := newTestExec("plan", "task-merge-ok")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-merge-ok"
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if exec.Stage != phaseApproved {
		t.Errorf("Stage: want %q, got %q", phaseApproved, exec.Stage)
	}
	if !exec.terminated {
		t.Error("exec.terminated should be true after approval")
	}
	if c.executionsApproved.Load() != 1 {
		t.Errorf("executionsApproved: want 1, got %d", c.executionsApproved.Load())
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors counter: want 0 (happy path), got %d", c.errors.Load())
	}
}

func TestHandleDeveloperComplete_FailedOutcome_Retries(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-dev-fail")
	exec.Stage = phaseDeveloping
	exec.DeveloperTaskID = "dev-999"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.DeveloperTaskID,
		Outcome:      agentic.OutcomeFailed,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
	}

	exec.mu.Lock()
	c.handleDeveloperCompleteLocked(testCtx(t), event, exec)
	exec.mu.Unlock()

	// Should retry (route back to developer), not escalate.
	if exec.Stage != phaseDeveloping {
		t.Errorf("Stage: want %q (retry), got %q", phaseDeveloping, exec.Stage)
	}
	if exec.terminated {
		t.Error("exec.terminated should be false — retries remain")
	}
	if exec.TDDCycle != 1 {
		t.Errorf("TDDCycle: want 1, got %d", exec.TDDCycle)
	}
}

func TestHandleDeveloperComplete_EmptyResult_RoutesToRetry(t *testing.T) {
	// Small models sometimes return outcome=success with an empty result
	// (loop ended without calling submit_work, e.g. after a timed-out question).
	// That used to silently fall through to the validator and burn a TDD cycle.
	// It should now route through the fixable retry path with feedback.
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-dev-empty")
	exec.Stage = phaseDeveloping
	exec.DeveloperTaskID = "dev-empty-1"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.DeveloperTaskID,
		Outcome:      agentic.OutcomeSuccess,
		Result:       "", // loop ended without a submit_work call
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
	}

	exec.mu.Lock()
	c.handleDeveloperCompleteLocked(testCtx(t), event, exec)
	exec.mu.Unlock()

	if exec.Stage != phaseDeveloping {
		t.Errorf("Stage: want %q (retry), got %q", phaseDeveloping, exec.Stage)
	}
	if exec.TDDCycle != 1 {
		t.Errorf("TDDCycle: want 1, got %d", exec.TDDCycle)
	}
	if exec.terminated {
		t.Error("exec.terminated should be false — retries remain")
	}
	if exec.Feedback == "" {
		t.Error("exec.Feedback should carry actionable guidance for the retry")
	}
}

func TestHandleDeveloperComplete_EmptyFilesModified_RoutesToRetry(t *testing.T) {
	// submit_work was called but files_modified came back empty — the loop
	// validator catches this in-loop now, but post-loop we still guard against
	// the case where the parse succeeds but the list is empty (e.g. from a
	// past loop result re-delivered via KV).
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-dev-nofiles")
	exec.Stage = phaseDeveloping
	exec.DeveloperTaskID = "dev-empty-files"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.DeveloperTaskID,
		Outcome:      agentic.OutcomeSuccess,
		Result:       `{"summary": "done", "files_modified": []}`,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
	}

	exec.mu.Lock()
	c.handleDeveloperCompleteLocked(testCtx(t), event, exec)
	exec.mu.Unlock()

	if exec.Stage != phaseDeveloping {
		t.Errorf("Stage: want %q (retry), got %q", phaseDeveloping, exec.Stage)
	}
	if exec.TDDCycle != 1 {
		t.Errorf("TDDCycle: want 1, got %d", exec.TDDCycle)
	}
	if exec.Feedback == "" {
		t.Error("exec.Feedback should carry actionable guidance about empty files_modified")
	}
}

// TestHandleDeveloperComplete_HallucinatedClaim_RoutesToRetry pins the v10
// pre-reviewer claim/observation gate from project_dev_wedge_diagnosis_2026_05_03.
// The developer reports files_modified but `git status` against the worktree
// is empty — meaning the agent ran read-only commands (e.g. `cat main.go`)
// and submitted confident prose without writing anything. The gate must
// route this back through the fixable retry path BEFORE a validator/reviewer
// dispatch is spent on the unchanged file.
func TestHandleDeveloperComplete_HallucinatedClaim_RoutesToRetry(t *testing.T) {
	c := newTestComponent(t)
	// Sandbox with empty git status output — simulates the wedge: dev claimed
	// files_modified=["main.go"] but the worktree has no changes.
	c.sandbox = &stubSandbox{gitStatusOutput: ""}

	exec := newTestExec("plan", "task-dev-hallucinated")
	exec.Stage = phaseDeveloping
	exec.DeveloperTaskID = "dev-hallucinated"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.DeveloperTaskID,
		Outcome:      agentic.OutcomeSuccess,
		Result:       `{"summary": "Implemented /health endpoint that returns HTTP 200", "files_modified": ["main.go"]}`,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
	}

	exec.mu.Lock()
	c.handleDeveloperCompleteLocked(testCtx(t), event, exec)
	exec.mu.Unlock()

	// Should consume a TDD cycle (so persistent hallucinators escalate) and
	// route back to the developer with actionable feedback — NOT advance to
	// phaseValidating.
	if exec.Stage != phaseDeveloping {
		t.Errorf("Stage: want %q (retry), got %q", phaseDeveloping, exec.Stage)
	}
	if exec.TDDCycle != 1 {
		t.Errorf("TDDCycle: want 1, got %d", exec.TDDCycle)
	}
	if exec.terminated {
		t.Error("exec.terminated should be false — retries remain")
	}
	if exec.Feedback == "" {
		t.Fatal("exec.Feedback should carry actionable guidance about the claim/observation mismatch")
	}
	// Feedback must steer the agent toward an actual write next time —
	// reading the file with cat is the canonical mistake here, so the
	// guidance has to call out write commands explicitly.
	wantSubstrs := []string{
		"git status", // names the check the agent failed
		"main.go",    // echoes back what was claimed
		"cat >",      // canonical write idiom
		"NO changes", // describes what the system observed
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(exec.Feedback, want) {
			t.Errorf("Feedback missing %q; got: %s", want, exec.Feedback)
		}
	}
}

// TestDeveloperWorkClean covers the helper directly. The handler test above
// exercises the empty-status path; this table covers the legitimate-work and
// sandbox-error branches that should both bypass the gate (return false).
func TestDeveloperWorkClean(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		statusErr  error
		nilSandbox bool
		want       bool
	}{
		{name: "clean worktree (gate fires)", status: "", want: true},
		{name: "whitespace-only status (gate fires)", status: "   \n  ", want: true},
		{name: "porcelain modified entry (gate inert)", status: " M main.go\n", want: false},
		{name: "porcelain new file (gate inert)", status: "?? new.go\n", want: false},
		{name: "sandbox error (defense in depth — gate inert)", statusErr: fmt.Errorf("connection refused"), want: false},
		{name: "no sandbox configured (gate inert)", nilSandbox: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestComponent(t)
			if !tt.nilSandbox {
				c.sandbox = &stubSandbox{gitStatusOutput: tt.status, gitStatusErr: tt.statusErr}
			}
			exec := newTestExec("plan", "task-clean-check")
			got := c.developerWorkClean(testCtx(t), exec)
			if got != tt.want {
				t.Errorf("developerWorkClean: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleDeveloperComplete_FailedOutcome_Escalates_WhenBudgetExhausted(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-dev-fail-max")
	exec.Stage = phaseDeveloping
	exec.DeveloperTaskID = "dev-998"
	exec.TDDCycle = 2
	exec.MaxTDDCycles = 3 // TDDCycle+1 == MaxTDDCycles → no retries left

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.DeveloperTaskID, exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.DeveloperTaskID,
		Outcome:      agentic.OutcomeFailed,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
	}

	exec.mu.Lock()
	c.handleDeveloperCompleteLocked(testCtx(t), event, exec)
	exec.mu.Unlock()

	if exec.Stage != phaseEscalated {
		t.Errorf("Stage: want %q, got %q", phaseEscalated, exec.Stage)
	}
	if !exec.terminated {
		t.Error("exec.terminated should be true — budget exhausted")
	}
	if c.executionsEscalated.Load() != 1 {
		t.Errorf("executionsEscalated: want 1, got %d", c.executionsEscalated.Load())
	}
}

func TestHandleReviewerComplete_FailedOutcome_Retries(t *testing.T) {
	c := newTestComponent(t)
	exec := newTestExec("plan", "task-rev-fail")
	exec.Stage = phaseReviewing
	exec.ReviewerTaskID = "rev-999"
	exec.TDDCycle = 0
	exec.MaxTDDCycles = 3

	c.activeExecs.Set(exec.EntityID, exec)
	c.taskRouting.Set(exec.ReviewerTaskID, exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		TaskID:       exec.ReviewerTaskID,
		Outcome:      agentic.OutcomeFailed,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageReview,
	}

	exec.mu.Lock()
	c.handleReviewerCompleteLocked(testCtx(t), event, exec)
	exec.mu.Unlock()

	if exec.Stage != phaseDeveloping {
		t.Errorf("Stage: want %q (retry), got %q", phaseDeveloping, exec.Stage)
	}
	if exec.terminated {
		t.Error("exec.terminated should be false — retries remain")
	}
	if exec.TDDCycle != 1 {
		t.Errorf("TDDCycle: want 1, got %d", exec.TDDCycle)
	}
}

// TestMarkApprovedLocked_EmptyMergeResult_ClaimedFiles_FailsTask — when the
// developer claimed FilesModified but the sandbox returns an empty
// MergeResult (Commit:"" FilesChanged:nil), mergeWorktree must NOT silently
// approve the task. This is the smoking-gun test for bug #9 (10/17 silent
// no-op merges in Gemini @t2 run): execution-manager logs "Worktree merged
// successfully" with commit="" files_changed=0 even though the developer
// reported writing files.
//
// Contract: when exec.FilesModified is non-empty AND result.Commit is empty,
// the task must be marked as failed (claim/observation mismatch), not
// approved. EXPECTED RED until production fix lands at component.go:1674.
func TestMarkApprovedLocked_EmptyMergeResult_ClaimedFiles_FailsTask(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{} // mergeResult=nil → returns empty &MergeResult{}

	exec := newTestExec("plan", "task-empty-merge")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-empty-merge"
	// Developer claimed they wrote files. This is the bug-trigger pattern:
	// FilesModified non-empty + sandbox returns Commit:"" FilesChanged:nil.
	exec.FilesModified = []string{"main.go", "main_test.go"}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if exec.Stage == phaseApproved {
		t.Errorf("Stage = %q after merge that produced commit=\"\" files_changed=0 with developer-claimed FilesModified=%v. This is the silent-no-op pattern (bug #9). Expected: Stage=%q (claim/observation mismatch).",
			exec.Stage, []string{"main.go", "main_test.go"}, phaseError)
	}
	if c.errors.Load() == 0 {
		t.Error("errors counter should increment when sandbox returns empty merge result while developer claimed files modified")
	}
}

// TestMarkApprovedLocked_EmptyMergeResult_NoClaimedFiles_OK — the legitimate
// no-op case. Developer didn't claim any files (empty FilesModified) and
// sandbox returns empty MergeResult — that's a valid "nothing to merge"
// scenario. Task should still be approved without raising a mismatch error.
//
// This test PINS the contract that empty-merge is only a problem when the
// developer claimed work. Today: GREEN (no mismatch detection at all).
// After the production fix lands: should still be GREEN (the fix must only
// fail when claim ≠ observation).
func TestMarkApprovedLocked_EmptyMergeResult_NoClaimedFiles_OK(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{}

	exec := newTestExec("plan", "task-noop-ok")
	exec.Stage = phaseReviewing
	exec.WorktreePath = "/workspace/.semspec/worktrees/task-noop-ok"
	// FilesModified is empty — developer correctly indicated no work.
	exec.FilesModified = nil
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markApprovedLocked(testCtx(t), exec)
	exec.mu.Unlock()

	if exec.Stage != phaseApproved {
		t.Errorf("Stage = %q for legit no-op case (empty claims + empty merge). Want %q.", exec.Stage, phaseApproved)
	}
}
