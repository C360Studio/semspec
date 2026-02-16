package answerer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		want    bool
	}{
		// Exact matches
		{"api.semstreams", "api.semstreams", true},
		{"architecture", "architecture", true},
		{"api.semstreams", "api.other", false},

		// Single wildcard (*)
		{"api.*", "api.semstreams", true},
		{"api.*", "api.auth", true},
		{"api.*", "api", false},
		{"api.*", "other.semstreams", false},
		{"*.semstreams", "api.semstreams", true},
		{"*.semstreams", "other.semstreams", true},

		// Multi-level wildcard (**)
		{"api.**", "api.semstreams", true},
		{"api.**", "api.semstreams.loops", true},
		{"api.**", "api", true},
		{"api.**", "other.something", false},
		{"**", "anything.at.all", true},
		{"**", "simple", true},

		// Complex patterns
		{"api.*.loops", "api.semstreams.loops", true},
		{"api.*.loops", "api.auth.loops", true},
		{"api.*.loops", "api.semstreams.other", false},
		{"*.*.loops", "api.semstreams.loops", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.topic, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.topic)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.topic, got, tt.want)
			}
		})
	}
}

func TestParseAnswererType(t *testing.T) {
	tests := []struct {
		answerer string
		want     AnswererType
	}{
		{"agent/architect", AnswererAgent},
		{"agent/security-reviewer", AnswererAgent},
		{"team/semstreams", AnswererTeam},
		{"team/security", AnswererTeam},
		{"human/requester", AnswererHuman},
		{"human/tech-lead", AnswererHuman},
		{"tool/web-search", AnswererTool},
		{"tool/docs-search", AnswererTool},
		{"invalid", AnswererHuman},      // Defaults to human
		{"unknown/type", AnswererHuman}, // Unknown prefix defaults to human
	}

	for _, tt := range tests {
		t.Run(tt.answerer, func(t *testing.T) {
			got := parseAnswererType(tt.answerer)
			if got != tt.want {
				t.Errorf("parseAnswererType(%q) = %v, want %v", tt.answerer, got, tt.want)
			}
		})
	}
}

func TestGetAnswererName(t *testing.T) {
	tests := []struct {
		answerer string
		want     string
	}{
		{"agent/architect", "architect"},
		{"team/semstreams", "semstreams"},
		{"human/requester", "requester"},
		{"tool/web-search", "web-search"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.answerer, func(t *testing.T) {
			got := GetAnswererName(tt.answerer)
			if got != tt.want {
				t.Errorf("GetAnswererName(%q) = %v, want %v", tt.answerer, got, tt.want)
			}
		})
	}
}

func TestRegistryMatch(t *testing.T) {
	r := NewRegistry()

	// Add routes
	r.AddRoute(Route{
		Pattern:    "api.semstreams.*",
		Answerer:   "team/semstreams",
		SLA:        Duration(4 * time.Hour),
		EscalateTo: "human/tech-lead",
	})
	r.AddRoute(Route{
		Pattern:    "architecture.*",
		Answerer:   "agent/architect",
		Capability: "planning",
		SLA:        Duration(1 * time.Hour),
	})
	r.AddRoute(Route{
		Pattern:    "security.**",
		Answerer:   "agent/security-reviewer",
		Capability: "reviewing",
		SLA:        Duration(30 * time.Minute),
		EscalateTo: "team/security",
	})

	tests := []struct {
		topic        string
		wantAnswerer string
		wantType     AnswererType
	}{
		{"api.semstreams.loops", "team/semstreams", AnswererTeam},
		{"api.semstreams.auth", "team/semstreams", AnswererTeam},
		{"architecture.database", "agent/architect", AnswererAgent},
		{"architecture.messaging", "agent/architect", AnswererAgent},
		{"security.tokens", "agent/security-reviewer", AnswererAgent},
		{"security.auth.jwt", "agent/security-reviewer", AnswererAgent},
		{"unknown.topic", "human/requester", AnswererHuman}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			route := r.Match(tt.topic)
			if route.Answerer != tt.wantAnswerer {
				t.Errorf("Match(%q).Answerer = %v, want %v", tt.topic, route.Answerer, tt.wantAnswerer)
			}
			if route.Type != tt.wantType {
				t.Errorf("Match(%q).Type = %v, want %v", tt.topic, route.Type, tt.wantType)
			}
		})
	}
}

