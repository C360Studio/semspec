package webingester

import (
	"strings"
	"testing"
)

func TestExtractHTMLTitle(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "simple title",
			html:     "<html><head><title>My Page</title></head><body></body></html>",
			expected: "My Page",
		},
		{
			name:     "title with whitespace",
			html:     "<html><head><title>  Spaced Title  </title></head></html>",
			expected: "Spaced Title",
		},
		{
			name:     "no title",
			html:     "<html><head></head><body>Content</body></html>",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHTMLTitle([]byte(tt.html))
			if got != tt.expected {
				t.Errorf("extractHTMLTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractMarkdownTitle(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		expected string
	}{
		{
			name:     "H1 at start",
			markdown: "# Hello World\n\nContent here",
			expected: "Hello World",
		},
		{
			name:     "H1 with leading space",
			markdown: "Some text\n\n# Title Here\n\nMore content",
			expected: "Title Here",
		},
		{
			name:     "no H1",
			markdown: "## Section\n\nContent",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMarkdownTitle(tt.markdown)
			if got != tt.expected {
				t.Errorf("extractMarkdownTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCleanMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "excessive newlines",
			input: "Line 1\n\n\n\n\n\nLine 2",
		},
		{
			name:  "trailing spaces",
			input: "Line with trailing space   \nAnother line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanMarkdown(tt.input)
			// Should not have more than 3 consecutive newlines
			if strings.Contains(got, "\n\n\n\n") {
				t.Error("cleanMarkdown should remove excessive newlines")
			}
			// Should not have trailing spaces
			lines := strings.Split(got, "\n")
			for _, line := range lines {
				if strings.HasSuffix(line, " ") {
					t.Errorf("cleanMarkdown should remove trailing spaces: %q", line)
				}
			}
		})
	}
}

func TestConverter(t *testing.T) {
	converter := NewConverter()

	html := []byte(`<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<nav>Navigation</nav>
<main>
<h1>Main Heading</h1>
<p>This is a paragraph with <strong>bold</strong> text.</p>
<ul>
<li>Item 1</li>
<li>Item 2</li>
</ul>
</main>
<footer>Footer</footer>
</body>
</html>`)

	result, err := converter.Convert(html)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	if result.Title != "Test Page" {
		t.Errorf("Title = %q, want %q", result.Title, "Test Page")
	}

	// Should contain the main heading
	if !strings.Contains(result.Markdown, "Main Heading") {
		t.Error("Markdown should contain 'Main Heading'")
	}

	// Should contain list items
	if !strings.Contains(result.Markdown, "Item 1") {
		t.Error("Markdown should contain 'Item 1'")
	}
}
