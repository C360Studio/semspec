// Package client provides test clients for e2e scenarios.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	codeAst "github.com/c360studio/semspec/processor/ast"
)

// HTTPClient provides HTTP operations for e2e tests.
// It communicates with semspec via the HTTP gateway.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient creates a new HTTP client for e2e testing.
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 240 * time.Second,
		},
	}
}

// MessageRequest represents a user message sent via HTTP.
type MessageRequest struct {
	Content     string `json:"content"`
	UserID      string `json:"user_id,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
}

// MessageResponse represents the response from the HTTP gateway.
type MessageResponse struct {
	ResponseID string `json:"response_id,omitempty"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	Error      string `json:"error,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
}

// SendMessage sends a user message via the HTTP gateway.
func (c *HTTPClient) SendMessage(ctx context.Context, content string) (*MessageResponse, error) {
	return c.SendMessageWithOptions(ctx, content, "e2e", fmt.Sprintf("e2e-%d", time.Now().UnixNano()), "e2e-runner")
}

// SendMessageWithOptions sends a user message with custom options.
func (c *HTTPClient) SendMessageWithOptions(ctx context.Context, content, channelType, channelID, userID string) (*MessageResponse, error) {
	req := MessageRequest{
		Content:     content,
		UserID:      userID,
		ChannelType: channelType,
		ChannelID:   channelID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/agentic-dispatch/message", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var msgResp MessageResponse
	if err := json.Unmarshal(body, &msgResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &msgResp, nil
}

// LogEntry represents an entry from the message-logger.
// Matches the semstreams MessageLogEntry struct.
type LogEntry struct {
	Sequence    int64           `json:"sequence"`
	Timestamp   time.Time       `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type,omitempty"`
	MessageID   string          `json:"message_id,omitempty"`
	Summary     string          `json:"summary"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
}

// GetMessageLogEntries retrieves message-logger entries.
func (c *HTTPClient) GetMessageLogEntries(ctx context.Context, limit int, subjectFilter string) ([]LogEntry, error) {
	url := fmt.Sprintf("%s/message-logger/entries?limit=%d", c.baseURL, limit)
	if subjectFilter != "" {
		url += "&subject=" + subjectFilter
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var entries []LogEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return entries, nil
}

// LogStats represents statistics from the message-logger.
type LogStats struct {
	TotalMessages int            `json:"total_messages"`
	SubjectCounts map[string]int `json:"subject_counts"`
	StartTime     time.Time      `json:"start_time"`
	LastMessage   time.Time      `json:"last_message"`
}

// GetMessageLogStats retrieves message-logger statistics.
func (c *HTTPClient) GetMessageLogStats(ctx context.Context) (*LogStats, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/message-logger/stats", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var stats LogStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &stats, nil
}

// KVEntry represents a key-value entry.
type KVEntry struct {
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
	Revision uint64          `json:"revision"`
	Created  time.Time       `json:"created"`
	Modified time.Time       `json:"modified"`
}

// KVEntriesResponse represents the response from /message-logger/kv/{bucket}.
type KVEntriesResponse struct {
	Bucket  string    `json:"bucket"`
	Entries []KVEntry `json:"entries"`
}

// GetKVEntries retrieves KV bucket entries.
func (c *HTTPClient) GetKVEntries(ctx context.Context, bucket string) (*KVEntriesResponse, error) {
	url := fmt.Sprintf("%s/message-logger/kv/%s", c.baseURL, bucket)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var entries KVEntriesResponse
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &entries, nil
}

// GetKVEntry retrieves a single KV entry.
func (c *HTTPClient) GetKVEntry(ctx context.Context, bucket, key string) (*KVEntry, error) {
	url := fmt.Sprintf("%s/message-logger/kv/%s/%s", c.baseURL, bucket, key)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var entry KVEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &entry, nil
}

// HealthCheck checks if the semspec service is healthy.
func (c *HTTPClient) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/readyz", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health check failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// WaitForHealthy waits for the service to become healthy.
func (c *HTTPClient) WaitForHealthy(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for service to be healthy: %w", ctx.Err())
		case <-ticker.C:
			if err := c.HealthCheck(ctx); err == nil {
				return nil
			}
		}
	}
}

// WaitForMessageSubject waits for a message with the given subject pattern to appear in logs.
func (c *HTTPClient) WaitForMessageSubject(ctx context.Context, subjectPrefix string, minCount int) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for messages with subject %s: %w", subjectPrefix, ctx.Err())
		case <-ticker.C:
			entries, err := c.GetMessageLogEntries(ctx, 100, subjectPrefix)
			if err != nil {
				continue
			}
			if len(entries) >= minCount {
				return nil
			}
		}
	}
}

// WaitForLanguageEntities waits for AST entity messages of a specific language to appear in logs.
// It filters entities by the code.artifact.language predicate in the triples.
func (c *HTTPClient) WaitForLanguageEntities(ctx context.Context, language string, minCount int) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s entities: %w", language, ctx.Err())
		case <-ticker.C:
			entries, err := c.GetMessageLogEntries(ctx, 500, "graph.ingest.entity")
			if err != nil {
				continue
			}
			count := countLanguageEntities(entries, language)
			if count >= minCount {
				return nil
			}
		}
	}
}

// countLanguageEntities counts entities with the specified language in their triples.
func countLanguageEntities(entries []LogEntry, language string) int {
	count := 0
	for _, entry := range entries {
		if entry.MessageType != "ast.entity.v1" {
			continue
		}
		if len(entry.RawData) == 0 {
			continue
		}
		var baseMsg map[string]any
		if err := json.Unmarshal(entry.RawData, &baseMsg); err != nil {
			continue
		}
		payload, ok := baseMsg["payload"].(map[string]any)
		if !ok {
			continue
		}
		triples, ok := payload["triples"].([]any)
		if !ok {
			continue
		}
		for _, t := range triples {
			triple, _ := t.(map[string]any)
			pred, _ := triple["predicate"].(string)
			obj, _ := triple["object"].(string)
			if pred == codeAst.CodeLanguage && obj == language {
				count++
				break
			}
		}
	}
	return count
}

// GetMaxSequence returns the current max sequence number from the message-logger.
// This can be used as a baseline to filter for "new" messages.
func (c *HTTPClient) GetMaxSequence(ctx context.Context) (int64, error) {
	entries, err := c.GetMessageLogEntries(ctx, 1, "")
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	return entries[0].Sequence, nil
}

// WaitForNewLanguageEntities waits for NEW entities to appear after the given baseline sequence.
// It filters entities by sequence number to only count those created after the baseline.
func (c *HTTPClient) WaitForNewLanguageEntities(ctx context.Context, language string, minNewCount int, baselineSequence int64) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for new %s entities (baseline seq %d): %w", language, baselineSequence, ctx.Err())
		case <-ticker.C:
			entries, err := c.GetMessageLogEntries(ctx, 5000, "graph.ingest.entity")
			if err != nil {
				continue
			}
			count := countLanguageEntitiesAfterSequence(entries, language, baselineSequence)
			if count >= minNewCount {
				return nil
			}
		}
	}
}

// countLanguageEntitiesAfterSequence counts entities with the specified language
// that have a sequence number greater than the baseline.
func countLanguageEntitiesAfterSequence(entries []LogEntry, language string, baselineSequence int64) int {
	count := 0
	for _, entry := range entries {
		// Only count entries after the baseline
		if entry.Sequence <= baselineSequence {
			continue
		}
		if entry.MessageType != "ast.entity.v1" {
			continue
		}
		if len(entry.RawData) == 0 {
			continue
		}
		var baseMsg map[string]any
		if err := json.Unmarshal(entry.RawData, &baseMsg); err != nil {
			continue
		}
		payload, ok := baseMsg["payload"].(map[string]any)
		if !ok {
			continue
		}
		triples, ok := payload["triples"].([]any)
		if !ok {
			continue
		}
		for _, t := range triples {
			triple, _ := t.(map[string]any)
			pred, _ := triple["predicate"].(string)
			obj, _ := triple["object"].(string)
			if pred == codeAst.CodeLanguage && obj == language {
				count++
				break
			}
		}
	}
	return count
}

// ContextBuilderResponse represents a context build response.
type ContextBuilderResponse struct {
	RequestID    string `json:"request_id"`
	TaskType     string `json:"task_type"`
	TokenCount   int    `json:"token_count"`
	TokensUsed   int    `json:"tokens_used"`
	TokensBudget int    `json:"tokens_budget"`
	Truncated    bool   `json:"truncated"`
	Error        string `json:"error,omitempty"`
}

// GetContextBuilderResponse retrieves a context response by request ID.
// Returns the response, HTTP status code, and any error.
func (c *HTTPClient) GetContextBuilderResponse(ctx context.Context, requestID string) (*ContextBuilderResponse, int, error) {
	url := fmt.Sprintf("%s/context-builder/responses/%s", c.baseURL, requestID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var ctxResp ContextBuilderResponse
	if err := json.Unmarshal(body, &ctxResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &ctxResp, resp.StatusCode, nil
}

// SynthesisResult represents a review synthesis result.
type SynthesisResult struct {
	Verdict   string `json:"verdict"`
	Passed    bool   `json:"passed"`
	Summary   string `json:"summary"`
	Findings  []any  `json:"findings"`
	Reviewers []any  `json:"reviewers"`
}

// GetPlanReviews retrieves review synthesis results for a plan slug.
// Returns the result, HTTP status code, and any error.
func (c *HTTPClient) GetPlanReviews(ctx context.Context, slug string) (*SynthesisResult, int, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/reviews", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result SynthesisResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, resp.StatusCode, nil
}

// Trajectory represents aggregated data about an agent loop's LLM interactions.
type Trajectory struct {
	LoopID     string            `json:"loop_id"`
	TraceID    string            `json:"trace_id,omitempty"`
	Steps      int               `json:"steps"`
	ToolCalls  int               `json:"tool_calls"`
	ModelCalls int               `json:"model_calls"`
	TokensIn   int               `json:"tokens_in"`
	TokensOut  int               `json:"tokens_out"`
	DurationMs int64             `json:"duration_ms"`
	Status     string            `json:"status,omitempty"`
	StartedAt  *time.Time        `json:"started_at,omitempty"`
	EndedAt    *time.Time        `json:"ended_at,omitempty"`
	Entries    []TrajectoryEntry `json:"entries,omitempty"`
}

// StorageRef references an artifact in ObjectStore.
type StorageRef struct {
	StorageInstance string `json:"storage_instance,omitempty"`
	Key             string `json:"key,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	Size            int64  `json:"size,omitempty"`
}

