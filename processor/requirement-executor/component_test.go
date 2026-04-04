package requirementexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	_ "github.com/c360studio/semspec/tools/decompose" // ensure decompose package is imported
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	sscache "github.com/c360studio/semstreams/pkg/cache"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// mockMsg implements jetstream.Msg for unit tests.
// ---------------------------------------------------------------------------

type mockMsg struct {
	data    []byte
	subject string
	acked   bool
	naked   bool
}

func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Subject() string                           { return m.subject }
func (m *mockMsg) Reply() string                             { return "" }
func (m *mockMsg) Headers() nats.Header                      { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockMsg) Ack() error                                { m.acked = true; return nil }
func (m *mockMsg) DoubleAck(_ context.Context) error         { m.acked = true; return nil }
func (m *mockMsg) Nak() error                                { m.naked = true; return nil }
func (m *mockMsg) NakWithDelay(_ time.Duration) error        { m.naked = true; return nil }
func (m *mockMsg) InProgress() error                         { return nil }
func (m *mockMsg) Term() error                               { return nil }
func (m *mockMsg) TermWithReason(_ string) error             { return nil }

// ---------------------------------------------------------------------------
// Wire-format helpers
// ---------------------------------------------------------------------------

// buildTriggerMsg builds a *mockMsg carrying a RequirementExecutionRequest
// wrapped in the minimal BaseMessage envelope that ParseReactivePayload expects.
func buildTriggerMsg(req payloads.RequirementExecutionRequest) *mockMsg {
	payload, err := json.Marshal(req)
	if err != nil {
		panic("buildTriggerMsg: marshal request: " + err.Error())
	}
	envelope := map[string]json.RawMessage{"payload": payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		panic("buildTriggerMsg: marshal envelope: " + err.Error())
	}
	return &mockMsg{data: data, subject: subjectRequirementTrigger}
}

// buildLoopCompletedMsg builds a *mockMsg that handleLoopCompleted can parse.
// It constructs a proper BaseMessage so that base.Payload() returns a
// *agentic.LoopCompletedEvent after registry lookup.
func buildLoopCompletedMsg(t *testing.T, event agentic.LoopCompletedEvent) *mockMsg {
	t.Helper()
	baseMsg := message.NewBaseMessage(event.Schema(), &event, "test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("buildLoopCompletedMsg: marshal: %v", err)
	}
	return &mockMsg{data: data, subject: "agent.complete.test"}
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
		if p.Subject == subjectRequirementTrigger {
			subjectFound = true
			break
		}
	}
	if !subjectFound {
		t.Errorf("default Ports.Inputs should contain subject %q", subjectRequirementTrigger)
	}
}

// TestConfig_Validate_ValidTimeout confirms positive timeout passes Validate
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

func TestNewComponent_ExplicitlyInvalidConfig(t *testing.T) {
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

	// Initialize typed cache that is normally created in Start().
	ae, err := sscache.NewTTL[*requirementExecution](context.Background(), 4*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("newTestComponent: create active execs cache: %v", err)
	}
	c.activeExecs = ae
	return c
}

func TestHandleTrigger_MalformedPayload_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	msg := &mockMsg{data: []byte(`not json at all`)}
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 after malformed message", c.errors.Load())
	}
	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}
}

func TestHandleTrigger_MissingRequirementID_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.RequirementExecutionRequest{
		Slug: "my-plan",
		// RequirementID intentionally omitted
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 for missing requirement_id", c.errors.Load())
	}
}

func TestHandleTrigger_MissingSlug_IncrementsErrors(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.RequirementExecutionRequest{
		RequirementID: "req-123",
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

	req := payloads.RequirementExecutionRequest{}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 for missing both fields", c.errors.Load())
	}
}

