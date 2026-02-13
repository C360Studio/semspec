package providers

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenAIProvider_Name(t *testing.T) {
	p := &OpenAIProvider{}
	assert.Equal(t, "openai", p.Name())
}

func TestOpenAIProvider_BuildURL(t *testing.T) {
	p := &OpenAIProvider{}

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "empty uses default",
			baseURL: "",
			want:    "https://api.openai.com/v1/chat/completions",
		},
		{
			name:    "custom base URL (OpenRouter)",
			baseURL: "https://openrouter.ai/api/v1",
			want:    "https://openrouter.ai/api/v1/chat/completions",
		},
		{
			name:    "trailing slash handled",
			baseURL: "https://api.openai.com/v1/",
			want:    "https://api.openai.com/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.BuildURL(tt.baseURL)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOpenAIProvider_SetHeaders(t *testing.T) {
	p := &OpenAIProvider{}

	t.Run("sets authorization header", func(t *testing.T) {
		// Set env var for test
		oldKey := os.Getenv("OPENAI_API_KEY")
		os.Setenv("OPENAI_API_KEY", "test-api-key")
		defer os.Setenv("OPENAI_API_KEY", oldKey)

		req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
		p.SetHeaders(req)

		assert.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))
	})

	t.Run("sets OpenRouter headers when env vars present", func(t *testing.T) {
		// Set env vars for test
		oldSiteURL := os.Getenv("OPENROUTER_SITE_URL")
		oldSiteName := os.Getenv("OPENROUTER_SITE_NAME")
		os.Setenv("OPENROUTER_SITE_URL", "https://myapp.com")
		os.Setenv("OPENROUTER_SITE_NAME", "My App")
		defer func() {
			os.Setenv("OPENROUTER_SITE_URL", oldSiteURL)
			os.Setenv("OPENROUTER_SITE_NAME", oldSiteName)
		}()

		req, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", nil)
		p.SetHeaders(req)

		assert.Equal(t, "https://myapp.com", req.Header.Get("HTTP-Referer"))
		assert.Equal(t, "My App", req.Header.Get("X-Title"))
	})

	t.Run("no headers when env vars not set", func(t *testing.T) {
		// Clear env vars
		oldKey := os.Getenv("OPENAI_API_KEY")
		oldSiteURL := os.Getenv("OPENROUTER_SITE_URL")
		oldSiteName := os.Getenv("OPENROUTER_SITE_NAME")
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("OPENROUTER_SITE_URL")
		os.Unsetenv("OPENROUTER_SITE_NAME")
		defer func() {
			if oldKey != "" {
				os.Setenv("OPENAI_API_KEY", oldKey)
			}
			if oldSiteURL != "" {
				os.Setenv("OPENROUTER_SITE_URL", oldSiteURL)
			}
			if oldSiteName != "" {
				os.Setenv("OPENROUTER_SITE_NAME", oldSiteName)
			}
		}()

		req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
		p.SetHeaders(req)

		assert.Empty(t, req.Header.Get("Authorization"))
		assert.Empty(t, req.Header.Get("HTTP-Referer"))
		assert.Empty(t, req.Header.Get("X-Title"))
	})
}
