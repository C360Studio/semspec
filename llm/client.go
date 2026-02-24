// Package llm provides a provider-agnostic LLM client with retry and fallback support.
// It integrates with the model.Registry for capability-based model selection.
package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/google/uuid"
)

// maxResponseSize limits the LLM response body to prevent memory exhaustion.
const maxResponseSize = 10 * 1024 * 1024 // 10MB

// Client is a provider-agnostic LLM client with retry and fallback support.
type Client struct {
	registry    *model.Registry
	httpClient  *http.Client
	retryConfig RetryConfig
	logger      *slog.Logger

	// callStore optionally persists LLM calls for trajectory tracking.
	// If nil, call recording is disabled.
	callStore *CallStore
}

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`    // "system", "user", or "assistant"
	Content string `json:"content"` // Message content
}

// Request defines an LLM completion request.
type Request struct {
	// Capability specifies the semantic capability ("planning", "writing", "fast", etc.).
	// The registry resolves this to available models.
	Capability string

	// Messages is the chat history to send to the LLM.
	Messages []Message

	// Temperature controls randomness. nil uses endpoint default, 0 is deterministic.
	Temperature *float64

	// MaxTokens limits response length. 0 uses endpoint default.
	MaxTokens int
}

// TokenUsage represents token consumption details for an LLM call.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response contains the LLM completion result.
type Response struct {
	// RequestID uniquely identifies this LLM call for trajectory correlation.
	// Set by Complete() so callers can thread it through callbacks and events.
	RequestID string

	// Content is the generated text.
	Content string

	// Model is the actual model that was used.
	Model string

	// TokensUsed is the total tokens consumed (if available).
	// Deprecated: Use Usage.TotalTokens instead.
	TokensUsed int

	// Usage contains detailed token consumption metrics.
	Usage TokenUsage

	// FinishReason indicates why generation stopped.
	FinishReason string
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(client *Client) {
		client.httpClient = c
	}
}

// WithRetryConfig sets the retry configuration.
func WithRetryConfig(cfg RetryConfig) ClientOption {
	return func(client *Client) {
		client.retryConfig = cfg
	}
}

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(client *Client) {
		client.logger = logger
	}
}

// WithCallStore sets the LLM call store for trajectory tracking.
// When set, all LLM calls will be recorded with timing and token usage.
func WithCallStore(store *CallStore) ClientOption {
	return func(client *Client) {
		client.callStore = store
	}
}

// NewClient creates a new LLM client with the given model registry.
func NewClient(registry *model.Registry, opts ...ClientOption) *Client {
	c := &Client{
		registry:    registry,
		retryConfig: DefaultRetryConfig(),
		httpClient: &http.Client{
			Timeout: 180 * time.Second, // Allow time for LLM responses
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Complete sends a completion request, handling retry and fallback logic.
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	if req.Capability == "" {
		return nil, fmt.Errorf("capability is required")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("at least one message is required")
	}

	// Generate request ID and capture timing for trajectory tracking
	requestID := uuid.New().String()
	startedAt := time.Now()
	traceCtx := GetTraceContext(ctx)

	// Parse capability and get fallback chain filtered by health
	capVal := model.ParseCapability(req.Capability)
	if capVal == "" {
		capVal = model.CapabilityFast // Default to fast for unknown capabilities
	}
	chain := c.registry.GetAvailableFallbackChain(capVal)

	if len(chain) == 0 {
		return nil, fmt.Errorf("no models configured for capability %s", req.Capability)
	}

	var lastErr error
	var fallbacksUsed []string
	var successModel string
	var successProvider string
	var retries int

	for _, modelName := range chain {
		endpoint := c.registry.GetEndpoint(modelName)
		if endpoint == nil {
			c.logger.Debug("No endpoint for model, skipping", "model", modelName)
			continue
		}

		// Check circuit breaker status
		if !c.registry.IsEndpointAvailable(modelName) {
			c.logger.Debug("Endpoint circuit open, skipping", "model", modelName)
			continue
		}

		resp, attempts, err := c.tryEndpointWithRetryTracked(ctx, endpoint, modelName, req)
		retries += attempts - 1 // First attempt isn't a retry

		if err == nil {
			successModel = modelName
			successProvider = endpoint.Provider

			// Set request ID on response for caller correlation
			resp.RequestID = requestID

			// Record successful call
			c.recordCall(ctx, &CallRecord{
				RequestID:        requestID,
				TraceID:          traceCtx.TraceID,
				LoopID:           traceCtx.LoopID,
				Capability:       req.Capability,
				Model:            resp.Model,
				Provider:         successProvider,
				Messages:         req.Messages,
				Response:         resp.Content,
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				FinishReason:     resp.FinishReason,
				StartedAt:        startedAt,
				CompletedAt:      time.Now(),
				DurationMs:       time.Since(startedAt).Milliseconds(),
				Retries:          retries,
				FallbacksUsed:    fallbacksUsed,
				ContextBudget:    endpoint.MaxTokens,
			})

			return resp, nil
		}

		// Track this as a fallback attempt
		fallbacksUsed = append(fallbacksUsed, modelName)
		lastErr = err

		c.logger.Warn("Endpoint failed, trying fallback",
			"model", modelName,
			"provider", endpoint.Provider,
			"error", err)

		// Check if error is fatal (non-retryable)
		if IsFatal(err) {
			c.logger.Warn("Fatal error, not trying fallbacks", "error", err)

			// Record failed call
			c.recordCall(ctx, &CallRecord{
				RequestID:     requestID,
				TraceID:       traceCtx.TraceID,
				LoopID:        traceCtx.LoopID,
				Capability:    req.Capability,
				Model:         modelName,
				Provider:      endpoint.Provider,
				Messages:      req.Messages,
				StartedAt:     startedAt,
				CompletedAt:   time.Now(),
				DurationMs:    time.Since(startedAt).Milliseconds(),
				Error:         err.Error(),
				Retries:       retries,
				FallbacksUsed: fallbacksUsed,
				ContextBudget: endpoint.MaxTokens,
			})

			return nil, err
		}
	}

	// Record failed call (all endpoints exhausted)
	c.recordCall(ctx, &CallRecord{
		RequestID:     requestID,
		TraceID:       traceCtx.TraceID,
		LoopID:        traceCtx.LoopID,
		Capability:    req.Capability,
		Model:         successModel,    // Empty if all failed
		Provider:      successProvider, // Empty if all failed
		Messages:      req.Messages,
		StartedAt:     startedAt,
		CompletedAt:   time.Now(),
		DurationMs:    time.Since(startedAt).Milliseconds(),
		Error:         fmt.Sprintf("all endpoints failed: %v", lastErr),
		Retries:       retries,
		FallbacksUsed: fallbacksUsed,
	})

	return nil, fmt.Errorf("all endpoints failed for capability %s: %w", req.Capability, lastErr)
}

// recordCall stores an LLM call record if the call store is configured.
// Failures are logged but don't affect the LLM call itself.
func (c *Client) recordCall(ctx context.Context, record *CallRecord) {
	if c.callStore == nil {
		return
	}

	if err := c.callStore.Store(ctx, record); err != nil {
		c.logger.Warn("Failed to record LLM call",
			"request_id", record.RequestID,
			"trace_id", record.TraceID,
			"capability", record.Capability,
			"error", err)
	}
}

// tryEndpointWithRetry attempts a request with retry logic.
// Deprecated: Use tryEndpointWithRetryTracked instead for trajectory tracking.
func (c *Client) tryEndpointWithRetry(ctx context.Context, ep *model.EndpointConfig, modelName string, req Request) (*Response, error) {
	resp, _, err := c.tryEndpointWithRetryTracked(ctx, ep, modelName, req)
	return resp, err
}

// tryEndpointWithRetryTracked attempts a request with retry logic and returns the attempt count.
func (c *Client) tryEndpointWithRetryTracked(ctx context.Context, ep *model.EndpointConfig, modelName string, req Request) (*Response, int, error) {
	var lastErr error

	for attempt := 1; attempt <= c.retryConfig.MaxAttempts; attempt++ {
		resp, err := c.doRequest(ctx, ep, req)
		if err == nil {
			// Mark endpoint as healthy on success
			c.registry.MarkEndpointSuccess(modelName)
			return resp, attempt, nil
		}

		lastErr = err

		// Don't retry fatal errors
		if IsFatal(err) {
			// Fatal errors may indicate config issues, not endpoint health
			// Don't mark as unhealthy for auth/bad request errors
			return nil, attempt, err
		}

		// Check if we should retry
		if attempt < c.retryConfig.MaxAttempts {
			backoff := c.calculateBackoff(attempt)
			c.logger.Debug("Request failed, retrying",
				"attempt", attempt,
				"max_attempts", c.retryConfig.MaxAttempts,
				"backoff", backoff,
				"error", err)

			select {
			case <-ctx.Done():
				return nil, attempt, ctx.Err()
			case <-time.After(backoff):
				// Continue to retry
			}
		}
	}

	// All retries exhausted - mark endpoint as unhealthy
	c.registry.MarkEndpointFailure(modelName)

	return nil, c.retryConfig.MaxAttempts, lastErr
}

// calculateBackoff computes exponential backoff duration with jitter.
// Jitter prevents thundering herd when multiple clients retry simultaneously.
func (c *Client) calculateBackoff(attempt int) time.Duration {
	multiplier := 1.0
	for i := 1; i < attempt; i++ {
		multiplier *= c.retryConfig.BackoffMultiplier
	}

	backoff := time.Duration(float64(c.retryConfig.BackoffBase) * multiplier)
	if backoff > c.retryConfig.MaxBackoff {
		backoff = c.retryConfig.MaxBackoff
	}

	// Add jitter: +/- 25% to prevent synchronized retries
	jitter := float64(backoff) * 0.25 * (rand.Float64()*2 - 1)
	return backoff + time.Duration(jitter)
}

// doRequest executes a single HTTP request to the LLM endpoint.
func (c *Client) doRequest(ctx context.Context, ep *model.EndpointConfig, req Request) (*Response, error) {
	provider := GetProvider(ep.Provider)
	if provider == nil {
		return nil, NewFatalError(fmt.Errorf("unknown provider: %s", ep.Provider))
	}

	// Build request URL
	url := provider.BuildURL(ep.URL)

	// Build request body
	body, err := provider.BuildRequestBody(ep.Model, req.Messages, req.Temperature, req.MaxTokens)
	if err != nil {
		return nil, NewFatalError(fmt.Errorf("build request body: %w", err))
	}

	c.logger.Debug("Sending LLM request",
		"provider", ep.Provider,
		"model", ep.Model,
		"url", url,
		"messages", len(req.Messages))

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, NewFatalError(fmt.Errorf("create HTTP request: %w", err))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	provider.SetHeaders(httpReq)

	// Execute request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Network errors are transient
		return nil, NewTransientError(fmt.Errorf("HTTP request failed: %w", err))
	}
	defer httpResp.Body.Close()

	// Read response body with size limit to prevent memory exhaustion
	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseSize))
	if err != nil {
		return nil, NewTransientError(fmt.Errorf("read response body: %w", err))
	}

	// Handle HTTP errors
	if httpResp.StatusCode != http.StatusOK {
		return nil, classifyHTTPError(httpResp.StatusCode, respBody)
	}

	// Parse response
	return provider.ParseResponse(respBody, ep.Model)
}

// classifyHTTPError determines if an HTTP error is transient or fatal.
func classifyHTTPError(statusCode int, body []byte) error {
	bodyStr := string(body)
	if len(bodyStr) > 200 {
		bodyStr = bodyStr[:200] + "..."
	}

	err := fmt.Errorf("LLM API error (status %d): %s", statusCode, bodyStr)

	switch {
	case statusCode == http.StatusTooManyRequests:
		// Rate limiting is transient
		return NewTransientError(err)
	case statusCode == http.StatusServiceUnavailable,
		statusCode == http.StatusBadGateway,
		statusCode == http.StatusGatewayTimeout:
		// Server errors are transient
		return NewTransientError(err)
	case statusCode >= 500:
		// Other 5xx errors are transient
		return NewTransientError(err)
	case statusCode == http.StatusUnauthorized,
		statusCode == http.StatusForbidden:
		// Auth errors are fatal
		return NewFatalError(err)
	case statusCode == http.StatusBadRequest:
		// Bad requests are fatal
		return NewFatalError(err)
	default:
		// Unknown errors default to fatal
		return NewFatalError(err)
	}
}
