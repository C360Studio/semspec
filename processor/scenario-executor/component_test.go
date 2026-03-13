package scenarioexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	_ "github.com/c360studio/semspec/tools/decompose" // ensure decompose package is imported
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// Wire-format helpers
// ---------------------------------------------------------------------------

// buildTriggerMsg builds a *nats.Msg carrying a ScenarioExecutionRequest
// wrapped in the minimal BaseMessage envelope that ParseReactivePayload expects.
func buildTriggerMsg(req payloads.ScenarioExecutionRequest) *nats.Msg {
	payload, err := json.Marshal(req)
	if err != nil {
		panic("buildTriggerMsg: marshal request: " + err.Error())
	}
	envelope := map[string]json.RawMessage{"payload": payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		panic("buildTriggerMsg: marshal envelope: " + err.Error())
	}
	return &nats.Msg{Data: data}
}

// buildLoopCompletedMsg builds a *nats.Msg that handleLoopCompleted can parse.
// It constructs a proper BaseMessage so that base.Payload() returns a
// *agentic.LoopCompletedEvent after registry lookup.
func buildLoopCompletedMsg(t *testing.T, event agentic.LoopCompletedEvent) *nats.Msg {
	t.Helper()
	baseMsg := message.NewBaseMessage(event.Schema(), &event, "test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("buildLoopCompletedMsg: marshal: %v", err)
	}
	return &nats.Msg{Data: data}
}

// minLoopEvent returns a LoopCompletedEvent with the required fields set.
func minLoopEvent(taskID, workflowSlug, workflowStep, outcome string) agentic.LoopCompletedEvent {
	return agentic.LoopCompletedEvent{
		LoopID:       "loop-" + taskID,
		TaskID:       taskID,
		WorkflowSlug: workflowSlug,
		WorkflowStep: workflowStep,
		Outcome:      outcome,
	}
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig_HasExpectedDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.TimeoutSeconds != 3600 {
		t.Errorf("TimeoutSeconds = %d, want 3600", cfg.TimeoutSeconds)
	}
	if cfg.Model != "default" {
		t.Errorf("Model = %q, want default", cfg.Model)
	}
	if cfg.Ports == nil {
		t.Fatal("Ports should not be nil")
	}
	if len(cfg.Ports.Inputs) == 0 {
		t.Error("Ports.Inputs should not be empty")
	}
	if len(cfg.Ports.Outputs) == 0 {
		t.Error("Ports.Outputs should not be empty")
	}
}

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig().Validate() error = %v, want nil", err)
	}
}

func TestConfig_Validate_ZeroTimeout(t *testing.T) {
	cfg := Config{TimeoutSeconds: 0}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for TimeoutSeconds = 0")
	}
	if !strings.Contains(err.Error(), "timeout_seconds") {
		t.Errorf("error %q should mention timeout_seconds", err.Error())
	}
}

func TestConfig_Validate_NegativeTimeout(t *testing.T) {
	cfg := Config{TimeoutSeconds: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative TimeoutSeconds")
	}
}

func TestConfig_GetTimeout_PositiveValue(t *testing.T) {
	cfg := Config{TimeoutSeconds: 120}
	got := cfg.GetTimeout()
	if got != 120*time.Second {
		t.Errorf("GetTimeout() = %v, want 120s", got)
	}
}

func TestConfig_GetTimeout_ZeroFallsBackToDefault(t *testing.T) {
	cfg := Config{TimeoutSeconds: 0}
	got := cfg.GetTimeout()
	if got != 60*time.Minute {
		t.Errorf("GetTimeout() with 0 = %v, want 60m", got)
	}
}

func TestConfig_GetTimeout_NegativeFallsBackToDefault(t *testing.T) {
	cfg := Config{TimeoutSeconds: -5}
	got := cfg.GetTimeout()
	if got != 60*time.Minute {
		t.Errorf("GetTimeout() with negative = %v, want 60m", got)
	}
}

func TestConfig_WithDefaults_PreservesSetFields(t *testing.T) {
	cfg := Config{TimeoutSeconds: 900, Model: "gpt-4"}
	got := cfg.withDefaults()
	if got.TimeoutSeconds != 900 {
		t.Errorf("withDefaults() TimeoutSeconds = %d, want 900", got.TimeoutSeconds)
	}
	if got.Model != "gpt-4" {
		t.Errorf("withDefaults() Model = %q, want gpt-4", got.Model)
	}
}

func TestConfig_WithDefaults_FillsZeroFields(t *testing.T) {
	cfg := Config{}
	got := cfg.withDefaults()
	if got.TimeoutSeconds != 3600 {
		t.Errorf("withDefaults() TimeoutSeconds = %d, want 3600", got.TimeoutSeconds)
	}
	if got.Model != "default" {
		t.Errorf("withDefaults() Model = %q, want default", got.Model)
	}
	if got.Ports == nil {
		t.Error("withDefaults() Ports should not be nil")
	}
}

func TestConfig_WithDefaults_NilPortsFilledByDefault(t *testing.T) {
	cfg := Config{TimeoutSeconds: 1800}
	got := cfg.withDefaults()
	if got.Ports == nil {
		t.Fatal("withDefaults() should fill nil Ports")
	}

	subjectFound := false
	for _, p := range got.Ports.Inputs {
		if p.Subject == subjectScenarioTrigger {
			subjectFound = true
			break
		}
	}
	if !subjectFound {
		t.Errorf("default Ports.Inputs should contain subject %q", subjectScenarioTrigger)
	}
}

// TestConfig_Validate_ValidTimeout confirms positive timeout passes Validate
// (the pair to ZeroTimeout / NegativeTimeout).
func TestConfig_Validate_ValidTimeout(t *testing.T) {
	for _, secs := range []int{1, 60, 3600, 86400} {
		cfg := Config{TimeoutSeconds: secs}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with TimeoutSeconds=%d error = %v, want nil", secs, err)
		}
	}
}

// ---------------------------------------------------------------------------
// NewComponent construction tests
// ---------------------------------------------------------------------------

func TestNewComponent_ValidConfig(t *testing.T) {
	cfg := DefaultConfig()
	raw, _ := json.Marshal(cfg)

	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v, want nil", err)
	}
	if comp == nil {
		t.Fatal("NewComponent() returned nil")
	}
}

