package httptool

import (
	"bytes"
	"fmt"
	nurl "net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Format identifies which view the agent wants of an HTTP response.
//
// summary  - title + excerpt + outline + top links + main excerpt (default)
// markdown - cleaned full markdown via Readability, capped at maxChars
// links    - deduplicated [anchor text, href] pairs only
// headings - flat outline of h1..h6 only
// raw      - original body, capped at maxChars
type Format string

// Format constants identify the supported response shapes for http_request.
// See the Format type comment for what each shape contains.
const (
	FormatSummary  Format = "summary"
	FormatMarkdown Format = "markdown"
	FormatLinks    Format = "links"
	FormatHeadings Format = "headings"
	FormatRaw      Format = "raw"
)

// parseFormat normalises and validates a format argument. Unknown values
// fall back to FormatSummary so a typo doesn't surprise the caller with a
// hard error.
func parseFormat(raw string) Format {
	switch Format(strings.ToLower(strings.TrimSpace(raw))) {
	case FormatMarkdown:
		return FormatMarkdown
	case FormatLinks:
		return FormatLinks
	case FormatHeadings:
		return FormatHeadings
	case FormatRaw:
		return FormatRaw
	case FormatSummary, "":
		return FormatSummary
	default:
		return FormatSummary
	}
}

// summaryExcerptChars caps the main-content snippet inside a summary view.
// Tuned to keep total summary output well under 2K chars on a typical page.
const summaryExcerptChars = 800

// summaryLinkCount caps how many links are listed in a summary view.
const summaryLinkCount = 10

// link is one anchor extracted from HTML.
type link struct {
	text string
	href string
}

// heading is one h1..h6 element extracted from HTML.
type heading struct {
	level int
	text  string
}

// formatResponse renders the chosen view from the converter output and the
// original body. body is the raw HTTP body (used for link/heading
// extraction); conv is the Converter's output (used for the markdown / main
// content surface). maxChars is the per-format cap; 0 means "use default".
func formatResponse(format Format, conv *convertResult, body []byte, pageURL string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = defaultMaxChars
	}

	switch format {
	case FormatRaw:
		return truncateChars(string(body), maxChars)
	case FormatLinks:
		return renderLinks(extractLinks(body, pageURL), maxChars)
	case FormatHeadings:
		return renderHeadings(extractHeadings(body), maxChars)
	case FormatMarkdown:
		return renderMarkdown(conv, maxChars)
	case FormatSummary:
		fallthrough
	default:
		return renderSummary(conv, body, pageURL, maxChars)
	}
}

// renderMarkdown returns the title-prefixed full markdown, capped.
func renderMarkdown(conv *convertResult, maxChars int) string {
	if conv == nil || conv.Markdown == "" {
		return ""
	}
	if conv.Title != "" {
		return truncateChars(fmt.Sprintf("# %s\n\n%s", conv.Title, conv.Markdown), maxChars)
	}
	return truncateChars(conv.Markdown, maxChars)
}

