package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/c360studio/semspec/llm"
)

// OllamaProvider implements the OpenAI-compatible API used by Ollama, vLLM, etc.
type OllamaProvider struct{}

func init() {
	llm.RegisterProvider(&OllamaProvider{})
}

// Name returns the provider identifier.
func (o *OllamaProvider) Name() string {
	return "ollama"
}

// BuildURL constructs the chat completions endpoint.
func (o *OllamaProvider) BuildURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Check if URL already ends with chat/completions
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}

	return baseURL + "/chat/completions"
}

// SetHeaders adds OpenAI-compatible headers.
func (o *OllamaProvider) SetHeaders(req *http.Request) {
	// Check for API key (for OpenRouter, vLLM, etc.)
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// openAIRequest is the OpenAI-compatible request format.
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"` // string or object
}

// openAITool represents a tool in OpenAI function calling format.
type openAITool struct {
	Type     string         `json:"type"` // "function"
	Function openAIFunction `json:"function"`
}

// openAIFunction represents function details in OpenAI format.
type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// openAIMessage represents a message in OpenAI format.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`  // For assistant with tool calls
	ToolCallID string           `json:"tool_call_id,omitempty"` // For tool result messages
}

// openAIToolCall represents a tool call in the OpenAI format.
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

// BuildRequestBody creates the OpenAI-compatible request body.
func (o *OllamaProvider) BuildRequestBody(model string, messages []llm.Message, temperature *float64, maxTokens int,
	tools []llm.ToolDefinition, toolChoice string) ([]byte, error) {
	apiMessages := make([]openAIMessage, 0, len(messages))

	for _, msg := range messages {
		apiMsg := openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Handle assistant messages with tool calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			apiMsg.ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				// Serialize arguments to JSON string
				argsJSON, err := json.Marshal(tc.Arguments)
				if err != nil {
					argsJSON = []byte("{}")
				}
				apiMsg.ToolCalls[i] = openAIToolCall{
					ID:   tc.ID,
					Type: "function",
				}
				apiMsg.ToolCalls[i].Function.Name = tc.Name
				apiMsg.ToolCalls[i].Function.Arguments = string(argsJSON)
			}
		}

		// Handle tool result messages
		if msg.Role == "tool" {
			apiMsg.ToolCallID = msg.ToolCallID
		}

		apiMessages = append(apiMessages, apiMsg)
	}

	req := openAIRequest{
		Model:       model,
		Messages:    apiMessages,
		Temperature: temperature, // nil = use default, 0 = deterministic
	}

	// Only set max_tokens if explicitly provided
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}

	// Add tools if provided
	if len(tools) > 0 {
		req.Tools = make([]openAITool, len(tools))
		for i, tool := range tools {
			req.Tools[i] = openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			}
		}

		// Set tool choice if specified
		if toolChoice != "" {
			switch toolChoice {
			case "auto":
				req.ToolChoice = "auto"
			case "required":
				req.ToolChoice = "required"
			case "none":
				req.ToolChoice = "none"
			default:
				// Specific tool name
				req.ToolChoice = map[string]any{
					"type": "function",
					"function": map[string]string{
						"name": toolChoice,
					},
				}
			}
		}
	}

	return json.Marshal(req)
}

// openAIResponse is the OpenAI-compatible response format.
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ParseResponse extracts content from OpenAI-compatible response.
func (o *OllamaProvider) ParseResponse(body []byte, _ string) (*llm.Response, error) {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	var toolCalls []llm.ToolCall

	// Parse tool calls if present
	for _, tc := range choice.Message.ToolCalls {
		// Parse arguments from JSON string
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			args = make(map[string]any)
		}
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return &llm.Response{
		Content:    choice.Message.Content,
		Model:      resp.Model,
		TokensUsed: resp.Usage.TotalTokens, // Keep for backward compatibility
		Usage: llm.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		FinishReason: choice.FinishReason,
		ToolCalls:    toolCalls,
	}, nil
}
