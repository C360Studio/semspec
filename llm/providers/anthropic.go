// Package providers implements LLM provider adapters.
package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/c360studio/semspec/llm"
)

// AnthropicProvider implements the Anthropic API.
type AnthropicProvider struct{}

// anthropicVersion is the API version to use.
const anthropicVersion = "2023-06-01"

func init() {
	llm.RegisterProvider(&AnthropicProvider{})
}

// Name returns the provider identifier.
func (a *AnthropicProvider) Name() string {
	return "anthropic"
}

// BuildURL constructs the Anthropic messages endpoint.
func (a *AnthropicProvider) BuildURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return baseURL + "/v1/messages"
}

// SetHeaders adds Anthropic-specific authentication headers.
func (a *AnthropicProvider) SetHeaders(req *http.Request) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	req.Header.Set("anthropic-version", anthropicVersion)
}

// anthropicRequest is the Anthropic API request format.
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
}

// anthropicTool represents a tool definition in Anthropic format.
type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicToolChoice controls tool selection behavior.
type anthropicToolChoice struct {
	Type string `json:"type"` // "auto", "any", "tool"
	Name string `json:"name,omitempty"` // Only for type="tool"
}

// anthropicMessage represents a message in Anthropic format.
// Content can be a string or an array of content blocks.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

// anthropicContentBlock represents a content block in Anthropic messages.
type anthropicContentBlock struct {
	Type      string         `json:"type"` // "text", "tool_use", "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`          // For tool_use
	Name      string         `json:"name,omitempty"`        // For tool_use
	Input     map[string]any `json:"input,omitempty"`       // For tool_use - must be non-nil when type=tool_use
	ToolUseID string         `json:"tool_use_id,omitempty"` // For tool_result
	Content   string         `json:"content,omitempty"`     // For tool_result
}

// BuildRequestBody creates the Anthropic API request body.
func (a *AnthropicProvider) BuildRequestBody(model string, messages []llm.Message, temperature *float64, maxTokens int,
	tools []llm.ToolDefinition, toolChoice string) ([]byte, error) {
	// Extract system message if present
	var systemPrompt string
	var apiMessages []anthropicMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}

		// Handle messages with tool calls (assistant responses with tool_use)
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var blocks []anthropicContentBlock
			// Add text content if present
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{
					Type: "text",
					Text: msg.Content,
				})
			}
			// Add tool_use blocks
			for _, tc := range msg.ToolCalls {
				// Anthropic requires input field to be present, even if empty
				input := tc.Arguments
				if input == nil {
					input = make(map[string]any)
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			apiMessages = append(apiMessages, anthropicMessage{
				Role:    "assistant",
				Content: blocks,
			})
			continue
		}

		// Handle tool result messages
		if msg.Role == "tool" && msg.ToolCallID != "" {
			apiMessages = append(apiMessages, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
			continue
		}

		// Regular text message
		apiMessages = append(apiMessages, anthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Default max tokens if not specified
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	req := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Messages:    apiMessages,
		System:      systemPrompt,
		Temperature: temperature, // nil = use default, 0 = deterministic
	}

	// Add tools if provided
	if len(tools) > 0 {
		req.Tools = make([]anthropicTool, len(tools))
		for i, tool := range tools {
			req.Tools[i] = anthropicTool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.Parameters,
			}
		}

		// Set tool choice if specified
		if toolChoice != "" {
			switch toolChoice {
			case "auto":
				req.ToolChoice = &anthropicToolChoice{Type: "auto"}
			case "required", "any":
				req.ToolChoice = &anthropicToolChoice{Type: "any"}
			case "none":
				// Don't set tool_choice - Anthropic doesn't have "none", just don't send tools
				req.Tools = nil
			default:
				// Assume it's a specific tool name
				req.ToolChoice = &anthropicToolChoice{Type: "tool", Name: toolChoice}
			}
		}
	}

	return json.Marshal(req)
}

// anthropicResponse is the Anthropic API response format.
type anthropicResponse struct {
	ID      string                   `json:"id"`
	Type    string                   `json:"type"`
	Role    string                   `json:"role"`
	Content []anthropicResponseBlock `json:"content"`
	Model   string                   `json:"model"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// anthropicResponseBlock represents a content block in the response.
type anthropicResponseBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`  // For "text" type
	ID    string         `json:"id,omitempty"`    // For "tool_use" type
	Name  string         `json:"name,omitempty"`  // For "tool_use" type
	Input map[string]any `json:"input,omitempty"` // For "tool_use" type
}

// ParseResponse extracts content from Anthropic response.
func (a *AnthropicProvider) ParseResponse(body []byte, _ string) (*llm.Response, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}

	// Extract text content and tool calls
	var content string
	var toolCalls []llm.ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}

	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
	return &llm.Response{
		Content:    content,
		Model:      resp.Model,
		TokensUsed: totalTokens, // Keep for backward compatibility
		Usage: llm.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      totalTokens,
		},
		FinishReason: resp.StopReason,
		ToolCalls:    toolCalls,
	}, nil
}
