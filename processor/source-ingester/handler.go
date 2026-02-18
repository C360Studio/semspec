package sourceingester

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/source/chunker"
	"github.com/c360studio/semspec/source/parser"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/c360studio/semstreams/message"
)

// IngestRequest is an alias for source.IngestRequest for backward compatibility.
type IngestRequest = source.IngestRequest

// Handler processes document ingestion requests.
type Handler struct {
	registry     *parser.Registry
	chunkerInst  *chunker.Chunker
	analyzer     *source.Analyzer
	sourcesDir   string
	analysisTime time.Duration
}

// NewHandler creates a new ingestion handler.
func NewHandler(llmClient *llm.Client, sourcesDir string, chunkCfg ChunkConfig, analysisTimeout time.Duration) (*Handler, error) {
	chunkerCfg := chunker.Config{
		TargetTokens: chunkCfg.TargetTokens,
		MaxTokens:    chunkCfg.MaxTokens,
		MinTokens:    chunkCfg.MinTokens,
	}
	if chunkerCfg.TargetTokens == 0 {
		chunkerCfg = chunker.DefaultConfig()
	}

	c, err := chunker.New(chunkerCfg)
	if err != nil {
		return nil, fmt.Errorf("create chunker: %w", err)
	}

	return &Handler{
		registry:     parser.DefaultRegistry,
		chunkerInst:  c,
		analyzer:     source.NewAnalyzer(llmClient),
		sourcesDir:   sourcesDir,
		analysisTime: analysisTimeout,
	}, nil
}

// IngestDocument processes a document and returns entities for graph ingestion.
func (h *Handler) IngestDocument(ctx context.Context, req IngestRequest) ([]*SourceEntityPayload, error) {
	// Resolve path
	path := req.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(h.sourcesDir, path)
	}

	// Read document content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read document: %w", err)
	}

	// Get parser for file
	var p parser.Parser
	if req.MimeType != "" {
		p = h.registry.GetByMimeType(req.MimeType)
	}
	if p == nil {
		p = h.registry.GetByExtension(filepath.Base(path))
	}
	if p == nil {
		return nil, fmt.Errorf("no parser for file: %s", path)
	}

	// Parse document
	doc, err := p.Parse(filepath.Base(path), content)
	if err != nil {
		return nil, fmt.Errorf("parse document: %w", err)
	}

	// Extract metadata (frontmatter or LLM analysis)
	var meta *source.AnalysisResult
	if doc.HasFrontmatter() {
		meta = doc.FrontmatterAsAnalysis()
	}

	// If no valid frontmatter metadata, use LLM analysis
	if meta == nil || meta.Category == "" {
		if h.analyzer == nil {
			return nil, fmt.Errorf("no frontmatter and no LLM client available for analysis")
		}
		analysisCtx, cancel := context.WithTimeout(ctx, h.analysisTime)
		defer cancel()

		meta, err = h.analyzer.Analyze(analysisCtx, doc.Content)
		if err != nil {
			return nil, fmt.Errorf("analyze document: %w", err)
		}
	}

	// Chunk document
	chunks := h.chunkerInst.Chunk(doc.ID, doc.Content)

	// Build entities
	var entities []*SourceEntityPayload

	// Compute content hash and mime type
	contentHash := parser.ContentHash(content)
	mimeType := req.MimeType
	if mimeType == "" {
		mimeType = parser.MimeTypeFromExtension(filepath.Ext(path))
	}

	// Build parent entity
	parentEntity := h.buildParentEntity(doc, meta, path, contentHash, mimeType, req, len(chunks))
	entities = append(entities, parentEntity)

	// Build chunk entities
	// Determine format from file extension for 6-part chunk IDs
	docFormat := parser.FormatFromExtension(filepath.Ext(path))
	for _, chunk := range chunks {
		chunkEntity := h.buildChunkEntity(doc.ID, chunk, meta.Category, docFormat, content)
		entities = append(entities, chunkEntity)
	}

	return entities, nil
}

