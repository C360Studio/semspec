// Package llm provides a provider-agnostic LLM client with retry and fallback support.
// It integrates with model.Registry for capability-based model selection and a
// pluggable model.HealthPolicy (default RollingWindowBreaker) for circuit breaking.
package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semspec/model"
	ssmodel "github.com/c360studio/semstreams/model"
)

// maxResponseSize limits the LLM response body to prevent memory exhaustion.
const maxResponseSize = 10 * 1024 * 1024 // 10MB

// Client is a provider-agnostic LLM client with retry and fallback support.
//
// The healthPolicy field tracks per-endpoint circuit-breaker state. The
// default (RollingWindowBreaker) gives strict-superset behaviour relative
// to the prior consecutive-failure tracking that lived on
// semspec/model.Registry — sliding window over recent observations, lazy
// Open→HalfOpen→Closed transitions, half-open thundering-herd protection.
// Callers can swap in their own via WithHealthPolicy (e.g. shared-state
// across processes, or NewAlwaysHealthyPolicy() to disable in tests).
type Client struct {
	registry     *model.Registry
	httpClient   *http.Client
	retryConfig  RetryConfig
	logger       *slog.Logger
	governor     *ConcurrencyGovernor
	healthPolicy ssmodel.HealthPolicy
}

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"`                   // "system", "user", "assistant", or "tool"
	Content    string     `json:"content,omitempty"`      // Message content
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // For assistant messages with tool invocations
	ToolCallID string     `json:"tool_call_id,omitempty"` // For tool result messages
}

// ToolDefinition describes a tool available for the LLM to use.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema for parameters
}

// ToolCall represents an LLM request to invoke a tool.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
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

	// Tools is the list of tools available for the LLM to use.
	// Only sent to tool-capable endpoints.
	Tools []ToolDefinition `json:"-"`

	// ToolChoice controls tool selection behavior.
	// Values: "auto" (let model decide), "required" (must use tool), "none" (no tools).
	ToolChoice string `json:"-"`
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
	// Common values: "stop", "length", "tool_use" (Anthropic), "tool_calls" (OpenAI)
	FinishReason string

	// ToolCalls contains tool invocation requests from the model.
	// Non-empty when FinishReason is "tool_use" or "tool_calls".
	ToolCalls []ToolCall
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

// WithGovernor attaches a ConcurrencyGovernor to the client.
// The governor controls per-endpoint rate limiting and concurrency.
// A nil governor is valid and disables all governing.
func WithGovernor(g *ConcurrencyGovernor) ClientOption {
	return func(c *Client) { c.governor = g }
}

// WithTimeout sets the default HTTP client timeout for all LLM requests.
// 0 means no client-level timeout — the caller's context deadline governs
// duration. Per-endpoint RequestTimeout (if set) layers on top via context.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithHealthPolicy replaces the default in-process RollingWindowBreaker
// with a caller-provided HealthPolicy. Use ssmodel.NewAlwaysHealthyPolicy()
// to disable circuit breaking in tests, or wire a custom policy (e.g. one
// backed by NATS KV for cross-process shared state).
func WithHealthPolicy(p ssmodel.HealthPolicy) ClientOption {
	return func(c *Client) {
		if p != nil {
			c.healthPolicy = p
		}
	}
}