// TrajectoryEntry represents a single event in the trajectory.
type TrajectoryEntry struct {
	Type            string      `json:"type"`
	Timestamp       time.Time   `json:"timestamp"`
	DurationMs      int64       `json:"duration_ms,omitempty"`
	Model           string      `json:"model,omitempty"`
	Provider        string      `json:"provider,omitempty"`
	Capability      string      `json:"capability,omitempty"`
	TokensIn        int         `json:"tokens_in,omitempty"`
	TokensOut       int         `json:"tokens_out,omitempty"`
	FinishReason    string      `json:"finish_reason,omitempty"`
	Error           string      `json:"error,omitempty"`
	Retries         int         `json:"retries,omitempty"`
	MessagesCount   int         `json:"messages_count,omitempty"`
	ResponsePreview string      `json:"response_preview,omitempty"`
	RequestID       string      `json:"request_id,omitempty"`
	StorageRef      *StorageRef `json:"storage_ref,omitempty"`
}

// GetTrajectoryByLoop retrieves trajectory data for a specific loop.
// Returns the trajectory, HTTP status code, and any error.
func (c *HTTPClient) GetTrajectoryByLoop(ctx context.Context, loopID string, includeEntries bool) (*Trajectory, int, error) {
	format := "summary"
	if includeEntries {
		format = "json"
	}

	url := fmt.Sprintf("%s/trajectory-api/loops/%s?format=%s", c.baseURL, loopID, format)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var trajectory Trajectory
	if err := json.Unmarshal(body, &trajectory); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &trajectory, resp.StatusCode, nil
}

