package trajectoryapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
)

func TestComponent_StateTransitions(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
	}

	// Initial state should be stopped
	if c.state.Load() != stateStopped {
		t.Errorf("Initial state = %d, want %d (stopped)", c.state.Load(), stateStopped)
	}

	// Health should report unhealthy when stopped
	health := c.Health()
	if health.Healthy {
		t.Error("Health.Healthy = true, want false when stopped")
	}
	if health.Status != "stopped" {
		t.Errorf("Health.Status = %q, want %q", health.Status, "stopped")
	}
}

func TestComponent_Meta(t *testing.T) {
	c := &Component{
		name: "trajectory-api",
	}

	meta := c.Meta()

	if meta.Name != "trajectory-api" {
		t.Errorf("Meta.Name = %q, want %q", meta.Name, "trajectory-api")
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q, want %q", meta.Type, "processor")
	}
	if meta.Version == "" {
		t.Error("Meta.Version should not be empty")
	}
	if meta.Description == "" {
		t.Error("Meta.Description should not be empty")
	}
}

func TestComponent_Ports(t *testing.T) {
	c := &Component{}

	// Trajectory-api has no input/output ports (HTTP only)
	inputPorts := c.InputPorts()
	if len(inputPorts) != 0 {
		t.Errorf("InputPorts count = %d, want 0", len(inputPorts))
	}

	outputPorts := c.OutputPorts()
	if len(outputPorts) != 0 {
		t.Errorf("OutputPorts count = %d, want 0", len(outputPorts))
	}
}

func TestComponent_ConfigSchema(t *testing.T) {
	c := &Component{}

	schema := c.ConfigSchema()

	// Schema should have properties
	if len(schema.Properties) == 0 {
		t.Error("ConfigSchema.Properties should not be empty")
	}
}

func TestComponent_DataFlow(t *testing.T) {
	c := &Component{}

	flow := c.DataFlow()

	// DataFlow should return zero metrics (no streaming data)
	if flow.MessagesPerSecond != 0 {
		t.Errorf("DataFlow.MessagesPerSecond = %f, want 0", flow.MessagesPerSecond)
	}
	if flow.BytesPerSecond != 0 {
		t.Errorf("DataFlow.BytesPerSecond = %f, want 0", flow.BytesPerSecond)
	}
}

func TestComponent_Initialize(t *testing.T) {
	c := &Component{
		logger: slog.Default(),
		config: Config{
			LLMCallsBucket: "LLM_CALLS",
			LoopsBucket:    "AGENT_LOOPS",
		},
	}

	err := c.Initialize()
	if err != nil {
		t.Errorf("Initialize() error = %v, want nil", err)
	}
}

func TestComponent_StartWithoutNATSClient(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
		config: Config{
			LLMCallsBucket: "LLM_CALLS",
			LoopsBucket:    "AGENT_LOOPS",
		},
		// natsClient is nil
	}

	ctx := context.Background()
	err := c.Start(ctx)

	if err == nil {
		t.Error("Start() should return error when NATS client is nil")
	}

	// State should remain stopped after failed start
	if c.state.Load() != stateStopped {
		t.Errorf("State after failed start = %d, want %d (stopped)", c.state.Load(), stateStopped)
	}
}

func TestComponent_StopWhenStopped(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
	}
	c.state.Store(stateStopped)

	// Stopping when already stopped should be safe
	err := c.Stop(time.Second)
	if err != nil {
		t.Errorf("Stop() when stopped error = %v, want nil", err)
	}
}

func TestComponent_HealthStatusMapping(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
	}

	tests := []struct {
		name          string
		state         int32
		expectedOK    bool
		expectedState string
	}{
		{
			name:          "stopped state",
			state:         stateStopped,
			expectedOK:    false,
			expectedState: "stopped",
		},
		{
			name:          "starting state",
			state:         stateStarting,
			expectedOK:    false,
			expectedState: "starting",
		},
		{
			name:          "running state",
			state:         stateRunning,
			expectedOK:    true,
			expectedState: "running",
		},
		{
			name:          "stopping state",
			state:         stateStopping,
			expectedOK:    false,
			expectedState: "stopping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.state.Store(tt.state)
			health := c.Health()

			if health.Healthy != tt.expectedOK {
				t.Errorf("Health.Healthy = %v, want %v", health.Healthy, tt.expectedOK)
			}
			if health.Status != tt.expectedState {
				t.Errorf("Health.Status = %q, want %q", health.Status, tt.expectedState)
			}
		})
	}
}

func TestComponent_ConcurrentHealthChecks(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
	}
	c.state.Store(stateRunning)
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			health := c.Health()
			// All goroutines should see running state
			if health.Status != "running" {
				t.Errorf("Health.Status = %q, want %q", health.Status, "running")
			}
		}()
	}
	wg.Wait()
}

