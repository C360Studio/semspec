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
	body, err := p.BuildRequestBody("qwen2.5-coder:14b", messages, &temp, 2048, nil, "")
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

	body, err := p.BuildRequestBody("test-model", messages, nil, 0, nil, "")
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
	body, err := p.BuildRequestBody("test-model", messages, &temp, 0, nil, "")
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

func TestOllamaProvider_BuildRequestBody_WithTools(t *testing.T) {
	p := &OllamaProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Create a file"},
	}

	tools := []llm.ToolDefinition{
		{
			Name:        "file_write",
			Description: "Write content to a file",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
			},
		},
	}

	body, err := p.BuildRequestBody("qwen", messages, nil, 0, tools, "auto")
	require.NoError(t, err)

	// Verify tools are included in OpenAI format
	assert.Contains(t, string(body), `"tools":[`)
	assert.Contains(t, string(body), `"type":"function"`)
	assert.Contains(t, string(body), `"name":"file_write"`)
	assert.Contains(t, string(body), `"tool_choice":"auto"`)
}

func TestOllamaProvider_BuildRequestBody_WithToolCalls(t *testing.T) {
	p := &OllamaProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Create a file"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{
					ID:        "call_123",
					Name:      "file_write",
					Arguments: map[string]any{"path": "test.txt", "content": "hello"},
				},
			},
		},
		{
			Role:       "tool",
			ToolCallID: "call_123",
			Content:    "File written",
		},
	}

	body, err := p.BuildRequestBody("qwen", messages, nil, 0, nil, "")
	require.NoError(t, err)

	// Verify tool_calls in assistant message (OpenAI format)
	assert.Contains(t, string(body), `"tool_calls":[`)
	assert.Contains(t, string(body), `"id":"call_123"`)
	assert.Contains(t, string(body), `"type":"function"`)

	// Verify tool result message
	assert.Contains(t, string(body), `"role":"tool"`)
	assert.Contains(t, string(body), `"tool_call_id":"call_123"`)
}

func TestOllamaProvider_ParseResponse_WithToolCalls(t *testing.T) {
	p := &OllamaProvider{}

	responseBody := []byte(`{
		"id": "chatcmpl-123",
		"model": "qwen2.5",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "I'll create that file.",
				"tool_calls": [{
					"id": "call_456",
					"type": "function",
					"function": {
						"name": "file_write",
						"arguments": "{\"path\":\"test.txt\",\"content\":\"hello\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
	}`)

	resp, err := p.ParseResponse(responseBody, "qwen")
	require.NoError(t, err)

	assert.Equal(t, "I'll create that file.", resp.Content)
	assert.Equal(t, "tool_calls", resp.FinishReason)
	assert.Len(t, resp.ToolCalls, 1)

	tc := resp.ToolCalls[0]
	assert.Equal(t, "call_456", tc.ID)
	assert.Equal(t, "file_write", tc.Name)
	assert.Equal(t, "test.txt", tc.Arguments["path"])
	assert.Equal(t, "hello", tc.Arguments["content"])
}
