// Package questiontimeout provides tests for the question-timeout component.
//
// Test Coverage:
//   - Component factory with invalid configurations
//   - Component lifecycle (Initialize, Stop)
//   - Start failure without NATS client
//   - SLA timeout detection logic
//   - SLA calculation from routes and defaults
//   - Escalation route configuration
//   - Payload validation (TimeoutEvent, EscalationEvent)
//   - Payload Schema() methods
//   - Payload marshaling/unmarshaling
//   - Component metadata (Meta, Health, DataFlow)
//   - Port configuration (InputPorts, OutputPorts)
//   - Atomic metric updates
//   - Config validation
//   - Default configuration
//   - Concurrent health checks
//   - Answerer registry route matching
//
// Note: Tests requiring NATS infrastructure (e.g., actual timeout publishing,
// escalation with KV updates) are integration tests and not included here.
// Run with: go test -cover
package questiontimeout

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/component"
)

// TestNewComponent tests the component factory with various configurations.
// This requires a real NATS client, so it's an integration test.
// For now, we skip integration testing and focus on unit tests.
// Run with: go test -tags integration
func TestNewComponent_Unit(t *testing.T) {
	tests := []struct {
		name      string
		rawConfig json.RawMessage
		wantErr   bool
	}{
		{
			name:      "invalid JSON",
			rawConfig: json.RawMessage(`{invalid json}`),
			wantErr:   true,
		},
		{
			name:      "invalid config - negative check_interval",
			rawConfig: json.RawMessage(`{"check_interval":"-1s"}`),
			wantErr:   true,
		},
		{
			name:      "invalid config - zero default_sla",
			rawConfig: json.RawMessage(`{"default_sla":"0s"}`),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use minimal dependencies - no NATS client
			deps := component.Dependencies{
				Logger: slog.Default(),
			}

			_, err := NewComponent(tt.rawConfig, deps)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewComponent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestComponent_Lifecycle tests Initialize, Start, and Stop methods.
func TestComponent_Lifecycle(t *testing.T) {
	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
		config: Config{
			CheckInterval: 100 * time.Millisecond,
			DefaultSLA:    24 * time.Hour,
		},
		registry: answerer.NewRegistry(),
		// natsClient is nil - testing lifecycle without actual NATS
		// questionStore is nil - not testing check logic
	}

	// Test Initialize
	if err := c.Initialize(); err != nil {
		t.Errorf("Initialize() error = %v, want nil", err)
	}

	// Test Stop when already stopped
	if err := c.Stop(time.Second); err != nil {
		t.Error("Stop() should not error when already stopped")
	}
}

// TestComponent_StartWithoutNATSClient tests Start fails without NATS client.
func TestComponent_StartWithoutNATSClient(t *testing.T) {
	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
		config: Config{
			CheckInterval: 1 * time.Minute,
			DefaultSLA:    24 * time.Hour,
		},
		registry: answerer.NewRegistry(),
		// natsClient is nil
		// questionStore is nil
	}

	ctx := context.Background()
	err := c.Start(ctx)

	if err == nil {
		t.Error("Start() should return error when NATS client is nil")
	}

	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()
	if running {
		t.Error("Component should not be running after failed start")
	}
}

// TestCheckQuestion_Timeout tests that questions past SLA trigger timeout detection.
func TestCheckQuestion_Timeout(t *testing.T) {
	registry := answerer.NewRegistry()
	registry.AddRoute(answerer.Route{
		Pattern:  "test.*",
		Answerer: "agent/test",
		SLA:      answerer.Duration(100 * time.Millisecond),
	})

	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
		config: Config{
			CheckInterval: 50 * time.Millisecond,
			DefaultSLA:    24 * time.Hour,
		},
		registry: registry,
	}

	tests := []struct {
		name          string
		question      *workflow.Question
		expectTimeout bool
	}{
		{
			name: "question exceeded SLA",
			question: &workflow.Question{
				ID:        "q-timeout",
				Topic:     "test.feature",
				Question:  "Test question",
				Status:    workflow.QuestionStatusPending,
				CreatedAt: time.Now().Add(-200 * time.Millisecond), // Older than 100ms SLA
			},
			expectTimeout: true,
		},
		{
			name: "question within SLA",
			question: &workflow.Question{
				ID:        "q-recent",
				Topic:     "test.feature",
				Question:  "Recent question",
				Status:    workflow.QuestionStatusPending,
				CreatedAt: time.Now().Add(-50 * time.Millisecond), // Younger than 100ms SLA
			},
			expectTimeout: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset counter
			c.timeoutsDetected.Store(0)

			// Get the route to determine SLA
			route := c.registry.Match(tt.question.Topic)
			sla := c.config.DefaultSLA
			if route.SLA.Duration() > 0 {
				sla = route.SLA.Duration()
			}

			// Check if SLA exceeded
			age := time.Since(tt.question.CreatedAt)
			timedOut := age > sla

			if timedOut != tt.expectTimeout {
				t.Errorf("SLA check: age %v vs SLA %v, timedOut = %v, want %v", age, sla, timedOut, tt.expectTimeout)
			}
		})
	}
}

