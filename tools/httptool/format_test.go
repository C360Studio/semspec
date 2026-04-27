package httptool

import (
	"strings"
	"testing"
)

const sampleHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Sample Page</title>
  <meta name="description" content="A page about widgets and gadgets.">
</head>
<body>
  <nav><a href="/home">Home</a></nav>
  <article>
    <h1>Widgets and Gadgets</h1>
    <p>Widgets are small things. Gadgets are clever.</p>
    <h2>Installing widgets</h2>
    <p>Run <code>npm install widget</code> and you're done.</p>
    <h2>Using widgets</h2>
    <p>Call <a href="/api/widgets">the widget API</a>.</p>
    <h3>Advanced</h3>
    <p>For advanced use cases see <a href="https://example.com/advanced">here</a>.</p>
  </article>
  <footer><a href="javascript:void(0)">Click me</a></footer>
</body>
</html>`

func TestExtractHeadings(t *testing.T) {
	got := extractHeadings([]byte(sampleHTML))
	if len(got) != 4 {
		t.Fatalf("want 4 headings, got %d: %+v", len(got), got)
	}
	if got[0].level != 1 || got[0].text != "Widgets and Gadgets" {
		t.Errorf("h1 mismatch: %+v", got[0])
	}
	if got[3].level != 3 || got[3].text != "Advanced" {
		t.Errorf("h3 mismatch: %+v", got[3])
	}
}

func TestExtractLinks_DropsJunkSchemes(t *testing.T) {
	links := extractLinks([]byte(sampleHTML), "https://example.com/docs")
	for _, l := range links {
		if strings.HasPrefix(strings.ToLower(l.href), "javascript:") {
			t.Errorf("javascript: href should be filtered, got %v", l)
		}
		if strings.HasPrefix(l.href, "#") {
			t.Errorf("fragment-only href should be filtered, got %v", l)
		}
	}
	if len(links) == 0 {
		t.Fatal("expected at least one link, got none")
	}
}

func TestExtractLinks_ResolvesRelative(t *testing.T) {
	links := extractLinks([]byte(sampleHTML), "https://example.com/docs/widgets")
	var foundAPILink bool
	for _, l := range links {
		if strings.Contains(l.href, "/api/widgets") {
			foundAPILink = true
			if !strings.HasPrefix(l.href, "https://example.com/") {
				t.Errorf("relative href not resolved: %q", l.href)
			}
		}
	}
	if !foundAPILink {
		t.Error("expected /api/widgets link to appear")
	}
}

func TestExtractLinks_Dedupe(t *testing.T) {
	dup := []byte(`<html><body>
		<a href="/a">A</a><a href="/a">A</a>
		<a href="/b">B</a>
	</body></html>`)
	links := extractLinks(dup, "https://example.com")
	if len(links) != 2 {
		t.Errorf("expected 2 deduped links, got %d: %+v", len(links), links)
	}
}

func TestParseFormat(t *testing.T) {
	cases := map[string]Format{
		"":          FormatSummary,
		"summary":   FormatSummary,
		"markdown":  FormatMarkdown,
		"links":     FormatLinks,
		"headings":  FormatHeadings,
		"raw":       FormatRaw,
		"  RAW  ":   FormatRaw,
		"MarkDown":  FormatMarkdown,
		"junk_word": FormatSummary, // unknown → default
	}
	for input, want := range cases {
		if got := parseFormat(input); got != want {
			t.Errorf("parseFormat(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestRenderHeadings(t *testing.T) {
	out := renderHeadings([]heading{
		{level: 1, text: "Top"},
		{level: 2, text: "Sub"},
		{level: 3, text: "Subsub"},
	}, 0)
	if !strings.Contains(out, "H1: Top") {
		t.Errorf("missing H1 line: %q", out)
	}
	if !strings.Contains(out, "  H2: Sub") {
		t.Errorf("missing indented H2 line: %q", out)
	}
	if !strings.Contains(out, "    H3: Subsub") {
		t.Errorf("missing double-indented H3 line: %q", out)
	}
}

func TestRenderLinks_Empty(t *testing.T) {
	if got := renderLinks(nil, 0); got != "(no links)" {
		t.Errorf("empty links = %q, want (no links)", got)
	}
}

func TestFormatResponse_RawReturnsBody(t *testing.T) {
	body := []byte("<html>hello</html>")
	got := formatResponse(FormatRaw, nil, body, "", 0)
	if got != string(body) {
		t.Errorf("raw should return body verbatim, got %q", got)
	}
}

func TestFormatResponse_HeadingsFromBody(t *testing.T) {
	got := formatResponse(FormatHeadings, nil, []byte(sampleHTML), "", 0)
	if !strings.Contains(got, "Widgets and Gadgets") {
		t.Errorf("headings format missing H1 text: %q", got)
	}
}

func TestFormatResponse_SummaryHasOutlineAndExcerpt(t *testing.T) {
	conv := &convertResult{
		Title:    "Widgets and Gadgets",
		Markdown: "# Widgets and Gadgets\n\nWidgets are small things.\n",
		Excerpt:  "A page about widgets and gadgets.",
	}
	got := formatResponse(FormatSummary, conv, []byte(sampleHTML), "https://example.com/docs", 0)

	wants := []string{
		"# Widgets and Gadgets",
		"URL: https://example.com/docs",
		"Excerpt: A page about widgets and gadgets.",
		"## Outline (",
		"## Links (",
		"## Main content excerpt",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("summary missing %q\n----\n%s\n----", w, got)
		}
	}
}

func TestTruncateChars(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := truncateChars(long, 20)
	if !strings.HasPrefix(got, strings.Repeat("a", 20)) {
		t.Errorf("truncate didn't take first 20 chars: %q", got)
	}
	if !strings.Contains(got, "[truncated]") {
		t.Errorf("truncate missing marker: %q", got)
	}
	short := "hi"
	if got := truncateChars(short, 20); got != short {
		t.Errorf("truncate altered short string: %q", got)
	}
}
