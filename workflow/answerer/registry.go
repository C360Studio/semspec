// Package answerer provides question routing based on topic patterns.
//
// The answerer registry maps topic patterns to answerers (agents, teams, humans, tools)
// with SLA tracking and escalation paths.
package answerer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// AnswererType defines who can answer questions.
type AnswererType string

const (
	// AnswererAgent routes to an LLM agent for auto-answering.
	AnswererAgent AnswererType = "agent"

	// AnswererTeam routes to a human team (e.g., Slack channel).
	AnswererTeam AnswererType = "team"

	// AnswererHuman routes to an individual human.
	AnswererHuman AnswererType = "human"

	// AnswererTool routes to an automated tool (e.g., web search).
	AnswererTool AnswererType = "tool"
)

// Route defines how questions with matching topics are handled.
type Route struct {
	// Pattern is a glob pattern for matching topics (e.g., "api.*", "architecture.*").
	Pattern string `yaml:"pattern" json:"pattern"`

	// Answerer identifies who handles the question (e.g., "agent/architect", "team/semstreams").
	Answerer string `yaml:"answerer" json:"answerer"`

	// Type is derived from the answerer prefix (agent/, team/, human/, tool/).
	Type AnswererType `yaml:"-" json:"type"`

	// Capability is the model capability for agent answerers (e.g., "planning", "reviewing").
	Capability string `yaml:"capability,omitempty" json:"capability,omitempty"`

	// SLA is the maximum time allowed to answer before escalation.
	SLA Duration `yaml:"sla,omitempty" json:"sla,omitempty"`

	// EscalateTo is the next answerer if SLA is exceeded.
	EscalateTo string `yaml:"escalate_to,omitempty" json:"escalate_to,omitempty"`

	// Notify is the notification channel (e.g., "slack://channel-name").
	Notify string `yaml:"notify,omitempty" json:"notify,omitempty"`
}

// Duration wraps time.Duration for YAML unmarshaling.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// RegistryConfig is the YAML configuration structure.
type RegistryConfig struct {
	Version string  `yaml:"version"`
	Routes  []Route `yaml:"routes"`
	Default Route   `yaml:"default"`
}

// Registry manages question routing based on topic patterns.
type Registry struct {
	mu           sync.RWMutex
	routes       []Route
	defaultRoute Route
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		routes: []Route{},
		defaultRoute: Route{
			Answerer: "human/requester",
			Type:     AnswererHuman,
			SLA:      Duration(24 * time.Hour),
		},
	}
}

// LoadRegistry loads a registry from a YAML file.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry file: %w", err)
	}

	var config RegistryConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse registry file: %w", err)
	}

	r := NewRegistry()

	// Process routes
	for _, route := range config.Routes {
		route.Type = parseAnswererType(route.Answerer)
		r.routes = append(r.routes, route)
	}

	// Process default
	if config.Default.Answerer != "" {
		config.Default.Type = parseAnswererType(config.Default.Answerer)
		r.defaultRoute = config.Default
	}

	return r, nil
}

// LoadRegistryFromDir searches for answerers.yaml in common locations.
func LoadRegistryFromDir(baseDir string) (*Registry, error) {
	paths := []string{
		filepath.Join(baseDir, "configs", "answerers.yaml"),
		filepath.Join(baseDir, "answerers.yaml"),
		"configs/answerers.yaml",
		"answerers.yaml",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return LoadRegistry(path)
		}
	}

	// Return default registry if no config found
	return NewRegistry(), nil
}

// parseAnswererType extracts the type from an answerer string.
func parseAnswererType(answerer string) AnswererType {
	parts := strings.SplitN(answerer, "/", 2)
	if len(parts) < 2 {
		return AnswererHuman // Default to human
	}

	switch parts[0] {
	case "agent":
		return AnswererAgent
	case "team":
		return AnswererTeam
	case "human":
		return AnswererHuman
	case "tool":
		return AnswererTool
	default:
		return AnswererHuman
	}
}

// Match returns the route for a given topic.
// Returns the default route if no pattern matches.
func (r *Registry) Match(topic string) *Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := range r.routes {
		if matchPattern(r.routes[i].Pattern, topic) {
			return &r.routes[i]
		}
	}

	return &r.defaultRoute
}

// matchPattern checks if a topic matches a glob pattern.
// Supports * for single-level wildcard and ** for multi-level.
func matchPattern(pattern, topic string) bool {
	// Exact match
	if pattern == topic {
		return true
	}

	// Handle ** (match everything)
	if pattern == "**" {
		return true
	}

	// Split into parts
	patternParts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	return matchParts(patternParts, topicParts)
}

// matchParts recursively matches pattern parts against topic parts.
func matchParts(pattern, topic []string) bool {
	// Base cases
	if len(pattern) == 0 && len(topic) == 0 {
		return true
	}
	if len(pattern) == 0 {
		return false
	}

	// ** matches zero or more parts
	if pattern[0] == "**" {
		// Try matching ** against zero parts
		if matchParts(pattern[1:], topic) {
			return true
		}
		// Try matching ** against one or more parts
		if len(topic) > 0 && matchParts(pattern, topic[1:]) {
			return true
		}
		return false
	}

	// Need at least one topic part for * or literal match
	if len(topic) == 0 {
		return false
	}

	// * matches exactly one part
	if pattern[0] == "*" {
		return matchParts(pattern[1:], topic[1:])
	}

	// Literal match
	if pattern[0] == topic[0] {
		return matchParts(pattern[1:], topic[1:])
	}

	return false
}

// AddRoute adds a route to the registry.
func (r *Registry) AddRoute(route Route) {
	r.mu.Lock()
	defer r.mu.Unlock()

	route.Type = parseAnswererType(route.Answerer)
	r.routes = append(r.routes, route)
}

// SetDefault sets the default route.
func (r *Registry) SetDefault(route Route) {
	r.mu.Lock()
	defer r.mu.Unlock()

	route.Type = parseAnswererType(route.Answerer)
	r.defaultRoute = route
}

// Routes returns all configured routes.
func (r *Registry) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]Route, len(r.routes))
	copy(routes, r.routes)
	return routes
}

// Default returns the default route.
func (r *Registry) Default() Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultRoute
}

// GetAnswererName extracts the name from an answerer string (e.g., "agent/architect" → "architect").
func GetAnswererName(answerer string) string {
	parts := strings.SplitN(answerer, "/", 2)
	if len(parts) < 2 {
		return answerer
	}
	return parts[1]
}
