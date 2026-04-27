// Package webingest converts HTTP bodies into chunked graph entities and
// publishes them to graph.ingest.entity.
//
// Re-homed from processor/web-ingester during WS-25. The old async pipeline
// (httptool publishes to source.web.ingest.* → web-ingester refetches +
// chunks + persists) was deleted in favour of synchronous in-tool ingestion.
// One fetch, one publish, no separate component.
package webingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/source/chunker"
	"github.com/c360studio/semspec/source/weburl"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/c360studio/semstreams/message"
)

// graphIngestSubject is the subject graph-ingest consumes. Mirrored from the
// old web-ingester component to preserve graph schema compatibility.
const graphIngestSubject = "graph.ingest.entity"

// NATSClient is the minimum NATS surface PublishGraphEntities needs. Both
// natsclient.Client and any test fake satisfy it — the interface keeps this
// package usable from unit tests without a live NATS connection.
type NATSClient interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// IngestRequest carries the inputs required to build the parent + chunk
// entities. Fields beyond URL are optional.
type IngestRequest struct {
	URL             string
	ContentType     string
	ETag            string
	AutoRefresh     bool
	RefreshInterval string
	ProjectID       string
}

// IngestResult is the chunked + tripled output of a single ingestion. The
// Entities slice is parent-first, chunks-after; PublishGraphEntities flips
// the publish order so chunks land before the parent.
type IngestResult struct {
	EntityID    string
	Title       string
	Markdown    string
	ContentHash string
	ChunkCount  int
	Entities    []*WebEntityPayload
}

// Ingest converts an already-fetched HTTP body into graph-ready entities.
// fetchTime should be the time the response body was received.
//
// The conversion uses Readability with a tag-stripping fallback (see
// converter.go). Failures bubble up — the caller decides whether to publish
// nothing, surface the error, or retry.
func Ingest(req IngestRequest, body []byte, conv *Converter, chk *chunker.Chunker, fetchTime time.Time) (*IngestResult, error) {
	if conv == nil {
		conv = NewConverter()
	}
	if chk == nil {
		var err error
		chk, err = chunker.New(chunker.DefaultConfig())
		if err != nil {
			return nil, fmt.Errorf("default chunker: %w", err)
		}
	}

	convResult, err := conv.Convert(body, req.URL)
	if err != nil {
		return nil, fmt.Errorf("convert html: %w", err)
	}

	entityID := weburl.GenerateEntityID(req.URL)
	contentHash := computeIngestHash(body)
	chunks := chk.Chunk(entityID, convResult.Markdown)

	parent := buildParentEntity(req, convResult, contentHash, len(chunks), fetchTime)

	entities := make([]*WebEntityPayload, 0, len(chunks)+1)
	entities = append(entities, parent)
	for _, ch := range chunks {
		entities = append(entities, buildChunkEntity(entityID, ch, fetchTime))
	}

	return &IngestResult{
		EntityID:    entityID,
		Title:       convResult.Title,
		Markdown:    convResult.Markdown,
		ContentHash: contentHash,
		ChunkCount:  len(chunks),
		Entities:    entities,
	}, nil
}

// PublishGraphEntities publishes parent + chunks to graph.ingest.entity.
// Chunks publish before the parent so the parent's WebChunkCount predicate
// references chunks the graph already knows about — same ordering web-
// ingester used.
//
// PublishToStreamer is the minimum NATS surface required (NATSClient already
// satisfies it). Errors are returned for the first failure; remaining
// entities are not attempted.
func PublishGraphEntities(ctx context.Context, nc NATSClient, result *IngestResult) error {
	if nc == nil {
		return fmt.Errorf("nats client required")
	}
	if result == nil || len(result.Entities) == 0 {
		return nil
	}

	// Chunks first.
	if len(result.Entities) > 1 {
		for _, chunk := range result.Entities[1:] {
			if err := publishEntity(ctx, nc, chunk); err != nil {
				return fmt.Errorf("publish chunk %s: %w", chunk.ID, err)
			}
		}
	}
	// Parent last.
	if err := publishEntity(ctx, nc, result.Entities[0]); err != nil {
		return fmt.Errorf("publish parent %s: %w", result.Entities[0].ID, err)
	}
	return nil
}

