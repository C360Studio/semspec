// Package parser provides document parsing functionality.
package parser

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/c360studio/semspec/source"
	"gopkg.in/yaml.v3"
)

// MarkdownParser parses markdown documents with optional YAML frontmatter.
type MarkdownParser struct{}

// NewMarkdownParser creates a new markdown parser.
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{}
}

// Parse parses a markdown document, extracting frontmatter and body.
func (p *MarkdownParser) Parse(filename string, content []byte) (*source.Document, error) {
	doc := &source.Document{
		ID:       generateID(filename, content),
		Filename: filepath.Base(filename),
		Content:  string(content),
	}

	// Check for YAML frontmatter
	str := string(content)
	if strings.HasPrefix(str, "---\n") || strings.HasPrefix(str, "---\r\n") {
		frontmatter, body, err := extractFrontmatter(str)
		if err != nil {
			// If frontmatter parsing fails, treat entire content as body
			doc.Body = str
		} else {
			doc.Frontmatter = frontmatter
			doc.Body = body
		}
	} else {
		doc.Body = str
	}

	return doc, nil
}

// CanParse returns true if this parser can handle the given MIME type.
func (p *MarkdownParser) CanParse(mimeType string) bool {
	switch mimeType {
	case "text/markdown", "text/x-markdown", "text/plain":
		return true
	default:
		return false
	}
}

// MimeType returns the primary MIME type for this parser.
func (p *MarkdownParser) MimeType() string {
	return "text/markdown"
}

// extractFrontmatter parses YAML frontmatter from markdown content.
// Returns the parsed frontmatter map, the remaining body, and any error.
func extractFrontmatter(content string) (map[string]any, string, error) {
	// Find the closing delimiter
	const delimiter = "---"
	const minDelimLen = 4 // "---\n"

	// Skip the opening delimiter
	start := len(delimiter)
	if len(content) > start && content[start] == '\r' {
		start++
	}
	if len(content) > start && content[start] == '\n' {
		start++
	}

	// Find the closing delimiter
	closeIdx := strings.Index(content[start:], "\n"+delimiter)
	if closeIdx == -1 {
		closeIdx = strings.Index(content[start:], "\r\n"+delimiter)
	}
	if closeIdx == -1 {
		return nil, content, fmt.Errorf("no closing frontmatter delimiter")
	}

	yamlContent := content[start : start+closeIdx]

	// Find where the body starts (after closing delimiter and newline)
	bodyStart := start + closeIdx + 1 + len(delimiter)
	for bodyStart < len(content) && (content[bodyStart] == '\n' || content[bodyStart] == '\r') {
		bodyStart++
	}

	body := ""
	if bodyStart < len(content) {
		body = content[bodyStart:]
	}

	// Parse YAML
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &frontmatter); err != nil {
		return nil, content, fmt.Errorf("parse YAML frontmatter: %w", err)
	}

	return frontmatter, body, nil
}

// generateID creates a stable document ID from filename and content hash.
func generateID(filename string, content []byte) string {
	// Use just the base filename without extension for readability
	base := filepath.Base(filename)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Sanitize for use as an ID
	name = sanitizeID(name)

	// Add hash suffix for uniqueness (12 chars = 48 bits, 50% collision at ~16M docs)
	hash := sha256.Sum256(content)
	shortHash := hex.EncodeToString(hash[:])[:12]

	return fmt.Sprintf("doc.%s.%s", name, shortHash)
}

// sanitizeID makes a string safe for use as an entity ID.
func sanitizeID(s string) string {
	var buf bytes.Buffer
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z':
			buf.WriteRune(r)
		case r >= '0' && r <= '9':
			buf.WriteRune(r)
		case r == '-' || r == '_':
			buf.WriteRune('-')
		case r == ' ':
			buf.WriteRune('-')
		}
	}
	return buf.String()
}

// ContentHash computes a SHA256 hash of the content.
func ContentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
