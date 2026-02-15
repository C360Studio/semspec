// Package contexthelper provides a shared helper for requesting context from the context-builder.
// It encapsulates the publish-to-subject/wait-for-KV-response pattern used by multiple components.
package contexthelper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	kvBucket      string
	timeout       time.Duration
	logger        *slog.Logger
	sourceName    string
}

// Config holds configuration for the context helper.
type Config struct {
	// SubjectPrefix is the base subject for context build requests.
	// Default: "context.build"
	SubjectPrefix string

	// ResponseBucket is the KV bucket where responses are stored.
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

// New creates a new context helper.
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
		natsClient:    natsClient,
		subjectPrefix: cfg.SubjectPrefix,
		kvBucket:      cfg.ResponseBucket,
		timeout:       cfg.Timeout,
		logger:        logger,
		sourceName:    cfg.SourceName,
	}
}

// BuildContext requests context from the centralized context-builder.
// It publishes a request to context.build.<task_type> and waits for a response in the KV bucket.
// Returns nil without error if context building times out or fails gracefully.
func (h *Helper) BuildContext(ctx context.Context, req *contextbuilder.ContextBuildRequest) (*contextbuilder.ContextBuildResponse, error) {
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	var result *contextbuilder.ContextBuildResponse

	// Use retry for transient failures (network issues, temporary KV unavailability)
	retryConfig := retry.DefaultConfig()
	err := retry.Do(ctxTimeout, retryConfig, func() error {
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

// buildContextOnce performs a single context build attempt.
func (h *Helper) buildContextOnce(ctx context.Context, req *contextbuilder.ContextBuildRequest) (*contextbuilder.ContextBuildResponse, error) {
	// Build subject based on task type
	subject := fmt.Sprintf("%s.%s", h.subjectPrefix, req.TaskType)

	// Wrap request in BaseMessage
	baseMsg := message.NewBaseMessage(req.Schema(), req, h.sourceName)
	reqBytes, err := json.Marshal(baseMsg)
	if err != nil {
		return nil, retry.NonRetryable(fmt.Errorf("marshal context request: %w", err))
	}

	// Publish context build request
	if err := h.natsClient.Publish(ctx, subject, reqBytes); err != nil {
		return nil, fmt.Errorf("publish context request: %w", err)
	}

	h.logger.Debug("Published context build request",
		"request_id", req.RequestID,
		"subject", subject,
		"task_type", req.TaskType)

	// Wait for context response from KV bucket
	resp, err := h.waitForContextResponse(ctx, req.RequestID)
	if err != nil {
		return nil, err // Already classified as retryable/non-retryable
	}

	return resp, nil
}

// waitForContextResponse waits for a context build response in the KV bucket using a watcher.
func (h *Helper) waitForContextResponse(ctx context.Context, reqID string) (*contextbuilder.ContextBuildResponse, error) {
	js, err := h.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	// Get KV bucket
	kv, err := js.KeyValue(ctx, h.kvBucket)
	if err != nil {
		return nil, fmt.Errorf("get kv bucket %s: %w", h.kvBucket, err)
	}

	// First, check if the response already exists
	entry, err := kv.Get(ctx, reqID)
	if err == nil {
		return h.parseContextResponse(entry.Value())
	}
	if err != jetstream.ErrKeyNotFound {
		return nil, fmt.Errorf("get response: %w", err)
	}

	// Create watcher for the specific key
	watcher, err := kv.Watch(ctx, reqID)
	if err != nil {
		return nil, fmt.Errorf("create kv watcher: %w", err)
	}
	defer watcher.Stop()

	// Wait for updates via reactive channel
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case entry := <-watcher.Updates():
			if entry == nil {
				// Initial nil signals watcher is ready, continue waiting
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete {
				// Key was deleted, treat as error
				return nil, fmt.Errorf("context response deleted before read")
			}
			return h.parseContextResponse(entry.Value())
		}
	}
}

// parseContextResponse unmarshals and validates a context build response.
func (h *Helper) parseContextResponse(data []byte) (*contextbuilder.ContextBuildResponse, error) {
	var resp contextbuilder.ContextBuildResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, retry.NonRetryable(fmt.Errorf("unmarshal response: %w", err))
	}

	if resp.Error != "" {
		return nil, retry.NonRetryable(fmt.Errorf("context build error: %s", resp.Error))
	}

	return &resp, nil
}
