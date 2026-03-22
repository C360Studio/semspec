package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	"golang.org/x/sync/singleflight"
)

const (
	manifestCacheTTL   = 5 * time.Minute
	manifestTimeout    = 3 * time.Second
	maxManifestBytes   = 1 << 20 // 1 MiB
	predicatesGQLQuery = `{"query":"{ predicates { predicates { predicate entityCount } total } }"}`
)

// excludedPredicateSuffixes are implementation-detail predicates that agents
// should never see or query directly.
var excludedPredicateSuffixes = []string{
	".chunk_index",
	".chunk_count",
	".etag",
	".content_hash",
	".error",
	".raw_content",
}

// GraphManifest summarizes what the knowledge graph contains.
type GraphManifest struct {
	PredicateFamilies   map[string]int // "code": 350, "source": 47
	PredicateCategories map[string]int // "code.function": 142, "code.type": 38
	TotalPredicates     int
}

// HasKnowledge returns true when at least one predicate family has entities.
func (m *GraphManifest) HasKnowledge() bool {
	if m == nil {
		return false
	}
	for _, count := range m.PredicateFamilies {
		if count > 0 {
			return true
		}
	}
	return false
}

// FormatForPrompt produces a human-readable manifest for LLM prompt injection.
func (m *GraphManifest) FormatForPrompt() string {
	if m == nil || len(m.PredicateFamilies) == 0 {
		return ""
	}

	families := make([]string, 0, len(m.PredicateFamilies))
	totalEntities := 0
	for f, count := range m.PredicateFamilies {
		families = append(families, f)
		totalEntities += count
	}
	sort.Strings(families)

	var sb strings.Builder
	sb.WriteString("--- Knowledge Graph Contents ---\n")
	sb.WriteString(fmt.Sprintf("%d predicate families indexed (%d entities):\n\n", len(families), totalEntities))

	for _, fam := range families {
		cats := categoriesForFamily(m.PredicateCategories, fam)
		if len(cats) > 0 {
			sb.WriteString(fmt.Sprintf("  %s (%d): %s\n", fam, m.PredicateFamilies[fam], strings.Join(cats, ", ")))
		} else {
			sb.WriteString(fmt.Sprintf("  %s (%d)\n", fam, m.PredicateFamilies[fam]))
		}
	}

	sb.WriteString("\nUse graph_search for questions, or graph_query\n")
	sb.WriteString("with entitiesByPredicate(predicate: \"...\") for targeted lookups.\n")

	return sb.String()
}

// categoriesForFamily returns sorted "subcategory (count)" strings for a family.
func categoriesForFamily(categories map[string]int, family string) []string {
	prefix := family + "."
	var cats []string
	for cat, count := range categories {
		if suffix, ok := strings.CutPrefix(cat, prefix); ok {
			cats = append(cats, fmt.Sprintf("%s (%d)", suffix, count))
		}
	}
	sort.Strings(cats)
	return cats
}

// ManifestClient fetches and caches a summary of graph-gateway contents.
type ManifestClient struct {
	gatewayURL string
	logger     *slog.Logger
	httpClient *http.Client

	mu       sync.RWMutex
	cached   *GraphManifest
	cachedAt time.Time
	sfGroup  singleflight.Group
}

// NewManifestClient creates a client. Returns nil if gatewayURL is empty.
func NewManifestClient(gatewayURL string, logger *slog.Logger) *ManifestClient {
	gatewayURL = strings.TrimSpace(gatewayURL)
	if gatewayURL == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ManifestClient{
		gatewayURL: gatewayURL,
		logger:     logger,
		httpClient: &http.Client{Timeout: manifestTimeout + 1*time.Second},
	}
}

// Fetch returns the cached manifest if fresh, else queries graph-gateway.
// On fetch failure it returns the stale cached value (graceful degradation).
func (c *ManifestClient) Fetch(ctx context.Context) *GraphManifest {
	c.mu.RLock()
	if c.cached != nil && time.Since(c.cachedAt) < manifestCacheTTL {
		defer c.mu.RUnlock()
		return c.cached
	}
	stale := c.cached
	c.mu.RUnlock()

	v, _, _ := c.sfGroup.Do("fetch", func() (any, error) {
		return c.doFetch(context.WithoutCancel(ctx)), nil
	})
	if fetched, ok := v.(*GraphManifest); ok && fetched != nil {
		c.mu.Lock()
		c.cached = fetched
		c.cachedAt = time.Now()
		c.mu.Unlock()
		return fetched
	}

	return stale // graceful degradation: stale is better than nothing
}

