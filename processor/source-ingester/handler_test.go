package sourceingester

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	_ "github.com/c360studio/semspec/llm/providers"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_IngestDocument_WithFrontmatter(t *testing.T) {
	// Create temp directory with test document
	tmpDir := t.TempDir()
	docPath := filepath.Join(tmpDir, "error-handling.md")
	docContent := `---
category: sop
applies_to:
  - "*.go"
severity: error
summary: Go error handling guidelines
requirements:
  - Always wrap errors with context
  - Never ignore errors
---

# Error Handling

Always wrap errors with context using fmt.Errorf.
`
	require.NoError(t, os.WriteFile(docPath, []byte(docContent), 0644))

	// Create handler with mock LLM (not needed for frontmatter)
	handler, err := NewHandler(
		nil, // LLM client not needed when frontmatter exists
		tmpDir,
		ChunkConfig{TargetTokens: 1000, MaxTokens: 1500, MinTokens: 200},
		30*time.Second,
	)
	require.NoError(t, err)

	// Ingest document
	entities, err := handler.IngestDocument(context.Background(), IngestRequest{
		Path:      "error-handling.md",
		ProjectID: workflow.ProjectEntityID("test-project"),
		AddedBy:   "test-user",
	})
	require.NoError(t, err)
	require.NotEmpty(t, entities)

	// First entity should be the parent
	parent := entities[0]
	assert.Contains(t, parent.ID, "error-handling")

	// Verify parent triples
	tripleMap := make(map[string]any)
	for _, triple := range parent.TripleData {
		tripleMap[triple.Predicate] = triple.Object
	}

	assert.Equal(t, "document", tripleMap[sourceVocab.SourceType])
	assert.Equal(t, "sop", tripleMap[sourceVocab.DocCategory])
	assert.Equal(t, "error", tripleMap[sourceVocab.DocSeverity])
	assert.Equal(t, "Go error handling guidelines", tripleMap[sourceVocab.DocSummary])
	assert.Equal(t, workflow.ProjectEntityID("test-project"), tripleMap[sourceVocab.SourceProject])
	assert.Equal(t, "test-user", tripleMap[sourceVocab.SourceAddedBy])

	// Verify parent has full body content (without frontmatter)
	content, ok := tripleMap[sourceVocab.DocContent].(string)
	require.True(t, ok, "parent entity should have source.doc.content")
	assert.Contains(t, content, "Error Handling")
	assert.Contains(t, content, "Always wrap errors with context using fmt.Errorf")
	assert.NotContains(t, content, "category: sop", "content should not include frontmatter")

	// Verify applies_to
	appliesTo, ok := tripleMap[sourceVocab.DocAppliesTo].([]string)
	require.True(t, ok, "applies_to should be []string")
	assert.Contains(t, appliesTo, "*.go")

	// Verify requirements
	reqs, ok := tripleMap[sourceVocab.DocRequirements].([]string)
	require.True(t, ok, "requirements should be []string")
	assert.Contains(t, reqs, "Always wrap errors with context")
}

func TestHandler_IngestDocument_WithLLMAnalysis(t *testing.T) {
	// Create mock LLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": `{"category":"spec","applies_to":["*.ts"],"severity":"","summary":"TypeScript guidelines","requirements":["Use strict mode"]}`,
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create temp directory with test document (no frontmatter)
	tmpDir := t.TempDir()
	docPath := filepath.Join(tmpDir, "typescript-guide.md")
	docContent := `# TypeScript Guidelines

Use strict mode for all TypeScript files.
`
	require.NoError(t, os.WriteFile(docPath, []byte(docContent), 0644))

	// Create LLM client with mock server
	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Preferred: []string{"test-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"test-model": {
				Provider: "ollama",
				URL:      server.URL,
				Model:    "test-model",
			},
		},
	)
	llmClient := llm.NewClient(registry)

	// Create handler
	handler, err := NewHandler(
		llmClient,
		tmpDir,
		ChunkConfig{TargetTokens: 1000, MaxTokens: 1500, MinTokens: 200},
		30*time.Second,
	)
	require.NoError(t, err)

	// Ingest document
	entities, err := handler.IngestDocument(context.Background(), IngestRequest{
		Path: "typescript-guide.md",
	})
	require.NoError(t, err)
	require.NotEmpty(t, entities)

	// Verify parent entity has LLM-extracted metadata
	parent := entities[0]
	tripleMap := make(map[string]any)
	for _, triple := range parent.TripleData {
		tripleMap[triple.Predicate] = triple.Object
	}

	assert.Equal(t, "spec", tripleMap[sourceVocab.DocCategory])
	assert.Equal(t, "TypeScript guidelines", tripleMap[sourceVocab.DocSummary])
}

