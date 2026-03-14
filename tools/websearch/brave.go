package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	braveSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"
	maxBraveResults     = 10
	responseBodyLimit   = 100 * 1024 // 100KB
)

// BraveProvider implements SearchProvider using the Brave Search API.
type BraveProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewBraveProvider creates a new Brave Search provider with the given API key.
func NewBraveProvider(apiKey string) *BraveProvider {
	return &BraveProvider{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the provider name.
func (p *BraveProvider) Name() string {
	return "brave"
}

// Search executes a web search query and returns up to maxResults results.
// The count is capped at maxBraveResults (10) regardless of the requested value.
func (p *BraveProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > maxBraveResults {
		maxResults = maxBraveResults
	}

	reqURL, err := url.Parse(braveSearchEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("count", strconv.Itoa(maxResults))
	reqURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Subscription-Token", p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, responseBodyLimit))
		return nil, fmt.Errorf("brave search returned %d: %s", resp.StatusCode, string(body))
	}

	// Cap response body to prevent unbounded memory consumption.
	var raw braveResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, responseBodyLimit)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([]SearchResult, 0, len(raw.Web.Results))
	for _, r := range raw.Web.Results {
		results = append(results, SearchResult{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}
	return results, nil
}

// braveResponse is the top-level Brave Search API response shape.
type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}
