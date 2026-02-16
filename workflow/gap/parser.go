// Package gap provides detection and parsing of knowledge gaps in LLM output.
//
// When an LLM encounters uncertainty during document generation, it can signal
// a knowledge gap using structured XML blocks:
//
//	<gap>
//	  <topic>api.semstreams</topic>
//	  <question>Does LoopInfo include workflow_slug?</question>
//	  <context>Need to know available fields for state tracking</context>
//	  <urgency>high</urgency>
//	</gap>
//
// The parser extracts these gaps and converts them to Question payloads
// that can block the workflow until answered.
package gap

import (
	"regexp"
	"strings"
)

// Gap represents a detected knowledge gap from LLM output.
type Gap struct {
	Topic    string `json:"topic"`             // Hierarchical topic (e.g., "api.semstreams")
	Question string `json:"question"`          // The actual question
	Context  string `json:"context,omitempty"` // Additional context
	Urgency  string `json:"urgency,omitempty"` // low, normal, high, blocking
}

// ParseResult contains the result of parsing LLM output for gaps.
type ParseResult struct {
	Gaps          []Gap  // Detected gaps
	CleanedOutput string // Output with gap blocks removed
	HasGaps       bool   // Whether any gaps were detected
}

// Parser extracts knowledge gaps from LLM-generated content.
type Parser struct {
	// Pattern matches <gap>...</gap> blocks including nested tags
	gapPattern *regexp.Regexp

	// Tag patterns for extracting individual fields
	topicPattern    *regexp.Regexp
	questionPattern *regexp.Regexp
	contextPattern  *regexp.Regexp
	urgencyPattern  *regexp.Regexp
}

// NewParser creates a new gap parser.
func NewParser() *Parser {
	return &Parser{
		// Match <gap>...</gap> blocks (non-greedy, case-insensitive)
		gapPattern: regexp.MustCompile(`(?is)<gap>(.*?)</gap>`),

		// Individual field patterns
		topicPattern:    regexp.MustCompile(`(?is)<topic>(.*?)</topic>`),
		questionPattern: regexp.MustCompile(`(?is)<question>(.*?)</question>`),
		contextPattern:  regexp.MustCompile(`(?is)<context>(.*?)</context>`),
		urgencyPattern:  regexp.MustCompile(`(?is)<urgency>(.*?)</urgency>`),
	}
}

// Parse extracts all knowledge gaps from the given content.
func (p *Parser) Parse(content string) *ParseResult {
	result := &ParseResult{
		Gaps:          []Gap{},
		CleanedOutput: content,
		HasGaps:       false,
	}

	// Find all gap blocks
	matches := p.gapPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return result
	}

	result.HasGaps = true

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		gapContent := match[1]
		gap := p.parseGapContent(gapContent)

		// Only include gaps with at least a question
		if gap.Question != "" {
			result.Gaps = append(result.Gaps, gap)
		}
	}

	// Remove gap blocks from output
	result.CleanedOutput = p.gapPattern.ReplaceAllString(content, "")
	// Clean up extra whitespace from removal
	result.CleanedOutput = cleanWhitespace(result.CleanedOutput)

	return result
}

// parseGapContent extracts fields from inside a <gap> block.
func (p *Parser) parseGapContent(content string) Gap {
	gap := Gap{
		Urgency: "normal", // Default urgency
	}

	// Extract topic
	if matches := p.topicPattern.FindStringSubmatch(content); len(matches) > 1 {
		gap.Topic = strings.TrimSpace(matches[1])
	}

	// Extract question
	if matches := p.questionPattern.FindStringSubmatch(content); len(matches) > 1 {
		gap.Question = strings.TrimSpace(matches[1])
	}

	// Extract context (optional)
	if matches := p.contextPattern.FindStringSubmatch(content); len(matches) > 1 {
		gap.Context = strings.TrimSpace(matches[1])
	}

	// Extract urgency (optional)
	if matches := p.urgencyPattern.FindStringSubmatch(content); len(matches) > 1 {
		urgency := strings.ToLower(strings.TrimSpace(matches[1]))
		if isValidUrgency(urgency) {
			gap.Urgency = urgency
		}
	}

	return gap
}

// HasGaps returns true if the content contains any gap blocks.
func (p *Parser) HasGaps(content string) bool {
	return p.gapPattern.MatchString(content)
}

// CountGaps returns the number of gap blocks in the content.
func (p *Parser) CountGaps(content string) int {
	return len(p.gapPattern.FindAllString(content, -1))
}

// isValidUrgency checks if the urgency value is valid.
func isValidUrgency(urgency string) bool {
	switch urgency {
	case "low", "normal", "high", "blocking":
		return true
	default:
		return false
	}
}

// cleanWhitespace removes extra blank lines and trims whitespace.
func cleanWhitespace(s string) string {
	// Replace multiple consecutive newlines with double newline
	multipleNewlines := regexp.MustCompile(`\n{3,}`)
	s = multipleNewlines.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// DefaultParser is the shared parser instance.
var DefaultParser = NewParser()

// Parse uses the default parser to extract gaps from content.
func Parse(content string) *ParseResult {
	return DefaultParser.Parse(content)
}

// HasGaps uses the default parser to check for gaps.
func HasGaps(content string) bool {
	return DefaultParser.HasGaps(content)
}
