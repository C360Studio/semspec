package providers

import (
	"net/http"
	"os"
	"strings"

	"github.com/c360studio/semspec/llm"
)

// OpenAIProvider implements the OpenAI API for direct OpenAI or OpenRouter usage.
// This is separate from OllamaProvider to allow different default URLs and auth.
type OpenAIProvider struct {
	OllamaProvider // Embed for shared request/response format
}

func init() {
	llm.RegisterProvider(&OpenAIProvider{})
}

// Name returns the provider identifier.
func (o *OpenAIProvider) Name() string {
	return "openai"
}

// BuildURL constructs the OpenAI API endpoint.
func (o *OpenAIProvider) BuildURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}

	return baseURL + "/chat/completions"
}

// SetHeaders adds OpenAI authentication headers.
func (o *OpenAIProvider) SetHeaders(req *http.Request) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Support OpenRouter
	if siteURL := os.Getenv("OPENROUTER_SITE_URL"); siteURL != "" {
		req.Header.Set("HTTP-Referer", siteURL)
	}
	if siteName := os.Getenv("OPENROUTER_SITE_NAME"); siteName != "" {
		req.Header.Set("X-Title", siteName)
	}
}
