package workfloworchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetWorkflowContext(t *testing.T) {
	tests := []struct {
		name         string
		state        *LoopState
		expectedSlug string
		expectedStep string
	}{
		{
			name: "top-level fields",
			state: &LoopState{
				WorkflowSlug: "my-slug",
				WorkflowStep: "propose",
			},
			expectedSlug: "my-slug",
			expectedStep: "propose",
		},
		{
			name: "metadata fields",
			state: &LoopState{
				Metadata: map[string]string{
					"workflow_slug": "meta-slug",
					"workflow_step": "design",
				},
			},
			expectedSlug: "meta-slug",
			expectedStep: "design",
		},
		{
			name: "top-level takes precedence",
			state: &LoopState{
				WorkflowSlug: "top-slug",
				WorkflowStep: "top-step",
				Metadata: map[string]string{
					"workflow_slug": "meta-slug",
					"workflow_step": "meta-step",
				},
			},
			expectedSlug: "top-slug",
			expectedStep: "top-step",
		},
		{
			name: "mixed sources",
			state: &LoopState{
				WorkflowSlug: "top-slug",
				Metadata: map[string]string{
					"workflow_step": "meta-step",
				},
			},
			expectedSlug: "top-slug",
			expectedStep: "meta-step",
		},
		{
			name:         "empty state",
			state:        &LoopState{},
			expectedSlug: "",
			expectedStep: "",
		},
		{
			name: "nil metadata",
			state: &LoopState{
				Metadata: nil,
			},
			expectedSlug: "",
			expectedStep: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, step := tt.state.GetWorkflowContext()
			if slug != tt.expectedSlug {
				t.Errorf("slug = %q, want %q", slug, tt.expectedSlug)
			}
			if step != tt.expectedStep {
				t.Errorf("step = %q, want %q", step, tt.expectedStep)
			}
		})
	}
}

func TestLoadRules(t *testing.T) {
	// Create a temporary rules file
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")

	content := `
version: "1.0"
rules:
  - name: test-rule
    description: "Test rule"
    condition:
      kv_bucket: "AGENT_LOOPS"
      key_pattern: "COMPLETE_*"
      match:
        role: "proposal-writer"
        status: "complete"
    action:
      type: publish_task
      subject: "agent.task.workflow"
      payload:
        role: "design-writer"
        workflow_step: "design"
role_capabilities:
  proposal-writer: writing
  design-writer: planning
`
	if err := os.WriteFile(rulesPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write rules file: %v", err)
	}

	rules, err := LoadRules(rulesPath)
	if err != nil {
		t.Fatalf("failed to load rules: %v", err)
	}

	if len(rules.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules.Rules))
	}

	if rules.Rules[0].Name != "test-rule" {
		t.Errorf("expected rule name 'test-rule', got %q", rules.Rules[0].Name)
	}

	if rules.RoleCapabilities["proposal-writer"] != "writing" {
		t.Errorf("expected proposal-writer capability 'writing', got %q", rules.RoleCapabilities["proposal-writer"])
	}
}