// GetTrajectoryByTrace retrieves trajectory data for a specific trace.
// Returns the trajectory, HTTP status code, and any error.
func (c *HTTPClient) GetTrajectoryByTrace(ctx context.Context, traceID string, includeEntries bool) (*Trajectory, int, error) {
	format := "summary"
	if includeEntries {
		format = "json"
	}

	url := fmt.Sprintf("%s/trajectory-api/traces/%s?format=%s", c.baseURL, traceID, format)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var trajectory Trajectory
	if err := json.Unmarshal(body, &trajectory); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &trajectory, resp.StatusCode, nil
}

// FullLLMCall represents the complete LLM call record from ObjectStore.
type FullLLMCall struct {
	RequestID        string       `json:"request_id"`
	TraceID          string       `json:"trace_id"`
	Capability       string       `json:"capability"`
	Model            string       `json:"model"`
	Provider         string       `json:"provider"`
	Messages         []LLMMessage `json:"messages"`
	Response         string       `json:"response"`
	PromptTokens     int          `json:"prompt_tokens"`
	CompletionTokens int          `json:"completion_tokens"`
	TotalTokens      int          `json:"total_tokens"`
	FinishReason     string       `json:"finish_reason"`
	DurationMs       int64        `json:"duration_ms"`
	Error            string       `json:"error,omitempty"`
}

