package taskdispatcher

import (
	"testing"

	"github.com/c360studio/semstreams/component"
)

// mockRegistry implements RegistryInterface for testing.
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

func TestRegister(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		registry := &mockRegistry{}
		err := Register(registry)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !registry.registered {
			t.Error("expected registry.RegisterWithConfig to be called")
		}

		cfg := registry.lastConfig
		if cfg.Name != "task-dispatcher" {
			t.Errorf("expected Name 'task-dispatcher', got %s", cfg.Name)
		}
		if cfg.Type != "processor" {
			t.Errorf("expected Type 'processor', got %s", cfg.Type)
		}
		if cfg.Protocol != "workflow" {
			t.Errorf("expected Protocol 'workflow', got %s", cfg.Protocol)
		}
		if cfg.Domain != "semspec" {
			t.Errorf("expected Domain 'semspec', got %s", cfg.Domain)
		}
		if cfg.Version != "0.1.0" {
			t.Errorf("expected Version '0.1.0', got %s", cfg.Version)
		}
		if cfg.Factory == nil {
			t.Error("expected Factory to be set")
		}
		if cfg.Schema.Properties == nil {
			t.Error("expected Schema to have Properties")
		}
	})

	t.Run("nil registry returns error", func(t *testing.T) {
		err := Register(nil)
		if err == nil {
			t.Error("expected error for nil registry")
		}
	})
}
