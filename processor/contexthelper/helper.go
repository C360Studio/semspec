// Package contexthelper provides a shared helper for requesting context from the context-builder.
// It publishes requests to context.build.<task_type> and receives responses via a JetStream
// consumer on context.built.<requestID>.
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
	streamName    string

	// JetStream consumer for responses (replaces KV polling)
	pendingMu sync.Mutex
	pending   map[string]chan *contextbuilder.ContextBuildResponse
}

// Config holds configuration for the context helper.
type Config struct {
	// SubjectPrefix is the base subject for context build requests.
	// Default: "context.build"
	SubjectPrefix string

	// ResponseBucket is kept for backward-compat config parsing but unused.
	// Deprecated: responses are received via JetStream, not KV.
	ResponseBucket string

	// Timeout is the maximum time to wait for a context response.
	// Default: 30s
	Timeout time.Duration

	// SourceName identifies the component making requests (for logging).
	SourceName string

	// StreamName is the JetStream stream containing context.built.> subjects.
	// Default: "AGENT"
	StreamName string
}

// DefaultConfig returns default helper configuration.
func DefaultConfig() Config {
	return Config{
		SubjectPrefix: "context.build",
		Timeout:       30 * time.Second,
		SourceName:    "unknown",
		StreamName:    "AGENT",
	}
}

// New creates a new context helper. Call Start() before using BuildContext().
func New(natsClient *natsclient.Client, cfg Config, logger *slog.Logger) *Helper {
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = DefaultConfig().SubjectPrefix
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultConfig().Timeout
	}
	if cfg.SourceName == "" {
		cfg.SourceName = DefaultConfig().SourceName
	}
	if cfg.StreamName == "" {
		cfg.StreamName = DefaultConfig().StreamName
	}

	return &Helper{
		natsClient:    natsClient,
		subjectPrefix: cfg.SubjectPrefix,
		timeout:       cfg.Timeout,
		logger:        logger,
		sourceName:    cfg.SourceName,
		streamName:    cfg.StreamName,
		pending:       make(map[string]chan *contextbuilder.ContextBuildResponse),
	}
}

// Start sets up a JetStream consumer on context.built.> to receive responses.
// The consumer lifecycle is tied to ctx — it stops when ctx is cancelled.
// Must be called before BuildContext().
func (h *Helper) Start(ctx context.Context) error {
	if err := h.natsClient.ConsumeStream(ctx, h.streamName, "context.built.>", func(msg jetstream.Msg) {
		h.handleResponse(msg)
	}); err != nil {
		return fmt.Errorf("start context response consumer on %s: %w", h.streamName, err)
	}

	h.logger.Debug("Context helper started JetStream consumer",
		"source", h.sourceName,
		"stream", h.streamName,
		"filter", "context.built.>")

	return nil
}

// Stop is a no-op — the consumer lifecycle is managed by the context passed to Start().
func (h *Helper) Stop() {}

// handleResponse processes an incoming context.built.<requestID> message.
func (h *Helper) handleResponse(msg jetstream.Msg) {
	// Extract requestID from subject: context.built.<requestID>
	parts := strings.SplitN(msg.Subject(), ".", 3)
	if len(parts) < 3 {
		h.logger.Warn("Unexpected context response subject", "subject", msg.Subject())
		msg.Ack()
		return
	}
	requestID := parts[2]

	// Look up pending request
	h.pendingMu.Lock()
	ch, ok := h.pending[requestID]
	h.pendingMu.Unlock()

	if !ok {
		// Not our request — another helper instance may handle it, or it's already timed out.
		msg.Ack()
		return
	}

	// Parse BaseMessage-wrapped response
	resp, err := parseBaseMessageResponse(msg.Data())
	if err != nil {
		h.logger.Warn("Failed to parse context response",
			"request_id", requestID,
			"error", err)
		msg.Ack()
		return
	}

	// Deliver response — non-blocking send to avoid deadlock if channel is already closed.
	select {
	case ch <- resp:
	default:
	}

	msg.Ack()
}

// parseBaseMessageResponse unwraps a BaseMessage-encoded ContextBuildResponse.
func parseBaseMessageResponse(data []byte) (*contextbuilder.ContextBuildResponse, error) {
	var baseMsg struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		return nil, fmt.Errorf("unmarshal base message: %w", err)
	}

	var resp contextbuilder.ContextBuildResponse
	if err := json.Unmarshal(baseMsg.Payload, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return &resp, nil
}

// BuildContext requests context from the centralized context-builder.
// It publishes a request to context.build.<task_type> and waits for a response
// on the JetStream context.built.<requestID> subject.
func (h *Helper) BuildContext(ctx context.Context, req *contextbuilder.ContextBuildRequest) (*contextbuilder.ContextBuildResponse, error) {
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	var result *contextbuilder.ContextBuildResponse

	// Use retry for transient failures (network issues, temporary unavailability)
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
	// Register pending request before publishing to avoid race.
	ch := make(chan *contextbuilder.ContextBuildResponse, 1)
	h.pendingMu.Lock()
	h.pending[req.RequestID] = ch
	h.pendingMu.Unlock()

	defer func() {
		h.pendingMu.Lock()
		delete(h.pending, req.RequestID)
		h.pendingMu.Unlock()
	}()

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

	// Wait for response via JetStream consumer
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != "" {
			return nil, retry.NonRetryable(fmt.Errorf("context build error: %s", resp.Error))
		}
		return resp, nil
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
