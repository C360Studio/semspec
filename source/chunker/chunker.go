// Package chunker provides document chunking for context assembly.
package chunker

import (
	"fmt"
	"strings"
)

// Chunk represents a section of a document for context assembly. Re-homed
// from the parent `source` package during WS-25 — the type only had readers
// inside chunker and webingest, so colocating it here breaks the upward
// import that previously forced source/* to be cycle-prone.
type Chunk struct {
	// ParentID is the ID of the parent document.
	ParentID string `json:"parent_id"`

	// Index is the chunk sequence number (0-indexed internally, 1-indexed for display).
	Index int `json:"index"`

	// Section is the heading or section name.
	Section string `json:"section,omitempty"`

	// Content is the chunk text.
	Content string `json:"content"`

	// TokenCount is the estimated token count.
	TokenCount int `json:"token_count"`
}

// charsPerToken is the approximate average characters per token for GPT tokenizers.
const charsPerToken = 4

// Config holds chunking configuration.
type Config struct {
	// TargetTokens is the ideal chunk size in tokens.
	TargetTokens int

	// MaxTokens is the maximum chunk size.
	MaxTokens int

	// MinTokens is the minimum chunk size (smaller chunks are merged).
	MinTokens int
}

// DefaultConfig returns sensible chunking defaults.
func DefaultConfig() Config {
	return Config{
		TargetTokens: 1000,
		MaxTokens:    1500,
		MinTokens:    200,
	}
}

// Validate checks if the configuration is valid.
func (c Config) Validate() error {
	if c.MinTokens <= 0 {
		return fmt.Errorf("MinTokens must be positive, got %d", c.MinTokens)
	}
	if c.TargetTokens <= 0 {
		return fmt.Errorf("TargetTokens must be positive, got %d", c.TargetTokens)
	}
	if c.MaxTokens <= 0 {
		return fmt.Errorf("MaxTokens must be positive, got %d", c.MaxTokens)
	}
	if c.MinTokens >= c.TargetTokens {
		return fmt.Errorf("MinTokens (%d) must be less than TargetTokens (%d)", c.MinTokens, c.TargetTokens)
	}
	if c.TargetTokens > c.MaxTokens {
		return fmt.Errorf("TargetTokens (%d) must not exceed MaxTokens (%d)", c.TargetTokens, c.MaxTokens)
	}
	return nil
}

// Chunker splits documents into chunks for context assembly.
type Chunker struct {
	config Config
}

