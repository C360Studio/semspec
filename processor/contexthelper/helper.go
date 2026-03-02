// Package contexthelper provides a shared helper for requesting context from the context-builder.
// It publishes requests to context.build.<task_type> and receives responses via a per-request
// KV Watch on the CONTEXT_RESPONSES bucket (the KV twofer pattern).
package contexthelper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// Helper encapsulates context building via the centralized context-builder.
type Helper struct {
	natsClient    *natsclient.Client
	subjectPrefix string
	timeout       time.Duration
	logger        *slog.Logger
	sourceName    string

	// KV bucket for receiving responses (the twofer: Put IS the event)
	responseBucket string
	kvOnce         sync.Once
	kv             jetstream.KeyValue
	kvErr          error
}

// Config holds configuration for the context helper.
type Config struct {
	// SubjectPrefix is the base subject for context build requests.
	// Default: "context.build"
	SubjectPrefix string

	// ResponseBucket is the KV bucket where context-builder writes responses.
	// Default: "CONTEXT_RESPONSES"
	ResponseBucket string

	// Timeout is the maximum time to wait for a context response.
	// Default: 30s
	Timeout time.Duration

	// SourceName identifies the component making requests (for logging).
	SourceName string
}

// DefaultConfig returns default helper configuration.
func DefaultConfig() Config {
	return Config{
		SubjectPrefix:  "context.build",
		ResponseBucket: "CONTEXT_RESPONSES",
		Timeout:        30 * time.Second,
		SourceName:     "unknown",
	}
}

// New creates a new context helper. Call Start() before using BuildContext().
func New(natsClient *natsclient.Client, cfg Config, logger *slog.Logger) *Helper {
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = DefaultConfig().SubjectPrefix
	}
	if cfg.ResponseBucket == "" {
		cfg.ResponseBucket = DefaultConfig().ResponseBucket
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultConfig().Timeout
	}
	if cfg.SourceName == "" {
		cfg.SourceName = DefaultConfig().SourceName
	}

	return &Helper{
		natsClient:     natsClient,
		subjectPrefix:  cfg.SubjectPrefix,
		responseBucket: cfg.ResponseBucket,
		timeout:        cfg.Timeout,
		logger:         logger,
		sourceName:     cfg.SourceName,
	}
}

// Start is a lifecycle hook for callers. The KV bucket handle is acquired lazily
// on first use so there is no dependency on context-builder's start order.
func (h *Helper) Start(_ context.Context) error {
	h.logger.Debug("Context helper ready",
		"source", h.sourceName,
		"response_bucket", h.responseBucket)
	return nil
}

// getKV returns a cached KV bucket handle, initializing it on first call.
func (h *Helper) getKV(ctx context.Context) (jetstream.KeyValue, error) {
	h.kvOnce.Do(func() {
		js, err := h.natsClient.JetStream()
		if err != nil {
			h.kvErr = fmt.Errorf("get jetstream: %w", err)
			return
		}
		h.kv, h.kvErr = js.KeyValue(ctx, h.responseBucket)
		if h.kvErr != nil {
			h.kvErr = fmt.Errorf("get response bucket %s: %w", h.responseBucket, h.kvErr)
		}
	})
	return h.kv, h.kvErr
}

// Stop is a no-op — watcher lifecycle is per-request, tied to request context.
func (h *Helper) Stop() {}