func TestRuleMatches(t *testing.T) {
	rule := Rule{
		Name: "test-rule",
		Condition: Condition{
			KVBucket:   "AGENT_LOOPS",
			KeyPattern: "COMPLETE_*",
			Match: map[string]string{
				"role":                   "proposal-writer",
				"status":                 "complete",
				"metadata.auto_continue": "true",
			},
		},
	}

	tests := []struct {
		name     string
		state    *LoopState
		expected bool
	}{
		{
			name: "matching state",
			state: &LoopState{
				LoopID: "loop-123",
				Role:   "proposal-writer",
				Status: "complete",
				Metadata: map[string]string{
					"auto_continue": "true",
				},
			},
			expected: true,
		},
		{
			name: "wrong role",
			state: &LoopState{
				LoopID: "loop-123",
				Role:   "design-writer",
				Status: "complete",
				Metadata: map[string]string{
					"auto_continue": "true",
				},
			},
			expected: false,
		},
		{
			name: "wrong status",
			state: &LoopState{
				LoopID: "loop-123",
				Role:   "proposal-writer",
				Status: "failed",
				Metadata: map[string]string{
					"auto_continue": "true",
				},
			},
			expected: false,
		},
		{
			name: "missing metadata",
			state: &LoopState{
				LoopID:   "loop-123",
				Role:     "proposal-writer",
				Status:   "complete",
				Metadata: map[string]string{},
			},
			expected: false,
		},
		{
			name: "auto_continue false",
			state: &LoopState{
				LoopID: "loop-123",
				Role:   "proposal-writer",
				Status: "complete",
				Metadata: map[string]string{
					"auto_continue": "false",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rule.Matches(tt.state)
			if got != tt.expected {
				t.Errorf("Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWildcardMatch(t *testing.T) {
	rule := Rule{
		Name: "wildcard-rule",
		Condition: Condition{
			Match: map[string]string{
				"metadata.workflow_slug": "*", // Match any non-empty value
			},
		},
	}

	tests := []struct {
		name     string
		state    *LoopState
		expected bool
	}{
		{
			name: "has workflow_slug",
			state: &LoopState{
				Metadata: map[string]string{
					"workflow_slug": "add-auth",
				},
			},
			expected: true,
		},
		{
			name: "empty workflow_slug",
			state: &LoopState{
				Metadata: map[string]string{
					"workflow_slug": "",
				},
			},
			expected: false,
		},
		{
			name: "missing workflow_slug",
			state: &LoopState{
				Metadata: map[string]string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rule.Matches(tt.state)
			if got != tt.expected {
				t.Errorf("Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildPayload(t *testing.T) {
	action := Action{
		Type:    "publish_task",
		Subject: "agent.task.workflow",
		Payload: map[string]interface{}{
			"role":          "design-writer",
			"workflow_slug": "$entity.metadata.workflow_slug",
			"title":         "$entity.metadata.title",
			"auto_continue": true,
		},
	}

	state := &LoopState{
		LoopID: "loop-123",
		Role:   "proposal-writer",
		Metadata: map[string]string{
			"workflow_slug": "add-authentication",
			"title":         "Add Authentication",
		},
	}

	payload := action.BuildPayload(state)

	if payload["role"] != "design-writer" {
		t.Errorf("expected role 'design-writer', got %q", payload["role"])
	}

	if payload["workflow_slug"] != "add-authentication" {
		t.Errorf("expected workflow_slug 'add-authentication', got %q", payload["workflow_slug"])
	}

	if payload["title"] != "Add Authentication" {
		t.Errorf("expected title 'Add Authentication', got %q", payload["title"])
	}

	if payload["auto_continue"] != true {
		t.Errorf("expected auto_continue true, got %v", payload["auto_continue"])
	}
}

func TestSubstituteString(t *testing.T) {
	state := &LoopState{
		LoopID:       "loop-123",
		Role:         "proposal-writer",
		Status:       "complete",
		WorkflowSlug: "add-auth",
		WorkflowStep: "propose",
		Metadata: map[string]string{
			"title":       "Add Auth",
			"user_id":     "user-1",
			"channel_id":  "chan-1",
			"channel_type": "cli",
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"$entity.loop_id", "loop-123"},
		{"$entity.role", "proposal-writer"},
		{"$entity.status", "complete"},
		{"$entity.workflow_slug", "add-auth"},
		{"$entity.workflow_step", "propose"},
		{"$entity.metadata.title", "Add Auth"},
		{"$entity.metadata.user_id", "user-1"},
		{"user.response.$entity.metadata.channel_type.$entity.metadata.channel_id", "user.response.cli.chan-1"},
		{"Workflow $entity.metadata.title is $entity.status", "Workflow Add Auth is complete"},
		{"no substitution", "no substitution"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := substituteString(tt.input, state)
			if got != tt.expected {
				t.Errorf("substituteString(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