// New creates a new Chunker with the given configuration.
// Returns an error if the configuration is invalid.
func New(cfg Config) (*Chunker, error) {
	if cfg.TargetTokens == 0 {
		cfg = DefaultConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Chunker{config: cfg}, nil
}

// MustNew creates a new Chunker, panicking on invalid config.
// Use for known-good configurations.
func MustNew(cfg Config) *Chunker {
	c, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return c
}

// NewDefault creates a Chunker with default configuration.
func NewDefault() *Chunker {
	return MustNew(DefaultConfig())
}

// Chunk splits a document body into chunks.
// Returns a slice of Chunk structs with section names and content.
func (c *Chunker) Chunk(parentID string, content string) []Chunk {
	// Parse sections from markdown
	sections := c.parseSections(content)

	// Build chunks from sections
	var chunks []Chunk
	var currentChunk Chunk
	currentChunk.ParentID = parentID

	for _, section := range sections {
		sectionTokens := c.estimateTokens(section.Content)

		// If section alone exceeds max, split it
		if sectionTokens > c.config.MaxTokens {
			// Flush current chunk if non-empty
			if c.estimateTokens(currentChunk.Content) >= c.config.MinTokens {
				chunks = append(chunks, c.finalizeChunk(currentChunk, len(chunks)))
				currentChunk = Chunk{ParentID: parentID}
			}

			// Split large section into paragraphs
			subChunks := c.splitLargeSection(parentID, section, len(chunks))
			chunks = append(chunks, subChunks...)
			continue
		}

		currentTokens := c.estimateTokens(currentChunk.Content)

		// If adding this section would exceed target, finalize current chunk
		if currentTokens > 0 && currentTokens+sectionTokens > c.config.TargetTokens {
			chunks = append(chunks, c.finalizeChunk(currentChunk, len(chunks)))
			currentChunk = Chunk{ParentID: parentID}
		}

		// Add section to current chunk
		if currentChunk.Section == "" {
			currentChunk.Section = section.Heading
		}
		if currentChunk.Content != "" {
			currentChunk.Content += "\n\n"
		}
		currentChunk.Content += section.Content
	}

	// Flush remaining content
	if c.estimateTokens(currentChunk.Content) > 0 {
		chunks = append(chunks, c.finalizeChunk(currentChunk, len(chunks)))
	}

	// Merge small trailing chunks
	chunks = c.mergeSmallChunks(chunks)

	return chunks
}

// section represents a document section (heading + content).
type section struct {
	Heading string
	Content string
	Level   int // Heading level (1-6, 0 for no heading)
}

// parseSections extracts sections from markdown content.
func (c *Chunker) parseSections(content string) []section {
	lines := strings.Split(content, "\n")
	var sections []section
	var current section
	inCodeBlock := false

	for _, line := range lines {
		// Track code blocks to avoid splitting inside them
		if isCodeFence(line) {
			inCodeBlock = !inCodeBlock
		}

		// Check for heading (only outside code blocks)
		if !inCodeBlock && isHeading(line) {
			// Save current section if it has content
			if strings.TrimSpace(current.Content) != "" {
				sections = append(sections, current)
			}

			// Start new section
			level, heading := parseHeading(line)
			current = section{
				Heading: heading,
				Level:   level,
				Content: line,
			}
		} else {
			// Add line to current section
			if current.Content != "" {
				current.Content += "\n"
			}
			current.Content += line
		}
	}

	// Add final section
	if strings.TrimSpace(current.Content) != "" {
		sections = append(sections, current)
	}

	return sections
}

// splitLargeSection splits a section that exceeds max tokens.
func (c *Chunker) splitLargeSection(parentID string, sec section, startIndex int) []Chunk {
	var chunks []Chunk
	paragraphs := c.splitIntoParagraphs(sec.Content)

	var current Chunk
	current.ParentID = parentID
	current.Section = sec.Heading

	for _, para := range paragraphs {
		paraTokens := c.estimateTokens(para)

		// If single paragraph exceeds max, split by sentences
		if paraTokens > c.config.MaxTokens {
			// Flush current
			if c.estimateTokens(current.Content) >= c.config.MinTokens {
				chunks = append(chunks, c.finalizeChunk(current, startIndex+len(chunks)))
				current = Chunk{ParentID: parentID, Section: sec.Heading}
			}

			// Split paragraph by sentences (or just take it as-is if still too big)
			sentenceChunks := c.splitBySentences(parentID, sec.Heading, para, startIndex+len(chunks))
			chunks = append(chunks, sentenceChunks...)
			continue
		}

		currentTokens := c.estimateTokens(current.Content)
		if currentTokens > 0 && currentTokens+paraTokens > c.config.TargetTokens {
			chunks = append(chunks, c.finalizeChunk(current, startIndex+len(chunks)))
			current = Chunk{ParentID: parentID, Section: sec.Heading}
		}

		if current.Content != "" {
			current.Content += "\n\n"
		}
		current.Content += para
	}

	// Flush remaining
	if c.estimateTokens(current.Content) > 0 {
		chunks = append(chunks, c.finalizeChunk(current, startIndex+len(chunks)))
	}

	return chunks
}

// splitIntoParagraphs splits content by double newlines, preserving code blocks.
func (c *Chunker) splitIntoParagraphs(content string) []string {
	var paragraphs []string
	var current strings.Builder
	lines := strings.Split(content, "\n")
	inCodeBlock := false
	lastWasEmpty := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track code blocks (using trimmed to handle indented fences)
		if isCodeFence(trimmed) {
			inCodeBlock = !inCodeBlock
		}

		if !inCodeBlock && trimmed == "" {
			if !lastWasEmpty && current.Len() > 0 {
				paragraphs = append(paragraphs, strings.TrimSpace(current.String()))
				current.Reset()
			}
			lastWasEmpty = true
		} else {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
			lastWasEmpty = false
		}
	}

	if current.Len() > 0 {
		paragraphs = append(paragraphs, strings.TrimSpace(current.String()))
	}

	return paragraphs
}