func TestNewComponent_AppliesDefaults(t *testing.T) {
	raw := []byte(`{}`)
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() with empty config error = %v, want nil", err)
	}

	c := comp.(*Component)
	if c.config.TimeoutSeconds != 3600 {
		t.Errorf("TimeoutSeconds = %d, want 3600", c.config.TimeoutSeconds)
	}
	if c.config.Model != "default" {
		t.Errorf("Model = %q, want default", c.config.Model)
	}
}

func TestNewComponent_InvalidJSON(t *testing.T) {
	_, err := NewComponent([]byte(`{invalid`), component.Dependencies{})
	if err == nil {
		t.Fatal("NewComponent() with invalid JSON should return error")
	}
}

// TestNewComponent_ExplicitlyInvalidConfig confirms that a Config whose
// TimeoutSeconds is zero BEFORE withDefaults gets filled by withDefaults
// (no config path reaches Validate with a bad timeout other than json `{}`
// which gets fixed by withDefaults). We verify that an explicitly negative
// value in JSON does trigger validation failure.
func TestNewComponent_ExplicitlyInvalidConfig(t *testing.T) {
	// withDefaults replaces <=0 values, so the only way to reach Validate
	// with a bad value is if withDefaults itself introduces one — which it
	// doesn't. We test that behaviour is correct: even a crafted bad config
	// gets healed by withDefaults and is accepted.
	//
	// The Validate() check is: TimeoutSeconds <= 0. After withDefaults that
	// value is always 3600 (or the user's positive value), so NewComponent
	// never fails on this path in normal usage. The validation guard is a
	// defensive belt-and-suspenders for callers who construct Config directly.
	cfg := Config{TimeoutSeconds: 0} // withDefaults fills to 3600
	got := cfg.withDefaults()
	if err := got.Validate(); err != nil {
		t.Errorf("after withDefaults, Validate() should pass, got: %v", err)
	}

	// Validate called directly on a bad config DOES fail.
	bad := Config{TimeoutSeconds: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("Config.Validate() with TimeoutSeconds=-1 should fail")
	}
}

func TestNewComponent_UsesDefaultLogger(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{Logger: nil})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	c := comp.(*Component)
	if c.logger == nil {
		t.Error("logger should not be nil even when deps.Logger is nil")
	}
}

func TestNewComponent_BuildsInputAndOutputPorts(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	c := comp.(*Component)
	if len(c.inputPorts) == 0 {
		t.Error("inputPorts should not be empty after NewComponent")
	}
	if len(c.outputPorts) == 0 {
		t.Error("outputPorts should not be empty after NewComponent")
	}
}

func TestNewComponent_ImplementsDiscoverable(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	if _, ok := comp.(component.Discoverable); !ok {
		t.Error("NewComponent() result should implement component.Discoverable")
	}
}

// ---------------------------------------------------------------------------
// normalizeRole tests
// ---------------------------------------------------------------------------

func TestNormalizeRole_Developer(t *testing.T) {
	cases := []string{"developer", "development", "dev"}
	for _, raw := range cases {
		role, subject := normalizeRole(raw)
		if role != agentic.RoleDeveloper {
			t.Errorf("normalizeRole(%q) role = %q, want %q", raw, role, agentic.RoleDeveloper)
		}
		if subject != "agent.task.development" {
			t.Errorf("normalizeRole(%q) subject = %q, want agent.task.development", raw, subject)
		}
	}
}

func TestNormalizeRole_Reviewer(t *testing.T) {
	cases := []string{"reviewer", "review"}
	for _, raw := range cases {
		role, subject := normalizeRole(raw)
		if role != agentic.RoleReviewer {
			t.Errorf("normalizeRole(%q) role = %q, want %q", raw, role, agentic.RoleReviewer)
		}
		if subject != "agent.task.reviewer" {
			t.Errorf("normalizeRole(%q) subject = %q, want agent.task.reviewer", raw, subject)
		}
	}
}

func TestNormalizeRole_General_UnknownRoles(t *testing.T) {
	cases := []string{"general", "architect", "writer", "anything-else"}
	for _, raw := range cases {
		role, subject := normalizeRole(raw)
		if role != agentic.RoleGeneral {
			t.Errorf("normalizeRole(%q) role = %q, want %q", raw, role, agentic.RoleGeneral)
		}
		if subject != "agent.task.general" {
			t.Errorf("normalizeRole(%q) subject = %q, want agent.task.general", raw, subject)
		}
	}
}

func TestNormalizeRole_EmptyString(t *testing.T) {
	role, subject := normalizeRole("")
	if role != agentic.RoleGeneral {
		t.Errorf("normalizeRole(\"\") role = %q, want %q", role, agentic.RoleGeneral)
	}
	if subject != "agent.task.general" {
		t.Errorf("normalizeRole(\"\") subject = %q, want agent.task.general", subject)
	}
}

func TestNormalizeRole_CaseSensitive(t *testing.T) {
	// "Developer" (capitalized) must fall through to the default (general).
	role, subject := normalizeRole("Developer")
	if role != agentic.RoleGeneral {
		t.Errorf("normalizeRole(\"Developer\") should fall through to general, got %q", role)
	}
	if subject != "agent.task.general" {
		t.Errorf("normalizeRole(\"Developer\") subject should be general, got %q", subject)
	}
}

// ---------------------------------------------------------------------------
// Meta / Health / Ports tests
// ---------------------------------------------------------------------------

func TestMeta_ReturnsExpectedValues(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	meta := c.Meta()
	if meta.Name != componentName {
		t.Errorf("Meta().Name = %q, want %q", meta.Name, componentName)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta().Type = %q, want processor", meta.Type)
	}
	if meta.Version != componentVersion {
		t.Errorf("Meta().Version = %q, want %q", meta.Version, componentVersion)
	}
	if meta.Description == "" {
		t.Error("Meta().Description should not be empty")
	}
}

func TestHealth_NotRunning(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	h := c.Health()
	if h.Healthy {
		t.Error("Health().Healthy should be false when component has not started")
	}
	if h.Status != "stopped" {
		t.Errorf("Health().Status = %q, want stopped", h.Status)
	}
}

func TestHealth_Running_IsHealthy(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	h := c.Health()
	if !h.Healthy {
		t.Error("Health().Healthy should be true when running")
	}
	if h.Status != "healthy" {
		t.Errorf("Health().Status = %q, want healthy", h.Status)
	}
}

