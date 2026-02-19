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
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// BuildRequestBody creates the OpenAI-compatible request body.
func (o *OllamaProvider) BuildRequestBody(model string, messages []llm.Message, temperature *float64, maxTokens int) ([]byte, error) {
	apiMessages := make([]openAIMessage, len(messages))
	for i, msg := range messages {
		apiMessages[i] = openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
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
			Role    string `json:"role"`
			Content string `json:"content"`
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

	return &llm.Response{
		Content:      resp.Choices[0].Message.Content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.TotalTokens,
		FinishReason: resp.Choices[0].FinishReason,
	}, nil
}
