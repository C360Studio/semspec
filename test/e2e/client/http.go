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
			Timeout: 120 * time.Second,
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
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Revision  uint64    `json:"revision"`
	Created   time.Time `json:"created"`
	Modified  time.Time `json:"modified"`
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
