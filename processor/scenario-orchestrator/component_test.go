package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func init() {
	// Register payload types so BaseMessage.UnmarshalJSON can deserialize them.
	payloads.RegisterPayloads()
}

// makeTriggerBaseMessage wraps a ScenarioOrchestrationTrigger in a BaseMessage envelope.
// It bypasses payload Validate() by constructing the JSON manually, which allows
// testing invalid payloads (e.g., empty plan_slug) that would fail NewBaseMessage.
func makeTriggerBaseMessage(t *testing.T, trigger *payloads.ScenarioOrchestrationTrigger) []byte {
	t.Helper()
	payloadJSON, err := json.Marshal(trigger)
	if err != nil {
		t.Fatalf("marshal trigger payload: %v", err)
	}
	msgType := payloads.ScenarioOrchestrationTriggerType
	envelope := map[string]any{
		"id": "test-msg-id",
		"type": map[string]string{
			"domain":   msgType.Domain,
			"category": msgType.Category,
			"version":  msgType.Version,
		},
		"payload": json.RawMessage(payloadJSON),
		"meta": map[string]any{
			"created_at": 0,
			"source":     "test",
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal trigger envelope: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.StreamName != "WORKFLOW" {
		t.Errorf("StreamName = %q, want %q", cfg.StreamName, "WORKFLOW")
	}
	if cfg.ConsumerName != "scenario-orchestrator" {
		t.Errorf("ConsumerName = %q, want %q", cfg.ConsumerName, "scenario-orchestrator")
	}
	if cfg.TriggerSubject != "scenario.orchestrate.*" {
		t.Errorf("TriggerSubject = %q, want %q", cfg.TriggerSubject, "scenario.orchestrate.*")
	}
	if cfg.ExecutionTimeout != "120s" {
		t.Errorf("ExecutionTimeout = %q, want %q", cfg.ExecutionTimeout, "120s")
	}
	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want %d", cfg.MaxConcurrent, 5)
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
			name: "missing stream_name",
			cfg: Config{
				ConsumerName:   "consumer",
				TriggerSubject: "scenario.orchestrate.*",
				MaxConcurrent:  5,
			},
			wantErr: true,
			errMsg:  "stream_name is required",
		},
		{
			name: "missing consumer_name",
			cfg: Config{
				StreamName:     "WORKFLOW",
				TriggerSubject: "scenario.orchestrate.*",
				MaxConcurrent:  5,
			},
			wantErr: true,
			errMsg:  "consumer_name is required",
		},
		{
			name: "missing trigger_subject",
			cfg: Config{
				StreamName:    "WORKFLOW",
				ConsumerName:  "consumer",
				MaxConcurrent: 5,
			},
			wantErr: true,
			errMsg:  "trigger_subject is required",
		},
		{
			name: "max_concurrent below minimum",
			cfg: Config{
				StreamName:     "WORKFLOW",
				ConsumerName:   "consumer",
				TriggerSubject: "scenario.orchestrate.*",
				MaxConcurrent:  0,
			},
			wantErr: true,
			errMsg:  "max_concurrent must be at least 1",
		},
		{
			name: "max_concurrent exceeds maximum",
			cfg: Config{
				StreamName:     "WORKFLOW",
				ConsumerName:   "consumer",
				TriggerSubject: "scenario.orchestrate.*",
				MaxConcurrent:  21,
			},
			wantErr: true,
			errMsg:  "max_concurrent cannot exceed 20",
		},
		{
			name: "max_concurrent at boundary minimum",
			cfg: Config{
				StreamName:     "WORKFLOW",
				ConsumerName:   "consumer",
				TriggerSubject: "scenario.orchestrate.*",
				MaxConcurrent:  1,
			},
		},
		{
			name: "max_concurrent at boundary maximum",
			cfg: Config{
				StreamName:     "WORKFLOW",
				ConsumerName:   "consumer",
				TriggerSubject: "scenario.orchestrate.*",
				MaxConcurrent:  20,
			},
		},
		{
			name: "invalid execution_timeout",
			cfg: Config{
				StreamName:       "WORKFLOW",
				ConsumerName:     "consumer",
				TriggerSubject:   "scenario.orchestrate.*",
				MaxConcurrent:    5,
				ExecutionTimeout: "not-a-duration",
			},
			wantErr: true,
			errMsg:  "invalid execution_timeout",
		},
		{
			name: "empty execution_timeout is valid (uses default)",
			cfg: Config{
				StreamName:       "WORKFLOW",
				ConsumerName:     "consumer",
				TriggerSubject:   "scenario.orchestrate.*",
				MaxConcurrent:    5,
				ExecutionTimeout: "",
			},
		},
		{
			name: "valid non-default execution_timeout",
			cfg: Config{
				StreamName:       "WORKFLOW",
				ConsumerName:     "consumer",
				TriggerSubject:   "scenario.orchestrate.*",
				MaxConcurrent:    5,
				ExecutionTimeout: "30s",
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
				if tt.errMsg != "" && !containsSubstring(err.Error(), tt.errMsg) {
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

func TestConfig_GetExecutionTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected time.Duration
	}{
		{
			name:     "default 120s",
			timeout:  "120s",
			expected: 120 * time.Second,
		},
		{
			name:     "30 seconds",
			timeout:  "30s",
			expected: 30 * time.Second,
		},
		{
			name:     "5 minutes",
			timeout:  "5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "empty falls back to default",
			timeout:  "",
			expected: 120 * time.Second,
		},
		{
			name:     "invalid string falls back to default",
			timeout:  "not-a-duration",
			expected: 120 * time.Second,
		},
		{
			name:     "zero duration falls back to default",
			timeout:  "0s",
			expected: 120 * time.Second,
		},
		{
			name:     "negative duration falls back to default",
			timeout:  "-10s",
			expected: 120 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ExecutionTimeout: tt.timeout}
			got := cfg.GetExecutionTimeout()
			if got != tt.expected {
				t.Errorf("GetExecutionTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Component construction tests
// ---------------------------------------------------------------------------

func TestNewComponent_Defaults(t *testing.T) {
	// Pass an empty config — all defaults should be applied.
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

	if c.config.StreamName != "WORKFLOW" {
		t.Errorf("StreamName = %q, want %q", c.config.StreamName, "WORKFLOW")
	}
	if c.config.ConsumerName != "scenario-orchestrator" {
		t.Errorf("ConsumerName = %q, want %q", c.config.ConsumerName, "scenario-orchestrator")
	}
	if c.config.TriggerSubject != "scenario.orchestrate.*" {
		t.Errorf("TriggerSubject = %q, want %q", c.config.TriggerSubject, "scenario.orchestrate.*")
	}
	if c.config.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want %d", c.config.MaxConcurrent, 5)
	}
	if c.config.ExecutionTimeout != "120s" {
		t.Errorf("ExecutionTimeout = %q, want %q", c.config.ExecutionTimeout, "120s")
	}
}

func TestNewComponent_PartialOverride(t *testing.T) {
	// Only override some fields; the rest should remain as defaults.
	rawCfg, err := json.Marshal(map[string]any{
		"max_concurrent": 3,
		"consumer_name":  "my-consumer",
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
	if c.config.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, want 3", c.config.MaxConcurrent)
	}
	if c.config.ConsumerName != "my-consumer" {
		t.Errorf("ConsumerName = %q, want %q", c.config.ConsumerName, "my-consumer")
	}
	// Unset fields should have defaults applied.
	if c.config.StreamName != "WORKFLOW" {
		t.Errorf("StreamName = %q, want default %q", c.config.StreamName, "WORKFLOW")
	}
}

func TestNewComponent_InvalidConfig(t *testing.T) {
	// max_concurrent of 0 is explicitly set and should fail validation.
	rawCfg, err := json.Marshal(map[string]any{
		"stream_name":     "WORKFLOW",
		"consumer_name":   "consumer",
		"trigger_subject": "scenario.orchestrate.*",
		"max_concurrent":  25, // Exceeds max of 20
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	deps := component.Dependencies{}
	_, err = NewComponent(rawCfg, deps)
	if err == nil {
		t.Fatal("NewComponent() expected error for invalid config, got nil")
	}
}

func TestNewComponent_MalformedJSON(t *testing.T) {
	_, err := NewComponent([]byte("not json"), component.Dependencies{})
	if err == nil {
		t.Fatal("NewComponent() expected error for malformed JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// OrchestratorTrigger parsing tests
// ---------------------------------------------------------------------------

func TestOrchestratorTrigger_ValidJSON(t *testing.T) {
	raw := []byte(`{
		"plan_slug": "auth-refresh",
		"trace_id": "trace-abc"
	}`)

	var trigger OrchestratorTrigger
	if err := json.Unmarshal(raw, &trigger); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if trigger.PlanSlug != "auth-refresh" {
		t.Errorf("PlanSlug = %q, want %q", trigger.PlanSlug, "auth-refresh")
	}
	if trigger.TraceID != "trace-abc" {
		t.Errorf("TraceID = %q, want %q", trigger.TraceID, "trace-abc")
	}
}

func TestOrchestratorTrigger_EmptyPlanSlug(t *testing.T) {
	raw := []byte(`{"plan_slug": ""}`)

	var trigger OrchestratorTrigger
	if err := json.Unmarshal(raw, &trigger); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if trigger.PlanSlug != "" {
		t.Errorf("PlanSlug = %q, want empty", trigger.PlanSlug)
	}
}

// ---------------------------------------------------------------------------
// handleTrigger unit tests (with mockMsg)
// ---------------------------------------------------------------------------

func TestHandleTrigger_MalformedJSON(t *testing.T) {
	comp := newTestComponent(t)

	msg := &mockMsg{data: []byte("not-json-at-all")}
	comp.handleTrigger(context.Background(), msg)

	if !msg.naked {
		t.Error("expected message to be NAK'd on malformed JSON")
	}
	if msg.acked {
		t.Error("expected message NOT to be ACK'd on malformed JSON")
	}
}

func TestHandleTrigger_MissingPlanSlug(t *testing.T) {
	comp := newTestComponent(t)

	raw := makeTriggerBaseMessage(t, &payloads.ScenarioOrchestrationTrigger{
		Scenarios: []payloads.ScenarioOrchestrationRef{{ScenarioID: "sc-1", Prompt: "test"}},
	})
	msg := &mockMsg{data: raw}
	comp.handleTrigger(context.Background(), msg)

	if !msg.naked {
		t.Error("expected message to be NAK'd when plan_slug is missing")
	}
	if msg.acked {
		t.Error("expected message NOT to be ACK'd when plan_slug is missing")
	}
}

func TestHandleTrigger_EmptyPlan_NilNATSClient(t *testing.T) {
	// With no requirements on disk the dispatch returns nil and the
	// message should be ACK'd even when natsClient is nil.
	comp := newTestComponent(t)

	raw := makeTriggerBaseMessage(t, &payloads.ScenarioOrchestrationTrigger{
		PlanSlug: "my-plan",
	})
	msg := &mockMsg{data: raw}
	comp.handleTrigger(context.Background(), msg)

	// dispatchRequirements loads from disk (empty) and returns nil — ACK'd.
	if msg.naked {
		t.Error("expected message NOT to be NAK'd for plan with no requirements")
	}
	if !msg.acked {
		t.Error("expected message to be ACK'd for plan with no requirements")
	}
}

func TestHandleTrigger_CancelledContext(t *testing.T) {
	// With a pre-cancelled context, dispatch exits early. Validates no panic.
	comp := newTestComponent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	raw := makeTriggerBaseMessage(t, &payloads.ScenarioOrchestrationTrigger{
		PlanSlug: "my-plan",
	})
	msg := &mockMsg{data: raw}

	comp.handleTrigger(ctx, msg)

	if !msg.acked && !msg.naked {
		t.Error("expected message to be either ACK'd or NAK'd")
	}
}

func TestHandleTrigger_IncrementsTriggerCounter(t *testing.T) {
	comp := newTestComponent(t)

	raw := makeTriggerBaseMessage(t, &payloads.ScenarioOrchestrationTrigger{
		PlanSlug:  "my-plan",
		Scenarios: []payloads.ScenarioOrchestrationRef{},
	})
	msg := &mockMsg{data: raw}

	before := comp.triggersProcessed.Load()
	comp.handleTrigger(context.Background(), msg)
	after := comp.triggersProcessed.Load()

	if after != before+1 {
		t.Errorf("triggersProcessed = %d, want %d", after, before+1)
	}
}

// ---------------------------------------------------------------------------
// dispatchRequirements unit tests
// ---------------------------------------------------------------------------

func TestDispatchRequirements_NoRequirements(t *testing.T) {
	comp := newTestComponent(t)

	trigger := OrchestratorTrigger{
		PlanSlug: "my-plan",
	}

	// No requirements on disk — returns nil.
	err := comp.dispatchRequirements(context.Background(), &trigger)
	if err != nil {
		t.Errorf("dispatchRequirements() with no requirements = %v, want nil", err)
	}
}

func TestDispatchRequirements_ContextCancellation(t *testing.T) {
	comp := newTestComponent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	trigger := OrchestratorTrigger{
		PlanSlug: "my-plan",
	}

	// Should not panic and should handle cancellation gracefully.
	_ = comp.dispatchRequirements(ctx, &trigger)
}

// ---------------------------------------------------------------------------
// Meta / Health / Ports / IsRunning tests
// ---------------------------------------------------------------------------

func TestMeta(t *testing.T) {
	comp := newTestComponent(t)

	meta := comp.Meta()
	if meta.Name != "scenario-orchestrator" {
		t.Errorf("Meta().Name = %q, want %q", meta.Name, "scenario-orchestrator")
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
		t.Errorf("Health().Status = %q, want %q", health.Status, "stopped")
	}
}

func TestInputPorts(t *testing.T) {
	comp := newTestComponent(t)

	ports := comp.InputPorts()
	if len(ports) == 0 {
		t.Error("InputPorts() should return at least one port")
	}

	for _, p := range ports {
		if p.Direction != component.DirectionInput {
			t.Errorf("port %q direction = %v, want DirectionInput", p.Name, p.Direction)
		}
		natsCfg, ok := p.Config.(component.NATSPort)
		if !ok {
			t.Errorf("port %q Config is not NATSPort", p.Name)
			continue
		}
		if natsCfg.Subject == "" {
			t.Errorf("port %q NATSPort.Subject is empty", p.Name)
		}
	}
}

func TestOutputPorts(t *testing.T) {
	comp := newTestComponent(t)

	ports := comp.OutputPorts()
	if len(ports) == 0 {
		t.Error("OutputPorts() should return at least one port")
	}

	for _, p := range ports {
		if p.Direction != component.DirectionOutput {
			t.Errorf("port %q direction = %v, want DirectionOutput", p.Name, p.Direction)
		}
	}
}

func TestInputPorts_NilPortConfig(t *testing.T) {
	rawCfg, _ := json.Marshal(map[string]any{
		"stream_name":     "WORKFLOW",
		"consumer_name":   "consumer",
		"trigger_subject": "scenario.orchestrate.*",
		"max_concurrent":  1,
		"ports":           nil,
	})

	deps := component.Dependencies{}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		// Defaults will have set Ports — this path tests the nil check in InputPorts.
		// If defaults filled in Ports, we can skip this test gracefully.
		t.Skipf("defaults applied Ports config, skipping nil-ports test: %v", err)
	}

	c := comp.(*Component)
	c.config.Ports = nil

	inputs := c.InputPorts()
	if inputs == nil {
		t.Error("InputPorts() with nil Ports should return empty slice, not nil")
	}

	outputs := c.OutputPorts()
	if outputs == nil {
		t.Error("OutputPorts() with nil Ports should return empty slice, not nil")
	}
}

func TestIsRunning_BeforeStart(t *testing.T) {
	comp := newTestComponent(t)

	if comp.IsRunning() {
		t.Error("IsRunning() = true before Start(), want false")
	}
}

func TestDataFlow_ReturnsMetrics(t *testing.T) {
	comp := newTestComponent(t)

	flow := comp.DataFlow()
	// Just verify the call doesn't panic and returns a valid struct.
	_ = flow.MessagesPerSecond
	_ = flow.ErrorRate
}

func TestStart_NilNATSClient(t *testing.T) {
	comp := newTestComponent(t)

	err := comp.Start(context.Background())
	if err == nil {
		t.Error("Start() with nil NATS client should return error")
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	comp := newTestComponent(t)
	// Manually set running to simulate an already-started component.
	comp.mu.Lock()
	comp.running = true
	comp.mu.Unlock()

	err := comp.Start(context.Background())
	if err == nil {
		t.Error("Start() on already-running component should return error")
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

func TestInitialize(t *testing.T) {
	comp := newTestComponent(t)

	if err := comp.Initialize(); err != nil {
		t.Errorf("Initialize() = %v, want nil", err)
	}
}

func TestConfigSchema(t *testing.T) {
	comp := newTestComponent(t)

	schema := comp.ConfigSchema()
	// ConfigSchema should return a non-zero value.
	_ = schema
}

func TestRegister_NilRegistry(t *testing.T) {
	err := Register(nil)
	if err == nil {
		t.Error("Register(nil) should return error")
	}
}

// ---------------------------------------------------------------------------
// Helpers
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

// containsSubstring reports whether s contains substr.
func containsSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// mockMsg implements jetstream.Msg for unit tests.
// ---------------------------------------------------------------------------

type mockMsg struct {
	data  []byte
	acked bool
	naked bool
}

func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Subject() string                           { return "scenario.orchestrate.test" }
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
