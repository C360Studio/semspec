package webingester

import (
	"testing"

	"github.com/c360studio/semspec/source/weburl"
)

// TestGenerateWebEntityID tests entity ID generation via the shared weburl package.
// More comprehensive tests are in source/weburl/weburl_test.go.
func TestGenerateWebEntityID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "simple domain",
			url:      "https://go.dev",
			expected: "c360.semspec.source.web.page.go-dev",
		},
		{
			name:     "domain with path",
			url:      "https://go.dev/doc/effective_go",
			expected: "c360.semspec.source.web.page.go-dev-doc-effective-go",
		},
		{
			name:     "subdomain",
			url:      "https://pkg.go.dev/encoding/json",
			expected: "c360.semspec.source.web.page.pkg-go-dev-encoding-json",
		},
		{
			name:     "github docs",
			url:      "https://docs.github.com/en/rest",
			expected: "c360.semspec.source.web.page.docs-github-com-en-rest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weburl.GenerateEntityID(tt.url)
			if got != tt.expected {
				t.Errorf("GenerateEntityID(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestComputeHash(t *testing.T) {
	hash := computeHash([]byte("hello world"))
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("computeHash(\"hello world\") = %q, want %q", hash, expected)
	}
}

func TestExtractDomainName(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://go.dev/doc/effective_go", "go.dev"},
		{"https://docs.github.com/en/rest", "docs.github.com"},
		// Invalid URL returns the raw URL as fallback for title purposes
		{"invalid-url", "invalid-url"},
	}

	for _, tt := range tests {
		got := extractDomainName(tt.url)
		if got != tt.expected {
			t.Errorf("extractDomainName(%q) = %q, want %q", tt.url, got, tt.expected)
		}
	}
}