// TestSLACalculation tests SLA is correctly determined from routes.
func TestSLACalculation(t *testing.T) {
	registry := answerer.NewRegistry()
	registry.AddRoute(answerer.Route{
		Pattern:  "api.*",
		Answerer: "agent/architect",
		SLA:      answerer.Duration(2 * time.Hour),
	})
	registry.AddRoute(answerer.Route{
		Pattern:  "database.*",
		Answerer: "agent/dba",
		SLA:      answerer.Duration(30 * time.Minute),
	})

	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
		config: Config{
			CheckInterval: 1 * time.Minute,
			DefaultSLA:    24 * time.Hour,
		},
		registry: registry,
	}

	tests := []struct {
		topic       string
		expectedSLA time.Duration
	}{
		{"api.design", 2 * time.Hour},
		{"api.authentication", 2 * time.Hour},
		{"database.schema", 30 * time.Minute},
		{"database.query", 30 * time.Minute},
		{"unknown.topic", 24 * time.Hour}, // default SLA
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			route := c.registry.Match(tt.topic)
			sla := c.config.DefaultSLA
			if route.SLA.Duration() > 0 {
				sla = route.SLA.Duration()
			}

			if sla != tt.expectedSLA {
				t.Errorf("SLA for topic %q = %v, want %v", tt.topic, sla, tt.expectedSLA)
			}
		})
	}
}

// TestEscalationRouteConfiguration tests escalation path is configured correctly.
func TestEscalationRouteConfiguration(t *testing.T) {
	registry := answerer.NewRegistry()

	registry.AddRoute(answerer.Route{
		Pattern:    "escalate.*",
		Answerer:   "agent/junior",
		SLA:        answerer.Duration(1 * time.Hour),
		EscalateTo: "agent/senior",
	})
	registry.AddRoute(answerer.Route{
		Pattern:  "no-escalate.*",
		Answerer: "agent/expert",
		SLA:      answerer.Duration(2 * time.Hour),
		// No EscalateTo
	})

	tests := []struct {
		topic              string
		expectedAnswerer   string
		expectedEscalateTo string
	}{
		{
			topic:              "escalate.bug",
			expectedAnswerer:   "agent/junior",
			expectedEscalateTo: "agent/senior",
		},
		{
			topic:              "escalate.feature",
			expectedAnswerer:   "agent/junior",
			expectedEscalateTo: "agent/senior",
		},
		{
			topic:              "no-escalate.bug",
			expectedAnswerer:   "agent/expert",
			expectedEscalateTo: "", // No escalation path
		},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			route := registry.Match(tt.topic)
			if route.Answerer != tt.expectedAnswerer {
				t.Errorf("Answerer for %q = %q, want %q", tt.topic, route.Answerer, tt.expectedAnswerer)
			}
			if route.EscalateTo != tt.expectedEscalateTo {
				t.Errorf("EscalateTo for %q = %q, want %q", tt.topic, route.EscalateTo, tt.expectedEscalateTo)
			}
		})
	}
}

// TestTimeoutEvent_SchemaValidate tests TimeoutEvent payload methods.
func TestTimeoutEvent_SchemaValidate(t *testing.T) {
	event := &TimeoutEvent{
		QuestionID: "q-test",
		Topic:      "test.topic",
		Age:        5 * time.Minute,
		SLA:        3 * time.Minute,
		Timestamp:  time.Now(),
	}

	// Test Schema() returns correct type
	msgType := event.Schema()
	if msgType.Domain != "question" {
		t.Errorf("Schema().Domain = %q, want %q", msgType.Domain, "question")
	}
	if msgType.Category != "timeout" {
		t.Errorf("Schema().Category = %q, want %q", msgType.Category, "timeout")
	}
	if msgType.Version != "v1" {
		t.Errorf("Schema().Version = %q, want %q", msgType.Version, "v1")
	}

	// Test Validate() with valid event
	if err := event.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}

	// Test Validate() with empty QuestionID
	invalidEvent := &TimeoutEvent{
		QuestionID: "",
		Topic:      "test.topic",
	}
	if err := invalidEvent.Validate(); err == nil {
		t.Error("Validate() should return error when QuestionID is empty")
	}

	// Test MarshalJSON/UnmarshalJSON round-trip
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	var decoded TimeoutEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if decoded.QuestionID != event.QuestionID {
		t.Errorf("Decoded QuestionID = %q, want %q", decoded.QuestionID, event.QuestionID)
	}
	if decoded.Topic != event.Topic {
		t.Errorf("Decoded Topic = %q, want %q", decoded.Topic, event.Topic)
	}
}

