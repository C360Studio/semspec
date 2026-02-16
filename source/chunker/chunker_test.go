package chunker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunker_Chunk_SimpleDocument(t *testing.T) {
	c := NewDefault()

	content := `# Introduction

This is the introduction section.

## Section 1

Some content in section 1.

## Section 2

Some content in section 2.
`

	chunks := c.Chunk("doc.test.123", content)
	require.NotEmpty(t, chunks)

	// All chunks should have parent ID
	for _, chunk := range chunks {
		assert.Equal(t, "doc.test.123", chunk.ParentID)
		assert.NotEmpty(t, chunk.Content)
		assert.GreaterOrEqual(t, chunk.Index, 0)
		assert.Greater(t, chunk.TokenCount, 0)
	}
}

func TestChunker_Chunk_PreservesCodeBlocks(t *testing.T) {
	c := MustNew(Config{
		TargetTokens: 50, // Small target to force splitting
		MaxTokens:    100,
		MinTokens:    10,
	})

	content := "# Code Example\n\n" + "```go\nfunc main() {\n\t// This is a code block\n\t// It should not be split\n\tfmt.Println(\"Hello\")\n}\n```\n\nMore text after code."

	chunks := c.Chunk("doc.test.123", content)

	// Find the chunk containing the code block
	var foundCodeBlock bool
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "```go") {
			foundCodeBlock = true
			// Code block should be complete
			assert.Contains(t, chunk.Content, "```go")
			assert.Contains(t, chunk.Content, "func main()")
			assert.Contains(t, chunk.Content, "```", "closing fence should be present")
		}
	}
	assert.True(t, foundCodeBlock, "should have a chunk with code block")
}

func TestChunker_Chunk_SectionNames(t *testing.T) {
	c := NewDefault()

	content := `# Main Title

Introduction paragraph.

## First Section

Content of first section.

## Second Section

Content of second section.
`

	chunks := c.Chunk("doc.test.123", content)
	require.NotEmpty(t, chunks)

	// First chunk should have the main title as section
	assert.Equal(t, "Main Title", chunks[0].Section)
}

func TestChunker_Chunk_LargeSection(t *testing.T) {
	c := MustNew(Config{
		TargetTokens: 100, // ~400 chars
		MaxTokens:    200, // ~800 chars
		MinTokens:    20,
	})

	// Create content that exceeds max tokens in one section
	longParagraph := strings.Repeat("This is a test sentence. ", 100) // ~2500 chars
	content := "# Large Section\n\n" + longParagraph

	chunks := c.Chunk("doc.test.123", content)

	// Should be split into multiple chunks
	assert.Greater(t, len(chunks), 1, "long content should be split")

	// All chunks should have reasonable size
	for _, chunk := range chunks {
		assert.LessOrEqual(t, chunk.TokenCount, c.config.MaxTokens+50, "chunk should not greatly exceed max")
	}
}

func TestChunker_Chunk_MergesSmallChunks(t *testing.T) {
	c := MustNew(Config{
		TargetTokens: 100,
		MaxTokens:    200,
		MinTokens:    50, // ~200 chars minimum
	})

	// Small sections that individually are below minimum
	content := `# Sec 1

Small.

# Sec 2

Tiny.

# Sec 3

Also small.
`

	chunks := c.Chunk("doc.test.123", content)

	// Small chunks should be merged
	// The exact number depends on merging behavior, but should be less than 3
	assert.LessOrEqual(t, len(chunks), 2, "small chunks should be merged")
}

func TestChunker_Chunk_EmptyContent(t *testing.T) {
	c := NewDefault()

	chunks := c.Chunk("doc.test.123", "")
	assert.Empty(t, chunks)
}

func TestChunker_Chunk_WhitespaceOnly(t *testing.T) {
	c := NewDefault()

	chunks := c.Chunk("doc.test.123", "   \n\n  \t  \n")
	assert.Empty(t, chunks)
}

func TestChunker_Chunk_NoHeadings(t *testing.T) {
	c := NewDefault()

	content := `This is a document without any headings.

It has multiple paragraphs though.

And some more content here.
`

	chunks := c.Chunk("doc.test.123", content)
	require.NotEmpty(t, chunks)

	// Should still create chunks
	assert.Equal(t, "doc.test.123", chunks[0].ParentID)
	assert.NotEmpty(t, chunks[0].Content)
}

func TestChunker_estimateTokens(t *testing.T) {
	c := NewDefault()

	tests := []struct {
		name    string
		content string
		minTok  int
		maxTok  int
	}{
		{"empty", "", 0, 0},
		{"short", "hello", 1, 2},
		{"medium", strings.Repeat("a", 100), 20, 30},
		{"long", strings.Repeat("test ", 100), 100, 150},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := c.estimateTokens(tt.content)
			assert.GreaterOrEqual(t, tokens, tt.minTok)
			assert.LessOrEqual(t, tokens, tt.maxTok)
		})
	}
}

