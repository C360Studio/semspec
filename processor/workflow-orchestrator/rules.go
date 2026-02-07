package workfloworchestrator

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// RulesFile represents the workflow-rules.yaml structure.
type RulesFile struct {
	Version          string            `yaml:"version"`
	Rules            []Rule            `yaml:"rules"`
	RoleCapabilities map[string]string `yaml:"role_capabilities"`
	Retry            *RetryConfig      `yaml:"retry,omitempty"`
}

// Rule represents a single workflow rule.
type Rule struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Condition   Condition `yaml:"condition"`
	Action      Action    `yaml:"action"`
}

// Condition specifies when a rule should trigger.
type Condition struct {
	KVBucket   string            `yaml:"kv_bucket"`
	KeyPattern string            `yaml:"key_pattern"`
	Match      map[string]string `yaml:"match"`
}

// Action specifies what to do when a rule matches.
type Action struct {
	Type    string                 `yaml:"type"` // publish_task, publish_response
	Subject string                 `yaml:"subject"`
	Payload map[string]interface{} `yaml:"payload"`
	Content string                 `yaml:"content,omitempty"` // For publish_response
}

// RetryConfig specifies retry behavior.
type RetryConfig struct {
	MaxAttempts       int    `yaml:"max_attempts"`
	BackoffBase       string `yaml:"backoff_base"`
	BackoffMultiplier int    `yaml:"backoff_multiplier"`
}

// LoadRules loads rules from a YAML file.
func LoadRules(path string) (*RulesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules file: %w", err)
	}

	var rules RulesFile
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse rules file: %w", err)
	}

	return &rules, nil
}

// LoopState represents the state of a completed loop from KV.
type LoopState struct {
	LoopID       string            `json:"loop_id"`
	Role         string            `json:"role"`
	Status       string            `json:"status"` // complete, failed, blocked
	Model        string            `json:"model,omitempty"`
	Capability   string            `json:"capability,omitempty"`
	Error        string            `json:"error,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	CompletedAt  string            `json:"completed_at"`
	TaskID       string            `json:"task_id,omitempty"`
	WorkflowSlug string            `json:"workflow_slug,omitempty"`
	WorkflowStep string            `json:"workflow_step,omitempty"`

	// User routing fields (for error notifications)
	ChannelType string `json:"channel_type,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`

	// Failure details (from agent.failed.* events)
	Outcome    string `json:"outcome,omitempty"`    // "failed"
	Reason     string `json:"reason,omitempty"`     // "model_error", "timeout", "max_iterations"
	Iterations int    `json:"iterations,omitempty"` // number of iterations before failure
	FailedAt   string `json:"failed_at,omitempty"`

	// Question blocking fields (Sprint 2)
	BlockedBy     []string `json:"blocked_by,omitempty"`      // Question IDs blocking this loop
	BlockedReason string   `json:"blocked_reason,omitempty"`  // Why the loop is blocked
	BlockedAt     string   `json:"blocked_at,omitempty"`      // When the loop was blocked
}

// IsBlocked returns true if the loop is blocked by pending questions.
func (s *LoopState) IsBlocked() bool {
	return len(s.BlockedBy) > 0 || s.Status == "blocked"
}

// GetWorkflowContext returns the workflow slug and step, checking both
// top-level fields and metadata for backwards compatibility.
func (s *LoopState) GetWorkflowContext() (slug, step string) {
	slug = s.WorkflowSlug
	if slug == "" && s.Metadata != nil {
		slug = s.Metadata["workflow_slug"]
	}
	step = s.WorkflowStep
	if step == "" && s.Metadata != nil {
		step = s.Metadata["workflow_step"]
	}
	return
}

// Matches checks if a rule's condition matches the given loop state.
func (r *Rule) Matches(state *LoopState) bool {
	// Check role match
	if roleMatch, ok := r.Condition.Match["role"]; ok {
		if roleMatch != state.Role {
			return false
		}
	}

	// Check status match
	if statusMatch, ok := r.Condition.Match["status"]; ok {
		if statusMatch != state.Status {
			return false
		}
	}

	// Check metadata matches
	for key, expected := range r.Condition.Match {
		if strings.HasPrefix(key, "metadata.") {
			metaKey := strings.TrimPrefix(key, "metadata.")
			actual, exists := state.Metadata[metaKey]

			// Handle wildcard match
			if expected == "*" {
				if !exists || actual == "" {
					return false
				}
				continue
			}

			if !exists || actual != expected {
				return false
			}
		}
	}

	return true
}

// BuildPayload builds the action payload by substituting variables from state.
func (a *Action) BuildPayload(state *LoopState) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range a.Payload {
		result[key] = substituteValue(value, state)
	}

	return result
}

// BuildSubject builds the action subject by substituting variables from state.
func (a *Action) BuildSubject(state *LoopState) string {
	return substituteString(a.Subject, state)
}

// BuildContent builds the action content by substituting variables from state.
func (a *Action) BuildContent(state *LoopState) string {
	return substituteString(a.Content, state)
}

// substituteValue recursively substitutes variables in a value.
func substituteValue(value interface{}, state *LoopState) interface{} {
	switch v := value.(type) {
	case string:
		return substituteString(v, state)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = substituteValue(item, state)
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = substituteValue(val, state)
		}
		return result
	default:
		return value
	}
}

// substituteString substitutes $entity.* variables in a string.
func substituteString(s string, state *LoopState) string {
	if s == "" {
		return s
	}

	// Pattern: $entity.field or $entity.metadata.field
	// Use word boundary or $ to avoid matching into adjacent text
	re := regexp.MustCompile(`\$entity\.([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)?)`)

	return re.ReplaceAllStringFunc(s, func(match string) string {
		field := strings.TrimPrefix(match, "$entity.")
		return resolveField(field, state)
	})
}

// resolveField resolves a field path from the loop state.
func resolveField(field string, state *LoopState) string {
	switch field {
	case "id", "loop_id":
		return state.LoopID
	case "role":
		return state.Role
	case "status":
		return state.Status
	case "model":
		return state.Model
	case "capability":
		return state.Capability
	case "error":
		return state.Error
	case "task_id":
		return state.TaskID
	case "workflow_slug":
		return state.WorkflowSlug
	case "workflow_step":
		return state.WorkflowStep
	default:
		// Check metadata fields
		if strings.HasPrefix(field, "metadata.") {
			metaKey := strings.TrimPrefix(field, "metadata.")
			if val, ok := state.Metadata[metaKey]; ok {
				return val
			}
		}
		return ""
	}
}
