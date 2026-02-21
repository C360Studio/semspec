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
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// BuildRequestBody creates the Anthropic API request body.
func (a *AnthropicProvider) BuildRequestBody(model string, messages []llm.Message, temperature *float64, maxTokens int) ([]byte, error) {
	// Extract system message if present
	var systemPrompt string
	var apiMessages []anthropicMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
		} else {
			apiMessages = append(apiMessages, anthropicMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
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

	return json.Marshal(req)
}

// anthropicResponse is the Anthropic API response format.
type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ParseResponse extracts content from Anthropic response.
func (a *AnthropicProvider) ParseResponse(body []byte, _ string) (*llm.Response, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}

	// Extract text content
	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
	return &llm.Response{
		Content:      content,
		Model:        resp.Model,
		TokensUsed:   totalTokens, // Keep for backward compatibility
		Usage: llm.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      totalTokens,
		},
		FinishReason: resp.StopReason,
	}, nil
}
