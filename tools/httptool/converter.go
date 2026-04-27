package httptool

import (
	"bytes"
	"fmt"
	nurl "net/url"
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/go-shiori/go-readability"
	"golang.org/x/net/html"
)

// Pre-compiled regexes used by the markdown post-processing step.
var (
	convScriptRe         = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	convStyleRe          = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	convExcessiveLinesRe = regexp.MustCompile(`\n{4,}`)
)

// ConvertResult is the output of HTML→markdown conversion.
type convertResult struct {
	// Title is the page title (from <title>, OpenGraph, or first H1).
	Title string
	// Markdown is the cleaned, main-content markdown.
	Markdown string
	// Excerpt is a short readability-derived excerpt suitable for summary
	// views. Empty when readability could not extract one.
	Excerpt string
	// SiteName is the OpenGraph site name when present.
	SiteName string
}

// Converter turns HTML bodies into clean markdown via Readability with a
// permissive tag-stripping fallback. Extracted from processor/web-ingester
// during WS-25; the fallback path mirrors the previous main-content logic.
type converter struct {
	md *md.Converter
}

// NewConverter constructs a converter with GitHub-flavored markdown rules.
func newConverter() *converter {
	c := md.NewConverter("", true, nil)
	c.Use(plugin.GitHubFlavored())
	return &converter{md: c}
}

// Convert turns an HTML body into markdown. pageURL is used by Readability
// for relative-link resolution and metadata heuristics; an empty string is
// fine (Readability falls back to its own logic).
//
// On success the result always has Markdown and Title populated. Excerpt
// and SiteName may be empty when Readability cannot extract them.
func (c *converter) Convert(body []byte, pageURL string) (*convertResult, error) {
	var parsedURL *nurl.URL
	if pageURL != "" {
		parsedURL, _ = nurl.Parse(pageURL)
	}

	parser := readability.NewParser()
	article, err := parser.Parse(bytes.NewReader(body), parsedURL)
	if err == nil && strings.TrimSpace(article.Content) != "" {
		markdown, mdErr := c.md.ConvertString(article.Content)
		if mdErr == nil {
			markdown = cleanMarkdown(markdown)
			title := article.Title
			if title == "" {
				title = extractMarkdownTitle(markdown)
			}
			return &convertResult{
				Title:    title,
				Markdown: markdown,
				Excerpt:  strings.TrimSpace(article.Excerpt),
				SiteName: strings.TrimSpace(article.SiteName),
			}, nil
		}
	}

	// Readability failed or returned empty content (login walls, JS-rendered
	// pages, single-section content the heuristic doesn't recognise as an
	// article). Fall back to the permissive whole-body extractor so the agent
	// gets *something* back rather than a hard error.
	return c.convertFallback(body)
}

// convertFallback strips chrome elements with simple selectors and converts
// what's left to markdown. Used when Readability returns empty content.
func (c *converter) convertFallback(body []byte) (*convertResult, error) {
	title := extractHTMLTitle(body)
	cleaned := stripChrome(body)
	markdown, err := c.md.ConvertString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("html-to-markdown fallback: %w", err)
	}
	markdown = cleanMarkdown(markdown)
	if title == "" {
		title = extractMarkdownTitle(markdown)
	}
	return &convertResult{Title: title, Markdown: markdown}, nil
}

// extractHTMLTitle pulls the <title> out of an HTML byte stream.
func extractHTMLTitle(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}

	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return title
}

// stripChrome removes scripts, styles, and structural chrome, then re-renders
// the document body. Crude compared to Readability but good enough as a last
// resort.
func stripChrome(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		raw := string(body)
		raw = convScriptRe.ReplaceAllString(raw, "")
		raw = convStyleRe.ReplaceAllString(raw, "")
		return raw
	}

	skipTags := map[string]bool{
		"nav": true, "header": true, "footer": true, "aside": true,
		"script": true, "style": true, "noscript": true,
		"iframe": true, "object": true, "embed": true,
		"form": true, "input": true, "button": true,
	}
	skipClasses := map[string]bool{
		"nav": true, "navbar": true, "navigation": true,
		"sidebar": true, "menu": true, "toc": true,
		"table-of-contents": true, "footer": true, "header": true,
		"ad": true, "advertisement": true,
		"social": true, "share": true, "comments": true, "related": true,
		"breadcrumb": true,
	}

	removeMatching(doc, skipTags, skipClasses)

	if bodyNode := findElementByTag(doc, "body"); bodyNode != nil {
		var sb strings.Builder
		_ = html.Render(&sb, bodyNode)
		return sb.String()
	}
	return string(body)
}

// removeMatching detaches every element whose tag is in skipTags or whose
// class list intersects skipClasses.
func removeMatching(root *html.Node, skipTags, skipClasses map[string]bool) {
	var doomed []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if skipTags[n.Data] {
				doomed = append(doomed, n)
				return
			}
			for _, attr := range n.Attr {
				if attr.Key != "class" {
					continue
				}
				for cls := range strings.FieldsSeq(strings.ToLower(attr.Val)) {
					if skipClasses[cls] {
						doomed = append(doomed, n)
						return
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)

	for _, n := range doomed {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
}

// findElementByTag returns the first element node with the given tag name.
func findElementByTag(root *html.Node, tag string) *html.Node {
	if root.Type == html.ElementNode && root.Data == tag {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findElementByTag(child, tag); found != nil {
			return found
		}
	}
	return nil
}

// cleanMarkdown collapses excessive blank lines and trims trailing whitespace.
func cleanMarkdown(content string) string {
	content = convExcessiveLinesRe.ReplaceAllString(content, "\n\n\n")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// extractMarkdownTitle returns the first H1 heading in markdown, or "".
func extractMarkdownTitle(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return ""
}
