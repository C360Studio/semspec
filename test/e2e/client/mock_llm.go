package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MockLLMClient provides operations against the mock LLM server for e2e testing.
// It communicates directly with the mock-llm container (not via semspec gateway).
type MockLLMClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMockLLMClient creates a new client for the mock LLM server.
func NewMockLLMClient(baseURL string) *MockLLMClient {
	return &MockLLMClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// MockStats contains call statistics from the mock LLM server.
type MockStats struct {
	TotalCalls   int64            `json:"total_calls"`
	CallsByModel map[string]int64 `json:"calls_by_model"`
}

// MockChatMessage represents a single message in an LLM request.
type MockChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MockCapturedRequest represents a captured LLM request from the mock server.
type MockCapturedRequest struct {
	Model     string            `json:"model"`
	Messages  []MockChatMessage `json:"messages"`
	CallIndex int               `json:"call_index"`
	Timestamp int64             `json:"timestamp"`
}

// MockRequests contains captured requests from the mock LLM server.
type MockRequests struct {
	RequestsByModel map[string][]MockCapturedRequest `json:"requests_by_model"`
}

// GetStats retrieves call statistics from the mock LLM server.
func (c *MockLLMClient) GetStats(ctx context.Context) (*MockStats, error) {
	url := fmt.Sprintf("%s/stats", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var stats MockStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &stats, nil
}

// GetRequests retrieves captured request bodies from the mock LLM server.
// Optional model filter returns requests for a specific model only.
func (c *MockLLMClient) GetRequests(ctx context.Context, model string) (*MockRequests, error) {
	url := fmt.Sprintf("%s/requests", c.baseURL)
	if model != "" {
		url += "?model=" + model
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var requests MockRequests
	if err := json.Unmarshal(body, &requests); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &requests, nil
}

// GetRequestsByCall retrieves captured requests for a specific model and call index (1-indexed).
// This uses the mock server's ?model=X&call=N query parameters.
func (c *MockLLMClient) GetRequestsByCall(ctx context.Context, model string, call int) ([]MockCapturedRequest, error) {
	url := fmt.Sprintf("%s/requests?model=%s&call=%d", c.baseURL, model, call)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var requests MockRequests
	if err := json.Unmarshal(body, &requests); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	// Return requests for the specified model (should be filtered by server)
	if reqs, ok := requests.RequestsByModel[model]; ok {
		return reqs, nil
	}
	return nil, nil
}