// buildParentEntity creates the parent document entity.
func (h *Handler) buildParentEntity(doc *source.Document, meta *source.AnalysisResult, path, contentHash, mimeType string, req IngestRequest, chunkCount int) *SourceEntityPayload {
	// Use filename without extension as title, or extract from first heading
	title := doc.Filename
	if len(doc.Body) > 0 {
		// Try to extract title from first markdown heading
		if extracted := extractTitle(doc.Body); extracted != "" {
			title = extracted
		}
	}

	triples := []message.Triple{
		{Subject: doc.ID, Predicate: sourceVocab.SourceType, Object: "document"},
		{Subject: doc.ID, Predicate: sourceVocab.DocType, Object: "document"},
		{Subject: doc.ID, Predicate: sourceVocab.SourceName, Object: title},
		{Subject: doc.ID, Predicate: sourceVocab.DocCategory, Object: meta.Category},
		{Subject: doc.ID, Predicate: sourceVocab.DocFilePath, Object: path},
		{Subject: doc.ID, Predicate: sourceVocab.DocFileHash, Object: contentHash},
		{Subject: doc.ID, Predicate: sourceVocab.DocMimeType, Object: mimeType},
		{Subject: doc.ID, Predicate: sourceVocab.DocChunkCount, Object: chunkCount},
		{Subject: doc.ID, Predicate: sourceVocab.SourceStatus, Object: "ready"},
		{Subject: doc.ID, Predicate: sourceVocab.SourceAddedAt, Object: time.Now().Format(time.RFC3339)},
	}

	// Add optional fields
	if meta.Summary != "" {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.DocSummary, Object: meta.Summary,
		})
	}

	if meta.Severity != "" {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.DocSeverity, Object: meta.Severity,
		})
	}

	if meta.Scope != "" {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.DocScope, Object: meta.Scope,
		})
	}

	if len(meta.AppliesTo) > 0 {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.DocAppliesTo, Object: meta.AppliesTo,
		})
	}

	if len(meta.Requirements) > 0 {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.DocRequirements, Object: meta.Requirements,
		})
	}

	if req.ProjectID != "" {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.SourceProject, Object: req.ProjectID,
		})
	}

	if req.AddedBy != "" {
		triples = append(triples, message.Triple{
			Subject: doc.ID, Predicate: sourceVocab.SourceAddedBy, Object: req.AddedBy,
		})
	}

	return &SourceEntityPayload{
		EntityID_:  doc.ID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}
}

// buildChunkEntity creates a chunk entity with a 6-part entity ID.
func (h *Handler) buildChunkEntity(parentID string, chunk source.Chunk, category, format string, parentContent []byte) *SourceEntityPayload {
	// Generate 6-part chunk ID: c360.semspec.source.chunk.{format}.{hash}{index}
	chunkID := parser.GenerateChunkID(format, parentContent, chunk.Index+1)

	triples := []message.Triple{
		{Subject: chunkID, Predicate: sourceVocab.CodeBelongs, Object: parentID},
		{Subject: chunkID, Predicate: sourceVocab.DocType, Object: "chunk"},
		{Subject: chunkID, Predicate: sourceVocab.DocCategory, Object: category},
		{Subject: chunkID, Predicate: sourceVocab.DocContent, Object: chunk.Content},
		{Subject: chunkID, Predicate: sourceVocab.DocChunkIndex, Object: chunk.Index + 1}, // 1-indexed
	}

	if chunk.Section != "" {
		triples = append(triples, message.Triple{
			Subject: chunkID, Predicate: sourceVocab.DocSection, Object: chunk.Section,
		})
	}

	return &SourceEntityPayload{
		EntityID_:  chunkID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}
}

// extractTitle tries to extract a title from markdown content.
// Looks for the first H1 heading (# Title).
func extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Check for H1 heading: starts with # followed by space
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return ""
}