// NewClient creates a new LLM client with the given model registry.
// The HTTP client has no default timeout — callers (e.g., agentic-model)
// control duration via context deadlines. Per-endpoint RequestTimeout
// provides an optional safety cap for cloud endpoints.
func NewClient(registry *model.Registry, opts ...ClientOption) *Client {
	c := &Client{
		registry:     registry,
		retryConfig:  DefaultRetryConfig(),
		httpClient:   &http.Client{},
		logger:       slog.Default(),
		healthPolicy: ssmodel.NewRollingWindowBreaker(ssmodel.BreakerConfig{}),
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

	// Generate request ID for caller correlation
	requestID := uuid.New().String()

	// Parse capability and get the raw fallback chain. The per-endpoint
	// IsHealthy filter is applied below in the iteration so a stale chain
	// snapshot can't leak Open endpoints between Resolve and dispatch.
	capVal := model.ParseCapability(req.Capability)
	if capVal == "" {
		capVal = model.CapabilityFast // Default to fast for unknown capabilities
	}
	chain := c.registry.GetFallbackChain(capVal)

	if len(chain) == 0 {
		return nil, fmt.Errorf("no models configured for capability %s", req.Capability)
	}

	var lastErr error

	for _, modelName := range chain {
		endpoint := c.registry.GetEndpoint(modelName)
		if endpoint == nil {
			c.logger.Debug("No endpoint for model, skipping", "model", modelName)
			continue
		}

		// Check circuit breaker status. RollingWindowBreaker treats endpoints
		// with no recorded observations as Closed, so the first dispatch to
		// a fresh endpoint always proceeds.
		if !c.healthPolicy.IsHealthy(modelName) {
			c.logger.Debug("Endpoint circuit open, skipping", "model", modelName)
			continue
		}

		resp, _, err := c.tryEndpointWithRetryTracked(ctx, endpoint, modelName, req)

		if err == nil {
			// Set request ID on response for caller correlation
			resp.RequestID = requestID
			return resp, nil
		}

		lastErr = err

		c.logger.Warn("Endpoint failed, trying fallback",
			"model", modelName,
			"provider", endpoint.Provider,
			"error", err)

		// Check if error is fatal (non-retryable)
		if IsFatal(err) {
			c.logger.Warn("Fatal error, not trying fallbacks", "error", err)
			return nil, err
		}
	}

	return nil, fmt.Errorf("all endpoints failed for capability %s: %w", req.Capability, lastErr)
}

// tryEndpointWithRetryTracked attempts a request with retry logic and returns the attempt count.
// The concurrency slot (if any) is held across all retry attempts so the caller does not
// lose its turn between retries.
func (c *Client) tryEndpointWithRetryTracked(ctx context.Context, ep *model.EndpointConfig, modelName string, req Request) (*Response, int, error) {
	if c.governor != nil {
		release, err := c.governor.Acquire(ctx, modelName)
		if err != nil {
			return nil, 0, err
		}
		defer release()
	}

	var lastErr error

	for attempt := 1; attempt <= c.retryConfig.MaxAttempts; attempt++ {
		resp, err := c.doRequest(ctx, ep, req)
		if err == nil {
			// Record the success against the breaker. Successful results
			// (including those that needed transient retries above) keep
			// the endpoint Closed.
			c.healthPolicy.RecordResult(modelName, ssmodel.Result{Success: true})
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

	// All retries exhausted — record the failure against the breaker.
	// Kind classification stays coarse (Unknown) for now; a future pass
	// could parse lastErr into Timeout/RateLimit/ServerError/Network so
	// EndpointStats's per-kind counters surface useful detail. Doesn't
	// affect breaker math today (Unknown is treated like ServerError).
	c.healthPolicy.RecordResult(modelName, ssmodel.Result{
		Success: false,
		Kind:    ssmodel.ErrorKindUnknown,
	})

	return nil, c.retryConfig.MaxAttempts, lastErr
}

// calculateBackoff computes exponential backoff duration with jitter.
// Jitter prevents thundering herd when multiple clients retry simultaneously.
func (c *Client) calculateBackoff(attempt int) time.Duration {
	multiplier := 1.0
	for i := 1; i < attempt; i++ {
		multiplier *= c.retryConfig.BackoffMultiplier
	}

	backoff := min(time.Duration(float64(c.retryConfig.BackoffBase)*multiplier), c.retryConfig.MaxBackoff)

	// Add jitter: +/- 25% to prevent synchronized retries
	jitter := float64(backoff) * 0.25 * (rand.Float64()*2 - 1)
	return backoff + time.Duration(jitter)
}

// doRequest executes a single HTTP request to the LLM endpoint.
func (c *Client) doRequest(ctx context.Context, ep *model.EndpointConfig, req Request) (*Response, error) {
	// Fail fast if endpoint explicitly requires an API key that isn't set.
	// Note: SetHeaders reads the env var again — intentional to keep the Provider
	// interface simple (env var name, not resolved value). os.Getenv is trivial.
	if ep.APIKeyEnv != "" && os.Getenv(ep.APIKeyEnv) == "" {
		return nil, NewFatalError(fmt.Errorf("endpoint %q requires %s but it is not set", ep.Model, ep.APIKeyEnv))
	}

	provider := GetProvider(ep.Provider)
	if provider == nil {
		return nil, NewFatalError(fmt.Errorf("unknown provider: %s", ep.Provider))
	}

	// Build request URL
	url := provider.BuildURL(ep.URL)

	// Build request body (pass tools only if endpoint supports them)
	var tools []ToolDefinition
	var toolChoice string
	if ep.SupportsTools && len(req.Tools) > 0 {
		tools = req.Tools
		toolChoice = req.ToolChoice
	}
	var opts *RequestOpts
	if ep.ReasoningEffort != "" {
		opts = &RequestOpts{ReasoningEffort: ep.ReasoningEffort}
	}
	body, err := provider.BuildRequestBody(ep.Model, req.Messages, req.Temperature, req.MaxTokens, tools, toolChoice, opts)
	if err != nil {
		return nil, NewFatalError(fmt.Errorf("build request body: %w", err))
	}

	c.logger.Debug("Sending LLM request",
		"provider", ep.Provider,
		"model", ep.Model,
		"url", url,
		"messages", len(req.Messages),
		"tools", len(tools))

	// Apply per-endpoint request timeout if configured.
	// This layers below the caller's context deadline (e.g., agentic-model timeout).
	if epTimeout := ep.GetRequestTimeout(); epTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, epTimeout)
		defer cancel()
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, NewFatalError(fmt.Errorf("create HTTP request: %w", err))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	provider.SetHeaders(httpReq, ep.APIKeyEnv)

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