// splitBySentences splits a paragraph by sentences as a last resort.
func (c *Chunker) splitBySentences(parentID, sectionName, content string, startIndex int) []Chunk {
	// Simple sentence splitting - split on . ? ! followed by space or newline
	var chunks []Chunk
	var current Chunk
	current.ParentID = parentID
	current.Section = sectionName

	// For very long content without sentence breaks, use hard split
	sentences := splitSentences(content)
	if len(sentences) <= 1 && c.estimateTokens(content) > c.config.MaxTokens {
		return c.hardSplit(parentID, sectionName, content, startIndex)
	}

	if len(sentences) <= 1 {
		current.Content = content
		current.TokenCount = c.estimateTokens(content)
		current.Index = startIndex
		return []Chunk{current}
	}

	for _, sentence := range sentences {
		sentenceTokens := c.estimateTokens(sentence)
		currentTokens := c.estimateTokens(current.Content)

		if currentTokens > 0 && currentTokens+sentenceTokens > c.config.TargetTokens {
			chunks = append(chunks, c.finalizeChunk(current, startIndex+len(chunks)))
			current = Chunk{ParentID: parentID, Section: sectionName}
		}

		if current.Content != "" {
			current.Content += " "
		}
		current.Content += sentence
	}

	if c.estimateTokens(current.Content) > 0 {
		chunks = append(chunks, c.finalizeChunk(current, startIndex+len(chunks)))
	}

	return chunks
}

// hardSplit splits content at character boundaries when no natural breaks exist.
// This is a last resort to ensure MaxTokens is never exceeded.
func (c *Chunker) hardSplit(parentID, sectionName, content string, startIndex int) []Chunk {
	var chunks []Chunk
	maxChars := c.config.MaxTokens * charsPerToken

	runes := []rune(content)
	for i := 0; i < len(runes); i += maxChars {
		end := i + maxChars
		if end > len(runes) {
			end = len(runes)
		}

		chunkContent := string(runes[i:end])
		chunks = append(chunks, Chunk{
			ParentID:   parentID,
			Section:    sectionName,
			Index:      startIndex + len(chunks),
			Content:    chunkContent,
			TokenCount: c.estimateTokens(chunkContent),
		})
	}

	return chunks
}

// mergeSmallChunks combines chunks that are below minimum size.
func (c *Chunker) mergeSmallChunks(chunks []Chunk) []Chunk {
	if len(chunks) <= 1 {
		return chunks
	}

	var result []Chunk
	for i := 0; i < len(chunks); i++ {
		chunk := chunks[i]

		// If chunk is too small and there's a next chunk, merge
		if chunk.TokenCount < c.config.MinTokens && i < len(chunks)-1 {
			next := chunks[i+1]
			combined := chunk.Content + "\n\n" + next.Content
			combinedTokens := c.estimateTokens(combined)

			// Only merge if combined doesn't exceed max
			if combinedTokens <= c.config.MaxTokens {
				chunks[i+1] = Chunk{
					ParentID:   chunk.ParentID,
					Section:    chunk.Section,
					Content:    combined,
					TokenCount: combinedTokens,
				}
				continue
			}
		}

		result = append(result, chunk)
	}

	// Re-index after merge
	for i := range result {
		result[i].Index = i
	}

	return result
}

// finalizeChunk sets the index and token count for a chunk.
func (c *Chunker) finalizeChunk(chunk Chunk, index int) Chunk {
	chunk.Index = index
	chunk.TokenCount = c.estimateTokens(chunk.Content)
	return chunk
}

// estimateTokens estimates token count using the chars/token heuristic.
func (c *Chunker) estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return (len(content) + charsPerToken - 1) / charsPerToken
}

// isCodeFence checks if a line is a code fence (``` or ~~~).
func isCodeFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// isHeading checks if a line is a markdown heading.
func isHeading(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#")
}

// parseHeading extracts the level and text from a heading line.
func parseHeading(line string) (int, string) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for _, ch := range trimmed {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level > 6 {
		level = 6
	}

	text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
	return level, text
}

// splitSentences splits text into sentences.
func splitSentences(text string) []string {
	// Simple approach: split on sentence-ending punctuation followed by space
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence end
		if runes[i] == '.' || runes[i] == '?' || runes[i] == '!' {
			// Look ahead for space or end of text
			if i == len(runes)-1 || (i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n')) {
				sentences = append(sentences, strings.TrimSpace(current.String()))
				current.Reset()
				// Skip the space
				if i+1 < len(runes) && runes[i+1] == ' ' {
					i++
				}
			}
		}
	}

	// Add remaining text
	if current.Len() > 0 {
		sentences = append(sentences, strings.TrimSpace(current.String()))
	}

	return sentences
}