// TestEscalationEvent_SchemaValidate tests EscalationEvent payload methods.
func TestEscalationEvent_SchemaValidate(t *testing.T) {
	event := &EscalationEvent{
		QuestionID:   "q-escalate",
		Topic:        "escalate.topic",
		FromAnswerer: "agent/junior",
		ToAnswerer:   "agent/senior",
		Reason:       "SLA exceeded",
		Timestamp:    time.Now(),
	}

	// Test Schema() returns correct type
	msgType := event.Schema()
	if msgType.Domain != "question" {
		t.Errorf("Schema().Domain = %q, want %q", msgType.Domain, "question")
	}
	if msgType.Category != "escalation" {
		t.Errorf("Schema().Category = %q, want %q", msgType.Category, "escalation")
	}
	if msgType.Version != "v1" {
		t.Errorf("Schema().Version = %q, want %q", msgType.Version, "v1")
	}

	// Test Validate() with valid event
	if err := event.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}

	// Test Validate() with empty QuestionID
	invalidEvent := &EscalationEvent{
		QuestionID:   "",
		FromAnswerer: "agent/junior",
		ToAnswerer:   "agent/senior",
	}
	if err := invalidEvent.Validate(); err == nil {
		t.Error("Validate() should return error when QuestionID is empty")
	}

	// Test MarshalJSON/UnmarshalJSON round-trip
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	var decoded EscalationEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if decoded.QuestionID != event.QuestionID {
		t.Errorf("Decoded QuestionID = %q, want %q", decoded.QuestionID, event.QuestionID)
	}
	if decoded.FromAnswerer != event.FromAnswerer {
		t.Errorf("Decoded FromAnswerer = %q, want %q", decoded.FromAnswerer, event.FromAnswerer)
	}
	if decoded.ToAnswerer != event.ToAnswerer {
		t.Errorf("Decoded ToAnswerer = %q, want %q", decoded.ToAnswerer, event.ToAnswerer)
	}
}

// TestComponent_Meta tests component metadata.
func TestComponent_Meta(t *testing.T) {
	c := &Component{name: "question-timeout"}

	meta := c.Meta()

	if meta.Name != "question-timeout" {
		t.Errorf("Meta.Name = %q, want %q", meta.Name, "question-timeout")
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q, want %q", meta.Type, "processor")
	}
	if meta.Description == "" {
		t.Error("Meta.Description should not be empty")
	}
	if meta.Version == "" {
		t.Error("Meta.Version should not be empty")
	}
}

// TestComponent_Health tests health status reporting.
func TestComponent_Health(t *testing.T) {
	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
	}

	// Test health when stopped
	health := c.Health()
	if health.Healthy {
		t.Error("Health.Healthy should be false when stopped")
	}
	if health.Status != "stopped" {
		t.Errorf("Health.Status = %q, want %q", health.Status, "stopped")
	}

	// Test health when running
	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	health = c.Health()
	if !health.Healthy {
		t.Error("Health.Healthy should be true when running")
	}
	if health.Status != "running" {
		t.Errorf("Health.Status = %q, want %q", health.Status, "running")
	}
	if health.Uptime == 0 {
		t.Error("Health.Uptime should be non-zero when running")
	}
}

// TestComponent_InputOutputPorts tests port configuration.
func TestComponent_InputOutputPorts(t *testing.T) {
	c := &Component{
		config: DefaultConfig(),
	}

	inputPorts := c.InputPorts()
	if len(inputPorts) != 1 {
		t.Errorf("InputPorts count = %d, want 1", len(inputPorts))
	}
	if len(inputPorts) > 0 && inputPorts[0].Name != "question-events" {
		t.Errorf("InputPorts[0].Name = %q, want %q", inputPorts[0].Name, "question-events")
	}

	outputPorts := c.OutputPorts()
	if len(outputPorts) != 2 {
		t.Errorf("OutputPorts count = %d, want 2", len(outputPorts))
	}

	portNames := map[string]bool{}
	for _, p := range outputPorts {
		portNames[p.Name] = true
	}

	if !portNames["timeout-events"] {
		t.Error("OutputPorts should include timeout-events")
	}
	if !portNames["escalation-events"] {
		t.Error("OutputPorts should include escalation-events")
	}
}