func TestHandler_IngestDocument_ChunksCreated(t *testing.T) {
	// Create temp directory with document that will produce multiple chunks
	tmpDir := t.TempDir()
	docPath := filepath.Join(tmpDir, "large-doc.md")

	// Create document with frontmatter and multiple sections
	docContent := `---
category: reference
applies_to: []
---

# Large Document

## Section 1

` + generateLongContent(500) + `

## Section 2

` + generateLongContent(500) + `

## Section 3

` + generateLongContent(500) + `
`
	require.NoError(t, os.WriteFile(docPath, []byte(docContent), 0644))

	// Create handler with small chunk size to force multiple chunks
	handler, err := NewHandler(
		nil,
		tmpDir,
		ChunkConfig{TargetTokens: 100, MaxTokens: 200, MinTokens: 50},
		30*time.Second,
	)
	require.NoError(t, err)

	// Ingest document
	entities, err := handler.IngestDocument(context.Background(), IngestRequest{
		Path: "large-doc.md",
	})
	require.NoError(t, err)

	// Should have parent + multiple chunks
	assert.Greater(t, len(entities), 1, "should have multiple entities (parent + chunks)")

	// First entity is parent
	parent := entities[0]
	assert.Contains(t, parent.ID, "large-doc")

	// Verify chunk count on parent
	var chunkCount int
	for _, triple := range parent.TripleData {
		if triple.Predicate == sourceVocab.DocChunkCount {
			chunkCount = triple.Object.(int)
			break
		}
	}
	assert.Equal(t, len(entities)-1, chunkCount, "chunk count should match number of chunk entities")

	// Verify chunks reference parent
	for i, entity := range entities[1:] {
		assert.Contains(t, entity.ID, ".chunk.", "chunk ID should contain .chunk.")

		// Verify belongs relationship
		var belongsTo string
		var chunkIndex int
		for _, triple := range entity.TripleData {
			if triple.Predicate == sourceVocab.CodeBelongs {
				belongsTo = triple.Object.(string)
			}
			if triple.Predicate == sourceVocab.DocChunkIndex {
				chunkIndex = triple.Object.(int)
			}
		}
		assert.Equal(t, parent.ID, belongsTo, "chunk should belong to parent")
		assert.Equal(t, i+1, chunkIndex, "chunk index should be 1-indexed")
	}
}

func TestHandler_IngestDocument_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	handler, err := NewHandler(
		nil,
		tmpDir,
		ChunkConfig{TargetTokens: 1000, MaxTokens: 1500, MinTokens: 200},
		30*time.Second,
	)
	require.NoError(t, err)

	_, err = handler.IngestDocument(context.Background(), IngestRequest{
		Path: "nonexistent.md",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read document")
}

func TestHandler_IngestDocument_UnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	docPath := filepath.Join(tmpDir, "test.docx")
	require.NoError(t, os.WriteFile(docPath, []byte("fake docx content"), 0644))

	handler, err := NewHandler(
		nil,
		tmpDir,
		ChunkConfig{TargetTokens: 1000, MaxTokens: 1500, MinTokens: 200},
		30*time.Second,
	)
	require.NoError(t, err)

	_, err = handler.IngestDocument(context.Background(), IngestRequest{
		Path: "test.docx",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parser")
}

func TestHandler_IngestDocument_AbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()
	docPath := filepath.Join(tmpDir, "abs-path-test.md")
	docContent := `---
category: api
---

# API Test
`
	require.NoError(t, os.WriteFile(docPath, []byte(docContent), 0644))

	handler, err := NewHandler(
		nil,
		"/some/other/dir", // Different sources dir
		ChunkConfig{TargetTokens: 1000, MaxTokens: 1500, MinTokens: 200},
		30*time.Second,
	)
	require.NoError(t, err)

	// Use absolute path
	entities, err := handler.IngestDocument(context.Background(), IngestRequest{
		Path: docPath, // Absolute path should work regardless of sources_dir
	})
	require.NoError(t, err)
	require.NotEmpty(t, entities)
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "simple heading",
			content: "# My Title\n\nSome content.",
			want:    "My Title",
		},
		{
			name:    "heading with leading newlines",
			content: "\n\n# Another Title\n\nContent here.",
			want:    "Another Title",
		},
		{
			name:    "no heading",
			content: "Just some content without a heading.",
			want:    "",
		},
		{
			name:    "h2 heading only",
			content: "## Section\n\nThis is not H1.",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitle(tt.content)
			assert.Equal(t, tt.want, got)
		})
	}
}

// generateLongContent creates repetitive content of approximately n words.
func generateLongContent(words int) string {
	sentence := "This is a test sentence that is used to generate content. "
	result := ""
	wordsPerSentence := 10
	for i := 0; i < words/wordsPerSentence; i++ {
		result += sentence
	}
	return result
}
