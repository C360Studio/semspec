package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultIndexingBudget is the default time to wait for a commit to be indexed.
	DefaultIndexingBudget = 60 * time.Second

	indexingPollCap      = 8 * time.Second // max backoff interval
	indexingQueryTimeout = 3 * time.Second // per-query HTTP timeout
	maxIndexingBytes     = 1 << 20         // 1 MiB response limit

	// GraphQL query to find commit entities by predicate.
	// NOTE: This fetches ALL commit entities. As the graph accumulates history,
	// this response grows. The maxIndexingBytes limit (1 MiB) provides a safety
	// cap. Future improvement: use a targeted entity ID query once the commit
	// entity ID format (org.semsource.git.system.commit.<sha7>) is known.
	commitQueryGQL = `{"query":"{ entitiesByPredicate(predicate: \"source.git.commit.sha\") { id predicates { predicate object } } }"}`
)

// IndexingGate checks whether semsource has indexed a specific commit
// by querying graph-gateway for the commit entity.
type IndexingGate struct {
	graphGatewayURL string
	httpClient      *http.Client
	logger          *slog.Logger
}

// IndexingGateOption configures an IndexingGate.
type IndexingGateOption func(*IndexingGate)

// WithIndexingQueryTimeout overrides the per-query HTTP timeout (default 3s + 1s buffer).
func WithIndexingQueryTimeout(d time.Duration) IndexingGateOption {
	return func(g *IndexingGate) {
		if d > 0 {
			g.httpClient = &http.Client{Timeout: d}
		}
	}
}

// NewIndexingGate creates a gate. Returns nil if gatewayURL is empty.
func NewIndexingGate(gatewayURL string, logger *slog.Logger, opts ...IndexingGateOption) *IndexingGate {
	gatewayURL = strings.TrimSpace(gatewayURL)
	if gatewayURL == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	g := &IndexingGate{
		graphGatewayURL: gatewayURL,
		httpClient:      &http.Client{Timeout: indexingQueryTimeout + time.Second},
		logger:          logger,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// AwaitCommitIndexed polls graph-gateway until an entity with
// source.git.commit.sha matching commitSHA exists, or the budget is exhausted.
// Returns nil on success, an error on timeout or context cancellation.
//
// Backoff: 1s, 2s, 4s, 8s, 8s, 8s... (capped at indexingPollCap).
// A nil receiver is a no-op (returns nil immediately).
func (g *IndexingGate) AwaitCommitIndexed(ctx context.Context, commitSHA string, budget time.Duration) error {
	if g == nil {
		return nil
	}

	deadline := time.Now().Add(budget)
	backoff := 1 * time.Second

	g.logger.Debug("indexing gate: waiting for commit",
		"commit", commitSHA,
		"budget", budget)

	for {
		if g.isCommitIndexed(ctx, commitSHA) {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("commit %s not indexed after %s", commitSHA[:min(12, len(commitSHA))], budget)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, indexingPollCap)
	}
}

// isCommitIndexed queries graph-gateway for a commit entity matching the SHA.
func (g *IndexingGate) isCommitIndexed(ctx context.Context, commitSHA string) bool {
	queryCtx, cancel := context.WithTimeout(ctx, indexingQueryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(queryCtx, http.MethodPost,
		g.graphGatewayURL+"/graphql",
		strings.NewReader(commitQueryGQL))
	if err != nil {
		g.logger.Debug("indexing gate: request creation failed", "error", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		g.logger.Debug("indexing gate: query failed", "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.logger.Debug("indexing gate: non-200 response", "status", resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexingBytes))
	if err != nil {
		g.logger.Debug("indexing gate: read body failed", "error", err)
		return false
	}

	return containsCommitSHA(body, commitSHA)
}

// containsCommitSHA parses a GraphQL response and checks if any entity
// has a source.git.commit.sha predicate matching the target SHA.
func containsCommitSHA(body []byte, targetSHA string) bool {
	var gqlResp struct {
		Data struct {
			EntitiesByPredicate []struct {
				Predicates []struct {
					Predicate string `json:"predicate"`
					Object    string `json:"object"`
				} `json:"predicates"`
			} `json:"entitiesByPredicate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return false
	}

	for _, entity := range gqlResp.Data.EntitiesByPredicate {
		for _, p := range entity.Predicates {
			if p.Predicate == "source.git.commit.sha" && p.Object == targetSHA {
				return true
			}
		}
	}
	return false
}