func TestRegistryMatchOrder(t *testing.T) {
	r := NewRegistry()

	// More specific pattern first
	r.AddRoute(Route{
		Pattern:  "api.semstreams.auth",
		Answerer: "agent/auth-specialist",
	})
	// More general pattern second
	r.AddRoute(Route{
		Pattern:  "api.semstreams.*",
		Answerer: "team/semstreams",
	})

	// Should match the first (more specific) route
	route := r.Match("api.semstreams.auth")
	if route.Answerer != "agent/auth-specialist" {
		t.Errorf("Expected first matching route, got %v", route.Answerer)
	}

	// Should match the second route
	route = r.Match("api.semstreams.other")
	if route.Answerer != "team/semstreams" {
		t.Errorf("Expected second matching route, got %v", route.Answerer)
	}
}

func TestLoadRegistry(t *testing.T) {
	// Create a temp config file
	dir := t.TempDir()
	configPath := filepath.Join(dir, "answerers.yaml")

	config := `version: "1"
routes:
  - pattern: "api.*"
    answerer: team/api-team
    sla: 2h
    notify: slack://api-alerts
    escalate_to: human/lead

  - pattern: "architecture.*"
    answerer: agent/architect
    capability: planning
    sla: 1h

default:
  answerer: human/requester
  sla: 24h
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	r, err := LoadRegistry(configPath)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	// Check routes loaded
	routes := r.Routes()
	if len(routes) != 2 {
		t.Errorf("Expected 2 routes, got %d", len(routes))
	}

	// Check first route
	if routes[0].Pattern != "api.*" {
		t.Errorf("Route 0 pattern = %v, want api.*", routes[0].Pattern)
	}
	if routes[0].Type != AnswererTeam {
		t.Errorf("Route 0 type = %v, want team", routes[0].Type)
	}
	if routes[0].SLA.Duration() != 2*time.Hour {
		t.Errorf("Route 0 SLA = %v, want 2h", routes[0].SLA.Duration())
	}
	if routes[0].Notify != "slack://api-alerts" {
		t.Errorf("Route 0 notify = %v, want slack://api-alerts", routes[0].Notify)
	}

	// Check second route
	if routes[1].Capability != "planning" {
		t.Errorf("Route 1 capability = %v, want planning", routes[1].Capability)
	}

	// Check default
	def := r.Default()
	if def.Answerer != "human/requester" {
		t.Errorf("Default answerer = %v, want human/requester", def.Answerer)
	}
	if def.SLA.Duration() != 24*time.Hour {
		t.Errorf("Default SLA = %v, want 24h", def.SLA.Duration())
	}
}

func TestLoadRegistryFromDir(t *testing.T) {
	// Create a temp directory structure
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "answerers.yaml")
	config := `version: "1"
routes:
  - pattern: "test.*"
    answerer: agent/test
default:
  answerer: human/default
  sla: 1h
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	r, err := LoadRegistryFromDir(dir)
	if err != nil {
		t.Fatalf("LoadRegistryFromDir failed: %v", err)
	}

	routes := r.Routes()
	if len(routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(routes))
	}
}

func TestLoadRegistryFromDir_NoConfig(t *testing.T) {
	// Empty temp directory - should return default registry
	dir := t.TempDir()

	r, err := LoadRegistryFromDir(dir)
	if err != nil {
		t.Fatalf("LoadRegistryFromDir failed: %v", err)
	}

	// Should have default registry
	routes := r.Routes()
	if len(routes) != 0 {
		t.Errorf("Expected 0 routes for default registry, got %d", len(routes))
	}

	def := r.Default()
	if def.Answerer != "human/requester" {
		t.Errorf("Default answerer = %v, want human/requester", def.Answerer)
	}
}

func TestDurationParsing(t *testing.T) {
	// Test duration parsing via YAML unmarshaling
	config := `version: "1"
routes:
  - pattern: "test.*"
    answerer: agent/test
    sla: 2h30m
default:
  answerer: human/default
  sla: 1h
`
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	r, err := LoadRegistry(configPath)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	routes := r.Routes()
	if len(routes) != 1 {
		t.Fatalf("Expected 1 route, got %d", len(routes))
	}

	expectedSLA := 2*time.Hour + 30*time.Minute
	if routes[0].SLA.Duration() != expectedSLA {
		t.Errorf("Route SLA = %v, want %v", routes[0].SLA.Duration(), expectedSLA)
	}

	def := r.Default()
	if def.SLA.Duration() != time.Hour {
		t.Errorf("Default SLA = %v, want 1h", def.SLA.Duration())
	}
}