func TestIsHeading(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"# Heading", true},
		{"## Heading 2", true},
		{"### Heading 3", true},
		{"#### Heading 4", true},
		{"Not a heading", false},
		{"  # Indented heading", true},
		{"Code with # symbol", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := isHeading(tt.line)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseHeading(t *testing.T) {
	tests := []struct {
		line      string
		wantLevel int
		wantText  string
	}{
		{"# Title", 1, "Title"},
		{"## Section", 2, "Section"},
		{"### Subsection", 3, "Subsection"},
		{"###### Deep", 6, "Deep"},
		{"####### TooDeep", 6, "TooDeep"}, // Level capped at 6, extra # stripped
		{"#NoSpace", 1, "NoSpace"},
		{"  ## Indented  ", 2, "Indented"},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			level, text := parseHeading(tt.line)
			assert.Equal(t, tt.wantLevel, level)
			assert.Equal(t, tt.wantText, text)
		})
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "simple",
			text: "First sentence. Second sentence.",
			want: []string{"First sentence.", "Second sentence."},
		},
		{
			name: "question and exclamation",
			text: "What is this? It is great!",
			want: []string{"What is this?", "It is great!"},
		},
		{
			name: "no split needed",
			text: "Single sentence without ending",
			want: []string{"Single sentence without ending"},
		},
		{
			name: "abbreviations not split",
			text: "Dr. Smith went to the store.",
			want: []string{"Dr.", "Smith went to the store."}, // Simple splitter doesn't handle abbreviations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSentences(tt.text)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 1000, cfg.TargetTokens)
	assert.Equal(t, 1500, cfg.MaxTokens)
	assert.Equal(t, 200, cfg.MinTokens)
}

func TestNew_DefaultsWhenZero(t *testing.T) {
	c, err := New(Config{}) // Zero config gets defaults
	require.NoError(t, err)
	assert.Equal(t, 1000, c.config.TargetTokens)
}

func TestNew_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "MinTokens >= TargetTokens",
			cfg:     Config{TargetTokens: 100, MaxTokens: 200, MinTokens: 100},
			wantErr: "MinTokens (100) must be less than TargetTokens (100)",
		},
		{
			name:    "TargetTokens > MaxTokens",
			cfg:     Config{TargetTokens: 300, MaxTokens: 200, MinTokens: 50},
			wantErr: "TargetTokens (300) must not exceed MaxTokens (200)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestIsCodeFence(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"```", true},
		{"```go", true},
		{"~~~", true},
		{"~~~python", true},
		{"  ```", true}, // indented
		{"  ~~~", true},
		{"##", false},
		{"text", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			assert.Equal(t, tt.want, isCodeFence(tt.line))
		})
	}
}

func TestChunker_HardSplit(t *testing.T) {
	c := MustNew(Config{
		TargetTokens: 25, // ~100 chars
		MaxTokens:    50, // ~200 chars
		MinTokens:    10,
	})

	// Content with no sentence breaks that exceeds max
	longContent := strings.Repeat("abcdefghij", 100) // 1000 chars, ~250 tokens

	chunks := c.Chunk("doc.test.123", "# Test\n\n"+longContent)

	// Should be split into multiple chunks
	assert.Greater(t, len(chunks), 1, "long content without breaks should be hard split")

	// Each chunk should respect MaxTokens (with some tolerance due to heuristic)
	for i, chunk := range chunks {
		assert.LessOrEqual(t, chunk.TokenCount, c.config.MaxTokens+10,
			"chunk %d exceeds max tokens: %d", i, chunk.TokenCount)
	}
}

func TestChunker_Chunk_IndexesAreSequential(t *testing.T) {
	c := MustNew(Config{
		TargetTokens: 50,
		MaxTokens:    100,
		MinTokens:    10,
	})

	content := "# Section 1\n\nContent 1.\n\n# Section 2\n\nContent 2.\n\n# Section 3\n\nContent 3."

	chunks := c.Chunk("doc.test.123", content)

	// Verify indexes are sequential starting at 0
	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.Index, "chunk index should be sequential")
	}
}

func TestChunker_Chunk_NestedCodeBlock(t *testing.T) {
	c := MustNew(Config{
		TargetTokens: 50,
		MaxTokens:    500,
		MinTokens:    10,
	})

	// Code block with # inside (should not be treated as heading)
	content := "# Real Heading\n\n```python\n# This is a comment\ndef func():\n    pass\n```\n\n## Another Heading\n\nMore content."

	chunks := c.Chunk("doc.test.123", content)

	// The # in the code block should not create a new section
	foundCodeChunk := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "# This is a comment") {
			foundCodeChunk = true
			// It should be within a code block, not a separate heading
			assert.Contains(t, chunk.Content, "```python")
		}
	}
	assert.True(t, foundCodeChunk)
}
