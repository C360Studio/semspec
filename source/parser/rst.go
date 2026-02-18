package parser

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/c360studio/semspec/source"
)

// reStructuredText patterns
var (
	// Section underlines: ===, ---, ~~~, ^^^, etc.
	rstSectionUnderline = regexp.MustCompile(`^(={3,}|-{3,}|~{3,}|\^{3,}|\+{3,}|#{3,}|\*{3,}|_{3,})$`)

	// Code blocks: .. code-block:: or :: followed by indented content
	rstCodeBlockDirective = regexp.MustCompile(`^\.\. code(?:-block)?::`)
	rstCodeBlockShort     = regexp.MustCompile(`::$`)

	// Field list: :field-name: value
	rstFieldList = regexp.MustCompile(`^:([^:]+):(.*)$`)

	// Directive: .. directive-name::
	rstDirective = regexp.MustCompile(`^\.\. ([a-z-]+)::`)
)

// RSTParser parses reStructuredText documents.
type RSTParser struct{}

// NewRSTParser creates a new RST parser.
func NewRSTParser() *RSTParser {
	return &RSTParser{}
}

// Parse parses an RST document.
func (p *RSTParser) Parse(filename string, content []byte) (*source.Document, error) {
	str := string(content)

	// Convert RST to a markdown-like format for compatibility with the chunker
	converted := p.convertToMarkdownStyle(str)

	// Extract frontmatter-like metadata from field lists at the start
	frontmatter, body := p.extractFieldListMetadata(str)

	doc := &source.Document{
		ID:          GenerateDocID("rst", filename, content),
		Filename:    filepath.Base(filename),
		Content:     str,
		Body:        converted,
		Frontmatter: frontmatter,
	}

	if body != "" && frontmatter != nil {
		doc.Body = p.convertToMarkdownStyle(body)
	}

	return doc, nil
}

// CanParse returns true if this parser can handle the given MIME type.
func (p *RSTParser) CanParse(mimeType string) bool {
	switch mimeType {
	case "text/x-rst", "text/rst", "text/restructuredtext":
		return true
	default:
		return false
	}
}

// MimeType returns the primary MIME type for this parser.
func (p *RSTParser) MimeType() string {
	return "text/x-rst"
}

// convertToMarkdownStyle converts RST sections to markdown-style headings.
func (p *RSTParser) convertToMarkdownStyle(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	var inCodeBlock bool
	var codeBlockIndent int

	// Character to heading level mapping (RST section hierarchy)
	underlineToLevel := make(map[rune]int)
	currentLevel := 1

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Handle code blocks
		if rstCodeBlockDirective.MatchString(strings.TrimSpace(line)) {
			inCodeBlock = true
			codeBlockIndent = len(line) - len(strings.TrimLeft(line, " \t"))
			result = append(result, "```")
			continue
		}

		// Short-form code block (line ending with ::)
		if !inCodeBlock && rstCodeBlockShort.MatchString(strings.TrimSpace(line)) && i+1 < len(lines) {
			// Check if next non-empty line is indented
			for j := i + 1; j < len(lines); j++ {
				nextLine := lines[j]
				if strings.TrimSpace(nextLine) == "" {
					continue
				}
				if len(nextLine) > 0 && (nextLine[0] == ' ' || nextLine[0] == '\t') {
					// Remove :: from current line
					line = strings.TrimSuffix(strings.TrimSpace(line), ":")
					line = strings.TrimSuffix(line, ":")
					result = append(result, line)
					result = append(result, "```")
					inCodeBlock = true
					codeBlockIndent = len(nextLine) - len(strings.TrimLeft(nextLine, " \t"))
				}
				break
			}
			if inCodeBlock {
				continue
			}
		}

		// Detect end of code block (less indentation)
		if inCodeBlock {
			if strings.TrimSpace(line) != "" {
				currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
				if currentIndent < codeBlockIndent {
					result = append(result, "```")
					inCodeBlock = false
				} else {
					// Remove the indent for code block content
					if len(line) >= codeBlockIndent {
						line = line[codeBlockIndent:]
					}
				}
			}
			result = append(result, line)
			continue
		}

		// Check for section underline on next line
		if i+1 < len(lines) && rstSectionUnderline.MatchString(strings.TrimSpace(lines[i+1])) {
			underline := strings.TrimSpace(lines[i+1])
			if len(underline) >= len(strings.TrimSpace(line)) && strings.TrimSpace(line) != "" {
				// This is a section title
				titleChar := rune(underline[0])
				level, exists := underlineToLevel[titleChar]
				if !exists {
					level = currentLevel
					underlineToLevel[titleChar] = level
					currentLevel++
					if currentLevel > 6 {
						currentLevel = 6
					}
				}

				// Convert to markdown heading
				prefix := strings.Repeat("#", level)
				result = append(result, prefix+" "+strings.TrimSpace(line))
				i++ // Skip the underline
				continue
			}
		}

		// Convert field lists to bold labels (like frontmatter display)
		if match := rstFieldList.FindStringSubmatch(line); match != nil {
			result = append(result, "**"+match[1]+":**"+match[2])
			continue
		}

		// Skip directives we don't handle (but keep their content)
		if rstDirective.MatchString(strings.TrimSpace(line)) {
			// Just skip the directive line, content will be processed
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

// extractFieldListMetadata extracts field list metadata from the start of an RST document.
func (p *RSTParser) extractFieldListMetadata(content string) (map[string]any, string) {
	lines := strings.Split(content, "\n")
	metadata := make(map[string]any)
	bodyStart := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		match := rstFieldList.FindStringSubmatch(trimmed)
		if match != nil {
			key := strings.ToLower(strings.TrimSpace(match[1]))
			value := strings.TrimSpace(match[2])
			metadata[key] = value
			bodyStart = i + 1
		} else {
			// First non-field-list, non-empty line marks end of metadata
			break
		}
	}

	if len(metadata) == 0 {
		return nil, content
	}

	// Return the body without the field list
	body := strings.Join(lines[bodyStart:], "\n")
	return metadata, strings.TrimLeft(body, "\n")
}
