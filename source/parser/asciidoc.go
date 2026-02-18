package parser

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/c360studio/semspec/source"
)

// AsciiDoc patterns
var (
	// Section titles: = Title, == Subtitle, etc.
	adocSectionTitle = regexp.MustCompile(`^(={1,6})\s+(.+)$`)

	// Attribute definitions: :name: value
	adocAttribute = regexp.MustCompile(`^:([^:]+):\s*(.*)$`)

	// Source code block: [source,lang] or ---- block
	adocSourceBlock = regexp.MustCompile(`^\[source(?:,\s*([^\]]+))?\]`)
	adocListingBlock = regexp.MustCompile(`^----$`)

	// Passthrough block: ++++
	adocPassthroughBlock = regexp.MustCompile(`^\+\+\+\+$`)

	// Literal block: ....
	adocLiteralBlock = regexp.MustCompile(`^\.\.\.\.+$`)

	// Sidebar block: ****
	adocSidebarBlock = regexp.MustCompile(`^\*\*\*\*+$`)

	// Example block: ====
	adocExampleBlock = regexp.MustCompile(`^====+$`)

	// Admonition blocks: NOTE:, TIP:, WARNING:, etc.
	adocAdmonition = regexp.MustCompile(`^(NOTE|TIP|IMPORTANT|WARNING|CAUTION):\s*(.*)$`)

	// Block macro: name::target[attributes]
	adocBlockMacro = regexp.MustCompile(`^([a-z]+)::([^\[]*)\[([^\]]*)\]$`)
)

// AsciiDocParser parses AsciiDoc documents.
type AsciiDocParser struct{}

// NewAsciiDocParser creates a new AsciiDoc parser.
func NewAsciiDocParser() *AsciiDocParser {
	return &AsciiDocParser{}
}

// Parse parses an AsciiDoc document.
func (p *AsciiDocParser) Parse(filename string, content []byte) (*source.Document, error) {
	str := string(content)

	// Extract document attributes (similar to frontmatter)
	attributes, body := p.extractAttributes(str)

	// Convert to markdown-style for compatibility
	converted := p.convertToMarkdownStyle(body)

	doc := &source.Document{
		ID:          GenerateDocID("asciidoc", filename, content),
		Filename:    filepath.Base(filename),
		Content:     str,
		Body:        converted,
		Frontmatter: attributes,
	}

	return doc, nil
}

// CanParse returns true if this parser can handle the given MIME type.
func (p *AsciiDocParser) CanParse(mimeType string) bool {
	switch mimeType {
	case "text/asciidoc", "text/x-asciidoc":
		return true
	default:
		return false
	}
}

// MimeType returns the primary MIME type for this parser.
func (p *AsciiDocParser) MimeType() string {
	return "text/asciidoc"
}

// extractAttributes extracts document attributes from the header.
func (p *AsciiDocParser) extractAttributes(content string) (map[string]any, string) {
	lines := strings.Split(content, "\n")
	attributes := make(map[string]any)
	bodyStart := 0

	// Skip any leading title (= Document Title)
	inHeader := true

	for i, line := range lines {
		// Check for document title (must be first non-blank, non-attribute line)
		if inHeader && strings.HasPrefix(line, "= ") && !strings.HasPrefix(line, "==") {
			// Document title - extract it
			attributes["title"] = strings.TrimPrefix(line, "= ")
			bodyStart = i + 1
			continue
		}

		// Check for attribute
		if match := adocAttribute.FindStringSubmatch(line); match != nil {
			key := strings.ToLower(strings.TrimSpace(match[1]))
			value := strings.TrimSpace(match[2])
			if value == "" {
				// Unset attribute or boolean flag
				attributes[key] = true
			} else {
				attributes[key] = value
			}
			bodyStart = i + 1
			continue
		}

		// Empty line in header is allowed
		if strings.TrimSpace(line) == "" && inHeader {
			continue
		}

		// Any other content ends the header
		inHeader = false
		if bodyStart == 0 {
			bodyStart = i
		}
	}

	if len(attributes) == 0 {
		return nil, content
	}

	// Return the body without the header
	body := strings.Join(lines[bodyStart:], "\n")
	return attributes, strings.TrimLeft(body, "\n")
}

// convertToMarkdownStyle converts AsciiDoc to markdown-style format.
func (p *AsciiDocParser) convertToMarkdownStyle(content string) string {
	lines := strings.Split(content, "\n")
	var result []string

	var inCodeBlock bool
	var codeBlockDelim string

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Handle code block toggle
		if adocListingBlock.MatchString(trimmed) {
			if inCodeBlock && codeBlockDelim == "----" {
				result = append(result, "```")
				inCodeBlock = false
				codeBlockDelim = ""
			} else if !inCodeBlock {
				result = append(result, "```")
				inCodeBlock = true
				codeBlockDelim = "----"
			}
			continue
		}

		// Handle literal block toggle
		if adocLiteralBlock.MatchString(trimmed) {
			if inCodeBlock && codeBlockDelim == "...." {
				result = append(result, "```")
				inCodeBlock = false
				codeBlockDelim = ""
			} else if !inCodeBlock {
				result = append(result, "```")
				inCodeBlock = true
				codeBlockDelim = "...."
			}
			continue
		}

		// Handle passthrough block toggle (keep as-is)
		if adocPassthroughBlock.MatchString(trimmed) {
			if inCodeBlock && codeBlockDelim == "++++" {
				inCodeBlock = false
				codeBlockDelim = ""
			} else if !inCodeBlock {
				inCodeBlock = true
				codeBlockDelim = "++++"
			}
			continue
		}

		// Skip sidebar, example block delimiters (but keep content)
		if adocSidebarBlock.MatchString(trimmed) || adocExampleBlock.MatchString(trimmed) {
			continue
		}

		// If inside code block, pass through
		if inCodeBlock {
			result = append(result, line)
			continue
		}

		// Source block annotation - next line or block is code
		if match := adocSourceBlock.FindStringSubmatch(trimmed); match != nil {
			lang := match[1]
			if lang != "" {
				result = append(result, "```"+lang)
			} else {
				result = append(result, "```")
			}
			// The next ---- will close it, or we need to handle single-line
			inCodeBlock = true
			codeBlockDelim = "source"
			continue
		}

		// Handle section titles
		if match := adocSectionTitle.FindStringSubmatch(trimmed); match != nil {
			level := len(match[1])
			prefix := strings.Repeat("#", level)
			result = append(result, prefix+" "+match[2])
			continue
		}

		// Handle admonitions
		if match := adocAdmonition.FindStringSubmatch(trimmed); match != nil {
			admonType := match[1]
			text := match[2]
			result = append(result, "**"+admonType+":** "+text)
			continue
		}

		// Handle block macros (image, include, etc.)
		if match := adocBlockMacro.FindStringSubmatch(trimmed); match != nil {
			macroType := match[1]
			target := match[2]
			attrs := match[3]
			switch macroType {
			case "image":
				// Convert to markdown image
				alt := attrs
				if alt == "" {
					alt = filepath.Base(target)
				}
				result = append(result, "!["+alt+"]("+target+")")
			case "include":
				result = append(result, "_[Include: "+target+"]_")
			default:
				result = append(result, "_["+macroType+": "+target+"]_")
			}
			continue
		}

		result = append(result, line)
	}

	// Close any open code block
	if inCodeBlock {
		result = append(result, "```")
	}

	return strings.Join(result, "\n")
}