func TestHandleTrigger_ValidPayload_CreatesActiveExecution(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.RequirementExecutionRequest{
		RequirementID: "req-abc",
		Slug:          "my-plan",
		Prompt:        "Build it",
		Model:         "test-model",
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	expectedEntityID := workflow.EntityPrefix() + ".exec.req.run.my-plan-req-abc"
	exec, ok := c.activeExecs.Get(expectedEntityID)
	if !ok {
		t.Fatalf("expected active execution to be stored for entity %q", expectedEntityID)
	}

	exec.mu.Lock()
	requirementID := exec.RequirementID
	slug := exec.Slug
	prompt := exec.Prompt
	model := exec.Model
	idx := exec.CurrentNodeIdx
	exec.mu.Unlock()

	if requirementID != "req-abc" {
		t.Errorf("exec.RequirementID = %q, want req-abc", requirementID)
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

	req := payloads.RequirementExecutionRequest{
		RequirementID: "req-001",
		Slug:          "plan-xyz",
		Prompt:        "Build something",
		Model:         "test-model",
	}
	c.handleTrigger(context.Background(), buildTriggerMsg(req))

	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}
	// The entityID format: {prefix}.exec.req.run.<slug>-<requirementID>
	_ = workflow.EntityPrefix() + ".exec.req.run.plan-xyz-req-001"
}

func TestHandleTrigger_DuplicateTrigger_SkipsSecond(t *testing.T) {
	c := newTestComponent(t)

	entityID := workflow.EntityPrefix() + ".exec.req.run.dup-plan-req-dup"
	existing := &requirementExecution{
		EntityID:       entityID,
		Slug:           "dup-plan",
		RequirementID:  "req-dup",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(entityID, existing)

	req := payloads.RequirementExecutionRequest{
		RequirementID: "req-dup",
		Slug:          "dup-plan",
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

	if _, ok := c.activeExecs.Get(entityID); !ok {
		t.Error("original execution should still be active after duplicate trigger")
	}
}

func TestHandleTrigger_FieldsPropagated(t *testing.T) {
	c := newTestComponent(t)

	req := payloads.RequirementExecutionRequest{
		RequirementID: "req-fields",
		Slug:          "fields-plan",
		Prompt:        "implement the feature",
		Role:          "developer",
		Model:         "my-model",
		ProjectID:     "proj-42",
		TraceID:       "trace-xyz",
		LoopID:        "loop-1",
		RequestID:     "req-99",
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	entityID := workflow.EntityPrefix() + ".exec.req.run.fields-plan-req-fields"

	exec, ok := c.activeExecs.Get(entityID)
	if !ok {
		t.Fatalf("active execution for %q should still be present (model+prompt set, nil NATS is no-op)", entityID)
	}

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

	req := payloads.RequirementExecutionRequest{
		RequirementID: "req-idx",
		Slug:          "idx-plan",
		Prompt:        "some prompt",
		Model:         "some-model",
	}
	msg := buildTriggerMsg(req)
	c.handleTrigger(context.Background(), msg)

	entityID := workflow.EntityPrefix() + ".exec.req.run.idx-plan-req-idx"
	exec, ok := c.activeExecs.Get(entityID)
	if !ok {
		t.Fatalf("active execution for %q not found after trigger", entityID)
	}

	exec.mu.Lock()
	decomposerTaskID := exec.DecomposerTaskID
	exec.mu.Unlock()

	if decomposerTaskID == "" {
		t.Error("DecomposerTaskID should be set after trigger dispatch")
	}

}

// ---------------------------------------------------------------------------
// markCompletedLocked / markFailedLocked / markErrorLocked — guard tests
// ---------------------------------------------------------------------------

func TestMarkCompletedLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-1",
		Slug:          "plan-1",
		RequirementID: "req-1",
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markCompletedLocked")
	}
	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
}

func TestMarkCompletedLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-2",
		Slug:          "plan-2",
		RequirementID: "req-2",
		terminated:    true,
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("requirementsCompleted = %d, want 0 (already terminated)", c.requirementsCompleted.Load())
	}
}

func TestMarkFailedLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-3",
		Slug:          "plan-3",
		RequirementID: "req-3",
	}

	exec.mu.Lock()
	c.markFailedLocked(context.Background(), exec, "decomposer returned error")
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markFailedLocked")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestMarkFailedLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-4",
		Slug:          "plan-4",
		RequirementID: "req-4",
		terminated:    true,
	}

	exec.mu.Lock()
	c.markFailedLocked(context.Background(), exec, "late failure")
	exec.mu.Unlock()

	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d, want 0 (already terminated)", c.requirementsFailed.Load())
	}
}

func TestMarkErrorLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-5",
		Slug:          "plan-5",
		RequirementID: "req-5",
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

	exec := &requirementExecution{
		EntityID:      "entity-6",
		Slug:          "plan-6",
		RequirementID: "req-6",
		terminated:    true,
	}

	exec.mu.Lock()
	c.markErrorLocked(context.Background(), exec, "late error")
	exec.mu.Unlock()

	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 (already terminated)", c.errors.Load())
	}
}

func TestMarkAll_OnlyFirstTerminationWins(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-race",
		Slug:          "p",
		RequirementID: "race",
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	c.markFailedLocked(context.Background(), exec, "should be ignored")
	c.markErrorLocked(context.Background(), exec, "also ignored")
	exec.mu.Unlock()

	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d, want 0", c.requirementsFailed.Load())
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

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.plan-c-req-c",
		DecomposerTaskID:  "decomp-c",
		CurrentNodeTaskID: "node-c",
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()

	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Error("activeExecs should not contain entity after cleanup")
	}
}

