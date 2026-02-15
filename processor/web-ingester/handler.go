package webingester

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/source/chunker"
	"github.com/c360studio/semspec/source/weburl"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/c360studio/semstreams/message"
)

// IngestRequest is an alias for source.AddWebSourceRequest for backward compatibility.
type IngestRequest = source.AddWebSourceRequest

// Handler processes web source ingestion requests.
type Handler struct {
	fetcher         *Fetcher
	converter       *Converter
	chunkerInst     *chunker.Chunker
	analyzer        *source.Analyzer
	analysisEnabled bool
	analysisTimeout time.Duration
	logger          *slog.Logger
}

// NewHandler creates a new web ingestion handler.
func NewHandler(fetcher *Fetcher, chunkCfg ChunkConfig, analyzer *source.Analyzer,
	analysisEnabled bool, analysisTimeout time.Duration, logger *slog.Logger) (*Handler, error) {
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

	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		fetcher:         fetcher,
		converter:       NewConverter(),
		chunkerInst:     c,
		analyzer:        analyzer,
		analysisEnabled: analysisEnabled,
		analysisTimeout: analysisTimeout,
		logger:          logger,
	}, nil
}

// IngestWebResult contains the result of web source ingestion.
type IngestWebResult struct {
	Entities    []*WebEntityPayload
	Title       string
	ContentHash string
	ChunkCount  int
}

// IngestWebSource processes a web source and returns entities for graph ingestion.
func (h *Handler) IngestWebSource(ctx context.Context, req IngestRequest) (*IngestWebResult, error) {
	// Fetch web content
	fetchResult, err := h.fetcher.Fetch(ctx, req.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}

	return h.processContent(ctx, req, fetchResult)
}

// RefreshWebSource fetches content with ETag support for conditional refresh.
func (h *Handler) RefreshWebSource(ctx context.Context, req IngestRequest, etag string) (*IngestWebResult, bool, error) {
	// Fetch with ETag
	fetchResult, err := h.fetcher.FetchWithETag(ctx, req.URL, etag)
	if err != nil {
		return nil, false, fmt.Errorf("fetch URL: %w", err)
	}

	// If 304 Not Modified, return nil result with changed=false
	if fetchResult.StatusCode == 304 {
		return nil, false, nil
	}

	result, err := h.processContent(ctx, req, fetchResult)
	return result, true, err
}

// processContent converts fetched content to entities.
func (h *Handler) processContent(ctx context.Context, req IngestRequest, fetchResult *FetchResult) (*IngestWebResult, error) {
	// Capture time once for consistent timestamps across all entities
	now := time.Now()

	// Convert HTML to markdown
	convertResult, err := h.converter.Convert(fetchResult.Body)
	if err != nil {
		return nil, fmt.Errorf("convert HTML: %w", err)
	}

	// Perform LLM analysis if enabled
	var analysis *source.AnalysisResult
	var analysisSkipped bool
	if h.analysisEnabled && h.analyzer != nil {
		analysis, analysisSkipped = h.performAnalysis(ctx, convertResult.Markdown)
	}

	// Generate entity ID from URL using shared package
	entityID := weburl.GenerateEntityID(req.URL)

	// Compute content hash
	contentHash := computeHash(fetchResult.Body)

	// Chunk the markdown content
	chunks := h.chunkerInst.Chunk(entityID, convertResult.Markdown)

	// Build entities
	var entities []*WebEntityPayload

	// Build parent entity
	parentEntity := h.buildParentEntity(entityID, req, convertResult, fetchResult, contentHash, len(chunks), now, analysis, analysisSkipped)
	entities = append(entities, parentEntity)

	// Build chunk entities
	for _, chunk := range chunks {
		chunkEntity := h.buildChunkEntity(entityID, chunk, now)
		entities = append(entities, chunkEntity)
	}

	return &IngestWebResult{
		Entities:    entities,
		Title:       convertResult.Title,
		ContentHash: contentHash,
		ChunkCount:  len(chunks),
	}, nil
}

// performAnalysis runs LLM analysis on the converted markdown content.
// Returns the analysis result and a flag indicating if analysis was skipped.
func (h *Handler) performAnalysis(ctx context.Context, markdown string) (*source.AnalysisResult, bool) {
	analysisCtx, cancel := context.WithTimeout(ctx, h.analysisTimeout)
	defer cancel()

	result, err := h.analyzer.Analyze(analysisCtx, markdown)
	if err != nil {
		h.logger.Warn("LLM analysis failed, continuing without metadata",
			"error", err)
		return nil, true // skipped=true
	}

	h.logger.Debug("LLM analysis completed",
		"category", result.Category,
		"scope", result.Scope,
		"domains", result.Domain)

	return result, false
}