// BuildContext requests context from the centralized context-builder.
// It publishes a request to context.build.<task_type> and waits for a response
// via KV Watch on CONTEXT_RESPONSES.<requestID>.
func (h *Helper) BuildContext(ctx context.Context, req *contextbuilder.ContextBuildRequest) (*contextbuilder.ContextBuildResponse, error) {
	if _, err := h.getKV(ctx); err != nil {
		return nil, err
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	var result *contextbuilder.ContextBuildResponse

	// Use retry for transient failures (network issues, temporary unavailability)
	retryConfig := retry.DefaultConfig()
	err := retry.Do(ctxTimeout, retryConfig, func() error {
		// Generate a fresh RequestID per attempt so the KV watcher is clean.
		req.RequestID = uuid.New().String()
		resp, err := h.buildContextOnce(ctxTimeout, req)
		if err != nil {
			return err // retry.NonRetryable errors won't be retried
		}
		result = resp
		return nil
	})

	if err != nil {
		h.logger.Warn("Failed to build context after retries",
			"request_id", req.RequestID,
			"task_type", req.TaskType,
			"error", err,
			"retryable", !retry.IsNonRetryable(err))
		return nil, err
	}

	return result, nil
}

// BuildContextGraceful requests context but returns nil (not error) on failure.
// This allows components to continue without context when graph is unavailable.
func (h *Helper) BuildContextGraceful(ctx context.Context, req *contextbuilder.ContextBuildRequest) *contextbuilder.ContextBuildResponse {
	resp, err := h.BuildContext(ctx, req)
	if err != nil {
		h.logger.Warn("Context build failed gracefully",
			"request_id", req.RequestID,
			"task_type", req.TaskType,
			"error", err)
		return nil
	}
	return resp
}

// buildContextOnce performs a single context build attempt using per-request KV Watch.
// Watch-first pattern: start watching BEFORE publishing the request to eliminate any race.
func (h *Helper) buildContextOnce(ctx context.Context, req *contextbuilder.ContextBuildRequest) (*contextbuilder.ContextBuildResponse, error) {
	kv, err := h.getKV(ctx)
	if err != nil {
		return nil, err
	}

	// Watch the specific KV key BEFORE publishing to avoid race.
	// The bootstrap phase delivers the value if it's already written (unlikely but safe),
	// then live updates catch the write when context-builder responds.
	watcher, err := kv.Watch(ctx, req.RequestID)
	if err != nil {
		return nil, fmt.Errorf("watch response key %s: %w", req.RequestID, err)
	}
	defer watcher.Stop()

	// Build subject based on task type
	subject := fmt.Sprintf("%s.%s", h.subjectPrefix, req.TaskType)

	// Wrap request in BaseMessage
	baseMsg := message.NewBaseMessage(req.Schema(), req, h.sourceName)
	reqBytes, err := json.Marshal(baseMsg)
	if err != nil {
		return nil, retry.NonRetryable(fmt.Errorf("marshal context request: %w", err))
	}

	// Get JetStream context for publish with delivery confirmation
	js, err := h.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	// Use JetStream publish for delivery confirmation
	if _, err := js.Publish(ctx, subject, reqBytes); err != nil {
		return nil, fmt.Errorf("publish context request: %w", err)
	}

	h.logger.Debug("Published context build request",
		"request_id", req.RequestID,
		"subject", subject,
		"task_type", req.TaskType)

	// Wait for response via KV Watch (the twofer: Put IS the event)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil, fmt.Errorf("watcher closed for %s", req.RequestID)
			}
			if entry == nil {
				// Bootstrap complete — no existing value, wait for live update.
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete {
				continue
			}

			// KV value is bare ContextBuildResponse JSON (not BaseMessage-wrapped).
			var resp contextbuilder.ContextBuildResponse
			if err := json.Unmarshal(entry.Value(), &resp); err != nil {
				return nil, retry.NonRetryable(fmt.Errorf("unmarshal context response: %w", err))
			}

			if resp.Error != "" {
				return nil, retry.NonRetryable(fmt.Errorf("context build error: %s", resp.Error))
			}

			return &resp, nil
		}
	}
}

// FormatContextResponse converts a context-builder response to a formatted string.
// This is a shared helper to avoid code duplication across components.
func FormatContextResponse(resp *contextbuilder.ContextBuildResponse) string {
	if resp == nil {
		return ""
	}

	var parts []string

	// Include entities
	for _, entity := range resp.Entities {
		if entity.Content != "" {
			header := fmt.Sprintf("### %s: %s", entity.Type, entity.ID)
			parts = append(parts, header+"\n\n"+entity.Content)
		}
	}

	// Include documents
	for path, content := range resp.Documents {
		if content != "" {
			header := fmt.Sprintf("### Document: %s", path)
			parts = append(parts, header+"\n\n"+content)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n\n---\n\n")
}
