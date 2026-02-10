package main

import (
	"os"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandEnvWithDefaults verifies that environment variable expansion
// properly handles ${VAR:-default} syntax.
func TestExpandEnvWithDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		env      map[string]string
		expected string
	}{
		{
			name:     "default used when var unset",
			input:    `${LLM_API_URL:-http://localhost:11434}/v1`,
			env:      map[string]string{}, // LLM_API_URL not set
			expected: `http://localhost:11434/v1`,
		},
		{
			name:     "env value used when set",
			input:    `${LLM_API_URL:-http://localhost:11434}/v1`,
			env:      map[string]string{"LLM_API_URL": "http://prod:8080"},
			expected: `http://prod:8080/v1`,
		},
		{
			name:     "multiple vars with defaults",
			input:    `nats://${NATS_HOST:-localhost}:${NATS_PORT:-4222}`,
			env:      map[string]string{},
			expected: `nats://localhost:4222`,
		},
		{
			name:     "partial env set",
			input:    `nats://${NATS_HOST:-localhost}:${NATS_PORT:-4222}`,
			env:      map[string]string{"NATS_HOST": "nats.prod"},
			expected: `nats://nats.prod:4222`,
		},
		{
			name:     "empty default",
			input:    `prefix${OPTIONAL:-}suffix`,
			env:      map[string]string{},
			expected: `prefixsuffix`,
		},
		{
			name:     "simple var without default",
			input:    `${SIMPLE_VAR}`,
			env:      map[string]string{"SIMPLE_VAR": "value"},
			expected: `value`,
		},
		{
			name:     "simple var unset without default",
			input:    `${SIMPLE_VAR}`,
			env:      map[string]string{},
			expected: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars
			envVars := []string{"LLM_API_URL", "NATS_HOST", "NATS_PORT", "OPTIONAL", "SIMPLE_VAR"}
			for _, v := range envVars {
				os.Unsetenv(v)
			}

			// Set test env vars
			for k, v := range tt.env {
				require.NoError(t, os.Setenv(k, v))
			}

			result := config.ExpandEnvWithDefaults(tt.input)

			assert.Equal(t, tt.expected, result, "expansion mismatch for input: %s", tt.input)
		})
	}
}