func TestComponent_UptimeCalculation(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
	}
	c.state.Store(stateRunning)

	startTime := time.Now().Add(-5 * time.Second)
	c.mu.Lock()
	c.startTime = startTime
	c.mu.Unlock()

	health := c.Health()

	// Uptime should be at least 5 seconds
	if health.Uptime < 5*time.Second {
		t.Errorf("Health.Uptime = %v, want >= 5s", health.Uptime)
	}
	// And not unreasonably large
	if health.Uptime > 10*time.Second {
		t.Errorf("Health.Uptime = %v, want < 10s", health.Uptime)
	}
}

func TestConfig_DefaultValues(t *testing.T) {
	defaults := DefaultConfig()

	if defaults.LLMCallsBucket != "LLM_CALLS" {
		t.Errorf("DefaultConfig().LLMCallsBucket = %q, want %q", defaults.LLMCallsBucket, "LLM_CALLS")
	}
	if defaults.LoopsBucket != "AGENT_LOOPS" {
		t.Errorf("DefaultConfig().LoopsBucket = %q, want %q", defaults.LoopsBucket, "AGENT_LOOPS")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				LLMCallsBucket:  "LLM_CALLS",
				ToolCallsBucket: "TOOL_CALLS",
				LoopsBucket:     "AGENT_LOOPS",
			},
			wantErr: false,
		},
		{
			name: "missing tool_calls_bucket",
			config: Config{
				LLMCallsBucket:  "LLM_CALLS",
				ToolCallsBucket: "",
				LoopsBucket:     "AGENT_LOOPS",
			},
			wantErr: true,
		},
		{
			name: "missing llm_calls_bucket",
			config: Config{
				LLMCallsBucket: "",
				LoopsBucket:    "AGENT_LOOPS",
			},
			wantErr: true,
		},
		{
			name: "missing loops_bucket",
			config: Config{
				LLMCallsBucket: "LLM_CALLS",
				LoopsBucket:    "",
			},
			wantErr: true,
		},
		{
			name: "both missing",
			config: Config{
				LLMCallsBucket: "",
				LoopsBucket:    "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewComponent_ValidConfig(t *testing.T) {
	rawConfig := json.RawMessage(`{
		"llm_calls_bucket": "CUSTOM_LLM_CALLS",
		"loops_bucket": "CUSTOM_LOOPS"
	}`)

	// Create dependencies with required fields
	deps := component.Dependencies{
		Logger: slog.Default(),
	}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	c, ok := comp.(*Component)
	if !ok {
		t.Fatal("NewComponent() did not return *Component")
	}

	if c.config.LLMCallsBucket != "CUSTOM_LLM_CALLS" {
		t.Errorf("config.LLMCallsBucket = %q, want %q", c.config.LLMCallsBucket, "CUSTOM_LLM_CALLS")
	}
	if c.config.LoopsBucket != "CUSTOM_LOOPS" {
		t.Errorf("config.LoopsBucket = %q, want %q", c.config.LoopsBucket, "CUSTOM_LOOPS")
	}
}

func TestNewComponent_DefaultsApplied(t *testing.T) {
	// Empty config - defaults should be applied
	rawConfig := json.RawMessage(`{}`)

	deps := component.Dependencies{
		Logger: slog.Default(),
	}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	c, ok := comp.(*Component)
	if !ok {
		t.Fatal("NewComponent() did not return *Component")
	}

	if c.config.LLMCallsBucket != "LLM_CALLS" {
		t.Errorf("config.LLMCallsBucket = %q, want default %q", c.config.LLMCallsBucket, "LLM_CALLS")
	}
	if c.config.LoopsBucket != "AGENT_LOOPS" {
		t.Errorf("config.LoopsBucket = %q, want default %q", c.config.LoopsBucket, "AGENT_LOOPS")
	}
}

func TestNewComponent_InvalidJSON(t *testing.T) {
	rawConfig := json.RawMessage(`{invalid json}`)

	deps := component.Dependencies{
		Logger: slog.Default(),
	}

	_, err := NewComponent(rawConfig, deps)
	if err == nil {
		t.Error("NewComponent() should return error for invalid JSON")
	}
}

func TestComponent_DoubleStart(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
		config: Config{
			LLMCallsBucket: "LLM_CALLS",
			LoopsBucket:    "AGENT_LOOPS",
		},
	}

	// Simulate running state
	c.state.Store(stateRunning)

	ctx := context.Background()
	err := c.Start(ctx)

	if err == nil {
		t.Error("Start() should return error when already running")
	}
}

func TestComponent_DoubleStop(t *testing.T) {
	c := &Component{
		name:   "trajectory-api",
		logger: slog.Default(),
	}

	// First stop (from stopped state - should be no-op)
	err := c.Stop(time.Second)
	if err != nil {
		t.Errorf("First Stop() error = %v, want nil", err)
	}

	// Second stop should also be safe
	err = c.Stop(time.Second)
	if err != nil {
		t.Errorf("Second Stop() error = %v, want nil", err)
	}
}