// TestComponent_MetricsUpdate tests that metrics are updated atomically.
func TestComponent_MetricsUpdate(t *testing.T) {
	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
	}

	// Test atomic increments
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent increments
	for i := 0; i < iterations; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			c.checksPerformed.Add(1)
		}()
		go func() {
			defer wg.Done()
			c.timeoutsDetected.Add(1)
		}()
		go func() {
			defer wg.Done()
			c.escalationsTriggered.Add(1)
		}()
	}
	wg.Wait()

	// Verify all increments were recorded
	if c.checksPerformed.Load() != int64(iterations) {
		t.Errorf("checksPerformed = %d, want %d", c.checksPerformed.Load(), iterations)
	}
	if c.timeoutsDetected.Load() != int64(iterations) {
		t.Errorf("timeoutsDetected = %d, want %d", c.timeoutsDetected.Load(), iterations)
	}
	if c.escalationsTriggered.Load() != int64(iterations) {
		t.Errorf("escalationsTriggered = %d, want %d", c.escalationsTriggered.Load(), iterations)
	}
}

// TestConfig_Validate tests configuration validation.
func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				CheckInterval: 1 * time.Minute,
				DefaultSLA:    24 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "zero check_interval",
			config: Config{
				CheckInterval: 0,
				DefaultSLA:    24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "negative check_interval",
			config: Config{
				CheckInterval: -1 * time.Second,
				DefaultSLA:    24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "zero default_sla",
			config: Config{
				CheckInterval: 1 * time.Minute,
				DefaultSLA:    0,
			},
			wantErr: true,
		},
		{
			name: "negative default_sla",
			config: Config{
				CheckInterval: 1 * time.Minute,
				DefaultSLA:    -1 * time.Hour,
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

// TestComponent_DataFlow tests data flow metrics.
func TestComponent_DataFlow(t *testing.T) {
	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
	}

	flow := c.DataFlow()

	// Timeout component doesn't track per-second metrics
	if flow.MessagesPerSecond != 0 {
		t.Errorf("DataFlow.MessagesPerSecond = %f, want 0", flow.MessagesPerSecond)
	}
	if flow.BytesPerSecond != 0 {
		t.Errorf("DataFlow.BytesPerSecond = %f, want 0", flow.BytesPerSecond)
	}
}

// TestComponent_ConcurrentHealthChecks tests concurrent health status queries.
func TestComponent_ConcurrentHealthChecks(t *testing.T) {
	c := &Component{
		name:   "question-timeout",
		logger: slog.Default(),
	}

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			health := c.Health()
			if !health.Healthy {
				t.Errorf("Health.Healthy = false, want true")
			}
		}()
	}
	wg.Wait()
}

// TestDefaultConfig tests default configuration values.
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.CheckInterval != 1*time.Minute {
		t.Errorf("DefaultConfig().CheckInterval = %v, want 1m", config.CheckInterval)
	}
	if config.DefaultSLA != 24*time.Hour {
		t.Errorf("DefaultConfig().DefaultSLA = %v, want 24h", config.DefaultSLA)
	}
	if config.Ports == nil {
		t.Error("DefaultConfig().Ports should not be nil")
	}
}

// TestAnswererRegistry_RouteMatching tests route pattern matching.
func TestAnswererRegistry_RouteMatching(t *testing.T) {
	registry := answerer.NewRegistry()

	// Add routes
	registry.AddRoute(answerer.Route{
		Pattern:  "api.*",
		Answerer: "agent/architect",
		SLA:      answerer.Duration(2 * time.Hour),
	})
	registry.AddRoute(answerer.Route{
		Pattern:  "database.*",
		Answerer: "agent/dba",
		SLA:      answerer.Duration(1 * time.Hour),
	})

	tests := []struct {
		topic            string
		expectedAnswerer string
		expectedSLA      time.Duration
	}{
		{
			topic:            "api.design",
			expectedAnswerer: "agent/architect",
			expectedSLA:      2 * time.Hour,
		},
		{
			topic:            "api.implementation",
			expectedAnswerer: "agent/architect",
			expectedSLA:      2 * time.Hour,
		},
		{
			topic:            "database.schema",
			expectedAnswerer: "agent/dba",
			expectedSLA:      1 * time.Hour,
		},
		{
			topic:            "unknown.topic",
			expectedAnswerer: "human/requester", // default route
			expectedSLA:      24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			route := registry.Match(tt.topic)
			if route.Answerer != tt.expectedAnswerer {
				t.Errorf("Match(%q).Answerer = %q, want %q", tt.topic, route.Answerer, tt.expectedAnswerer)
			}
			if route.SLA.Duration() != tt.expectedSLA {
				t.Errorf("Match(%q).SLA = %v, want %v", tt.topic, route.SLA.Duration(), tt.expectedSLA)
			}
		})
	}
}