// buildParentEntity creates the parent web source entity.
func (h *Handler) buildParentEntity(entityID string, req IngestRequest, convertResult *ConvertResult, fetchResult *FetchResult, contentHash string, chunkCount int, now time.Time, analysis *source.AnalysisResult, analysisSkipped bool) *WebEntityPayload {
	triples := []message.Triple{
		{Subject: entityID, Predicate: sourceVocab.SourceType, Object: "web"},
		{Subject: entityID, Predicate: sourceVocab.WebType, Object: "web"},
		{Subject: entityID, Predicate: sourceVocab.WebURL, Object: req.URL},
		{Subject: entityID, Predicate: sourceVocab.WebContentHash, Object: contentHash},
		{Subject: entityID, Predicate: sourceVocab.WebChunkCount, Object: chunkCount},
		{Subject: entityID, Predicate: sourceVocab.SourceStatus, Object: "ready"},
		{Subject: entityID, Predicate: sourceVocab.WebLastFetched, Object: now.Format(time.RFC3339)},
		{Subject: entityID, Predicate: sourceVocab.SourceAddedAt, Object: now.Format(time.RFC3339)},
	}

	// Add title
	title := convertResult.Title
	if title == "" {
		title = extractDomainName(req.URL)
	}
	triples = append(triples, message.Triple{
		Subject: entityID, Predicate: sourceVocab.SourceName, Object: title,
	})
	triples = append(triples, message.Triple{
		Subject: entityID, Predicate: sourceVocab.WebTitle, Object: title,
	})

	// Add URL hostname
	if hostname := weburl.ExtractDomain(req.URL); hostname != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebDomain, Object: hostname,
		})
	}

	// Add content type
	if fetchResult.ContentType != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebContentType, Object: fetchResult.ContentType,
		})
	}

	// Add ETag if present
	if fetchResult.ETag != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebETag, Object: fetchResult.ETag,
		})
	}

	// Add auto-refresh settings
	if req.AutoRefresh {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebAutoRefresh, Object: true,
		})
	}
	if req.RefreshInterval != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebRefreshInterval, Object: req.RefreshInterval,
		})
	}

	// Add project if specified
	if req.ProjectID != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.SourceProject, Object: req.ProjectID,
		})
	}

	// Add LLM analysis results using Web predicates
	if analysis != nil {
		if analysis.Category != "" {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebCategory, Object: analysis.Category,
			})
		}
		if analysis.Scope != "" {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebScope, Object: analysis.Scope,
			})
		}
		if analysis.Summary != "" {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebSummary, Object: analysis.Summary,
			})
		}
		if analysis.Severity != "" {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebSeverity, Object: analysis.Severity,
			})
		}
		if len(analysis.AppliesTo) > 0 {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebAppliesTo, Object: analysis.AppliesTo,
			})
		}
		if len(analysis.Requirements) > 0 {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebRequirements, Object: analysis.Requirements,
			})
		}
		if len(analysis.Domain) > 0 {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebSemanticDomain, Object: analysis.Domain,
			})
		}
		if len(analysis.RelatedDomains) > 0 {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebRelatedDomains, Object: analysis.RelatedDomains,
			})
		}
		if len(analysis.Keywords) > 0 {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: sourceVocab.WebKeywords, Object: analysis.Keywords,
			})
		}
	}

	// Mark if analysis was skipped
	if analysisSkipped {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebAnalysisSkipped, Object: true,
		})
	}

	return &WebEntityPayload{
		EntityID_:  entityID,
		TripleData: triples,
		UpdatedAt:  now,
	}
}

// buildChunkEntity creates a chunk entity.
func (h *Handler) buildChunkEntity(parentID string, chunk source.Chunk, now time.Time) *WebEntityPayload {
	// Generate chunk ID: parentID.chunk.index
	chunkID := fmt.Sprintf("%s.chunk.%d", parentID, chunk.Index+1) // 1-indexed

	triples := []message.Triple{
		{Subject: chunkID, Predicate: sourceVocab.CodeBelongs, Object: parentID},
		{Subject: chunkID, Predicate: sourceVocab.WebType, Object: "chunk"},
		{Subject: chunkID, Predicate: sourceVocab.WebContent, Object: chunk.Content},
		{Subject: chunkID, Predicate: sourceVocab.WebChunkIndex, Object: chunk.Index + 1}, // 1-indexed
	}

	if chunk.Section != "" {
		triples = append(triples, message.Triple{
			Subject: chunkID, Predicate: sourceVocab.WebSection, Object: chunk.Section,
		})
	}

	return &WebEntityPayload{
		EntityID_:  chunkID,
		TripleData: triples,
		UpdatedAt:  now,
	}
}

// extractDomainName extracts the domain name from a URL for use as a fallback title.
func extractDomainName(rawURL string) string {
	domain := weburl.ExtractDomain(rawURL)
	if domain == "" {
		return rawURL
	}
	return domain
}

// computeHash computes SHA256 hash of content.
func computeHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