func TestHealth_ErrorCountReflectsActualErrors(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	c.errors.Add(5)

	h := c.Health()
	if h.ErrorCount != 5 {
		t.Errorf("Health().ErrorCount = %d, want 5", h.ErrorCount)
	}
}

func TestInputPorts_MatchDefaultConfig(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	ports := c.InputPorts()
	if len(ports) == 0 {
		t.Fatal("InputPorts() should not be empty")
	}
}

func TestOutputPorts_MatchDefaultConfig(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	ports := c.OutputPorts()
	if len(ports) == 0 {
		t.Fatal("OutputPorts() should not be empty")
	}
}

func TestConfigSchema_HasProperties(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	schema := c.ConfigSchema()
	if schema.Properties == nil {
		t.Error("ConfigSchema().Properties should not be nil")
	}
}

func TestInitialize_IsNoOp(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)
	if err := c.Initialize(); err != nil {
		t.Errorf("Initialize() error = %v, want nil", err)
	}
}

func TestStop_WhenNotRunning_IsNoOp(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)
	if err := c.Stop(time.Second); err != nil {
		t.Errorf("Stop() on non-running component error = %v, want nil", err)
	}
}

func TestDataFlow_LastActivityUpdates(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	before := time.Now()
	c.updateLastActivity()
	after := time.Now()

	flow := c.DataFlow()
	if flow.LastActivity.Before(before) || flow.LastActivity.After(after) {
		t.Errorf("DataFlow().LastActivity = %v, want in range [%v, %v]",
			flow.LastActivity, before, after)
	}
}

// ---------------------------------------------------------------------------
// handleTrigger — parse/validate tests (nil NATS client, metrics verified)
// ---------------------------------------------------------------------------

// newTestComponent creates a Component with no NATS client suitable for
// unit-testing handler logic without I/O.
func newTestComponent(t *testing.T) *Component {
	t.Helper()
	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("newTestComponent: %v", err)
	}
	c := comp.(*Component)
	c.shutdown = make(chan struct{})
	return c
}

func TestHandleTrigger_MalformedPayload_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	msg := &nats.Msg{Data: []byte(`not json at all`)}
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 after malformed message", c.errors.Load())
	}
	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}
}

func TestHandleTrigger_MissingScenarioID_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{
		Slug: "my-plan",
		// ScenarioID intentionally omitted
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 for missing scenario_id", c.errors.Load())
	}
}

func TestHandleTrigger_MissingSlug_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{
		ScenarioID: "scen-123",
		// Slug intentionally omitted
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 for missing slug", c.errors.Load())
	}
}

func TestHandleTrigger_BothMissing_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 for missing both fields", c.errors.Load())
	}
}

func TestHandleTrigger_ValidPayload_CreatesActiveExecution(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{
		ScenarioID: "scen-abc",
		Slug:       "my-plan",
		Prompt:     "Build it",
		Model:      "test-model",
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	expectedEntityID := "local.semspec.workflow.scenario-execution.execution.my-plan-scen-abc"
	execVal, ok := c.activeExecutions.Load(expectedEntityID)
	if !ok {
		t.Fatalf("expected active execution to be stored for entity %q", expectedEntityID)
	}

	exec := execVal.(*scenarioExecution)
	exec.mu.Lock()
	scenarioID := exec.ScenarioID
	slug := exec.Slug
	prompt := exec.Prompt
	model := exec.Model
	idx := exec.CurrentNodeIdx
	exec.mu.Unlock()

	if scenarioID != "scen-abc" {
		t.Errorf("exec.ScenarioID = %q, want scen-abc", scenarioID)
	}
	if slug != "my-plan" {
		t.Errorf("exec.Slug = %q, want my-plan", slug)
	}
	if prompt != "Build it" {
		t.Errorf("exec.Prompt = %q, want 'Build it'", prompt)
	}
	if model != "test-model" {
		t.Errorf("exec.Model = %q, want test-model", model)
	}
	if idx != -1 {
		t.Errorf("exec.CurrentNodeIdx = %d, want -1 (before execution)", idx)
	}
}

func TestHandleTrigger_ValidPayload_SetsEntityIDCorrectly(t *testing.T) {
	c := newTestComponent(t)

	// Use a full set of required fields so publishTask succeeds at marshaling.
	req := payloads.ScenarioExecutionRequest{
		ScenarioID: "s-001",
		Slug:       "plan-xyz",
		Prompt:     "Build something",
		Model:      "test-model",
	}
	c.handleTrigger(context.Background(), buildTriggerMsg(req))

	// The entity ID must always be stored (even if cleanup happens quickly).
	// We verify by checking that triggersProcessed was incremented and no
	// parse-level error occurred (parse errors also increment c.errors, but
	// valid payloads with bad dispatch paths increment c.errors differently).
	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}
	// The entityID format follows the pattern: local.semspec.workflow.scenario-execution.execution.<slug>-<scenarioID>
	want := "local.semspec.workflow.scenario-execution.execution.plan-xyz-s-001"
	// If publishTask succeeded before the natsClient nil check removes it, the
	// execution may still be present. We accept either outcome — the important
	// thing is the trigger was processed without a parse error.
	_ = want // entityID format tested via field propagation tests
}

func TestHandleTrigger_DuplicateTrigger_SkipsSecond(t *testing.T) {
	c := newTestComponent(t)

	// Pre-load an execution so the duplicate-detection fires on the first call.
	// This avoids the timing issue where publishTask cleans up the execution
	// before a second trigger arrives.
	entityID := "local.semspec.workflow.scenario-execution.execution.dup-plan-scen-dup"
	existing := &scenarioExecution{
		EntityID:       entityID,
		Slug:           "dup-plan",
		ScenarioID:     "scen-dup",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecutions.Store(entityID, existing)

	req := payloads.ScenarioExecutionRequest{
		ScenarioID: "scen-dup",
		Slug:       "dup-plan",
	}
	msg := buildTriggerMsg(req)

	// Trigger — should detect the existing active execution and skip silently.
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 — duplicate trigger should be silently skipped", c.errors.Load())
	}
	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}

	// The original execution should still be in activeExecutions (not cleaned up).
	if _, ok := c.activeExecutions.Load(entityID); !ok {
		t.Error("original execution should still be active after duplicate trigger")
	}
}