// LLMMessage represents a chat message in an LLM call.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GetFullLLMCall retrieves the complete LLM call record including full messages
// and response from the trajectory-api /calls/ endpoint.
func (c *HTTPClient) GetFullLLMCall(ctx context.Context, requestID, traceID string) (*FullLLMCall, int, error) {
	url := fmt.Sprintf("%s/trajectory-api/calls/%s?trace_id=%s", c.baseURL, requestID, traceID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var call FullLLMCall
	if err := json.Unmarshal(body, &call); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &call, resp.StatusCode, nil
}

// Question represents a knowledge gap question from the Q&A system.
type Question struct {
	ID            string     `json:"id"`
	FromAgent     string     `json:"from_agent"`
	Topic         string     `json:"topic"`
	Question      string     `json:"question"`
	Context       string     `json:"context,omitempty"`
	BlockedLoopID string     `json:"blocked_loop_id,omitempty"`
	TraceID       string     `json:"trace_id,omitempty"`
	Urgency       string     `json:"urgency"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	Deadline      *time.Time `json:"deadline,omitempty"`
	AssignedTo    string     `json:"assigned_to,omitempty"`
	AssignedAt    time.Time  `json:"assigned_at,omitempty"`
	AnsweredAt    *time.Time `json:"answered_at,omitempty"`
	Answer        string     `json:"answer,omitempty"`
	AnsweredBy    string     `json:"answered_by,omitempty"`
	AnswererType  string     `json:"answerer_type,omitempty"`
	Confidence    string     `json:"confidence,omitempty"`
	Sources       string     `json:"sources,omitempty"`
}

// ListQuestionsResponse represents the response from GET /workflow-api/questions.
type ListQuestionsResponse struct {
	Questions []*Question `json:"questions"`
	Total     int         `json:"total"`
}

// ListQuestions retrieves questions with optional filters.
// status: pending, answered, timeout, all (default: pending)
// topic: filter by topic pattern (e.g., "requirements.*")
// limit: max results (default: 50, max: 1000)
func (c *HTTPClient) ListQuestions(ctx context.Context, status, topic string, limit int) (*ListQuestionsResponse, error) {
	// Note: trailing slash is required to avoid 301 redirect from Go's ServeMux
	url := fmt.Sprintf("%s/workflow-api/questions/?", c.baseURL)

	params := make([]string, 0)
	if status != "" {
		params = append(params, "status="+status)
	}
	if topic != "" {
		params = append(params, "topic="+topic)
	}
	if limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", limit))
	}

	for i, p := range params {
		if i > 0 {
			url += "&"
		}
		url += p
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var listResp ListQuestionsResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &listResp, nil
}

// GetQuestion retrieves a single question by ID.
func (c *HTTPClient) GetQuestion(ctx context.Context, id string) (*Question, error) {
	url := fmt.Sprintf("%s/workflow-api/questions/%s", c.baseURL, id)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var question Question
	if err := json.Unmarshal(body, &question); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &question, nil
}

// AnswerQuestionRequest is the request body for answering a question.
type AnswerQuestionRequest struct {
	Answer     string `json:"answer"`
	Confidence string `json:"confidence,omitempty"`
	Sources    string `json:"sources,omitempty"`
}

// AnswerQuestion submits an answer to a question.
// Returns the updated question.
func (c *HTTPClient) AnswerQuestion(ctx context.Context, id, answer, confidence, sources string) (*Question, error) {
	url := fmt.Sprintf("%s/workflow-api/questions/%s/answer", c.baseURL, id)

	reqBody := AnswerQuestionRequest{
		Answer:     answer,
		Confidence: confidence,
		Sources:    sources,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var question Question
	if err := json.Unmarshal(body, &question); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &question, nil
}

// QuestionEvent represents an event from the questions SSE stream.
type QuestionEvent struct {
	Type     string    `json:"type"`
	ID       uint64    `json:"id,omitempty"`
	Question *Question `json:"question,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// StreamQuestions connects to the SSE stream and returns a channel of events.
// The channel is closed when the context is cancelled or an error occurs.
// status: optional filter for question status (pending, answered, timeout, all)
func (c *HTTPClient) StreamQuestions(ctx context.Context, status string) (<-chan QuestionEvent, error) {
	url := fmt.Sprintf("%s/workflow-api/questions/stream", c.baseURL)
	if status != "" {
		url += "?status=" + status
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	events := make(chan QuestionEvent, 100)

	go func() {
		defer close(events)
		defer resp.Body.Close()

		reader := resp.Body
		buf := make([]byte, 4096)
		var eventType, eventData string

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := reader.Read(buf)
			if err != nil {
				if err != io.EOF {
					events <- QuestionEvent{Type: "error", Error: err.Error()}
				}
				return
			}

			lines := splitSSELines(string(buf[:n]))
			for _, line := range lines {
				line = trimSSECR(line)

				if line == "" {
					// Empty line = end of event
					if eventType != "" && eventData != "" {
						event := QuestionEvent{Type: eventType}

						// Parse the data based on event type
						if eventType == "question_created" || eventType == "question_answered" || eventType == "question_timeout" {
							var q Question
							if err := json.Unmarshal([]byte(eventData), &q); err == nil {
								event.Question = &q
							}
						}

						select {
						case events <- event:
						case <-ctx.Done():
							return
						}
					}
					eventType = ""
					eventData = ""
					continue
				}

				if len(line) > 6 && line[:6] == "event:" {
					eventType = trimSSESpace(line[6:])
				} else if len(line) > 5 && line[:5] == "data:" {
					eventData = trimSSESpace(line[5:])
				}
			}
		}
	}()

	return events, nil
}

// splitSSELines splits a string by newlines for SSE parsing.
func splitSSELines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSSECR removes trailing carriage return for SSE parsing.
func trimSSECR(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\r' {
		return s[:len(s)-1]
	}
	return s
}

// trimSSESpace removes leading and trailing whitespace for SSE parsing.
func trimSSESpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// ============================================================================
// Workflow API Methods (REST endpoints replacing slash commands)
// ============================================================================

// Plan represents a plan in the workflow system.
type Plan struct {
	ID            string         `json:"id"`
	Slug          string         `json:"slug"`
	Title         string         `json:"title"`
	Goal          string         `json:"goal,omitempty"`
	Context       string         `json:"context,omitempty"`
	Scope         map[string]any `json:"scope,omitempty"`
	Approved      bool           `json:"approved"`
	ApprovedAt    *time.Time     `json:"approved_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Status        string         `json:"status,omitempty"`
	Stage         string         `json:"stage,omitempty"`
	ReviewVerdict           string          `json:"review_verdict,omitempty"`
	ReviewSummary           string          `json:"review_summary,omitempty"`
	ReviewedAt              *time.Time      `json:"reviewed_at,omitempty"`
	ReviewFindings          json.RawMessage `json:"review_findings,omitempty"`
	ReviewFormattedFindings string          `json:"review_formatted_findings,omitempty"`
	ReviewIteration         int             `json:"review_iteration,omitempty"`

	// Phase review fields
	PhasesApproved              bool            `json:"phases_approved,omitempty"`
	PhasesApprovedAt            *time.Time      `json:"phases_approved_at,omitempty"`
	PhaseReviewVerdict          string          `json:"phase_review_verdict,omitempty"`
	PhaseReviewSummary          string          `json:"phase_review_summary,omitempty"`
	PhaseReviewedAt             *time.Time      `json:"phase_reviewed_at,omitempty"`
	PhaseReviewFindings         json.RawMessage `json:"phase_review_findings,omitempty"`
	PhaseReviewFormattedFindings string         `json:"phase_review_formatted_findings,omitempty"`
	PhaseReviewIteration        int             `json:"phase_review_iteration,omitempty"`

	// Task review fields (separate from plan review)
	TaskReviewVerdict           string          `json:"task_review_verdict,omitempty"`
	TaskReviewSummary           string          `json:"task_review_summary,omitempty"`
	TaskReviewedAt              *time.Time      `json:"task_reviewed_at,omitempty"`
	TaskReviewFindings          json.RawMessage `json:"task_review_findings,omitempty"`
	TaskReviewFormattedFindings string          `json:"task_review_formatted_findings,omitempty"`
	TaskReviewIteration         int             `json:"task_review_iteration,omitempty"`

	Description string `json:"description,omitempty"`

	// LLM call history for drill-down from loop iterations to full artifacts
	LLMCallHistory *LLMCallHistory `json:"llm_call_history,omitempty"`
}

// LLMCallHistory tracks LLM request IDs per review iteration.
type LLMCallHistory struct {
	PlanReview  []IterationCalls `json:"plan_review,omitempty"`
	PhaseReview []IterationCalls `json:"phase_review,omitempty"`
	TaskReview  []IterationCalls `json:"task_review,omitempty"`
}

// IterationCalls records the LLM request IDs used during a single review iteration.
type IterationCalls struct {
	Iteration     int      `json:"iteration"`
	LLMRequestIDs []string `json:"llm_request_ids"`
	Verdict       string   `json:"verdict,omitempty"`
}

// Task represents a task within a plan.
type Task struct {
	ID                 string              `json:"id"`
	PlanID             string              `json:"plan_id"`
	PhaseID            string              `json:"phase_id,omitempty"`
	Sequence           int                 `json:"sequence"`
	Description        string              `json:"description"`
	Type               string              `json:"type"`
	Status             string              `json:"status"`
	Files              []string            `json:"files,omitempty"`
	DependsOn          []string            `json:"depends_on,omitempty"`
	AcceptanceCriteria []map[string]string `json:"acceptance_criteria,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	ApprovedBy         string              `json:"approved_by,omitempty"`
	ApprovedAt         *time.Time          `json:"approved_at,omitempty"`
	RejectionReason    string              `json:"rejection_reason,omitempty"`
}

// CreatePlanRequest is the request body for creating a plan.
type CreatePlanRequest struct {
	Description string `json:"description"`
}

// CreatePlanResponse is the response from creating a plan.
type CreatePlanResponse struct {
	Plan      *Plan  `json:"plan,omitempty"`
	Slug      string `json:"slug,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// CreatePlan creates a new plan via the workflow-api.
// POST /workflow-api/plans {"description": "..."}
func (c *HTTPClient) CreatePlan(ctx context.Context, description string) (*CreatePlanResponse, error) {
	reqBody := CreatePlanRequest{Description: description}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/workflow-api/plans", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var planResp CreatePlanResponse
	if err := json.Unmarshal(body, &planResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &planResp, nil
}

// GetPlans retrieves all plans via the workflow-api.
// GET /workflow-api/plans
func (c *HTTPClient) GetPlans(ctx context.Context) ([]*Plan, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/workflow-api/plans", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var plans []*Plan
	if err := json.Unmarshal(body, &plans); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return plans, nil
}

// GetPlan retrieves a single plan by slug.
// GET /workflow-api/plans/{slug}
func (c *HTTPClient) GetPlan(ctx context.Context, slug string) (*Plan, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var plan Plan
	if err := json.Unmarshal(body, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &plan, nil
}

// PromotePlanResponse is the response from promoting a plan.
// This matches the PromoteResponse struct in workflow-api/http.go.
// The response embeds PlanWithStatus (which embeds workflow.Plan) and review findings.
type PromotePlanResponse struct {
	// Plan fields come from the embedded PlanWithStatus → workflow.Plan
	Stage          string `json:"stage,omitempty"`
	ReviewVerdict  string `json:"review_verdict,omitempty"`
	ReviewSummary  string `json:"review_summary,omitempty"`
	ReviewFindings []struct {
		SOPID      string `json:"sop_id"`
		SOPTitle   string `json:"sop_title"`
		Severity   string `json:"severity"`
		Status     string `json:"status"`
		Issue      string `json:"issue,omitempty"`
		Suggestion string `json:"suggestion,omitempty"`
		Evidence   string `json:"evidence,omitempty"`
	} `json:"review_findings,omitempty"`

	// Standard response fields
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`

	// StatusCode is the HTTP status code (not serialized, set by client).
	StatusCode int `json:"-"`
}

// IsApproved returns true if the review verdict is "approved" or empty (no reviewer).
func (r *PromotePlanResponse) IsApproved() bool {
	return r.ReviewVerdict == "" || r.ReviewVerdict == "approved"
}

// NeedsChanges returns true if the review verdict is "needs_changes".
func (r *PromotePlanResponse) NeedsChanges() bool {
	return r.ReviewVerdict == "needs_changes"
}

// PromotePlan promotes (approves) a plan via the workflow-api.
// POST /workflow-api/plans/{slug}/promote
// Returns the response for both 200 (approved) and 422 (needs_changes) — both are valid.
func (c *HTTPClient) PromotePlan(ctx context.Context, slug string) (*PromotePlanResponse, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/promote", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// 422 is a valid response (plan review rejected), not an error
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var promoteResp PromotePlanResponse
	if err := json.Unmarshal(body, &promoteResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}
	promoteResp.StatusCode = resp.StatusCode

	return &promoteResp, nil
}

// ExecutePlanResponse is the response from executing a plan.
type ExecutePlanResponse struct {
	BatchID string `json:"batch_id,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ExecutePlan executes a plan via the workflow-api.
// POST /workflow-api/plans/{slug}/execute
func (c *HTTPClient) ExecutePlan(ctx context.Context, slug string) (*ExecutePlanResponse, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/execute", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var execResp ExecutePlanResponse
	if err := json.Unmarshal(body, &execResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &execResp, nil
}

// GetPlanTasks retrieves tasks for a plan via the workflow-api.
// GET /workflow-api/plans/{slug}/tasks
func (c *HTTPClient) GetPlanTasks(ctx context.Context, slug string) ([]*Task, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tasks []*Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return tasks, nil
}

// GenerateTasksResponse is the response from triggering task generation.
type GenerateTasksResponse struct {
	Slug      string `json:"slug,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GenerateTasks triggers task generation for a plan via the workflow-api.
// POST /workflow-api/plans/{slug}/tasks/generate
func (c *HTTPClient) GenerateTasks(ctx context.Context, slug string) (*GenerateTasksResponse, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks/generate", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var genResp GenerateTasksResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &genResp, nil
}

// GetTasks retrieves all tasks for a plan.
// GET /workflow-api/plans/{slug}/tasks
func (c *HTTPClient) GetTasks(ctx context.Context, slug string) ([]*Task, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tasks []*Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return tasks, nil
}

// ApproveTaskRequest is the request body for approving a task.
type ApproveTaskRequest struct {
	ApprovedBy string `json:"approved_by"`
}

// ApproveTask approves a single task.
// POST /workflow-api/plans/{slug}/tasks/{taskID}/approve
func (c *HTTPClient) ApproveTask(ctx context.Context, slug, taskID, approvedBy string) (*Task, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks/%s/approve", c.baseURL, slug, taskID)

	reqBody := ApproveTaskRequest{ApprovedBy: approvedBy}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var task Task
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &task, nil
}

// ApproveTasksResponse represents the response from task approval.
type ApproveTasksResponse struct {
	Stage string `json:"stage,omitempty"`
	Error string `json:"error,omitempty"`
}

// ApproveTasksPlan approves the generated tasks for a plan via the workflow-api.
// POST /workflow-api/plans/{slug}/tasks/approve
func (c *HTTPClient) ApproveTasksPlan(ctx context.Context, slug string) (*ApproveTasksResponse, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks/approve", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var approveResp ApproveTasksResponse
	if err := json.Unmarshal(body, &approveResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &approveResp, nil
}

// RejectTaskRequest represents a request to reject a task.
type RejectTaskRequest struct {
	Reason     string `json:"reason"`
	RejectedBy string `json:"rejected_by,omitempty"`
}

// RejectTask rejects a single task with a reason.
// POST /workflow-api/plans/{slug}/tasks/{taskId}/reject
func (c *HTTPClient) RejectTask(ctx context.Context, slug, taskID, reason, rejectedBy string) (*Task, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks/%s/reject", c.baseURL, slug, taskID)

	reqBody := RejectTaskRequest{Reason: reason, RejectedBy: rejectedBy}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var task Task
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &task, nil
}

// UpdateTaskRequest represents a request to update a task.
type UpdateTaskRequest struct {
	Description        *string  `json:"description,omitempty"`
	Type               *string  `json:"type,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	Files              []string `json:"files,omitempty"`
}

// UpdateTask updates a task's fields.
// PATCH /workflow-api/plans/{slug}/tasks/{taskId}
func (c *HTTPClient) UpdateTask(ctx context.Context, slug, taskID string, req *UpdateTaskRequest) (*Task, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks/%s", c.baseURL, slug, taskID)

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var task Task
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &task, nil
}

// DeleteTask deletes a task.
// DELETE /workflow-api/plans/{slug}/tasks/{taskId}
func (c *HTTPClient) DeleteTask(ctx context.Context, slug, taskID string) error {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/tasks/%s", c.baseURL, slug, taskID)

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================================================
// Project API Methods
// ============================================================================

// ProjectInitStatus represents the initialization status of the project workspace.
// Matches workflow.InitStatus.
type ProjectInitStatus struct {
	Initialized    bool   `json:"initialized"`
	HasProjectJSON bool   `json:"has_project_json"`
	HasChecklist   bool   `json:"has_checklist"`
	HasStandards   bool   `json:"has_standards"`
	SOPCount       int    `json:"sop_count"`
	WorkspacePath  string `json:"workspace_path"`
}

// DetectedLanguage represents a programming language detected in the project.
type DetectedLanguage struct {
	Name       string  `json:"name"`
	Version    *string `json:"version"`
	Marker     string  `json:"marker"`
	Confidence string  `json:"confidence"`
	Primary    bool    `json:"primary,omitempty"`
}

// DetectedFramework represents a framework detected in the project.
type DetectedFramework struct {
	Name       string `json:"name"`
	Language   string `json:"language"`
	Marker     string `json:"marker"`
	Confidence string `json:"confidence"`
}

// DetectedTool represents a build or development tool detected in the project.
type DetectedTool struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Language string `json:"language,omitempty"`
	Marker   string `json:"marker"`
}

// DetectedDoc represents an existing documentation file detected in the project.
type DetectedDoc struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"size_bytes"`
}

// ProjectCheck represents a single entry in the project's quality checklist.
type ProjectCheck struct {
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	Trigger     []string `json:"trigger"`
	Category    string   `json:"category"`
	Required    bool     `json:"required"`
	Timeout     string   `json:"timeout"`
	Description string   `json:"description"`
	WorkingDir  string   `json:"working_dir,omitempty"`
}

// ProjectDetectionResult represents the full result of stack detection.
// Matches workflow.DetectionResult.
type ProjectDetectionResult struct {
	Languages         []DetectedLanguage  `json:"languages"`
	Frameworks        []DetectedFramework `json:"frameworks"`
	Tooling           []DetectedTool      `json:"tooling"`
	ExistingDocs      []DetectedDoc       `json:"existing_docs"`
	ProposedChecklist []ProjectCheck      `json:"proposed_checklist"`
}

// ProjectInitInput contains the core project metadata for initialization.
type ProjectInitInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Languages   []string `json:"languages"`
	Frameworks  []string `json:"frameworks"`
	Repository  string   `json:"repository,omitempty"`
}

// StandardsInput contains the coding standards version and rules.
type StandardsInput struct {
	Version string `json:"version"`
	Rules   []any  `json:"rules"`
}

// ProjectInitRequest is the request body for POST /project-api/init.
type ProjectInitRequest struct {
	Project   ProjectInitInput `json:"project"`
	Checklist []ProjectCheck   `json:"checklist"`
	Standards StandardsInput   `json:"standards"`
}

// ProjectInitResponse is the response from POST /project-api/init.
type ProjectInitResponse struct {
	Success      bool     `json:"success"`
	FilesWritten []string `json:"files_written"`
}

// GetProjectStatus retrieves the project initialization status.
// GET /project-api/status
func (c *HTTPClient) GetProjectStatus(ctx context.Context) (*ProjectInitStatus, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/project-api/status", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var status ProjectInitStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &status, nil
}

// DetectProject runs stack detection on the workspace.
// POST /project-api/detect
func (c *HTTPClient) DetectProject(ctx context.Context) (*ProjectDetectionResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/project-api/detect", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result ProjectDetectionResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, nil
}

// InitProject initializes the project with confirmed settings.
// POST /project-api/init
func (c *HTTPClient) InitProject(ctx context.Context, req *ProjectInitRequest) (*ProjectInitResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/project-api/init", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var initResp ProjectInitResponse
	if err := json.Unmarshal(body, &initResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &initResp, nil
}

// ============================================================================
// Workflow Trajectory Methods
// ============================================================================

// WorkflowTrajectory aggregates LLM call data for an entire workflow.
type WorkflowTrajectory struct {
	Slug              string                   `json:"slug"`
	Status            string                   `json:"status"`
	Phases            map[string]*PhaseMetrics `json:"phases"`
	Totals            *AggregateMetrics        `json:"totals"`
	TraceIDs          []string                 `json:"trace_ids"`
	TruncationSummary *TruncationSummary       `json:"truncation_summary,omitempty"`
	StartedAt         *time.Time               `json:"started_at,omitempty"`
	CompletedAt       *time.Time               `json:"completed_at,omitempty"`
}

// PhaseMetrics contains token metrics for a workflow phase.
type PhaseMetrics struct {
	TokensIn     int                           `json:"tokens_in"`
	TokensOut    int                           `json:"tokens_out"`
	CallCount    int                           `json:"call_count"`
	DurationMs   int64                         `json:"duration_ms"`
	Capabilities map[string]*CapabilityMetrics `json:"capabilities,omitempty"`
}

// CapabilityMetrics contains metrics for a specific capability type.
type CapabilityMetrics struct {
	TokensIn       int `json:"tokens_in"`
	TokensOut      int `json:"tokens_out"`
	CallCount      int `json:"call_count"`
	TruncatedCount int `json:"truncated_count,omitempty"`
}

// AggregateMetrics contains totals across all phases.
type AggregateMetrics struct {
	TokensIn    int   `json:"tokens_in"`
	TokensOut   int   `json:"tokens_out"`
	TotalTokens int   `json:"total_tokens"`
	CallCount   int   `json:"call_count"`
	DurationMs  int64 `json:"duration_ms"`
}

// TruncationSummary summarizes context truncation statistics.
type TruncationSummary struct {
	TotalCalls     int                `json:"total_calls"`
	TruncatedCalls int                `json:"truncated_calls"`
	TruncationRate float64            `json:"truncation_rate"`
	ByCapability   map[string]float64 `json:"by_capability,omitempty"`
}

// GetWorkflowTrajectory retrieves aggregated trajectory data for a workflow.
// GET /trajectory-api/workflows/{slug}
func (c *HTTPClient) GetWorkflowTrajectory(ctx context.Context, slug string) (*WorkflowTrajectory, int, error) {
	url := fmt.Sprintf("%s/trajectory-api/workflows/%s", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var wt WorkflowTrajectory
	if err := json.Unmarshal(body, &wt); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &wt, resp.StatusCode, nil
}

// ============================================================================
// Context Stats Methods
// ============================================================================

// ContextStats provides context utilization metrics across LLM calls.
type ContextStats struct {
	Summary      *ContextSummary                    `json:"summary"`
	ByCapability map[string]*CapabilityContextStats `json:"by_capability"`
	Calls        []CallContextDetail                `json:"calls,omitempty"`
}

// ContextSummary contains aggregate context statistics.
type ContextSummary struct {
	TotalCalls      int     `json:"total_calls"`
	CallsWithBudget int     `json:"calls_with_budget"`
	AvgUtilization  float64 `json:"avg_utilization"`
	TruncationRate  float64 `json:"truncation_rate"`
	TotalBudget     int     `json:"total_budget"`
	TotalUsed       int     `json:"total_used"`
}

// CapabilityContextStats contains context stats for a specific capability.
type CapabilityContextStats struct {
	CallCount      int     `json:"call_count"`
	AvgBudget      int     `json:"avg_budget,omitempty"`
	AvgUsed        int     `json:"avg_used,omitempty"`
	AvgUtilization float64 `json:"avg_utilization"`
	TruncationRate float64 `json:"truncation_rate"`
	MaxUtilization float64 `json:"max_utilization,omitempty"`
}

// CallContextDetail contains context details for a single LLM call.
type CallContextDetail struct {
	RequestID   string    `json:"request_id"`
	TraceID     string    `json:"trace_id,omitempty"`
	Capability  string    `json:"capability"`
	Model       string    `json:"model,omitempty"`
	Budget      int       `json:"budget"`
	Used        int       `json:"used"`
	Utilization float64   `json:"utilization"`
	Truncated   bool      `json:"truncated"`
	Timestamp   time.Time `json:"timestamp"`
}

// GetContextStats retrieves context utilization statistics.
// GET /trajectory-api/context-stats?workflow=slug&format=json
func (c *HTTPClient) GetContextStats(ctx context.Context, workflowSlug string) (*ContextStats, int, error) {
	url := fmt.Sprintf("%s/trajectory-api/context-stats?workflow=%s&format=json", c.baseURL, workflowSlug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var stats ContextStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}

	return &stats, resp.StatusCode, nil
}

// ============================================================================
// Phase Methods
// ============================================================================

// Phase represents a phase within a plan.
type Phase struct {
	ID               string     `json:"id"`
	PlanID           string     `json:"plan_id"`
	Sequence         int        `json:"sequence"`
	Name             string     `json:"name"`
	Description      string     `json:"description,omitempty"`
	DependsOn        []string   `json:"depends_on,omitempty"`
	Status           string     `json:"status"`
	RequiresApproval bool       `json:"requires_approval,omitempty"`
	Approved         bool       `json:"approved,omitempty"`
	ApprovedBy       string     `json:"approved_by,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// GeneratePhasesResponse is the response from triggering phase generation.
type GeneratePhasesResponse struct {
	Slug      string `json:"slug,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GeneratePhases triggers phase generation for a plan via the workflow-api.
// POST /workflow-api/plans/{slug}/phases/generate
func (c *HTTPClient) GeneratePhases(ctx context.Context, slug string) (*GeneratePhasesResponse, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/phases/generate", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var genResp GeneratePhasesResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &genResp, nil
}

// GetPhases retrieves all phases for a plan.
// GET /workflow-api/plans/{slug}/phases
func (c *HTTPClient) GetPhases(ctx context.Context, slug string) ([]*Phase, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/phases", c.baseURL, slug)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var phases []*Phase
	if err := json.Unmarshal(body, &phases); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return phases, nil
}

// ApproveAllPhasesResponse is the response from approving all phases.
type ApproveAllPhasesResponse struct {
	Phases []Phase `json:"phases,omitempty"`
}

// ApproveAllPhases approves all phases for a plan.
// POST /workflow-api/plans/{slug}/phases/approve
func (c *HTTPClient) ApproveAllPhases(ctx context.Context, slug, approvedBy string) ([]*Phase, error) {
	url := fmt.Sprintf("%s/workflow-api/plans/%s/phases/approve", c.baseURL, slug)

	reqBody := struct {
		ApprovedBy string `json:"approved_by"`
	}{ApprovedBy: approvedBy}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var phases []*Phase
	if err := json.Unmarshal(body, &phases); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return phases, nil
}
