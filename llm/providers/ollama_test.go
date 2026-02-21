package providers

import (
	"testing"

	"github.com/c360studio/semspec/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaProvider_BuildURL(t *testing.T) {
	p := &OllamaProvider{}

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "empty uses default",
			baseURL: "",
			want:    "http://localhost:11434/v1/chat/completions",
		},
		{
			name:    "custom base URL",
			baseURL: "http://myserver:8080/v1",
			want:    "http://myserver:8080/v1/chat/completions",
		},
		{
			name:    "trailing slash handled",
			baseURL: "http://localhost:11434/v1/",
			want:    "http://localhost:11434/v1/chat/completions",
		},
		{
			name:    "already has endpoint",
			baseURL: "http://localhost:11434/v1/chat/completions",
			want:    "http://localhost:11434/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.BuildURL(tt.baseURL)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOllamaProvider_BuildRequestBody(t *testing.T) {
	p := &OllamaProvider{}

	messages := []llm.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	temp := 0.7
	body, err := p.BuildRequestBody("qwen2.5-coder:14b", messages, &temp, 2048)
	require.NoError(t, err)

	// Verify model is set
	assert.Contains(t, string(body), `"model":"qwen2.5-coder:14b"`)

	// Verify messages include system (OpenAI format keeps system as message)
	assert.Contains(t, string(body), `"role":"system"`)
	assert.Contains(t, string(body), `"role":"user"`)

	// Verify optional parameters
	assert.Contains(t, string(body), `"temperature":0.7`)
	assert.Contains(t, string(body), `"max_tokens":2048`)
}

func TestOllamaProvider_BuildRequestBody_NoOptionalParams(t *testing.T) {
	p := &OllamaProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Hello"},
	}

	body, err := p.BuildRequestBody("test-model", messages, nil, 0)
	require.NoError(t, err)

	// Should not contain temperature or max_tokens when nil/zero
	assert.NotContains(t, string(body), `"temperature"`)
	assert.NotContains(t, string(body), `"max_tokens"`)
}

func TestOllamaProvider_BuildRequestBody_ZeroTemperature(t *testing.T) {
	p := &OllamaProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Hello"},
	}

	temp := 0.0
	body, err := p.BuildRequestBody("test-model", messages, &temp, 0)
	require.NoError(t, err)

	// Temperature should be present even when 0 (deterministic)
	assert.Contains(t, string(body), `"temperature":0`)
}

func TestOllamaProvider_ParseResponse(t *testing.T) {
	p := &OllamaProvider{}

	responseBody := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "qwen2.5-coder:14b",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello! How can I help?"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 6,
			"total_tokens": 16
		}
	}`)

	resp, err := p.ParseResponse(responseBody, "test-model")
	require.NoError(t, err)

	assert.Equal(t, "Hello! How can I help?", resp.Content)
	assert.Equal(t, "qwen2.5-coder:14b", resp.Model)
	assert.Equal(t, 16, resp.TokensUsed)
	assert.Equal(t, "stop", resp.FinishReason)

	// Verify new Usage fields are populated
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 6, resp.Usage.CompletionTokens)
	assert.Equal(t, 16, resp.Usage.TotalTokens)
}

func TestOllamaProvider_ParseResponse_NoChoices(t *testing.T) {
	p := &OllamaProvider{}

	responseBody := []byte(`{
		"id": "chatcmpl-123",
		"choices": []
	}`)

	_, err := p.ParseResponse(responseBody, "test-model")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}
