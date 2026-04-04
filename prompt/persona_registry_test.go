package prompt

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPersonaRegistryForRole(t *testing.T) {
	reg := NewPersonaRegistry()
	reg.personas[RolePlanner] = &AgentPersona{
		DisplayName:  "Mary",
		SystemPrompt: "You are Mary, a strategic analyst.",
	}

	// Configured role returns persona.
	p := reg.ForRole(RolePlanner)
	if p == nil {
		t.Fatal("expected persona for planner")
	}
	if p.DisplayName != "Mary" {
		t.Errorf("expected display name Mary, got %s", p.DisplayName)
	}

	// Unconfigured role returns nil.
	if reg.ForRole(RoleDeveloper) != nil {
		t.Error("expected nil for unconfigured role")
	}
}

func TestPersonaRegistryNilSafe(t *testing.T) {
	var reg *PersonaRegistry

	if reg.ForRole(RolePlanner) != nil {
		t.Error("expected nil from nil registry")
	}
	if reg.Vocabulary() != nil {
		t.Error("expected nil vocabulary from nil registry")
	}
	if reg.ListRoles() != nil {
		t.Error("expected nil roles from nil registry")
	}
}

func TestPersonaRegistryVocabulary(t *testing.T) {
	reg := NewPersonaRegistry()

	// Empty registry has nil vocabulary.
	if reg.Vocabulary() != nil {
		t.Error("expected nil vocabulary for empty registry")
	}

	// Set vocabulary.
	reg.vocab = &Vocabulary{Agent: "Analyst", Plan: "Project Brief"}
	v := reg.Vocabulary()
	if v == nil {
		t.Fatal("expected non-nil vocabulary")
	}
	if v.Agent != "Analyst" {
		t.Errorf("expected Agent=Analyst, got %s", v.Agent)
	}
}

func TestLoadPresetFromFile(t *testing.T) {
	// Load the real bmad.json preset.
	path := filepath.Join("..", "configs", "presets", "bmad.json")
	reg, err := LoadPresetFromFile(path)
	if err != nil {
		t.Fatalf("LoadPresetFromFile: %v", err)
	}

	// Check that personas loaded for expected roles.
	expectedRoles := map[Role]string{
		RolePlanner:              "Mary",
		RoleRequirementGenerator: "John",
		RoleArchitect:            "Winston",
		RoleScenarioGenerator:    "Bob",
		RoleDeveloper:            "Amelia",
		Role("qa"):               "Murat",
	}

	for role, name := range expectedRoles {
		p := reg.ForRole(role)
		if p == nil {
			t.Errorf("expected persona for role %s", role)
			continue
		}
		if p.DisplayName != name {
			t.Errorf("role %s: expected display name %s, got %s", role, name, p.DisplayName)
		}
		if p.SystemPrompt == "" {
			t.Errorf("role %s: expected non-empty system prompt", role)
		}
	}

	// Vocabulary should be loaded.
	v := reg.Vocabulary()
	if v == nil {
		t.Fatal("expected vocabulary from bmad preset")
	}
	if v.Plan != "Project Brief" {
		t.Errorf("expected Plan='Project Brief', got %s", v.Plan)
	}
}

func TestLoadPresetFromFileMissing(t *testing.T) {
	_, err := LoadPresetFromFile("/nonexistent/file.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadPersonasFromConfigZeroConfig(t *testing.T) {
	// No persona_preset field — returns nil, nil.
	reg, err := LoadPersonasFromConfig([]byte(`{}`), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg != nil {
		t.Error("expected nil registry for zero-config")
	}
}

func TestLoadPersonasFromConfigWithPreset(t *testing.T) {
	// Create a temp preset file.
	dir := t.TempDir()
	presetsDir := filepath.Join(dir, "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	preset := `{
		"vocabulary": {"agent": "TestAgent", "plan": "TestPlan"},
		"personas": {
			"planner": {
				"display_name": "TestMary",
				"system_prompt": "You are TestMary."
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(presetsDir, "test.json"), []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}

	config := `{"persona_preset": "test"}`
	reg, err := LoadPersonasFromConfig([]byte(config), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}

	p := reg.ForRole(RolePlanner)
	if p == nil || p.DisplayName != "TestMary" {
		t.Error("expected TestMary persona for planner")
	}

	v := reg.Vocabulary()
	if v == nil || v.Agent != "TestAgent" {
		t.Error("expected TestAgent vocabulary")
	}
}

func TestLoadPersonasFromConfigMissingPreset(t *testing.T) {
	config := `{"persona_preset": "nonexistent"}`
	_, err := LoadPersonasFromConfig([]byte(config), t.TempDir())
	if err == nil {
		t.Error("expected error for missing preset file")
	}
}

func TestGlobalPersonasSingleton(t *testing.T) {
	ResetGlobalPersonas()
	defer ResetGlobalPersonas()

	reg := NewPersonaRegistry()
	reg.personas[RolePlanner] = &AgentPersona{DisplayName: "Singleton"}

	InitGlobalPersonas(reg)

	got := GlobalPersonas()
	p := got.ForRole(RolePlanner)
	if p == nil || p.DisplayName != "Singleton" {
		t.Error("expected initialized singleton persona")
	}
}

func TestGlobalPersonasDefaultEmpty(t *testing.T) {
	ResetGlobalPersonas()
	defer ResetGlobalPersonas()

	// Without InitGlobalPersonas, GlobalPersonas returns empty registry.
	got := GlobalPersonas()
	if got == nil {
		t.Fatal("expected non-nil default registry")
	}
	if got.ForRole(RolePlanner) != nil {
		t.Error("expected nil persona from default empty registry")
	}
}

func TestAssemblerPersonaInjection(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "base",
		Category: CategorySystemBase,
		Content:  "You are a developer.",
	})

	a := NewAssembler(r)

	// Without persona — no persona fragment.
	result := a.Assemble(&AssemblyContext{Role: RoleDeveloper})
	if strings.Contains(result.SystemMessage, "Mary") {
		t.Error("expected no persona without Persona set")
	}

	// With persona — persona fragment injected.
	result = a.Assemble(&AssemblyContext{
		Role: RolePlanner,
		Persona: &AgentPersona{
			DisplayName:  "Mary",
			SystemPrompt: "You are Mary, a strategic analyst.",
		},
	})
	if !strings.Contains(result.SystemMessage, "You are Mary, a strategic analyst.") {
		t.Error("expected persona system prompt in assembled output")
	}

	if !slices.Contains(result.FragmentsUsed, "dynamic.persona") {
		t.Error("expected dynamic.persona in FragmentsUsed")
	}
}