// renderSummary returns a compact view: title, excerpt, outline, top links,
// main-content excerpt. Designed to answer "is this page worth reading?"
// without spending a 20K-char budget on the answer.
func renderSummary(conv *convertResult, body []byte, pageURL string, maxChars int) string {
	var b strings.Builder

	if conv != nil && conv.Title != "" {
		fmt.Fprintf(&b, "# %s\n\n", conv.Title)
	}
	if pageURL != "" {
		fmt.Fprintf(&b, "URL: %s\n\n", pageURL)
	}
	if conv != nil && conv.SiteName != "" {
		fmt.Fprintf(&b, "Site: %s\n\n", conv.SiteName)
	}
	if conv != nil && conv.Excerpt != "" {
		fmt.Fprintf(&b, "Excerpt: %s\n\n", conv.Excerpt)
	}

	headings := extractHeadings(body)
	if len(headings) > 0 {
		fmt.Fprintf(&b, "## Outline (%d heading", len(headings))
		if len(headings) != 1 {
			b.WriteByte('s')
		}
		b.WriteString(")\n")
		for _, h := range headings {
			indent := strings.Repeat("  ", maxInt(0, h.level-1))
			fmt.Fprintf(&b, "%s- %s\n", indent, h.text)
		}
		b.WriteByte('\n')
	}

	links := extractLinks(body, pageURL)
	if len(links) > 0 {
		fmt.Fprintf(&b, "## Links (%d total", len(links))
		if len(links) > summaryLinkCount {
			fmt.Fprintf(&b, ", showing %d", summaryLinkCount)
		}
		b.WriteString(")\n")
		shown := links
		if len(shown) > summaryLinkCount {
			shown = shown[:summaryLinkCount]
		}
		for _, l := range shown {
			text := l.text
			if text == "" {
				text = l.href
			}
			fmt.Fprintf(&b, "- %s → %s\n", text, l.href)
		}
		if len(links) > summaryLinkCount {
			b.WriteString("- ... use format=links for the full list\n")
		}
		b.WriteByte('\n')
	}

	if conv != nil && conv.Markdown != "" {
		excerpt := truncateChars(conv.Markdown, summaryExcerptChars)
		fmt.Fprintf(&b, "## Main content excerpt\n%s\n", excerpt)
		if len(conv.Markdown) > summaryExcerptChars {
			b.WriteString("\n[truncated; use format=markdown for full text]\n")
		}
	}

	return truncateChars(strings.TrimRight(b.String(), "\n"), maxChars)
}

// renderLinks formats extracted links one per line.
func renderLinks(links []link, maxChars int) string {
	if len(links) == 0 {
		return "(no links)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Links (%d):\n", len(links))
	for _, l := range links {
		text := l.text
		if text == "" {
			text = "(no text)"
		}
		fmt.Fprintf(&b, "- %s → %s\n", text, l.href)
	}
	return truncateChars(strings.TrimRight(b.String(), "\n"), maxChars)
}

// renderHeadings formats the heading outline indented by depth.
func renderHeadings(headings []heading, maxChars int) string {
	if len(headings) == 0 {
		return "(no headings)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Headings (%d):\n", len(headings))
	for _, h := range headings {
		indent := strings.Repeat("  ", maxInt(0, h.level-1))
		fmt.Fprintf(&b, "%sH%d: %s\n", indent, h.level, h.text)
	}
	return truncateChars(strings.TrimRight(b.String(), "\n"), maxChars)
}

// extractLinks pulls every <a href="..."> from the body and returns deduped
// (anchor-text, href) pairs. Junk schemes (mailto, javascript, fragment-only)
// are dropped. Relative hrefs are resolved against pageURL when possible.
func extractLinks(body []byte, pageURL string) []link {
	var base *nurl.URL
	if pageURL != "" {
		base, _ = nurl.Parse(pageURL)
	}

	state := &linkExtractor{
		out:       []link{},
		seen:      make(map[string]bool),
		base:      base,
		tokenizer: html.NewTokenizer(bytes.NewReader(body)),
	}
	state.run()
	return state.out
}

// linkExtractor holds the in-flight state of extractLinks. Pulling it out
// keeps the main function under the revive function-length cap and lets
// each token-type branch be its own focused method.
type linkExtractor struct {
	out       []link
	seen      map[string]bool
	base      *nurl.URL
	tokenizer *html.Tokenizer
	curHref   string
	curText   strings.Builder
	inAnchor  int // depth — handles nested anchors gracefully
	skipDepth int // skip text inside script/style/noscript
}

func (s *linkExtractor) run() {
	for {
		tt := s.tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			s.handleStart()
		case html.EndTagToken:
			s.handleEnd()
		case html.TextToken:
			if s.skipDepth == 0 && s.inAnchor > 0 {
				s.curText.Write(s.tokenizer.Text())
			}
		}
	}
	if s.inAnchor > 0 {
		s.flush()
	}
}

func (s *linkExtractor) handleStart() {
	tn, hasAttr := s.tokenizer.TagName()
	a := atom.Lookup(tn)
	if a == atom.Script || a == atom.Style || a == atom.Noscript {
		s.skipDepth++
		return
	}
	if a != atom.A {
		return
	}
	href := s.readHrefAttr(hasAttr)
	href = resolveAndFilterHref(href, s.base)
	if href == "" {
		return
	}
	if s.inAnchor > 0 {
		s.flush()
	}
	s.curHref = href
	s.inAnchor++
}

