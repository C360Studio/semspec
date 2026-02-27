package providers

import (
	"testing"

	"github.com/c360studio/semspec/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicProvider_BuildURL(t *testing.T) {
	p := &AnthropicProvider{}

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "empty uses default",
			baseURL: "",
			want:    "https://api.anthropic.com/v1/messages",
		},
		{
			name:    "custom base URL",
			baseURL: "https://custom.api.com",
			want:    "https://custom.api.com/v1/messages",
		},
		{
			name:    "trailing slash handled",
			baseURL: "https://api.anthropic.com/",
			want:    "https://api.anthropic.com/v1/messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.BuildURL(tt.baseURL)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAnthropicProvider_BuildRequestBody(t *testing.T) {
	p := &AnthropicProvider{}

	messages := []llm.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "How are you?"},
	}

	temp := 0.7
	body, err := p.BuildRequestBody("claude-3-opus", messages, &temp, 2048, nil, "")
	require.NoError(t, err)

	// Verify system message is extracted
	assert.Contains(t, string(body), `"system":"You are helpful."`)

	// Verify model is set
	assert.Contains(t, string(body), `"model":"claude-3-opus"`)

	// Verify max_tokens
	assert.Contains(t, string(body), `"max_tokens":2048`)

	// Verify messages don't contain system
	assert.NotContains(t, string(body), `"role":"system"`)

	// Verify user/assistant messages are present
	assert.Contains(t, string(body), `"role":"user"`)
	assert.Contains(t, string(body), `"role":"assistant"`)
}

func TestAnthropicProvider_BuildRequestBody_DefaultMaxTokens(t *testing.T) {
	p := &AnthropicProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Hello"},
	}

	body, err := p.BuildRequestBody("claude-3-opus", messages, nil, 0, nil, "")
	require.NoError(t, err)

	// Should use default of 4096
	assert.Contains(t, string(body), `"max_tokens":4096`)
	// Temperature should not be in body when nil
	assert.NotContains(t, string(body), `"temperature"`)
}

func TestAnthropicProvider_BuildRequestBody_ZeroTemperature(t *testing.T) {
	p := &AnthropicProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Hello"},
	}

	temp := 0.0
	body, err := p.BuildRequestBody("claude-3-opus", messages, &temp, 0, nil, "")
	require.NoError(t, err)

	// Temperature should be present even when 0 (deterministic)
	assert.Contains(t, string(body), `"temperature":0`)
}

func TestAnthropicProvider_ParseResponse(t *testing.T) {
	p := &AnthropicProvider{}

	responseBody := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello! How can I help you?"}
		],
		"model": "claude-3-opus-20240229",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 15,
			"output_tokens": 8
		}
	}`)

	resp, err := p.ParseResponse(responseBody, "claude-3-opus")
	require.NoError(t, err)

	assert.Equal(t, "Hello! How can I help you?", resp.Content)
	assert.Equal(t, "claude-3-opus-20240229", resp.Model)
	assert.Equal(t, 23, resp.TokensUsed)
	assert.Equal(t, "end_turn", resp.FinishReason)

	// Verify new Usage fields are populated
	assert.Equal(t, 15, resp.Usage.PromptTokens)
	assert.Equal(t, 8, resp.Usage.CompletionTokens)
	assert.Equal(t, 23, resp.Usage.TotalTokens)
}

func TestAnthropicProvider_ParseResponse_MultipleContentBlocks(t *testing.T) {
	p := &AnthropicProvider{}

	responseBody := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "First part. "},
			{"type": "text", "text": "Second part."}
		],
		"model": "claude-3-opus",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`)

	resp, err := p.ParseResponse(responseBody, "claude-3-opus")
	require.NoError(t, err)

	assert.Equal(t, "First part. Second part.", resp.Content)
}

func TestAnthropicProvider_BuildRequestBody_WithTools(t *testing.T) {
	p := &AnthropicProvider{}

	messages := []llm.Message{
		{Role: "user", Content: "Create a file called test.txt"},
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
				"required": []string{"path", "content"},
			},
		},
	}

	body, err := p.BuildRequestBody("claude-3-opus", messages, nil, 4096, tools, "auto")
	require.NoError(t, err)

	// Verify tools are included
	assert.Contains(t, string(body), `"tools":[`)
	assert.Contains(t, string(body), `"name":"file_write"`)
	assert.Contains(t, string(body), `"input_schema"`)
	assert.Contains(t, string(body), `"tool_choice":{"type":"auto"}`)
}

func TestAnthropicProvider_BuildRequestBody_WithToolCalls(t *testing.T) {
	p := &AnthropicProvider{}

	// Simulate a conversation with tool call and result
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
			Content:    "File written successfully",
		},
	}

	body, err := p.BuildRequestBody("claude-3-opus", messages, nil, 4096, nil, "")
	require.NoError(t, err)

	// Verify tool_use block is included
	assert.Contains(t, string(body), `"type":"tool_use"`)
	assert.Contains(t, string(body), `"id":"call_123"`)
	assert.Contains(t, string(body), `"name":"file_write"`)

	// Verify tool_result block is included
	assert.Contains(t, string(body), `"type":"tool_result"`)
	assert.Contains(t, string(body), `"tool_use_id":"call_123"`)
}

func TestAnthropicProvider_ParseResponse_WithToolUse(t *testing.T) {
	p := &AnthropicProvider{}

	responseBody := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "I'll create that file for you."},
			{
				"type": "tool_use",
				"id": "toolu_123",
				"name": "file_write",
				"input": {"path": "test.txt", "content": "hello world"}
			}
		],
		"model": "claude-3-opus",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 20, "output_tokens": 50}
	}`)

	resp, err := p.ParseResponse(responseBody, "claude-3-opus")
	require.NoError(t, err)

	assert.Equal(t, "I'll create that file for you.", resp.Content)
	assert.Equal(t, "tool_use", resp.FinishReason)
	assert.Len(t, resp.ToolCalls, 1)

	tc := resp.ToolCalls[0]
	assert.Equal(t, "toolu_123", tc.ID)
	assert.Equal(t, "file_write", tc.Name)
	assert.Equal(t, "test.txt", tc.Arguments["path"])
	assert.Equal(t, "hello world", tc.Arguments["content"])
}
