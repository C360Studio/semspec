package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PersonaRegistry holds loaded persona and vocabulary configuration.
// Created at startup from a preset file referenced by semspec.json.
type PersonaRegistry struct {
	personas map[Role]*AgentPersona
	vocab    *Vocabulary
}

// NewPersonaRegistry creates an empty registry (no personas, no vocabulary).
func NewPersonaRegistry() *PersonaRegistry {
	return &PersonaRegistry{
		personas: make(map[Role]*AgentPersona),
	}
}

// ForRole returns the persona for a role, or nil if not configured.
func (r *PersonaRegistry) ForRole(role Role) *AgentPersona {
	if r == nil {
		return nil
	}
	return r.personas[role]
}

// Vocabulary returns the loaded vocabulary, or nil when no preset is loaded.
func (r *PersonaRegistry) Vocabulary() *Vocabulary {
	if r == nil {
		return nil
	}
	return r.vocab
}

// PresetConfig is the JSON structure of a preset file (e.g., configs/presets/bmad.json).
type PresetConfig struct {
	Vocabulary *Vocabulary              `json:"vocabulary,omitempty"`
	Personas   map[string]*AgentPersona `json:"personas,omitempty"`
}

// LoadPresetFromFile loads a persona preset from a JSON file.
func LoadPresetFromFile(path string) (*PersonaRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read preset file: %w", err)
	}

	var preset PresetConfig
	if err := json.Unmarshal(data, &preset); err != nil {
		return nil, fmt.Errorf("parse preset file: %w", err)
	}

	reg := NewPersonaRegistry()
	reg.vocab = preset.Vocabulary

	for roleStr, persona := range preset.Personas {
		reg.personas[Role(roleStr)] = persona
	}

	return reg, nil
}

// LoadPersonasFromConfig extracts "persona_preset" from the top-level semspec.json
// and loads the referenced preset from configDir/presets/{name}.json.
// Returns nil, nil when no preset is configured (zero-config path).
func LoadPersonasFromConfig(data []byte, configDir string) (*PersonaRegistry, error) {
	var top struct {
		PersonaPreset string `json:"persona_preset"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil // unparseable config, no personas
	}
	if top.PersonaPreset == "" {
		return nil, nil
	}

	path := filepath.Join(configDir, "presets", top.PersonaPreset+".json")
	return LoadPresetFromFile(path)
}

// Global registry instance and initialization guard.
var (
	globalPersonas *PersonaRegistry
	personaOnce    sync.Once
)

// GlobalPersonas returns the singleton persona registry.
// Returns an empty registry if not initialized (safe nil-free access).
func GlobalPersonas() *PersonaRegistry {
	personaOnce.Do(func() {
		globalPersonas = NewPersonaRegistry()
	})
	return globalPersonas
}

// InitGlobalPersonas initializes the global persona registry.
// Must be called before any call to GlobalPersonas() to take effect.
func InitGlobalPersonas(r *PersonaRegistry) {
	personaOnce.Do(func() {
		globalPersonas = r
	})
}

// ListRoles returns the roles that have personas configured.
func (r *PersonaRegistry) ListRoles() []Role {
	if r == nil {
		return nil
	}
	roles := make([]Role, 0, len(r.personas))
	for role := range r.personas {
		roles = append(roles, role)
	}
	return roles
}

// ResetGlobalPersonas resets the global persona registry for testing.
// NOT thread-safe — only use in tests.
func ResetGlobalPersonas() {
	personaOnce = sync.Once{}
	globalPersonas = nil
}