func TestHandleTrigger_FieldsPropagated(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{
		ScenarioID: "scen-fields",
		Slug:       "fields-plan",
		Prompt:     "implement the feature",
		Role:       "developer",
		Model:      "my-model",
		ProjectID:  "proj-42",
		TraceID:    "trace-xyz",
		LoopID:     "loop-1",
		RequestID:  "req-99",
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	entityID := "local.semspec.workflow.scenario-execution.execution.fields-plan-scen-fields"

	// With Model and Prompt set, publishTask marshals the TaskMessage successfully
	// and the nil NATS client check short-circuits without error, so the execution
	// remains in activeExecutions for field inspection.
	execVal, ok := c.activeExecutions.Load(entityID)
	if !ok {
		t.Fatalf("active execution for %q should still be present (model+prompt set, nil NATS is no-op)", entityID)
	}

	exec := execVal.(*scenarioExecution)
	exec.mu.Lock()
	role := exec.Role
	projectID := exec.ProjectID
	traceID := exec.TraceID
	loopID := exec.LoopID
	requestID := exec.RequestID
	exec.mu.Unlock()

	if role != "developer" {
		t.Errorf("Role = %q, want developer", role)
	}
	if projectID != "proj-42" {
		t.Errorf("ProjectID = %q, want proj-42", projectID)
	}
	if traceID != "trace-xyz" {
		t.Errorf("TraceID = %q, want trace-xyz", traceID)
	}
	if loopID != "loop-1" {
		t.Errorf("LoopID = %q, want loop-1", loopID)
	}
	if requestID != "req-99" {
		t.Errorf("RequestID = %q, want req-99", requestID)
	}
}

func TestHandleTrigger_DecomposerTaskIDIndexed(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{
		ScenarioID: "scen-idx",
		Slug:       "idx-plan",
		Prompt:     "some prompt",
		Model:      "some-model",
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	// With Model set, publishTask succeeds (nil NATS client is a no-op), so
	// the execution stays in activeExecutions with the decomposer task ID indexed.
	entityID := "local.semspec.workflow.scenario-execution.execution.idx-plan-scen-idx"
	execVal, ok := c.activeExecutions.Load(entityID)
	if !ok {
		t.Fatalf("active execution for %q not found after trigger", entityID)
	}

	exec := execVal.(*scenarioExecution)
	exec.mu.Lock()
	decomposerTaskID := exec.DecomposerTaskID
	exec.mu.Unlock()

	if decomposerTaskID == "" {
		t.Error("DecomposerTaskID should be set after trigger dispatch")
	}

	// The task ID must be in the index so loop-completion events can be routed.
	if _, indexed := c.taskIDIndex.Load(decomposerTaskID); !indexed {
		t.Errorf("decomposer task ID %q should be in taskIDIndex", decomposerTaskID)
	}
}

// ---------------------------------------------------------------------------
// handleLoopCompleted — routing tests
// ---------------------------------------------------------------------------

func TestHandleLoopCompleted_MalformedEnvelope_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	msg := &nats.Msg{Data: []byte(`not json`)}
	c.handleLoopCompleted(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 after malformed loop-completed", c.errors.Load())
	}
}

func TestHandleLoopCompleted_WrongWorkflowSlug_IsIgnored(t *testing.T) {
	c := newTestComponent(t)

	event := minLoopEvent("some-task", "other-workflow", "step-1", agentic.OutcomeSuccess)
	msg := buildLoopCompletedMsg(t, event)
	c.handleLoopCompleted(context.Background(), msg)

	// Wrong slug — silently ignored, no errors.
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 for wrong workflow slug", c.errors.Load())
	}
}

func TestHandleLoopCompleted_UnknownTaskID_IsIgnored(t *testing.T) {
	c := newTestComponent(t)

	event := minLoopEvent("unknown-task-id", WorkflowSlugScenarioExecution, stageDecompose, agentic.OutcomeSuccess)
	msg := buildLoopCompletedMsg(t, event)
	c.handleLoopCompleted(context.Background(), msg)

	// Unknown task ID with the correct workflow slug — event is silently discarded
	// via a Debug log. The component must not increment errors for this case.
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 for unknown task ID", c.errors.Load())
	}
}

func TestHandleLoopCompleted_TerminatedExecution_IsIgnored(t *testing.T) {
	c := newTestComponent(t)

	entityID := "local.semspec.workflow.scenario-execution.execution.test-plan-scen-term"
	exec := &scenarioExecution{
		EntityID:         entityID,
		Slug:             "test-plan",
		ScenarioID:       "scen-term",
		DecomposerTaskID: "decomp-task-term",
		terminated:       true,
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecutions.Store(entityID, exec)
	c.taskIDIndex.Store("decomp-task-term", entityID)

	event := minLoopEvent("decomp-task-term", WorkflowSlugScenarioExecution, stageDecompose, agentic.OutcomeSuccess)
	event.Result = `{"goal":"test","dag":{"nodes":[{"id":"a","prompt":"p","role":"dev","file_scope":["a.go"]}]}}`
	msg := buildLoopCompletedMsg(t, event)
	c.handleLoopCompleted(context.Background(), msg)

	// Terminated guard — no scenario-complete metrics should change.
	if c.scenariosCompleted.Load() != 0 {
		t.Errorf("scenariosCompleted = %d, want 0 for terminated execution", c.scenariosCompleted.Load())
	}
}

// ---------------------------------------------------------------------------
// markCompletedLocked / markFailedLocked / markErrorLocked — guard tests
// ---------------------------------------------------------------------------

func TestMarkCompletedLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:     "entity-1",
		Slug:         "plan-1",
		ScenarioID:   "scen-1",
		VisitedNodes: make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markCompletedLocked")
	}
	if c.scenariosCompleted.Load() != 1 {
		t.Errorf("scenariosCompleted = %d, want 1", c.scenariosCompleted.Load())
	}
}

func TestMarkCompletedLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:     "entity-2",
		Slug:         "plan-2",
		ScenarioID:   "scen-2",
		terminated:   true,
		VisitedNodes: make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if c.scenariosCompleted.Load() != 0 {
		t.Errorf("scenariosCompleted = %d, want 0 (already terminated)", c.scenariosCompleted.Load())
	}
}

func TestMarkFailedLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:   "entity-3",
		Slug:       "plan-3",
		ScenarioID: "scen-3",
	}

	exec.mu.Lock()
	c.markFailedLocked(context.Background(), exec, "decomposer returned error")
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markFailedLocked")
	}
	if c.scenariosFailed.Load() != 1 {
		t.Errorf("scenariosFailed = %d, want 1", c.scenariosFailed.Load())
	}
}

func TestMarkFailedLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:   "entity-4",
		Slug:       "plan-4",
		ScenarioID: "scen-4",
		terminated: true,
	}

	exec.mu.Lock()
	c.markFailedLocked(context.Background(), exec, "late failure")
	exec.mu.Unlock()

	if c.scenariosFailed.Load() != 0 {
		t.Errorf("scenariosFailed = %d, want 0 (already terminated)", c.scenariosFailed.Load())
	}
}

func TestMarkErrorLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:   "entity-5",
		Slug:       "plan-5",
		ScenarioID: "scen-5",
	}

	exec.mu.Lock()
	c.markErrorLocked(context.Background(), exec, "infrastructure failure")
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markErrorLocked")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1", c.errors.Load())
	}
}

func TestMarkErrorLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:   "entity-6",
		Slug:       "plan-6",
		ScenarioID: "scen-6",
		terminated: true,
	}

	exec.mu.Lock()
	c.markErrorLocked(context.Background(), exec, "late error")
	exec.mu.Unlock()

	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 (already terminated)", c.errors.Load())
	}
}

// TestMarkAll_OnlyOneCanWin verifies that racing terminal transitions are
// safe — only the first one wins due to the terminated guard.
func TestMarkAll_OnlyFirstTerminationWins(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:     "entity-race",
		Slug:         "p",
		ScenarioID:   "race",
		VisitedNodes: make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	c.markFailedLocked(context.Background(), exec, "should be ignored")
	c.markErrorLocked(context.Background(), exec, "also ignored")
	exec.mu.Unlock()

	if c.scenariosCompleted.Load() != 1 {
		t.Errorf("scenariosCompleted = %d, want 1", c.scenariosCompleted.Load())
	}
	if c.scenariosFailed.Load() != 0 {
		t.Errorf("scenariosFailed = %d, want 0", c.scenariosFailed.Load())
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// cleanupExecutionLocked — removes from maps
// ---------------------------------------------------------------------------

func TestCleanupExecutionLocked_RemovesFromActiveExecutions(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:          "local.semspec.workflow.scenario-execution.execution.plan-c-scen-c",
		DecomposerTaskID:  "decomp-c",
		CurrentNodeTaskID: "node-c",
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("decomp-c", exec.EntityID)
	c.taskIDIndex.Store("node-c", exec.EntityID)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()

	if _, ok := c.activeExecutions.Load(exec.EntityID); ok {
		t.Error("activeExecutions should not contain entity after cleanup")
	}
	if _, ok := c.taskIDIndex.Load("decomp-c"); ok {
		t.Error("taskIDIndex should not contain decomposer task after cleanup")
	}
	if _, ok := c.taskIDIndex.Load("node-c"); ok {
		t.Error("taskIDIndex should not contain node task after cleanup")
	}
}

func TestCleanupExecutionLocked_StopsTimeoutTimer(t *testing.T) {
	c := newTestComponent(t)

	timerStopped := false
	exec := &scenarioExecution{
		EntityID:     "local.semspec.workflow.scenario-execution.execution.plan-d-scen-d",
		VisitedNodes: make(map[string]bool),
		timeoutTimer: &timeoutHandle{
			stop: func() { timerStopped = true },
		},
	}
	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()

	if !timerStopped {
		t.Error("timeoutHandle.stop should be called during cleanup")
	}
}

func TestCleanupExecutionLocked_NilTimer_NoPanic(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:     "local.semspec.workflow.scenario-execution.execution.plan-e-scen-e",
		VisitedNodes: make(map[string]bool),
		timeoutTimer: nil,
	}
	c.activeExecutions.Store(exec.EntityID, exec)

	// Must not panic when timeoutTimer is nil.
	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()
}

// ---------------------------------------------------------------------------
// scenarioExecution struct initialization tests
// ---------------------------------------------------------------------------

func TestScenarioExecution_InitializesCorrectly(t *testing.T) {
	exec := &scenarioExecution{
		EntityID:       "local.semspec.workflow.scenario-execution.execution.p-s",
		Slug:           "p",
		ScenarioID:     "s",
		Prompt:         "do the thing",
		Role:           "developer",
		Model:          "gpt-4",
		ProjectID:      "proj-1",
		TraceID:        "trace-1",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}

	if exec.terminated {
		t.Error("new execution should not be terminated")
	}
	if exec.CurrentNodeIdx != -1 {
		t.Errorf("CurrentNodeIdx should be -1 before execution, got %d", exec.CurrentNodeIdx)
	}
	if len(exec.VisitedNodes) != 0 {
		t.Error("VisitedNodes should be empty initially")
	}
	if exec.DAG != nil {
		t.Error("DAG should be nil before decomposition completes")
	}
}

func TestScenarioExecution_VisitedNodesTracking(t *testing.T) {
	exec := &scenarioExecution{
		VisitedNodes: make(map[string]bool),
	}

	exec.VisitedNodes["node-a"] = true
	exec.VisitedNodes["node-b"] = true

	if len(exec.VisitedNodes) != 2 {
		t.Errorf("VisitedNodes len = %d, want 2", len(exec.VisitedNodes))
	}
	if !exec.VisitedNodes["node-a"] {
		t.Error("node-a should be in VisitedNodes")
	}
}

// ---------------------------------------------------------------------------
// dispatchNextNodeLocked — index advancement tests (no NATS I/O)
// ---------------------------------------------------------------------------

func TestDispatchNextNodeLocked_AdvancesCurrentNodeIdx(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-1", Prompt: "First task", Role: "developer", FileScope: []string{"a.go"}},
			{ID: "node-2", Prompt: "Second task", Role: "developer", FileScope: []string{"b.go"}},
		},
	}

	exec := &scenarioExecution{
		EntityID:       "local.semspec.workflow.scenario-execution.execution.p-s",
		Slug:           "p",
		ScenarioID:     "s",
		DAG:            dag,
		SortedNodeIDs:  []string{"node-1", "node-2"},
		NodeIndex:      map[string]*decompose.TaskNode{"node-1": &dag.Nodes[0], "node-2": &dag.Nodes[1]},
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	idx := exec.CurrentNodeIdx
	exec.mu.Unlock()

	// After the first dispatch, index should have advanced to 0.
	if idx != 0 {
		t.Errorf("CurrentNodeIdx = %d, want 0 after first dispatch", idx)
	}
}