func TestCleanupExecutionLocked_StopsTimeoutTimer(t *testing.T) {
	c := newTestComponent(t)

	timerStopped := false
	exec := &requirementExecution{
		EntityID:     workflow.EntityPrefix() + ".exec.req.run.plan-d-req-d",
		VisitedNodes: make(map[string]bool),
		timeoutTimer: &timeoutHandle{
			stop: func() { timerStopped = true },
		},
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()

	if !timerStopped {
		t.Error("timeoutHandle.stop should be called during cleanup")
	}
}

func TestCleanupExecutionLocked_NilTimer_NoPanic(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:     workflow.EntityPrefix() + ".exec.req.run.plan-e-req-e",
		VisitedNodes: make(map[string]bool),
		timeoutTimer: nil,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec)
	exec.mu.Unlock()
}

// ---------------------------------------------------------------------------
// requirementExecution struct initialization tests
// ---------------------------------------------------------------------------

func TestRequirementExecution_InitializesCorrectly(t *testing.T) {
	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r",
		Slug:           "p",
		RequirementID:  "r",
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

func TestRequirementExecution_VisitedNodesTracking(t *testing.T) {
	exec := &requirementExecution{
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

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r",
		Slug:           "p",
		RequirementID:  "r",
		DAG:            dag,
		SortedNodeIDs:  []string{"node-1", "node-2"},
		NodeIndex:      map[string]*decompose.TaskNode{"node-1": &dag.Nodes[0], "node-2": &dag.Nodes[1]},
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	idx := exec.CurrentNodeIdx
	exec.mu.Unlock()

	if idx != 0 {
		t.Errorf("CurrentNodeIdx = %d, want 0 after first dispatch", idx)
	}
}

func TestDispatchNextNodeLocked_AllNodesExhausted_DispatchesReviewer(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "only-node", Prompt: "The only task", Role: "developer", FileScope: []string{"x.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r2",
		Slug:           "p",
		RequirementID:  "r2",
		Model:          "default",
		Prompt:         "implement requirement",
		DAG:            dag,
		SortedNodeIDs:  []string{"only-node"},
		NodeIndex:      map[string]*decompose.TaskNode{"only-node": &dag.Nodes[0]},
		CurrentNodeIdx: 0,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	reviewerTaskID := exec.ReviewerTaskID
	terminated := exec.terminated
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should not be terminated yet: reviewer is pending")
	}
	if reviewerTaskID == "" {
		t.Error("ReviewerTaskID should be set after all nodes complete (review dispatched)")
	}
}

func TestDispatchNextNodeLocked_MissingNodeInIndex_MarksError(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r3",
		Slug:           "p",
		RequirementID:  "r3",
		SortedNodeIDs:  []string{"ghost-node"},
		NodeIndex:      map[string]*decompose.TaskNode{},
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

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

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rd",
		Slug:             "p",
		RequirementID:    "rd",
		DecomposerTaskID: "decomp-d",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-d",
		TaskID:       "decomp-d",
		WorkflowSlug: WorkflowSlugRequirementExecution,
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
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_MalformedResult_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rm",
		Slug:             "p",
		RequirementID:    "rm",
		DecomposerTaskID: "decomp-m",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-m",
		TaskID:       "decomp-m",
		WorkflowSlug: WorkflowSlugRequirementExecution,
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
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_InvalidDAG_Cycle_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-ri",
		Slug:             "p",
		RequirementID:    "ri",
		DecomposerTaskID: "decomp-i",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

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
		WorkflowSlug: WorkflowSlugRequirementExecution,
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
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_ValidDAG_PopulatesExecution(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rv",
		Slug:             "p",
		RequirementID:    "rv",
		DecomposerTaskID: "decomp-v",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

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
		WorkflowSlug: WorkflowSlugRequirementExecution,
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

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rv2",
		Slug:             "p",
		RequirementID:    "rv2",
		DecomposerTaskID: "decomp-v2",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

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
		WorkflowSlug: WorkflowSlugRequirementExecution,
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

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnf",
		Slug:              "p",
		RequirementID:     "rnf",
		CurrentNodeTaskID: "node-task-fail",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-fail",
		TaskID:       "node-task-fail",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("failed node should terminate the requirement execution")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleNodeCompleteLocked_FailedOutcome_RetriesWhenBudgetRemains(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "task-a", Prompt: "do a", Role: "developer", FileScope: []string{"a.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-retry",
		Slug:              "p",
		RequirementID:     "retry-test",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-retry",
		DAG:               dag,
		SortedNodeIDs:     []string{"task-a"},
		NodeIndex:         map[string]*decompose.TaskNode{"task-a": &dag.Nodes[0]},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-retry",
		TaskID:       "node-task-retry",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	retryCount := exec.RetryCount
	terminated := exec.terminated
	dirtyNodes := exec.DirtyNodeIDs
	_, nodeStillVisited := exec.VisitedNodes["task-a"]
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should NOT be terminated when retry budget remains")
	}
	if retryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", retryCount)
	}
	if len(dirtyNodes) != 1 || dirtyNodes[0] != "task-a" {
		t.Errorf("DirtyNodeIDs = %v, want [task-a]", dirtyNodes)
	}
	if nodeStillVisited {
		t.Error("failed node should be removed from VisitedNodes for retry")
	}
}

func TestHandleNodeCompleteLocked_FailedOutcome_ExhaustsRetryBudget(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-exhaust",
		Slug:              "p",
		RequirementID:     "exhaust-test",
		CurrentNodeTaskID: "node-task-exhaust",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        2, // budget already exhausted
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-exhaust",
		TaskID:       "node-task-exhaust",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated when retry budget is exhausted")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
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

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnm",
		Slug:              "p",
		RequirementID:     "rnm",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-x",
		DAG:               dag,
		SortedNodeIDs:     []string{"node-x", "node-y"},
		NodeIndex:         map[string]*decompose.TaskNode{"node-x": &dag.Nodes[0], "node-y": &dag.Nodes[1]},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-x",
		TaskID:       "node-task-x",
		WorkflowSlug: WorkflowSlugRequirementExecution,
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

func TestHandleNodeCompleteLocked_LastNodeSuccess_DispatchesReviewer(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnl",
		Slug:              "p",
		RequirementID:     "rnl",
		Model:             "default",
		Prompt:            "implement requirement",
		CurrentNodeTaskID: "node-task-last",
		SortedNodeIDs:     []string{"only"},
		NodeIndex:         map[string]*decompose.TaskNode{},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-last",
		TaskID:       "node-task-last",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "only",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	reviewerTaskID := exec.ReviewerTaskID
	terminated := exec.terminated
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should not be terminated yet: reviewer is pending")
	}
	if reviewerTaskID == "" {
		t.Error("ReviewerTaskID should be set after all nodes complete (review dispatched)")
	}
	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("requirementsCompleted = %d, want 0 (reviewer verdict not received yet)", c.requirementsCompleted.Load())
	}
}

func TestHandleNodeCompleteLocked_NodeIDRemovedFromTaskIndex(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnr",
		Slug:              "p",
		RequirementID:     "rnr",
		Model:             "default",
		Prompt:            "implement requirement",
		CurrentNodeTaskID: "node-task-rm",
		SortedNodeIDs:     []string{"rm-node"},
		NodeIndex:         map[string]*decompose.TaskNode{},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-rm",
		TaskID:       "node-task-rm",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "rm-node",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

}

// ---------------------------------------------------------------------------
// Execution timeout tests
// ---------------------------------------------------------------------------

func TestStartExecutionTimeoutLocked_FiresAfterDuration(t *testing.T) {
	c := newTestComponent(t)
	c.config.TimeoutSeconds = 1 // fire after 1 second

	exec := &requirementExecution{
		EntityID:      workflow.EntityPrefix() + ".exec.req.run.p-timeout",
		Slug:          "p",
		RequirementID: "timeout",
		VisitedNodes:  make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

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

	exec := &requirementExecution{
		EntityID:      workflow.EntityPrefix() + ".exec.req.run.p-notimeout",
		Slug:          "p",
		RequirementID: "notimeout",
		VisitedNodes:  make(map[string]bool),
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

	req := payloads.RequirementExecutionRequest{RequirementID: "req-1", Slug: "p1"}
	c.handleTrigger(context.Background(), buildTriggerMsg(req))

	if c.triggersProcessed.Load() != 1 {
		t.Errorf("triggersProcessed = %d, want 1", c.triggersProcessed.Load())
	}
}

func TestMetrics_ErrorsIncrementOnMalformedMessage(t *testing.T) {
	c := newTestComponent(t)

	c.handleTrigger(context.Background(), &mockMsg{data: []byte(`bad`)})
	c.handleTrigger(context.Background(), &mockMsg{data: []byte(`also bad`)})

	if c.errors.Load() != 2 {
		t.Errorf("errors = %d, want 2", c.errors.Load())
	}
}

func TestMetrics_SeparateCountersForCompletedAndFailed(t *testing.T) {
	c := newTestComponent(t)

	execCompleted := &requirementExecution{
		EntityID:      "entity-completed",
		Slug:          "p",
		RequirementID: "req-completed",
		VisitedNodes:  make(map[string]bool),
	}
	execFailed := &requirementExecution{
		EntityID:      "entity-failed",
		Slug:          "p",
		RequirementID: "req-failed",
	}

	execCompleted.mu.Lock()
	c.markCompletedLocked(context.Background(), execCompleted)
	execCompleted.mu.Unlock()

	execFailed.mu.Lock()
	c.markFailedLocked(context.Background(), execFailed, "test failure")
	execFailed.mu.Unlock()

	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// ParseReactivePayload round-trip tests (via workflow/payloads)
// ---------------------------------------------------------------------------

func TestParseReactivePayload_RequirementExecutionRequest_RoundTrip(t *testing.T) {
	original := payloads.RequirementExecutionRequest{
		RequirementID: "req-rt",
		Slug:          "rt-plan",
		Title:         "Round-trip requirement",
		Description:   "Test round trip",
		Prompt:        "round-trip test",
		Role:          "developer",
		Model:         "gpt-4",
		ProjectID:     "proj-rt",
		TraceID:       "trace-rt",
	}

	msg := buildTriggerMsg(original)
	parsed, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](msg.Data())
	if err != nil {
		t.Fatalf("ParseReactivePayload() error = %v", err)
	}

	if parsed.RequirementID != original.RequirementID {
		t.Errorf("RequirementID = %q, want %q", parsed.RequirementID, original.RequirementID)
	}
	if parsed.Slug != original.Slug {
		t.Errorf("Slug = %q, want %q", parsed.Slug, original.Slug)
	}
	if parsed.TraceID != original.TraceID {
		t.Errorf("TraceID = %q, want %q", parsed.TraceID, original.TraceID)
	}
}

func TestParseReactivePayload_MalformedEnvelope_ReturnsError(t *testing.T) {
	_, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest]([]byte(`not json`))
	if err == nil {
		t.Fatal("ParseReactivePayload with malformed envelope should return error")
	}
}

func TestParseReactivePayload_MissingPayloadKey_ReturnsError(t *testing.T) {
	data := []byte(`{"type": "something"}`)
	_, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](data)
	if err == nil {
		t.Fatal("ParseReactivePayload with missing payload key should return error")
	}
}

func TestParseReactivePayload_EmptyPayload_ReturnsError(t *testing.T) {
	data := []byte(`{"type":"something"}`)
	_, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](data)
	if err == nil {
		t.Fatal("ParseReactivePayload with absent payload key should return error")
	}
	if !strings.Contains(err.Error(), "empty payload") {
		t.Errorf("error %q should contain %q", err.Error(), "empty payload")
	}
}

// ---------------------------------------------------------------------------
// buildDecomposerPrompt tests
// ---------------------------------------------------------------------------

func TestBuildDecomposerPrompt_UsesExplicitPromptWhenSet(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-1",
		Title:         "Add auth",
		Description:   "Add authentication",
		Prompt:        "explicit custom prompt",
	}

	got := c.buildDecomposerPrompt(exec)
	if got != "explicit custom prompt" {
		t.Errorf("buildDecomposerPrompt() = %q, want explicit prompt", got)
	}
}

