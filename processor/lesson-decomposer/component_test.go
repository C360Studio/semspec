package lessondecomposer

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewComponent_AppliesDefaults(t *testing.T) {
	deps := component.Dependencies{Logger: discardLogger()}
	got, err := NewComponent(json.RawMessage(`{}`), deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c, ok := got.(*Component)
	if !ok {
		t.Fatalf("expected *Component, got %T", got)
	}
	if c.config.StreamName != "WORKFLOW" {
		t.Errorf("StreamName default = %q, want WORKFLOW", c.config.StreamName)
	}
	if c.config.ConsumerName != "lesson-decomposer" {
		t.Errorf("ConsumerName default = %q, want lesson-decomposer", c.config.ConsumerName)
	}
	if c.config.FilterSubject != "workflow.events.lesson.decompose.requested.>" {
		t.Errorf("FilterSubject default = %q", c.config.FilterSubject)
	}
}

func TestNewComponent_PreservesExplicitConfig(t *testing.T) {
	raw := json.RawMessage(`{
		"enabled": false,
		"stream_name": "ALT",
		"consumer_name": "custom",
		"filter_subject": "alt.subject"
	}`)
	got, err := NewComponent(raw, component.Dependencies{Logger: discardLogger()})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := got.(*Component)
	if c.config.Enabled {
		t.Error("Enabled should be false from config")
	}
	if c.config.StreamName != "ALT" {
		t.Errorf("StreamName = %q, want ALT", c.config.StreamName)
	}
	if c.config.ConsumerName != "custom" {
		t.Errorf("ConsumerName = %q", c.config.ConsumerName)
	}
	if c.config.FilterSubject != "alt.subject" {
		t.Errorf("FilterSubject = %q", c.config.FilterSubject)
	}
}

func TestComponent_MetaDescribesPhase(t *testing.T) {
	c := &Component{name: "lesson-decomposer"}
	meta := c.Meta()
	if meta.Name != "lesson-decomposer" {
		t.Errorf("Meta.Name = %q", meta.Name)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q", meta.Type)
	}
}

func TestComponent_HealthReportsStoppedBeforeStart(t *testing.T) {
	c := &Component{}
	h := c.Health()
	if h.Healthy {
		t.Error("Health should not be healthy before Start")
	}
	if h.Status != "stopped" {
		t.Errorf("Health.Status = %q, want stopped", h.Status)
	}
}

func TestComponent_InputPortsExposesFilterSubject(t *testing.T) {
	c := &Component{
		config: Config{FilterSubject: "test.subject"},
	}
	ports := c.InputPorts()
	if len(ports) != 1 {
		t.Fatalf("expected 1 input port, got %d", len(ports))
	}
	if ports[0].Direction != component.DirectionInput {
		t.Errorf("Direction = %v", ports[0].Direction)
	}
	natsPort, ok := ports[0].Config.(component.NATSPort)
	if !ok {
		t.Fatalf("Config is %T, want NATSPort", ports[0].Config)
	}
	if natsPort.Subject != "test.subject" {
		t.Errorf("Subject = %q, want test.subject", natsPort.Subject)
	}
}

func TestConfigValidate_RejectsEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing stream", Config{ConsumerName: "x", FilterSubject: "y"}},
		{"missing consumer", Config{StreamName: "x", FilterSubject: "y"}},
		{"missing filter", Config{StreamName: "x", ConsumerName: "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestRegister_NilRegistryReturnsError(t *testing.T) {
	if err := Register(nil); err == nil {
		t.Error("Register(nil) should return error")
	}
}