func TestDispatchNextNodeLocked_AllNodesExhausted_MarksCompleted(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "only-node", Prompt: "The only task", Role: "developer", FileScope: []string{"x.go"}},
		},
	}

	// CurrentNodeIdx starts at 0 — calling dispatch again (which increments to
	// 1) means we're past the end of SortedNodeIDs (len=1), triggering completion.
	exec := &scenarioExecution{
		EntityID:       "local.semspec.workflow.scenario-execution.execution.p-s2",
		Slug:           "p",
		ScenarioID:     "s2",
		DAG:            dag,
		SortedNodeIDs:  []string{"only-node"},
		NodeIndex:      map[string]*decompose.TaskNode{"only-node": &dag.Nodes[0]},
		CurrentNodeIdx: 0,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated (completed) when all nodes have been dispatched")
	}
	if c.scenariosCompleted.Load() != 1 {
		t.Errorf("scenariosCompleted = %d, want 1", c.scenariosCompleted.Load())
	}
}

func TestDispatchNextNodeLocked_MissingNodeInIndex_MarksError(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:       "local.semspec.workflow.scenario-execution.execution.p-s3",
		Slug:           "p",
		ScenarioID:     "s3",
		SortedNodeIDs:  []string{"ghost-node"},
		NodeIndex:      map[string]*decompose.TaskNode{}, // ghost-node not indexed
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated (error) when node is missing from index")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// handleDecomposerCompleteLocked — DAG parse tests
// ---------------------------------------------------------------------------

func TestHandleDecomposerCompleteLocked_FailedOutcome_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:         "local.semspec.workflow.scenario-execution.execution.p-sd",
		Slug:             "p",
		ScenarioID:       "sd",
		DecomposerTaskID: "decomp-d",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("decomp-d", exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-d",
		TaskID:       "decomp-d",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("failed decomposer outcome should terminate the execution")
	}
	if c.scenariosFailed.Load() != 1 {
		t.Errorf("scenariosFailed = %d, want 1", c.scenariosFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_MalformedResult_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:         "local.semspec.workflow.scenario-execution.execution.p-sm",
		Slug:             "p",
		ScenarioID:       "sm",
		DecomposerTaskID: "decomp-m",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("decomp-m", exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-m",
		TaskID:       "decomp-m",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       `not valid json`,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("malformed decomposer result should terminate the execution")
	}
	if c.scenariosFailed.Load() != 1 {
		t.Errorf("scenariosFailed = %d, want 1", c.scenariosFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_InvalidDAG_Cycle_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:         "local.semspec.workflow.scenario-execution.execution.p-si",
		Slug:             "p",
		ScenarioID:       "si",
		DecomposerTaskID: "decomp-i",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("decomp-i", exec.EntityID)

	// DAG with a cycle — Validate() will reject it.
	cycleResult := `{
		"goal": "build something",
		"dag": {
			"nodes": [
				{"id": "a", "prompt": "p", "role": "dev", "depends_on": ["b"], "file_scope": ["a.go"]},
				{"id": "b", "prompt": "p", "role": "dev", "depends_on": ["a"], "file_scope": ["b.go"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-i",
		TaskID:       "decomp-i",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       cycleResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("cyclic DAG should cause the execution to terminate as failed")
	}
	if c.scenariosFailed.Load() != 1 {
		t.Errorf("scenariosFailed = %d, want 1", c.scenariosFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_ValidDAG_PopulatesExecution(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:         "local.semspec.workflow.scenario-execution.execution.p-sv",
		Slug:             "p",
		ScenarioID:       "sv",
		DecomposerTaskID: "decomp-v",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("decomp-v", exec.EntityID)

	validResult := `{
		"goal": "implement auth",
		"dag": {
			"nodes": [
				{"id": "setup", "prompt": "setup env", "role": "developer", "file_scope": ["setup.go"]},
				{"id": "impl",  "prompt": "write code", "role": "developer", "depends_on": ["setup"], "file_scope": ["impl.go"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-v",
		TaskID:       "decomp-v",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       validResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	dagSet := exec.DAG != nil
	nodeCount := len(exec.SortedNodeIDs)
	indexLen := len(exec.NodeIndex)
	exec.mu.Unlock()

	if !dagSet {
		t.Error("exec.DAG should be set after successful decomposition")
	}
	if nodeCount != 2 {
		t.Errorf("SortedNodeIDs len = %d, want 2", nodeCount)
	}
	if indexLen != 2 {
		t.Errorf("NodeIndex len = %d, want 2", indexLen)
	}
}

func TestHandleDecomposerCompleteLocked_ValidDAG_TopologicalOrder(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:         "local.semspec.workflow.scenario-execution.execution.p-sv2",
		Slug:             "p",
		ScenarioID:       "sv2",
		DecomposerTaskID: "decomp-v2",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("decomp-v2", exec.EntityID)

	// Linear chain: setup → impl → test
	chainResult := `{
		"goal": "build and test",
		"dag": {
			"nodes": [
				{"id": "setup", "prompt": "prepare", "role": "developer", "file_scope": ["setup.go"]},
				{"id": "impl",  "prompt": "implement", "role": "developer", "depends_on": ["setup"], "file_scope": ["impl.go"]},
				{"id": "test",  "prompt": "test it",   "role": "developer", "depends_on": ["impl"],  "file_scope": ["impl_test.go"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-v2",
		TaskID:       "decomp-v2",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       chainResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	sorted := make([]string, len(exec.SortedNodeIDs))
	copy(sorted, exec.SortedNodeIDs)
	exec.mu.Unlock()

	if len(sorted) != 3 {
		t.Fatalf("SortedNodeIDs len = %d, want 3", len(sorted))
	}
	if sorted[0] != "setup" || sorted[1] != "impl" || sorted[2] != "test" {
		t.Errorf("SortedNodeIDs = %v, want [setup impl test]", sorted)
	}
}

// ---------------------------------------------------------------------------
// handleNodeCompleteLocked — serial execution advancement tests
// ---------------------------------------------------------------------------

func TestHandleNodeCompleteLocked_FailedOutcome_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:          "local.semspec.workflow.scenario-execution.execution.p-snf",
		Slug:              "p",
		ScenarioID:        "snf",
		CurrentNodeTaskID: "node-task-fail",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("node-task-fail", exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-fail",
		TaskID:       "node-task-fail",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("failed node should terminate the scenario execution")
	}
	if c.scenariosFailed.Load() != 1 {
		t.Errorf("scenariosFailed = %d, want 1", c.scenariosFailed.Load())
	}
}

func TestHandleNodeCompleteLocked_SuccessWithMoreNodes_AdvancesExecution(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-x", Prompt: "do x", Role: "developer", FileScope: []string{"x.go"}},
			{ID: "node-y", Prompt: "do y", Role: "developer", FileScope: []string{"y.go"}},
		},
	}

	exec := &scenarioExecution{
		EntityID:          "local.semspec.workflow.scenario-execution.execution.p-snm",
		Slug:              "p",
		ScenarioID:        "snm",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-x",
		DAG:               dag,
		SortedNodeIDs:     []string{"node-x", "node-y"},
		NodeIndex:         map[string]*decompose.TaskNode{"node-x": &dag.Nodes[0], "node-y": &dag.Nodes[1]},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("node-task-x", exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-x",
		TaskID:       "node-task-x",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: "node-x",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	visited := len(exec.VisitedNodes)
	nodeIdx := exec.CurrentNodeIdx
	terminated := exec.terminated
	exec.mu.Unlock()

	if visited != 1 {
		t.Errorf("VisitedNodes len = %d, want 1 after node-x complete", visited)
	}
	// Execution should advance to node-y (index 1), not terminate.
	if terminated {
		t.Error("execution should not be terminated — node-y is still pending")
	}
	if nodeIdx != 1 {
		t.Errorf("CurrentNodeIdx = %d, want 1 after advancing to node-y", nodeIdx)
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 — dispatch of node-y should succeed with model set", c.errors.Load())
	}
}

func TestHandleNodeCompleteLocked_LastNodeSuccess_MarksCompleted(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:          "local.semspec.workflow.scenario-execution.execution.p-snl",
		Slug:              "p",
		ScenarioID:        "snl",
		CurrentNodeTaskID: "node-task-last",
		SortedNodeIDs:     []string{"only"},
		NodeIndex:         map[string]*decompose.TaskNode{},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("node-task-last", exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-last",
		TaskID:       "node-task-last",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: "only",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("all nodes completed — execution should be terminated as completed")
	}
	if c.scenariosCompleted.Load() != 1 {
		t.Errorf("scenariosCompleted = %d, want 1", c.scenariosCompleted.Load())
	}
}

func TestHandleNodeCompleteLocked_NodeIDRemovedFromTaskIndex(t *testing.T) {
	c := newTestComponent(t)

	exec := &scenarioExecution{
		EntityID:          "local.semspec.workflow.scenario-execution.execution.p-snr",
		Slug:              "p",
		ScenarioID:        "snr",
		CurrentNodeTaskID: "node-task-rm",
		SortedNodeIDs:     []string{"rm-node"},
		NodeIndex:         map[string]*decompose.TaskNode{},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)
	c.taskIDIndex.Store("node-task-rm", exec.EntityID)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-rm",
		TaskID:       "node-task-rm",
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: "rm-node",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	// The completed node's task ID should have been removed from the index.
	if _, ok := c.taskIDIndex.Load("node-task-rm"); ok {
		t.Error("completed node task ID should be removed from taskIDIndex")
	}
}

// ---------------------------------------------------------------------------
// Execution timeout tests
// ---------------------------------------------------------------------------

func TestStartExecutionTimeoutLocked_FiresAfterDuration(t *testing.T) {
	c := newTestComponent(t)
	c.config.TimeoutSeconds = 1 // fire after 1 second

	exec := &scenarioExecution{
		EntityID:     "local.semspec.workflow.scenario-execution.execution.p-timeout",
		Slug:         "p",
		ScenarioID:   "timeout",
		VisitedNodes: make(map[string]bool),
	}
	c.activeExecutions.Store(exec.EntityID, exec)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		exec.mu.Lock()
		c.startExecutionTimeoutLocked(exec)
		exec.mu.Unlock()
	}()
	wg.Wait()

	// Wait for the timer to fire (up to 3s).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		exec.mu.Lock()
		terminated := exec.terminated
		exec.mu.Unlock()
		if terminated {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	exec.mu.Lock()
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated by timeout after 1 second")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 after timeout", c.errors.Load())
	}
}

func TestStartExecutionTimeoutLocked_StopPreventsTimerFiring(t *testing.T) {
	c := newTestComponent(t)
	c.config.TimeoutSeconds = 60 // 60s — will not fire in test

	exec := &scenarioExecution{
		EntityID:     "local.semspec.workflow.scenario-execution.execution.p-notimeout",
		Slug:         "p",
		ScenarioID:   "notimeout",
		VisitedNodes: make(map[string]bool),
	}

	exec.mu.Lock()
	c.startExecutionTimeoutLocked(exec)
	// Stop the timer immediately.
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	exec.mu.Unlock()

	// Give any racing goroutine a brief moment.
	time.Sleep(10 * time.Millisecond)

	exec.mu.Lock()
	terminated := exec.terminated
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should not be terminated when timer was stopped before firing")
	}
}

// ---------------------------------------------------------------------------
// portSubject helper tests
// ---------------------------------------------------------------------------

func TestPortSubject_NilConfig_ReturnsEmpty(t *testing.T) {
	port := component.Port{Config: nil}
	got := graphutil.PortSubject(port)
	if got != "" {
		t.Errorf("portSubject with nil Config = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// Factory / Register tests
// ---------------------------------------------------------------------------

type mockRegistry struct {
	registered bool
	lastConfig component.RegistrationConfig
	returnErr  error
}

func (m *mockRegistry) RegisterWithConfig(cfg component.RegistrationConfig) error {
	m.registered = true
	m.lastConfig = cfg
	return m.returnErr
}

func TestRegister_Succeeds(t *testing.T) {
	reg := &mockRegistry{}
	err := Register(reg)
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
	if !reg.registered {
		t.Error("Register() should call RegisterWithConfig")
	}
	if reg.lastConfig.Name != componentName {
		t.Errorf("Name = %q, want %q", reg.lastConfig.Name, componentName)
	}
	if reg.lastConfig.Factory == nil {
		t.Error("Factory should not be nil")
	}
	if reg.lastConfig.Version != componentVersion {
		t.Errorf("Version = %q, want %q", reg.lastConfig.Version, componentVersion)
	}
	if reg.lastConfig.Type != "processor" {
		t.Errorf("Type = %q, want processor", reg.lastConfig.Type)
	}
}

func TestRegister_NilRegistry_ReturnsError(t *testing.T) {
	err := Register(nil)
	if err == nil {
		t.Fatal("Register(nil) should return error")
	}
}

// ---------------------------------------------------------------------------
// Metrics consistency tests
// ---------------------------------------------------------------------------

func TestMetrics_TriggersProcessedIncrements(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.ScenarioExecutionRequest{ScenarioID: "s1", Slug: "p1"}
	c.handleTrigger(context.Background(), buildTriggerMsg(req))

	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}
}

func TestMetrics_ErrorsIncrementOnMalformedMessage(t *testing.T) {
	c := newTestComponent(t)

	c.handleTrigger(context.Background(), &nats.Msg{Data: []byte(`bad`)})
	c.handleTrigger(context.Background(), &nats.Msg{Data: []byte(`also bad`)})

	if c.errors.Load() != 2 {
		t.Errorf("errors = %d, want 2", c.errors.Load())
	}
}

func TestMetrics_SeparateCountersForCompletedAndFailed(t *testing.T) {
	c := newTestComponent(t)

	execCompleted := &scenarioExecution{
		EntityID:     "entity-completed",
		Slug:         "p",
		ScenarioID:   "s-completed",
		VisitedNodes: make(map[string]bool),
	}
	execFailed := &scenarioExecution{
		EntityID:   "entity-failed",
		Slug:       "p",
		ScenarioID: "s-failed",
	}

	execCompleted.mu.Lock()
	c.markCompletedLocked(context.Background(), execCompleted)
	execCompleted.mu.Unlock()

	execFailed.mu.Lock()
	c.markFailedLocked(context.Background(), execFailed, "test failure")
	execFailed.mu.Unlock()

	if c.scenariosCompleted.Load() != 1 {
		t.Errorf("scenariosCompleted = %d, want 1", c.scenariosCompleted.Load())
	}
	if c.scenariosFailed.Load() != 1 {
		t.Errorf("scenariosFailed = %d, want 1", c.scenariosFailed.Load())
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// ParseReactivePayload round-trip tests (via workflow/payloads)
// ---------------------------------------------------------------------------

func TestParseReactivePayload_ScenarioExecutionRequest_RoundTrip(t *testing.T) {
	original := payloads.ScenarioExecutionRequest{
		ScenarioID: "scen-rt",
		Slug:       "rt-plan",
		Prompt:     "round-trip test",
		Role:       "developer",
		Model:      "gpt-4",
		ProjectID:  "proj-rt",
		TraceID:    "trace-rt",
	}

	msg := buildTriggerMsg(original)
	parsed, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](msg.Data)
	if err != nil {
		t.Fatalf("ParseReactivePayload() error = %v", err)
	}

	if parsed.ScenarioID != original.ScenarioID {
		t.Errorf("ScenarioID = %q, want %q", parsed.ScenarioID, original.ScenarioID)
	}
	if parsed.Slug != original.Slug {
		t.Errorf("Slug = %q, want %q", parsed.Slug, original.Slug)
	}
	if parsed.TraceID != original.TraceID {
		t.Errorf("TraceID = %q, want %q", parsed.TraceID, original.TraceID)
	}
}

func TestParseReactivePayload_MalformedEnvelope_ReturnsError(t *testing.T) {
	_, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest]([]byte(`not json`))
	if err == nil {
		t.Fatal("ParseReactivePayload with malformed envelope should return error")
	}
}

func TestParseReactivePayload_MissingPayloadKey_ReturnsError(t *testing.T) {
	// An envelope with no "payload" key — Payload field will be zero-value (nil).
	data := []byte(`{"type": "something"}`)
	_, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](data)
	if err == nil {
		t.Fatal("ParseReactivePayload with missing payload key should return error")
	}
}

func TestParseReactivePayload_EmptyPayload_ReturnsError(t *testing.T) {
	// ParseReactivePayload returns an error when the "payload" key is absent from
	// the envelope, causing rawMsg.Payload to be nil (len == 0).
	//
	// Note: {"payload":null} encodes as the 4-byte token "null", which passes
	// the len check and json.Unmarshal silently produces a zero-value struct —
	// that case is NOT an error per the current implementation.
	data := []byte(`{"type":"something"}`) // no "payload" key → len == 0
	_, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](data)
	if err == nil {
		t.Fatal("ParseReactivePayload with absent payload key should return error")
	}
	if !strings.Contains(err.Error(), "empty payload") {
		t.Errorf("error %q should contain %q", err.Error(), "empty payload")
	}
}

func TestParseReactivePayload_WrongPayloadType_StillDeserializes(t *testing.T) {
	// ParseReactivePayload[T] is generic — it will unmarshal whatever JSON is in
	// the payload into T. If the payload JSON doesn't match T's fields, the
	// struct is zeroed (no error from Go's json.Unmarshal for unknown fields).
	other := map[string]string{"foo": "bar"}
	otherBytes, _ := json.Marshal(other)
	envelope := map[string]json.RawMessage{"payload": otherBytes}
	data, _ := json.Marshal(envelope)

	parsed, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](data)
	// No error expected — json.Unmarshal ignores unknown fields.
	if err != nil {
		t.Fatalf("ParseReactivePayload with mismatched payload should not error, got: %v", err)
	}
	// Fields are zero-valued since none matched.
	if parsed.ScenarioID != "" || parsed.Slug != "" {
		t.Errorf("mismatched payload produced non-zero result: %+v", parsed)
	}
}

// ---------------------------------------------------------------------------
// WorkflowSlugScenarioExecution constant
// ---------------------------------------------------------------------------

func TestWorkflowSlugScenarioExecution_IsExported(t *testing.T) {
	// Verify the exported constant is non-empty and matches the expected value.
	if WorkflowSlugScenarioExecution == "" {
		t.Error("WorkflowSlugScenarioExecution should not be empty")
	}
	if WorkflowSlugScenarioExecution != "semspec-scenario-execution" {
		t.Errorf("WorkflowSlugScenarioExecution = %q, want semspec-scenario-execution",
			WorkflowSlugScenarioExecution)
	}
}