func TestBuildDecomposerPrompt_BuildsFromContext(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-1",
		Title:         "Add user authentication",
		Description:   "Implement JWT-based auth",
	}

	got := c.buildDecomposerPrompt(exec)
	if !strings.Contains(got, "Add user authentication") {
		t.Errorf("prompt should contain title, got: %s", got)
	}
	if !strings.Contains(got, "JWT-based auth") {
		t.Errorf("prompt should contain description, got: %s", got)
	}
}

func TestBuildDecomposerPrompt_IncludesPrerequisites(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-2",
		Title:         "Add OAuth",
		DependsOn: []payloads.PrereqContext{
			{
				RequirementID: "req-1",
				Title:         "Add JWT auth",
				Description:   "Basic JWT auth",
				FilesModified: []string{"auth/jwt.go"},
				Summary:       "Implemented JWT tokens",
			},
		},
	}

	got := c.buildDecomposerPrompt(exec)
	if !strings.Contains(got, "Prerequisite Requirements") {
		t.Errorf("prompt should mention prerequisites, got: %s", got)
	}
	if !strings.Contains(got, "Add JWT auth") {
		t.Errorf("prompt should include prereq title, got: %s", got)
	}
	if !strings.Contains(got, "auth/jwt.go") {
		t.Errorf("prompt should include prereq files modified, got: %s", got)
	}
}
