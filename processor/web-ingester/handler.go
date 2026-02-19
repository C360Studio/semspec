package webingester

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	// Validate invariant: if analysis is enabled, analyzer must be provided
	if analysisEnabled && analyzer == nil {
		return nil, fmt.Errorf("analyzer required when analysis is enabled")
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

// parentEntityParams groups parameters for building the parent entity.
type parentEntityParams struct {
	entityID        string
	req             IngestRequest
	convertResult   *ConvertResult
	fetchResult     *FetchResult
	contentHash     string
	chunkCount      int
	timestamp       time.Time
	analysis        *source.AnalysisResult
	analysisSkipped bool
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

	// Perform LLM analysis if enabled (analyzer is guaranteed non-nil when enabled)
	var analysis *source.AnalysisResult
	var analysisSkipped bool
	if h.analysisEnabled {
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
	parentEntity := h.buildParentEntity(parentEntityParams{
		entityID:        entityID,
		req:             req,
		convertResult:   convertResult,
		fetchResult:     fetchResult,
		contentHash:     contentHash,
		chunkCount:      len(chunks),
		timestamp:       now,
		analysis:        analysis,
		analysisSkipped: analysisSkipped,
	})
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
		// Distinguish timeout errors from other failures for better debugging
		logFields := []any{"error", err}
		if errors.Is(err, context.DeadlineExceeded) {
			logFields = append(logFields, "reason", "timeout", "timeout_seconds", h.analysisTimeout.Seconds())
		}
		h.logger.Warn("LLM analysis failed, continuing without metadata", logFields...)
		return nil, true // skipped=true
	}

	h.logger.Debug("LLM analysis completed",
		"category", result.Category,
		"scope", result.Scope,
		"domains", result.Domain)

	return result, false
}

// buildParentEntity creates the parent web source entity.
func (h *Handler) buildParentEntity(p parentEntityParams) *WebEntityPayload {
	triples := []message.Triple{
		{Subject: p.entityID, Predicate: sourceVocab.SourceType, Object: "web"},
		{Subject: p.entityID, Predicate: sourceVocab.WebType, Object: "web"},
		{Subject: p.entityID, Predicate: sourceVocab.WebURL, Object: p.req.URL},
		{Subject: p.entityID, Predicate: sourceVocab.WebContentHash, Object: p.contentHash},
		{Subject: p.entityID, Predicate: sourceVocab.WebChunkCount, Object: p.chunkCount},
		{Subject: p.entityID, Predicate: sourceVocab.SourceStatus, Object: "ready"},
		{Subject: p.entityID, Predicate: sourceVocab.WebLastFetched, Object: p.timestamp.Format(time.RFC3339)},
		{Subject: p.entityID, Predicate: sourceVocab.SourceAddedAt, Object: p.timestamp.Format(time.RFC3339)},
	}

	// Add title
	title := p.convertResult.Title
	if title == "" {
		title = extractDomainName(p.req.URL)
	}
	triples = append(triples, message.Triple{
		Subject: p.entityID, Predicate: sourceVocab.SourceName, Object: title,
	})
	triples = append(triples, message.Triple{
		Subject: p.entityID, Predicate: sourceVocab.WebTitle, Object: title,
	})

	// Add URL hostname
	if hostname := weburl.ExtractDomain(p.req.URL); hostname != "" {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.WebDomain, Object: hostname,
		})
	}

	// Add content type
	if p.fetchResult.ContentType != "" {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.WebContentType, Object: p.fetchResult.ContentType,
		})
	}

	// Add ETag if present
	if p.fetchResult.ETag != "" {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.WebETag, Object: p.fetchResult.ETag,
		})
	}

	// Add auto-refresh settings
	if p.req.AutoRefresh {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.WebAutoRefresh, Object: true,
		})
	}
	if p.req.RefreshInterval != "" {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.WebRefreshInterval, Object: p.req.RefreshInterval,
		})
	}

	// Add project if specified
	if p.req.ProjectID != "" {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.SourceProject, Object: p.req.ProjectID,
		})
	}

	// Add LLM analysis results using Web predicates
	if p.analysis != nil {
		if p.analysis.Category != "" {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebCategory, Object: p.analysis.Category,
			})
		}
		if p.analysis.Scope != "" {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebScope, Object: p.analysis.Scope,
			})
		}
		if p.analysis.Summary != "" {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebSummary, Object: p.analysis.Summary,
			})
		}
		if p.analysis.Severity != "" {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebSeverity, Object: p.analysis.Severity,
			})
		}
		if len(p.analysis.AppliesTo) > 0 {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebAppliesTo, Object: p.analysis.AppliesTo,
			})
		}
		if len(p.analysis.Requirements) > 0 {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebRequirements, Object: p.analysis.Requirements,
			})
		}
		if len(p.analysis.Domain) > 0 {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebSemanticDomain, Object: p.analysis.Domain,
			})
		}
		if len(p.analysis.RelatedDomains) > 0 {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebRelatedDomains, Object: p.analysis.RelatedDomains,
			})
		}
		if len(p.analysis.Keywords) > 0 {
			triples = append(triples, message.Triple{
				Subject: p.entityID, Predicate: sourceVocab.WebKeywords, Object: p.analysis.Keywords,
			})
		}
	}

	// Mark if analysis was skipped
	if p.analysisSkipped {
		triples = append(triples, message.Triple{
			Subject: p.entityID, Predicate: sourceVocab.WebAnalysisSkipped, Object: true,
		})
	}

	return &WebEntityPayload{
		ID:         p.entityID,
		TripleData: triples,
		UpdatedAt:  p.timestamp,
	}
}

// buildChunkEntity creates a chunk entity with a 6-part entity ID.
func (h *Handler) buildChunkEntity(parentID string, chunk source.Chunk, now time.Time) *WebEntityPayload {
	// 6-part chunk ID: c360.semspec.source.web.chunk.{hash}{index}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", parentID, chunk.Index+1)))
	chunkID := fmt.Sprintf("c360.semspec.source.web.chunk.%s%04d", hex.EncodeToString(hash[:])[:12], chunk.Index+1)

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
		ID:         chunkID,
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
