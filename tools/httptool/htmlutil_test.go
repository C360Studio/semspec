package httptool

import (
	"strings"
	"testing"
)

func TestHtmlToText_BasicHTML(t *testing.T) {
	input := `<html><body><h1>Title</h1><p>Hello world</p></body></html>`
	text, truncated := htmlToText(strings.NewReader(input), 0)
	if truncated {
		t.Error("unexpected truncation")
	}
	if !strings.Contains(text, "# Title") {
		t.Errorf("missing heading: %q", text)
	}
	if !strings.Contains(text, "Hello world") {
		t.Errorf("missing paragraph text: %q", text)
	}
}

func TestHtmlToText_StripsScriptAndStyle(t *testing.T) {
	input := `<html><body><script>alert('xss')</script><style>.foo{color:red}</style><p>Content</p></body></html>`
	text, _ := htmlToText(strings.NewReader(input), 0)
	if strings.Contains(text, "alert") {
		t.Errorf("script content not stripped: %q", text)
	}
	if strings.Contains(text, ".foo") {
		t.Errorf("style content not stripped: %q", text)
	}
	if !strings.Contains(text, "Content") {
		t.Errorf("paragraph content missing: %q", text)
	}
}

func TestHtmlToText_StripsNavFooterHeader(t *testing.T) {
	input := `<html><body>
		<nav><a href="/">Home</a></nav>
		<header><h2>Site Header</h2></header>
		<p>Main content</p>
		<footer>Copyright 2024</footer>
	</body></html>`
	text, _ := htmlToText(strings.NewReader(input), 0)
	if strings.Contains(text, "Home") {
		t.Errorf("nav content not stripped: %q", text)
	}
	if strings.Contains(text, "Site Header") {
		t.Errorf("header content not stripped: %q", text)
	}
	if strings.Contains(text, "Copyright") {
		t.Errorf("footer content not stripped: %q", text)
	}
	if !strings.Contains(text, "Main content") {
		t.Errorf("main content missing: %q", text)
	}
}

func TestHtmlToText_ListItems(t *testing.T) {
	input := `<ul><li>First</li><li>Second</li><li>Third</li></ul>`
	text, _ := htmlToText(strings.NewReader(input), 0)
	if !strings.Contains(text, "- First") {
		t.Errorf("missing first list item: %q", text)
	}
	if !strings.Contains(text, "- Second") {
		t.Errorf("missing second list item: %q", text)
	}
}

func TestHtmlToText_Headings(t *testing.T) {
	input := `<h2>Section</h2><h3>Subsection</h3>`
	text, _ := htmlToText(strings.NewReader(input), 0)
	if !strings.Contains(text, "## Section") {
		t.Errorf("missing h2 heading: %q", text)
	}
	if !strings.Contains(text, "### Subsection") {
		t.Errorf("missing h3 heading: %q", text)
	}
}

func TestHtmlToText_Truncation(t *testing.T) {
	// 100 'x' chars in a paragraph — well over our 50-byte limit.
	input := `<p>` + strings.Repeat("x", 100) + `</p>`
	text, truncated := htmlToText(strings.NewReader(input), 50)
	if !truncated {
		t.Error("expected truncation flag to be true")
	}
	// Allow some slack for the newline added before/after the <p> tag.
	if len(text) > 60 {
		t.Errorf("text too long after truncation: %d chars, got: %q", len(text), text)
	}
}

func TestHtmlToText_NoTruncationUnderLimit(t *testing.T) {
	input := `<p>Short text</p>`
	_, truncated := htmlToText(strings.NewReader(input), 1000)
	if truncated {
		t.Error("should not be truncated")
	}
}

func TestExtractTitle(t *testing.T) {
	input := `<html><head><title>My Page Title</title></head><body></body></html>`
	title := extractTitle(strings.NewReader(input))
	if title != "My Page Title" {
		t.Errorf("title = %q, want %q", title, "My Page Title")
	}
}

func TestExtractTitle_Missing(t *testing.T) {
	input := `<html><body><p>No title here</p></body></html>`
	title := extractTitle(strings.NewReader(input))
	if title != "" {
		t.Errorf("expected empty title, got %q", title)
	}
}

func TestCollapseNewlines(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a\n\n\n\nb", "a\n\nb"},
		{"a\n\nb\n\nc", "a\n\nb\n\nc"},
		{"no newlines", "no newlines"},
		{"single\nnewline", "single\nnewline"},
		{"triple\n\n\nnewline", "triple\n\nnewline"},
	}
	for _, tt := range tests {
		got := collapseNewlines(tt.input)
		if got != tt.want {
			t.Errorf("collapseNewlines(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello   world", "hello world"},
		{"tabs\there", "tabs here"},
		{"newline\nhere", "newline here"},
		{"  leading", " leading"},
		{"no change", "no change"},
	}
	for _, tt := range tests {
		got := normalizeWhitespace(tt.input)
		if got != tt.want {
			t.Errorf("normalizeWhitespace(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
