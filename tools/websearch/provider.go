// Package websearch provides a web search tool backed by the Brave Search API.
package websearch

import "context"

// SearchProvider is the interface for web search backends.
type SearchProvider interface {
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
	Name() string
}

// SearchResult is a single web search result.
type SearchResult struct {
	Title       string
	URL         string
	Description string
}