func publishEntity(ctx context.Context, nc NATSClient, entity *WebEntityPayload) error {
	msg := message.NewBaseMessage(WebEntityType, entity, "semspec")
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal entity: %w", err)
	}
	return nc.PublishToStream(ctx, graphIngestSubject, data)
}

// buildParentEntity constructs the parent web-source entity. Predicates
// match what processor/web-ingester wrote so existing graph consumers keep
// working unchanged. WS-25 dropped the LLM-analysis predicates (Category,
// Severity, etc.) — those had no production consumer.
func buildParentEntity(req IngestRequest, conv *ConvertResult, contentHash string, chunkCount int, ts time.Time) *WebEntityPayload {
	triples := []message.Triple{
		{Subject: "", Predicate: sourceVocab.SourceType, Object: "web"},
		{Subject: "", Predicate: sourceVocab.WebType, Object: "web"},
		{Subject: "", Predicate: sourceVocab.WebURL, Object: req.URL},
		{Subject: "", Predicate: sourceVocab.WebContentHash, Object: contentHash},
		{Subject: "", Predicate: sourceVocab.WebChunkCount, Object: chunkCount},
		{Subject: "", Predicate: sourceVocab.SourceStatus, Object: "ready"},
		{Subject: "", Predicate: sourceVocab.WebLastFetched, Object: ts.Format(time.RFC3339)},
		{Subject: "", Predicate: sourceVocab.SourceAddedAt, Object: ts.Format(time.RFC3339)},
	}

	entityID := weburl.GenerateEntityID(req.URL)
	for i := range triples {
		triples[i].Subject = entityID
	}

	title := conv.Title
	if title == "" {
		title = fallbackTitleFromURL(req.URL)
	}
	triples = append(triples,
		message.Triple{Subject: entityID, Predicate: sourceVocab.SourceName, Object: title},
		message.Triple{Subject: entityID, Predicate: sourceVocab.WebTitle, Object: title},
	)

	if hostname := weburl.ExtractDomain(req.URL); hostname != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebDomain, Object: hostname,
		})
	}
	if req.ContentType != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebContentType, Object: req.ContentType,
		})
	}
	if req.ETag != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.WebETag, Object: req.ETag,
		})
	}
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
	if req.ProjectID != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.SourceProject, Object: req.ProjectID,
		})
	}

	return &WebEntityPayload{
		ID:         entityID,
		TripleData: triples,
		UpdatedAt:  ts,
	}
}

// buildChunkEntity constructs a chunk entity with a 6-part chunk ID.
// Format: c360.semspec.source.web.chunk.{hash}{index} — preserved verbatim
// from web-ingester so existing chunk lookups keep working.
func buildChunkEntity(parentID string, chunk chunker.Chunk, ts time.Time) *WebEntityPayload {
	hash := sha256.Sum256(fmt.Appendf(nil, "%s-%d", parentID, chunk.Index+1))
	chunkID := fmt.Sprintf("c360.semspec.source.web.chunk.%s%04d",
		hex.EncodeToString(hash[:])[:12], chunk.Index+1)

	triples := []message.Triple{
		{Subject: chunkID, Predicate: sourceVocab.CodeBelongs, Object: parentID},
		{Subject: chunkID, Predicate: sourceVocab.WebType, Object: "chunk"},
		{Subject: chunkID, Predicate: sourceVocab.WebContent, Object: chunk.Content},
		{Subject: chunkID, Predicate: sourceVocab.WebChunkIndex, Object: chunk.Index + 1},
	}
	if chunk.Section != "" {
		triples = append(triples, message.Triple{
			Subject: chunkID, Predicate: sourceVocab.WebSection, Object: chunk.Section,
		})
	}

	return &WebEntityPayload{
		ID:         chunkID,
		TripleData: triples,
		UpdatedAt:  ts,
	}
}

// computeIngestHash returns the SHA-256 hex digest of the response body.
func computeIngestHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// fallbackTitleFromURL returns the URL hostname when the page provides no
// title metadata. Better than dumping the full URL into the title triple.
func fallbackTitleFromURL(rawURL string) string {
	if domain := weburl.ExtractDomain(rawURL); domain != "" {
		return domain
	}
	return rawURL
}
