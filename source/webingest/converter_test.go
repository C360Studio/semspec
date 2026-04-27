package webingest

import (
	"strings"
	"testing"
)

func TestConverter_ExtractsArticleAndTitle(t *testing.T) {
	conv := NewConverter()
	got, err := conv.Convert([]byte(sampleHTML), "https://example.com/sample")
	if err != nil {
		t.Fatalf("Convert err: %v", err)
	}
	if got.Title == "" {
		t.Error("expected non-empty title")
	}
	if !strings.Contains(got.Markdown, "Widgets") {
		t.Errorf("markdown missing main content: %q", got.Markdown)
	}
	// Boilerplate should be excluded by Readability.
	if strings.Contains(got.Markdown, "javascript:void") {
		t.Errorf("converter leaked footer chrome: %q", got.Markdown)
	}
}

func TestConverter_FallbackOnEmptyReadability(t *testing.T) {
	// A document Readability typically can't extract from: too short, no
	// article markers. The fallback path should still return *something*.
	bare := []byte(`<html><head><title>Tiny</title></head><body>
		<p>This is a very small page with not much content at all.</p>
	</body></html>`)
	conv := NewConverter()
	got, err := conv.Convert(bare, "https://example.com")
	if err != nil {
		t.Fatalf("Convert err: %v", err)
	}
	if got.Markdown == "" && got.Title == "" {
		t.Errorf("fallback produced empty result")
	}
}

func TestExtractHTMLTitle(t *testing.T) {
	html := []byte(`<html><head><title>  My Title  </title></head><body></body></html>`)
	got := extractHTMLTitle(html)
	if got != "My Title" {
		t.Errorf("extractHTMLTitle = %q, want %q", got, "My Title")
	}
}

func TestExtractMarkdownTitle(t *testing.T) {
	md := "Some intro\n# The Real Title\n## Subhead\n"
	if got := extractMarkdownTitle(md); got != "The Real Title" {
		t.Errorf("extractMarkdownTitle = %q", got)
	}
	if got := extractMarkdownTitle("no headings here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestCleanMarkdown(t *testing.T) {
	in := "line1   \n\n\n\n\nline2\n\nline3 \t"
	got := cleanMarkdown(in)
	if strings.Contains(got, "\n\n\n\n") {
		t.Errorf("excessive newlines not collapsed: %q", got)
	}
	if strings.Contains(got, "   \n") || strings.Contains(got, " \t") {
		t.Errorf("trailing whitespace not trimmed: %q", got)
	}
}