func (s *linkExtractor) handleEnd() {
	tn, _ := s.tokenizer.TagName()
	a := atom.Lookup(tn)
	if a == atom.Script || a == atom.Style || a == atom.Noscript {
		if s.skipDepth > 0 {
			s.skipDepth--
		}
		return
	}
	if a == atom.A && s.inAnchor > 0 {
		s.inAnchor--
		s.flush()
	}
}

func (s *linkExtractor) readHrefAttr(hasAttr bool) string {
	if !hasAttr {
		return ""
	}
	for {
		k, v, more := s.tokenizer.TagAttr()
		if string(k) == "href" {
			return strings.TrimSpace(string(v))
		}
		if !more {
			return ""
		}
	}
}

func (s *linkExtractor) flush() {
	if s.curHref == "" {
		s.curText.Reset()
		return
	}
	text := strings.TrimSpace(normalizeWhitespace(s.curText.String()))
	key := text + "|" + s.curHref
	if !s.seen[key] {
		s.seen[key] = true
		s.out = append(s.out, link{text: text, href: s.curHref})
	}
	s.curHref = ""
	s.curText.Reset()
}

// resolveAndFilterHref returns "" for hrefs we don't want to expose to
// agents (empty, fragment-only, mailto, javascript), otherwise the resolved
// absolute URL.
func resolveAndFilterHref(href string, base *nurl.URL) string {
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	lower := strings.ToLower(href)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") {
		return ""
	}
	parsed, err := nurl.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil && !parsed.IsAbs() {
		return base.ResolveReference(parsed).String()
	}
	return parsed.String()
}

// extractHeadings walks the body and returns each h1..h6 with its level.
func extractHeadings(body []byte) []heading {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var out []heading
	var (
		curLevel  int
		curText   strings.Builder
		skipDepth int
	)

	flush := func() {
		if curLevel == 0 {
			curText.Reset()
			return
		}
		text := strings.TrimSpace(normalizeWhitespace(curText.String()))
		if text != "" {
			out = append(out, heading{level: curLevel, text: text})
		}
		curLevel = 0
		curText.Reset()
	}

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			a := atom.Lookup(tn)
			if a == atom.Script || a == atom.Style || a == atom.Noscript {
				skipDepth++
				continue
			}
			if level := headingLevelFromAtom(a); level > 0 {
				if curLevel != 0 {
					flush()
				}
				curLevel = level
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			a := atom.Lookup(tn)
			if a == atom.Script || a == atom.Style || a == atom.Noscript {
				if skipDepth > 0 {
					skipDepth--
				}
				continue
			}
			if level := headingLevelFromAtom(a); level > 0 && level == curLevel {
				flush()
			}
		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			if curLevel > 0 {
				curText.Write(tokenizer.Text())
			}
		}
	}
	if curLevel != 0 {
		flush()
	}
	return out
}

// headingLevelFromAtom returns 1..6 for h1..h6 atoms, 0 otherwise.
func headingLevelFromAtom(a atom.Atom) int {
	switch a {
	case atom.H1:
		return 1
	case atom.H2:
		return 2
	case atom.H3:
		return 3
	case atom.H4:
		return 4
	case atom.H5:
		return 5
	case atom.H6:
		return 6
	}
	return 0
}

// truncateChars trims s to maxChars, appending an ellipsis when trimmed.
func truncateChars(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "\n[truncated]"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// normalizeWhitespace collapses runs of whitespace characters to a single
// space. Used when extracting heading and link text from raw HTML, where
// browsers would normally collapse the whitespace at render time.
func normalizeWhitespace(s string) string {
	var sb strings.Builder
	prevSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			if !prevSpace {
				sb.WriteByte(' ')
				prevSpace = true
			}
		default:
			sb.WriteRune(r)
			prevSpace = false
		}
	}
	return sb.String()
}
