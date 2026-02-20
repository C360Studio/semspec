package taskdispatcher

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func TestNewComponent(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfgBytes, _ := json.Marshal(cfg)

		deps := component.Dependencies{
			// NATSClient would be nil, but NewComponent doesn't require it immediately
		}

		comp, err := NewComponent(cfgBytes, deps)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if comp == nil {
			t.Fatal("expected component to be created")
		}

		// Verify it implements Discoverable
		discoverable, ok := comp.(component.Discoverable)
		if !ok {
			t.Fatal("expected component to implement Discoverable")
		}

		meta := discoverable.Meta()
		if meta.Name != "task-dispatcher" {
			t.Errorf("expected Name 'task-dispatcher', got %s", meta.Name)
		}
		if meta.Type != "processor" {
			t.Errorf("expected Type 'processor', got %s", meta.Type)
		}
		if meta.Version != "0.1.0" {
			t.Errorf("expected Version '0.1.0', got %s", meta.Version)
		}
	})

	t.Run("applies defaults", func(t *testing.T) {
		// Empty config should use defaults
		cfgBytes := []byte(`{}`)

		deps := component.Dependencies{}

		comp, err := NewComponent(cfgBytes, deps)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		c := comp.(*Component)
		if c.config.StreamName != "WORKFLOWS" {
			t.Errorf("expected default StreamName, got %s", c.config.StreamName)
		}
		if c.config.MaxConcurrent != 3 {
			t.Errorf("expected default MaxConcurrent, got %d", c.config.MaxConcurrent)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		cfgBytes := []byte(`{invalid`)

		deps := component.Dependencies{}

		_, err := NewComponent(cfgBytes, deps)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("invalid config values", func(t *testing.T) {
		cfg := map[string]any{
			"stream_name":     "test",
			"consumer_name":   "test",
			"trigger_subject": "test",
			"max_concurrent":  100, // Too high
		}
		cfgBytes, _ := json.Marshal(cfg)

		deps := component.Dependencies{}

		_, err := NewComponent(cfgBytes, deps)
		if err == nil {
			t.Error("expected error for invalid max_concurrent")
		}
	})
}

func TestComponent_Meta(t *testing.T) {
	cfg := DefaultConfig()
	cfgBytes, _ := json.Marshal(cfg)
	deps := component.Dependencies{}

	comp, err := NewComponent(cfgBytes, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := comp.(*Component)
	meta := c.Meta()

	if meta.Name != "task-dispatcher" {
		t.Errorf("expected Name 'task-dispatcher', got %s", meta.Name)
	}
	if meta.Type != "processor" {
		t.Errorf("expected Type 'processor', got %s", meta.Type)
	}
	if meta.Description == "" {
		t.Error("expected Description to be set")
	}
	if meta.Version != "0.1.0" {
		t.Errorf("expected Version '0.1.0', got %s", meta.Version)
	}
}

func TestComponent_ConfigSchema(t *testing.T) {
	cfg := DefaultConfig()
	cfgBytes, _ := json.Marshal(cfg)
	deps := component.Dependencies{}

	comp, err := NewComponent(cfgBytes, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := comp.(*Component)
	schema := c.ConfigSchema()

	if schema.Properties == nil {
		t.Error("expected ConfigSchema to have Properties")
	}
}

func TestComponent_Ports(t *testing.T) {
	cfg := DefaultConfig()
	cfgBytes, _ := json.Marshal(cfg)
	deps := component.Dependencies{}

	comp, err := NewComponent(cfgBytes, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := comp.(*Component)

	inputPorts := c.InputPorts()
	if len(inputPorts) == 0 {
		t.Error("expected at least one input port")
	}

	outputPorts := c.OutputPorts()
	if len(outputPorts) == 0 {
		t.Error("expected at least one output port")
	}
}

func TestComponent_Health(t *testing.T) {
	cfg := DefaultConfig()
	cfgBytes, _ := json.Marshal(cfg)
	deps := component.Dependencies{}

	comp, err := NewComponent(cfgBytes, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := comp.(*Component)
	health := c.Health()

	// Component not started, should not be healthy
	if health.Healthy {
		t.Error("expected component to be unhealthy when not running")
	}
	if health.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %s", health.Status)
	}
}

func TestComponent_IsRunning(t *testing.T) {
	cfg := DefaultConfig()
	cfgBytes, _ := json.Marshal(cfg)
	deps := component.Dependencies{}

	comp, err := NewComponent(cfgBytes, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := comp.(*Component)

	if c.IsRunning() {
		t.Error("expected component to not be running initially")
	}
}

func TestComponent_Initialize(t *testing.T) {
	cfg := DefaultConfig()
	cfgBytes, _ := json.Marshal(cfg)
	deps := component.Dependencies{}

	comp, err := NewComponent(cfgBytes, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := comp.(*Component)
	err = c.Initialize()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBatchDispatchResult_Schema(t *testing.T) {
	result := &BatchDispatchResult{
		RequestID:       "req-1",
		Slug:            "test-slug",
		BatchID:         "batch-1",
		TaskCount:       5,
		DispatchedCount: 4,
		FailedCount:     1,
		Status:          "completed",
	}

	schema := result.Schema()
	if schema.Domain != "workflow" {
		t.Errorf("expected Domain 'workflow', got %s", schema.Domain)
	}
	if schema.Category != "dispatch-result" {
		t.Errorf("expected Category 'dispatch-result', got %s", schema.Category)
	}
	if schema.Version != "v1" {
		t.Errorf("expected Version 'v1', got %s", schema.Version)
	}
}

func TestBatchDispatchResult_Validate(t *testing.T) {
	result := &BatchDispatchResult{}
	err := result.Validate()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBatchDispatchResult_JSON(t *testing.T) {
	original := &BatchDispatchResult{
		RequestID:       "req-1",
		Slug:            "test-slug",
		BatchID:         "batch-1",
		TaskCount:       5,
		DispatchedCount: 4,
		FailedCount:     1,
		Status:          "completed",
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal
	var decoded BatchDispatchResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify fields
	if decoded.RequestID != original.RequestID {
		t.Errorf("expected RequestID %s, got %s", original.RequestID, decoded.RequestID)
	}
	if decoded.Slug != original.Slug {
		t.Errorf("expected Slug %s, got %s", original.Slug, decoded.Slug)
	}
	if decoded.BatchID != original.BatchID {
		t.Errorf("expected BatchID %s, got %s", original.BatchID, decoded.BatchID)
	}
	if decoded.TaskCount != original.TaskCount {
		t.Errorf("expected TaskCount %d, got %d", original.TaskCount, decoded.TaskCount)
	}
	if decoded.DispatchedCount != original.DispatchedCount {
		t.Errorf("expected DispatchedCount %d, got %d", original.DispatchedCount, decoded.DispatchedCount)
	}
	if decoded.FailedCount != original.FailedCount {
		t.Errorf("expected FailedCount %d, got %d", original.FailedCount, decoded.FailedCount)
	}
	if decoded.Status != original.Status {
		t.Errorf("expected Status %s, got %s", original.Status, decoded.Status)
	}
}

// Verify BatchDispatchResult implements message.Payload interface
var _ message.Payload = (*BatchDispatchResult)(nil)
