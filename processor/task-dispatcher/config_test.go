package taskdispatcher

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify sensible defaults
	if cfg.StreamName != "WORKFLOW" {
		t.Errorf("expected StreamName 'WORKFLOW', got %s", cfg.StreamName)
	}
	if cfg.ConsumerName != "task-dispatcher" {
		t.Errorf("expected ConsumerName 'task-dispatcher', got %s", cfg.ConsumerName)
	}
	if cfg.TriggerSubject != "workflow.trigger.task-dispatcher" {
		t.Errorf("expected TriggerSubject 'workflow.trigger.task-dispatcher', got %s", cfg.TriggerSubject)
	}
	if cfg.OutputSubject != "workflow.result.task-dispatcher" {
		t.Errorf("expected OutputSubject 'workflow.result.task-dispatcher', got %s", cfg.OutputSubject)
	}
	if cfg.MaxConcurrent != 3 {
		t.Errorf("expected MaxConcurrent 3, got %d", cfg.MaxConcurrent)
	}
	if cfg.ContextTimeout != "30s" {
		t.Errorf("expected ContextTimeout '30s', got %s", cfg.ContextTimeout)
	}
	if cfg.ExecutionTimeout != "300s" {
		t.Errorf("expected ExecutionTimeout '300s', got %s", cfg.ExecutionTimeout)
	}
	if cfg.ContextSubjectPrefix != "context.build" {
		t.Errorf("expected ContextSubjectPrefix 'context.build', got %s", cfg.ContextSubjectPrefix)
	}
	if cfg.ContextResponseBucket != "CONTEXT_RESPONSES" {
		t.Errorf("expected ContextResponseBucket 'CONTEXT_RESPONSES', got %s", cfg.ContextResponseBucket)
	}
	if cfg.WorkflowTriggerSubject != "workflow.trigger.task-execution-loop" {
		t.Errorf("expected WorkflowTriggerSubject 'workflow.trigger.task-execution-loop', got %s", cfg.WorkflowTriggerSubject)
	}
	if cfg.Ports == nil {
		t.Error("expected Ports to be set")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing stream_name",
			config: Config{
				ConsumerName:   "test",
				TriggerSubject: "test",
				MaxConcurrent:  3,
			},
			wantErr: true,
			errMsg:  "stream_name is required",
		},
		{
			name: "missing consumer_name",
			config: Config{
				StreamName:     "test",
				TriggerSubject: "test",
				MaxConcurrent:  3,
			},
			wantErr: true,
			errMsg:  "consumer_name is required",
		},
		{
			name: "missing trigger_subject",
			config: Config{
				StreamName:    "test",
				ConsumerName:  "test",
				MaxConcurrent: 3,
			},
			wantErr: true,
			errMsg:  "trigger_subject is required",
		},
		{
			name: "max_concurrent too low",
			config: Config{
				StreamName:     "test",
				ConsumerName:   "test",
				TriggerSubject: "test",
				MaxConcurrent:  0,
			},
			wantErr: true,
			errMsg:  "max_concurrent must be at least 1",
		},
		{
			name: "max_concurrent too high",
			config: Config{
				StreamName:     "test",
				ConsumerName:   "test",
				TriggerSubject: "test",
				MaxConcurrent:  11,
			},
			wantErr: true,
			errMsg:  "max_concurrent cannot exceed 10",
		},
		{
			name: "invalid context_timeout",
			config: Config{
				StreamName:     "test",
				ConsumerName:   "test",
				TriggerSubject: "test",
				MaxConcurrent:  3,
				ContextTimeout: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid context_timeout",
		},
		{
			name: "invalid execution_timeout",
			config: Config{
				StreamName:       "test",
				ConsumerName:     "test",
				TriggerSubject:   "test",
				MaxConcurrent:    3,
				ExecutionTimeout: "not-a-duration",
			},
			wantErr: true,
			errMsg:  "invalid execution_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && err.Error() != tt.errMsg && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfig_GetContextTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected time.Duration
	}{
		{
			name:     "empty returns default",
			timeout:  "",
			expected: 30 * time.Second,
		},
		{
			name:     "valid duration",
			timeout:  "45s",
			expected: 45 * time.Second,
		},
		{
			name:     "valid minutes",
			timeout:  "2m",
			expected: 2 * time.Minute,
		},
		{
			name:     "invalid returns default",
			timeout:  "invalid",
			expected: 30 * time.Second,
		},
		{
			name:     "negative returns default",
			timeout:  "-10s",
			expected: 30 * time.Second,
		},
		{
			name:     "zero returns default",
			timeout:  "0s",
			expected: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ContextTimeout: tt.timeout}
			got := cfg.GetContextTimeout()
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestConfig_GetExecutionTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected time.Duration
	}{
		{
			name:     "empty returns default",
			timeout:  "",
			expected: 300 * time.Second,
		},
		{
			name:     "valid duration",
			timeout:  "600s",
			expected: 600 * time.Second,
		},
		{
			name:     "valid minutes",
			timeout:  "10m",
			expected: 10 * time.Minute,
		},
		{
			name:     "invalid returns default",
			timeout:  "invalid",
			expected: 300 * time.Second,
		},
		{
			name:     "negative returns default",
			timeout:  "-60s",
			expected: 300 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ExecutionTimeout: tt.timeout}
			got := cfg.GetExecutionTimeout()
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
