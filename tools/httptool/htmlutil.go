// Package httptool implements the http_request agent tool.
// It fetches URLs (SSRF-checked, DNS-pinned), converts HTML to clean text
// suitable for LLM consumption, and enqueues the URL with web-ingester so
// the content lands in the graph through the canonical ingestion pipeline.
package httptool

import (
	"io"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const defaultMaxTextSize = 20000 // chars

// skipTagSet contains tags whose content (including children) is suppressed entirely.
var skipTagSet = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Style:    true,
	atom.Nav:      true,
	atom.Footer:   true,
	atom.Header:   true,
	atom.Noscript: true,
}

// blockTagSet contains block-level tags that emit surrounding newlines.
var blockTagSet = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true,
	atom.Tr: true, atom.Blockquote: true, atom.Pre: true,
	atom.Section: true, atom.Article: true, atom.Li: true,
	atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true,
}

// htmlToText converts HTML to clean text suitable for LLM consumption.
// Returns the converted text and whether it was truncated.
func htmlToText(r io.Reader, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxTextSize
	}

	tokenizer := html.NewTokenizer(r)
	var sb strings.Builder
	truncated := false
	skipDepth := 0

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		if sb.Len() >= maxBytes {
			truncated = true
			break
		}
		done := processHTMLToken(tt, tokenizer, &sb, &skipDepth, maxBytes, &truncated)
		if done {
			break
		}
	}

	result := collapseNewlines(strings.TrimSpace(sb.String()))
	return result, truncated
}

// processHTMLToken handles a single HTML token, writing text to sb and updating
// skipDepth. Returns true when the budget is exhausted and the caller should stop.
func processHTMLToken(tt html.TokenType, tokenizer *html.Tokenizer, sb *strings.Builder, skipDepth *int, maxBytes int, truncated *bool) bool {
	switch tt {
	case html.StartTagToken:
		emitStartTag(tokenizer, sb, skipDepth)
	case html.EndTagToken:
		emitEndTag(tokenizer, sb, skipDepth)
	case html.TextToken:
		if *skipDepth > 0 {
			return false
		}
		text := strings.TrimSpace(string(tokenizer.Text()))
		if text == "" {
			return false
		}
		text = normalizeWhitespace(text)
		remaining := maxBytes - sb.Len()
		if remaining <= 0 {
			*truncated = true
			return true
		}
		if len(text)+1 > remaining {
			sb.WriteString(text[:remaining])
			*truncated = true
			return true
		}
		sb.WriteString(text)
		sb.WriteByte(' ')
	}
	return false
}

// emitStartTag writes the appropriate prefix for a start tag and tracks skip depth.
func emitStartTag(tokenizer *html.Tokenizer, sb *strings.Builder, skipDepth *int) {
	tn, _ := tokenizer.TagName()
	a := atom.Lookup(tn)

	if skipTagSet[a] {
		*skipDepth++
		return
	}
	if *skipDepth > 0 {
		return
	}

	// Headings render as markdown prefixes; other block tags get a leading newline.
	switch a {
	case atom.H1:
		sb.WriteString("\n# ")
	case atom.H2:
		sb.WriteString("\n## ")
	case atom.H3:
		sb.WriteString("\n### ")
	case atom.H4:
		sb.WriteString("\n#### ")
	case atom.H5:
		sb.WriteString("\n##### ")
	case atom.H6:
		sb.WriteString("\n###### ")
	case atom.Li:
		sb.WriteString("\n- ")
	case atom.Br:
		sb.WriteByte('\n')
	default:
		if blockTagSet[a] {
			sb.WriteByte('\n')
		}
	}
}

// emitEndTag writes the appropriate suffix for an end tag and tracks skip depth.
func emitEndTag(tokenizer *html.Tokenizer, sb *strings.Builder, skipDepth *int) {
	tn, _ := tokenizer.TagName()
	a := atom.Lookup(tn)

	if skipTagSet[a] && *skipDepth > 0 {
		*skipDepth--
		return
	}
	if *skipDepth > 0 {
		return
	}
	if blockTagSet[a] {
		sb.WriteByte('\n')
	}
}

// extractTitle extracts the <title> content from HTML.
func extractTitle(r io.Reader) string {
	tokenizer := html.NewTokenizer(r)
	inTitle := false
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			return ""
		}
		switch tt {
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			if atom.Lookup(tn) == atom.Title {
				inTitle = true
			}
		case html.TextToken:
			if inTitle {
				return strings.TrimSpace(string(tokenizer.Text()))
			}
		case html.EndTagToken:
			if inTitle {
				return ""
			}
		}
	}
}

// normalizeWhitespace collapses runs of whitespace characters to a single space.
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

// collapseNewlines limits consecutive newlines to at most two.
func collapseNewlines(s string) string {
	var sb strings.Builder
	newlineCount := 0
	for _, r := range s {
		if r == '\n' {
			newlineCount++
			if newlineCount <= 2 {
				sb.WriteByte('\n')
			}
		} else {
			newlineCount = 0
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
