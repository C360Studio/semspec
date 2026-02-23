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
