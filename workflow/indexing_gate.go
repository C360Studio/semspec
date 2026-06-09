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

	// commitSHAPredicate is the vocabulary predicate semsource attaches to a git
	// commit entity carrying the FULL 40-char SHA (semsource handler/git emits
	// both source.git.commit.sha [full] and .short_sha). The graph-gateway
	// entitiesByPredicate resolver supports a `value:` argument that filters
	// server-side on this predicate's object, so we pass the full SHA directly.
	commitSHAPredicate = "source.git.commit.sha"
)

// commitQueryGQL builds the graph-gateway GraphQL request that asks for the
// entity IDs whose source.git.commit.sha predicate equals fullSHA.
//
// graph-gateway's entitiesByPredicate returns a list of entity-ID STRINGS
// (wire shape {"data":{"entitiesByPredicate":{"entities":[...]}}}), not entity
// objects — so the query carries no sub-selection. The `value:` arg lets the
// resolver filter by the SHA object server-side; we still verify the returned
// IDs client-side (see containsCommitSHA) so correctness does not depend on the
// value filter being honored. fullSHA is hex, so no escaping is required.
func commitQueryGQL(fullSHA string) string {
	return fmt.Sprintf(
		`{"query":"{ entitiesByPredicate(predicate: \"%s\", value: \"%s\") }"}`,
		commitSHAPredicate, fullSHA)
}

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
		strings.NewReader(commitQueryGQL(commitSHA)))
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

// containsCommitSHA parses a graph-gateway entitiesByPredicate response and
// reports whether the target commit is indexed.
//
// The resolver returns a list of entity-ID STRINGS, not entity objects. semsource
// builds the commit entity ID with the SHORT (7-char) sha
// (e.g. semspec.semsource.git.workspace.commit.<sha7>), while AwaitCommitIndexed
// is handed a full SHA. We therefore match the ".commit.<sha7>" suffix rather
// than a full-string compare — which also makes the result correct even if the
// gateway ignores the value: filter and returns every commit ID.
//
// The confirmed wire shape is {"data":{"entitiesByPredicate":{"entities":[...]}}}.
// We also tolerate a bare {"entitiesByPredicate":[...]} array, since the
// introspection schema advertises [String] and gateway versions may differ.
func containsCommitSHA(body []byte, targetSHA string) bool {
	short := targetSHA
	if len(short) > 7 {
		short = short[:7]
	}
	if short == "" {
		return false
	}
	suffix := ".commit." + short

	var gqlResp struct {
		Data struct {
			EntitiesByPredicate json.RawMessage `json:"entitiesByPredicate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return false
	}
	raw := gqlResp.Data.EntitiesByPredicate
	if len(raw) == 0 {
		return false
	}

	matches := func(ids []string) bool {
		for _, id := range ids {
			if strings.HasSuffix(id, suffix) {
				return true
			}
		}
		return false
	}

	// Confirmed shape: object with an "entities" string list.
	var obj struct {
		Entities []string `json:"entities"`
	}
	if json.Unmarshal(raw, &obj) == nil && matches(obj.Entities) {
		return true
	}
	// Fallback shape: bare array of entity-ID strings.
	var ids []string
	if json.Unmarshal(raw, &ids) == nil && matches(ids) {
		return true
	}
	return false
}