func (c *ManifestClient) doFetch(ctx context.Context) *GraphManifest {
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gatewayURL+"/graphql",
		strings.NewReader(predicatesGQLQuery))
	if err != nil {
		c.logger.Debug("manifest: failed to create request", "error", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Debug("manifest: fetch failed", "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Debug("manifest: non-200 response", "status", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
	if err != nil {
		c.logger.Debug("manifest: failed to read body", "error", err)
		return nil
	}

	var gqlResp struct {
		Data struct {
			Predicates struct {
				Predicates []struct {
					Predicate   string `json:"predicate"`
					EntityCount int    `json:"entityCount"`
				} `json:"predicates"`
				Total int `json:"total"`
			} `json:"predicates"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors,omitempty"`
	}

	if err := json.Unmarshal(body, &gqlResp); err != nil {
		c.logger.Debug("manifest: failed to parse JSON", "error", err)
		return nil
	}

	if len(gqlResp.Errors) > 0 {
		c.logger.Debug("manifest: GraphQL errors", "error", gqlResp.Errors[0].Message)
		return nil
	}

	// Group by family and category, filtering out implementation-detail predicates.
	families := make(map[string]int)
	categories := make(map[string]int)
	filteredCount := 0

	for _, p := range gqlResp.Data.Predicates.Predicates {
		if isExcludedPredicate(p.Predicate) {
			continue
		}

		// Extract family (first dot-separated segment).
		family := p.Predicate
		if idx := strings.IndexByte(p.Predicate, '.'); idx > 0 {
			family = p.Predicate[:idx]
		}

		families[family] += p.EntityCount
		filteredCount++

		// Build two-level category key (e.g. "code.function" from "code.function.main.Run").
		parts := strings.SplitN(p.Predicate, ".", 3)
		if len(parts) >= 2 {
			cat := parts[0] + "." + parts[1]
			categories[cat] += p.EntityCount
		}
	}

	return &GraphManifest{
		PredicateFamilies:   families,
		PredicateCategories: categories,
		TotalPredicates:     filteredCount,
	}
}

// isExcludedPredicate reports whether a predicate is an implementation detail
// that should not be surfaced to agents.
func isExcludedPredicate(predicate string) bool {
	for _, suffix := range excludedPredicateSuffixes {
		if strings.HasSuffix(predicate, suffix) {
			return true
		}
	}
	return false
}

// FormatFederatedSummary produces a compacted multi-source knowledge summary
// for injection into agent system prompts. Each source gets one line showing
// domain breakdowns. This is the "tier 1" of the 3-tier knowledge access
// pattern — agents see what's available without any tool calls.
func FormatFederatedSummary(summaries []gatherers.SourceSummary) string {
	if len(summaries) == 0 {
		return ""
	}

	var sb strings.Builder
	wrote := false

	for _, s := range summaries {
		if s.TotalEntities == 0 {
			continue
		}
		if !wrote {
			sb.WriteString("--- Knowledge Sources ---\n")
			wrote = true
		}
		// Format: [source] N entities (domain: count, domain: count)
		var domains []string
		for _, d := range s.Domains {
			if d.EntityCount > 0 {
				domains = append(domains, fmt.Sprintf("%s: %d", d.Domain, d.EntityCount))
			}
		}
		sort.Strings(domains)

		name := s.Source
		if name == "" {
			name = "unknown"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %d entities (%s)\n", name, s.TotalEntities, strings.Join(domains, ", ")))
	}

	if !wrote {
		return ""
	}

	sb.WriteString("\nUse graph_search for questions about these sources.\n")
	sb.WriteString("Use graph_query with entitiesByPredicate or entity(id) for specific lookups.\n")

	return sb.String()
}

// FederatedManifestFetchFn returns a closure suitable for GraphManifestFragment
// that fetches the federated summary from all connected semsource instances.
// Falls back to the legacy ManifestClient (local graph-gateway only) when
// the global GraphRegistry is not initialized.
func FederatedManifestFetchFn() func() string {
	var (
		mu       sync.RWMutex
		cached   string
		cachedAt time.Time
		sfGroup  singleflight.Group
	)

	return func() string {
		mu.RLock()
		if cached != "" && time.Since(cachedAt) < manifestCacheTTL {
			defer mu.RUnlock()
			return cached
		}
		stale := cached
		mu.RUnlock()

		v, _, _ := sfGroup.Do("federated-manifest", func() (any, error) {
			// Try federated summary first.
			if reg := gatherers.GlobalRegistry(); reg != nil {
				fg := gatherers.NewFederatedGraphGatherer(reg, nil)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				summaries, err := fg.GraphSummary(ctx)
				if err == nil && len(summaries) > 0 {
					return FormatFederatedSummary(summaries), nil
				}
			}
			// Fallback: legacy manifest client (local graph-gateway predicates).
			if mc := GetManifestClient(); mc != nil {
				m := mc.Fetch(context.Background())
				if m != nil && m.HasKnowledge() {
					return m.FormatForPrompt(), nil
				}
			}
			return "", nil
		})

		result, _ := v.(string)
		if result != "" {
			mu.Lock()
			cached = result
			cachedAt = time.Now()
			mu.Unlock()
		}

		if result != "" {
			return result
		}
		return stale
	}
}
