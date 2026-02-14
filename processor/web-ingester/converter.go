package webingester

import (
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"golang.org/x/net/html"
)

// Pre-compiled regexes for better performance and to avoid ReDoS with runtime compilation
var (
	scriptRe       = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe        = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	excessiveLinesRe = regexp.MustCompile(`\n{4,}`)
)

// ConvertResult contains the result of HTML to markdown conversion.
type ConvertResult struct {
	Title    string
	Markdown string
}

// Converter converts HTML to markdown with documentation-focused extraction.
type Converter struct {
	converter *md.Converter
}

// NewConverter creates a new HTML to markdown converter.
func NewConverter() *Converter {
	converter := md.NewConverter("", true, nil)

	// Add plugins for better markdown output
	converter.Use(plugin.GitHubFlavored())

	return &Converter{
		converter: converter,
	}
}

// Convert transforms HTML content to markdown.
func (c *Converter) Convert(htmlContent []byte) (*ConvertResult, error) {
	// Parse HTML to extract title first
	title := extractHTMLTitle(htmlContent)

	// Extract main content area
	cleaned := extractMainContent(htmlContent)

	// Convert to markdown
	markdown, err := c.converter.ConvertString(cleaned)
	if err != nil {
		return nil, err
	}

	// Clean up the markdown
	markdown = cleanMarkdown(markdown)

	// If no title found in HTML, try to extract from markdown
	if title == "" {
		title = extractMarkdownTitle(markdown)
	}

	return &ConvertResult{
		Title:    title,
		Markdown: markdown,
	}, nil
}

// extractHTMLTitle extracts the title from HTML.
func extractHTMLTitle(content []byte) string {
	doc, err := html.Parse(strings.NewReader(string(content)))
	if err != nil {
		return ""
	}

	var title string
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if title != "" {
				return
			}
			extract(c)
		}
	}
	extract(doc)

	return title
}

// extractMainContent extracts the main content area from HTML.
func extractMainContent(content []byte) string {
	doc, err := html.Parse(strings.NewReader(string(content)))
	if err != nil {
		// Fall back to basic cleanup if parsing fails
		return basicHTMLCleanup(string(content))
	}

	// Try to find main content areas in priority order
	mainSelectors := []string{"main", "article", "[role=main]"}
	for _, selector := range mainSelectors {
		if node := findElement(doc, selector); node != nil {
			return renderNode(node)
		}
	}

	// If no main content found, remove unwanted elements and use body
	removeElements(doc, []string{
		"nav", "header", "footer", "aside", "script", "style", "noscript",
		"iframe", "object", "embed", "form", "input", "button",
	})
	removeByClass(doc, []string{
		"nav", "navbar", "navigation", "sidebar", "menu", "toc",
		"table-of-contents", "footer", "header", "ad", "advertisement",
		"social", "share", "comments", "related", "breadcrumb",
	})

	// Find and return body content
	if body := findElement(doc, "body"); body != nil {
		return renderNode(body)
	}

	return string(content)
}

// findElement finds the first element matching a simple selector.
func findElement(n *html.Node, selector string) *html.Node {
	var result *html.Node
	var find func(*html.Node)
	find = func(node *html.Node) {
		if result != nil {
			return
		}
		if node.Type == html.ElementNode {
			if matchesSelector(node, selector) {
				result = node
				return
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(n)
	return result
}

// matchesSelector checks if a node matches a simple selector.
func matchesSelector(n *html.Node, selector string) bool {
	if strings.HasPrefix(selector, "[") && strings.HasSuffix(selector, "]") {
		// Attribute selector like [role=main]
		attr := strings.TrimSuffix(strings.TrimPrefix(selector, "["), "]")
		parts := strings.SplitN(attr, "=", 2)
		if len(parts) == 2 {
			for _, a := range n.Attr {
				if a.Key == parts[0] && a.Val == parts[1] {
					return true
				}
			}
		}
		return false
	}
	// Tag name selector
	return n.Data == selector
}

// removeElements removes all elements with the given tag names.
func removeElements(n *html.Node, tags []string) {
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}

	var toRemove []*html.Node
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node.Type == html.ElementNode && tagSet[node.Data] {
			toRemove = append(toRemove, node)
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(n)

	for _, node := range toRemove {
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}
}

// removeByClass removes elements that have any of the given class names.
func removeByClass(n *html.Node, classes []string) {
	classSet := make(map[string]bool)
	for _, class := range classes {
		classSet[strings.ToLower(class)] = true
	}

	var toRemove []*html.Node
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, a := range node.Attr {
				if a.Key == "class" {
					nodeClasses := strings.Fields(strings.ToLower(a.Val))
					for _, c := range nodeClasses {
						if classSet[c] {
							toRemove = append(toRemove, node)
							return
						}
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(n)

	for _, node := range toRemove {
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}
}

// renderNode renders a node and its children back to HTML string.
func renderNode(n *html.Node) string {
	var sb strings.Builder
	html.Render(&sb, n)
	return sb.String()
}

// basicHTMLCleanup provides basic HTML cleanup when parsing fails.
func basicHTMLCleanup(content string) string {
	// Remove script and style tags with content using pre-compiled regexes
	content = scriptRe.ReplaceAllString(content, "")
	content = styleRe.ReplaceAllString(content, "")
	return content
}

// cleanMarkdown cleans up converted markdown.
func cleanMarkdown(content string) string {
	// Remove excessive blank lines (more than 2) using pre-compiled regex
	content = excessiveLinesRe.ReplaceAllString(content, "\n\n\n")

	// Remove leading/trailing whitespace from lines
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	content = strings.Join(lines, "\n")

	// Trim overall content
	content = strings.TrimSpace(content)

	return content
}

// extractMarkdownTitle extracts the first H1 heading from markdown.
func extractMarkdownTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return ""
}
