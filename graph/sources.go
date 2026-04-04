// Package graph provides graph querying implementations for the knowledge graph.
//
// SourceRegistry is the central registry for graph sources. It manages
// config-driven sources with explicit GraphQL and status URLs, provides
// prefix-based query routing, lazy readiness checking with circuit breaker,
// and cached summary formatting for agent prompt injection.
//
// Ported from semdragon/processor/questbridge/graphsources.go (ADR-032).
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Source represents a queryable graph endpoint.
type Source struct {
	// Name identifies this source (e.g., "local", "workspace").
	Name string `json:"name"`

	// GraphQLURL is the graph-gateway GraphQL endpoint.
	GraphQLURL string `json:"graphql_url"`

	// StatusURL is the semsource readiness endpoint (empty for local sources).
	// GET returns statusPayload with aggregate phase.
	StatusURL string `json:"status_url,omitempty"`

	// Type is "local" (our graph) or "semsource" (external knowledge source).
	Type string `json:"type"`

	// EntityPrefix is the entity ID prefix owned by this source (e.g., "semspec.semsource.").
	// Used for prefix-based routing of entity/relationship queries.
	EntityPrefix string `json:"entity_prefix,omitempty"`

	// AlwaysQuery means this source is queried for every search/nlq query.
	// Local sources are always queried; semsource sources are only queried when ready.
	AlwaysQuery bool `json:"always_query"`

	// URL is a legacy field for backward compatibility. When GraphQLURL is empty
	// but URL is set, GraphQLURL and StatusURL are derived from it.
	URL string `json:"url,omitempty"`

	// ready is set to true only when the source reports phase "ready" or "degraded".
	ready atomic.Bool
	// skipped is set when the circuit breaker trips (3 failures). WaitForReady
	// stops blocking, but checkAndUpdateReady still re-checks on each call.
	skipped atomic.Bool
	// failCount tracks consecutive status check failures for circuit-breaker.
	failCount atomic.Int32
}

// SourceRegistry manages multiple graph sources for query routing.
type SourceRegistry struct {
	sources      []*Source
	queryTimeout time.Duration
	logger       *slog.Logger
	client       *http.Client

	// readinessBudget is the max time to wait for semsource on the first
	// summary fetch. Consumed lazily via readinessOnce — never blocks startup.
	// Set via SetReadinessBudget after construction.
	readinessBudget time.Duration
	readinessOnce   sync.Once

	// Summary cache for prompt injection — keyed by summary URL.
	summaryMu    sync.Mutex
	summaryCache map[string]summCacheEntry

	// Examples cache — keyed by entityPrefix, stores domain→[]entityID maps.
	examplesMu    sync.Mutex
	examplesCache map[string]examplesCacheEntry
}

// examplesCacheEntry caches the domain→entity ID examples for one source prefix.
type examplesCacheEntry struct {
	byDomain map[string][]string
	fetched  time.Time
}

// summCacheEntry holds a parsed semsource summary with its fetch timestamp.
type summCacheEntry struct {
	summary *sourceSummary
	fetched time.Time
}

// sourceSummary mirrors the semsource /summary response JSON.
type sourceSummary struct {
	Namespace      string          `json:"namespace"`
	Phase          string          `json:"phase"`
	EntityIDFormat string          `json:"entity_id_format"`
	TotalEntities  int             `json:"total_entities"`
	Domains        []SummaryDomain `json:"domains"`
}

// SummaryDomain is the per-domain section of a semsource /summary response.
type SummaryDomain struct {
	Domain      string        `json:"domain"`
	EntityCount int           `json:"entity_count"`
	Types       []SummaryType `json:"types"`
	Sources     []string      `json:"sources"`
	ExampleIDs  []string      `json:"example_ids,omitempty"`
}

// SummaryType is the per-entity-type breakdown within a SummaryDomain.
type SummaryType struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// statusPayload matches semsource's StatusPayload schema.
type statusPayload struct {
	Phase         string         `json:"phase"`
	Sources       []sourceStatus `json:"sources"`
	TotalEntities int            `json:"total_entities"`
}

type sourceStatus struct {
	InstanceName string `json:"instance_name"`
	SourceType   string `json:"source_type"`
	Phase        string `json:"phase"`
	EntityCount  int    `json:"entity_count"`
	ErrorCount   int    `json:"error_count"`
}

const summCacheTTL = 5 * time.Minute

// NewSourceRegistry creates a registry from config.
func NewSourceRegistry(sources []Source, logger *slog.Logger) *SourceRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	ptrs := make([]*Source, len(sources))
	for i := range sources {
		ptrs[i] = &sources[i]
		// Backward compat: derive GraphQLURL/StatusURL from URL if needed.
		if ptrs[i].GraphQLURL == "" && ptrs[i].URL != "" {
			ptrs[i].GraphQLURL = strings.TrimRight(ptrs[i].URL, "/") + "/graph-gateway/graphql"
			if ptrs[i].Type == "semsource" && ptrs[i].StatusURL == "" {
				ptrs[i].StatusURL = strings.TrimRight(ptrs[i].URL, "/") + "/source-manifest/status"
			}
		}
		// Local sources are always ready.
		if sources[i].Type == "local" || sources[i].AlwaysQuery {
			ptrs[i].ready.Store(true)
		}
	}
	return &SourceRegistry{
		sources:       ptrs,
		queryTimeout:  3 * time.Second,
		logger:        logger,
		client:        &http.Client{Timeout: 5 * time.Second},
		summaryCache:  make(map[string]summCacheEntry),
		examplesCache: make(map[string]examplesCacheEntry),
	}
}

// SourcesForQuery returns the graph sources that should handle a given query.
// For entity/relationship queries with an entity ID, routes to the matching prefix.
// For search/nlq queries, fans out to all ready sources.
func (r *SourceRegistry) SourcesForQuery(queryType, entityID, prefix string) []*Source {
	switch queryType {
	case "entity", "relationships":
		if entityID != "" {
			if src := r.resolveByPrefix(entityID); src != nil {
				return []*Source{src}
			}
		}
		return r.ReadySources()

	case "prefix":
		if prefix != "" {
			if src := r.resolveByPrefix(prefix); src != nil {
				return []*Source{src}
			}
		}
		return r.ReadySources()

	case "search", "nlq", "predicate":
		return r.ReadySources()

	case "summary":
		return r.SourcesForSummary()

	default:
		return r.ReadySources()
	}
}

// ResolveEntity returns the source that owns a given entity ID, or nil.
func (r *SourceRegistry) ResolveEntity(entityID string) *Source {
	return r.resolveByPrefix(entityID)
}

// GraphQLURLsForQuery returns the GraphQL endpoint URLs to query for a given
// query type and entity context.
func (r *SourceRegistry) GraphQLURLsForQuery(queryType, entityID, prefix string) []string {
	sources := r.SourcesForQuery(queryType, entityID, prefix)
	urls := make([]string, 0, len(sources))
	for _, src := range sources {
		if src.GraphQLURL != "" {
			urls = append(urls, src.GraphQLURL)
		}
	}
	return urls
}

// SummaryURL derives the summary endpoint URL from StatusURL by replacing
// "/status" with "/summary". Returns empty string when StatusURL is empty.
func (s *Source) SummaryURL() string {
	if s.StatusURL == "" {
		return ""
	}
	return strings.Replace(s.StatusURL, "/status", "/summary", 1)
}

// SourcesForSummary returns only ready semsource sources with valid summary URLs.
func (r *SourceRegistry) SourcesForSummary() []*Source {
	var result []*Source
	for _, src := range r.sources {
		if src.Type == "semsource" && src.ready.Load() && src.SummaryURL() != "" {
			result = append(result, src)
		}
	}
	return result
}

// HasSemsources returns true if any semsource-type sources are configured.
func (r *SourceRegistry) HasSemsources() bool {
	for _, src := range r.sources {
		if src.Type == "semsource" {
			return true
		}
	}
	return false
}

// ReadySources returns all sources that are ready to be queried.
func (r *SourceRegistry) ReadySources() []*Source {
	var result []*Source
	for _, src := range r.sources {
		if src.ready.Load() {
			result = append(result, src)
		}
	}
	return result
}

// QueryTimeout returns the per-source query timeout used by FederatedGraphGatherer.
func (r *SourceRegistry) QueryTimeout() time.Duration {
	return r.queryTimeout
}

// SetReadinessBudget configures the max time to wait for semsource readiness
// on the first summary fetch. This gates the first agent prompt assembly,
// not startup. Zero means no waiting.
func (r *SourceRegistry) SetReadinessBudget(d time.Duration) {
	r.readinessBudget = d
}

// LocalGraphQLURL returns the GraphQL URL of the first local source, or empty string.
func (r *SourceRegistry) LocalGraphQLURL() string {
	for _, src := range r.sources {
		if src.Type == "local" && src.GraphQLURL != "" {
			return src.GraphQLURL
		}
	}
	return ""
}

// WaitForReady polls all semsource sources until they report ready.
// Returns nil when all sources are ready, or an error on timeout.
func (r *SourceRegistry) WaitForReady(ctx context.Context, timeout time.Duration) error {
	if !r.HasSemsources() {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Check immediately before entering the loop.
	if r.checkAllReady(ctx) {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			for _, src := range r.sources {
				if src.Type == "semsource" && !src.ready.Load() {
					r.logger.Warn("semsource not ready at timeout",
						"source", src.Name, "status_url", src.StatusURL)
				}
			}
			return fmt.Errorf("semsource readiness timeout after %s", timeout)
		case <-ticker.C:
			if r.checkAllReady(ctx) {
				return nil
			}
		}
	}
}

// checkAllReady polls each semsource source and returns true if all are ready.
func (r *SourceRegistry) checkAllReady(ctx context.Context) bool {
	allReady := true
	for _, src := range r.sources {
		if src.Type != "semsource" || src.ready.Load() || src.skipped.Load() {
			continue
		}
		if src.StatusURL == "" {
			src.ready.Store(true)
			continue
		}

		phase, entities, err := r.fetchStatus(ctx, src.StatusURL)
		if err != nil {
			failures := src.failCount.Add(1)
			r.logger.Debug("semsource status check failed",
				"source", src.Name, "error", err, "consecutive_failures", failures)
			// After 3 consecutive failures, stop blocking WaitForReady but
			// do NOT set ready — checkAndUpdateReady will re-check lazily.
			if failures >= 3 {
				src.skipped.Store(true)
				r.logger.Warn("semsource unreachable after 3 attempts, skipping for now",
					"source", src.Name)
				continue
			}
			allReady = false
			continue
		}
		src.failCount.Store(0)
		src.skipped.Store(false) // Reset skip on successful contact

		switch phase {
		case "ready":
			src.ready.Store(true)
			r.logger.Info("semsource ready",
				"source", src.Name, "entities", entities)
		case "degraded":
			src.ready.Store(true)
			r.logger.Warn("semsource degraded (proceeding with partial data)",
				"source", src.Name, "entities", entities)
		default:
			r.logger.Debug("semsource not yet ready",
				"source", src.Name, "phase", phase, "entities", entities)
			allReady = false
		}
	}
	return allReady
}

// fetchStatus calls a semsource status endpoint and returns aggregate phase + entity count.
func (r *SourceRegistry) fetchStatus(ctx context.Context, statusURL string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return "", 0, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, err
	}

	var status statusPayload
	if err := json.Unmarshal(body, &status); err != nil {
		return "", 0, err
	}

	return status.Phase, status.TotalEntities, nil
}

// checkAndUpdateReady performs a single lazy status check on a semsource source.
func (r *SourceRegistry) checkAndUpdateReady(ctx context.Context, src *Source) bool {
	if src.ready.Load() {
		return true
	}
	if src.StatusURL == "" {
		return false
	}
	phase, _, err := r.fetchStatus(ctx, src.StatusURL)
	if err != nil {
		return false
	}
	if phase == "ready" || phase == "degraded" {
		src.ready.Store(true)
		r.logger.Info("semsource became ready (lazy check)", "source", src.Name, "phase", phase)
		return true
	}
	return false
}

// resolveByPrefix finds the source whose EntityPrefix matches the given ID.
// Falls back to the first local source if no prefix matches.
func (r *SourceRegistry) resolveByPrefix(id string) *Source {
	var localFallback *Source
	for _, src := range r.sources {
		if src.EntityPrefix != "" && strings.HasPrefix(id, src.EntityPrefix) {
			if src.ready.Load() {
				return src
			}
			return nil // Source owns this prefix but isn't ready.
		}
		if src.Type == "local" && localFallback == nil {
			localFallback = src
		}
	}
	return localFallback
}

// fetchedSrc pairs a source with its summary and example IDs for formatting.
type fetchedSrc struct {
	src      *Source
	summary  *sourceSummary
	examples map[string][]string
}

// FormatSummaryForPrompt fetches and formats aggregated graph summary data
// for injection into agent prompts. Covers all semsource sources.
// Results are cached for summCacheTTL (5 minutes).
// Returns empty string when no sources have data.
func (r *SourceRegistry) FormatSummaryForPrompt(ctx context.Context) string {
	fetched, totalEntities := r.gatherSummaries(ctx)
	if len(fetched) == 0 {
		return ""
	}
	return formatSummaryText(fetched, totalEntities)
}

// gatherSummaries fetches summaries and examples from all ready semsource sources.
// On the first call, blocks up to readinessBudget waiting for semsource readiness.
func (r *SourceRegistry) gatherSummaries(ctx context.Context) ([]fetchedSrc, int) {
	// Gate first call on semsource readiness (matches semdragon's waitForKnowledgeSources).
	if r.readinessBudget > 0 {
		r.readinessOnce.Do(func() {
			if r.HasSemsources() {
				r.logger.Info("waiting for semsource readiness before first summary fetch",
					"budget", r.readinessBudget)
				if err := r.WaitForReady(ctx, r.readinessBudget); err != nil {
					r.logger.Warn("semsource readiness wait failed, proceeding", "error", err)
				}
			}
		})
	}

	// Sort sources by name for stable output.
	sorted := make([]*Source, len(r.sources))
	copy(sorted, r.sources)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Name < sorted[j-1].Name; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	var fetched []fetchedSrc
	totalEntities := 0

	for _, src := range sorted {
		if src.Type != "semsource" || !r.checkAndUpdateReady(ctx, src) {
			continue
		}
		var sm *sourceSummary
		if summURL := src.SummaryURL(); summURL != "" {
			sm = r.fetchSummaryWithCache(ctx, summURL)
		}
		if sm == nil || sm.TotalEntities == 0 {
			continue
		}
		examples := r.fetchExampleIDs(ctx, src)
		fetched = append(fetched, fetchedSrc{src: src, summary: sm, examples: examples})
		totalEntities += sm.TotalEntities
	}
	return fetched, totalEntities
}

// formatSummaryText renders the gathered summaries into prompt-ready text.
func formatSummaryText(fetched []fetchedSrc, totalEntities int) string {
	var sb strings.Builder
	sb.WriteString("--- Knowledge Graph ---\n")
	sb.WriteString(fmt.Sprintf("%d ", totalEntities))
	if totalEntities == 1 {
		sb.WriteString("entity")
	} else {
		sb.WriteString("entities")
	}
	sb.WriteString(fmt.Sprintf(" indexed from %d ", len(fetched)))
	if len(fetched) == 1 {
		sb.WriteString("source")
	} else {
		sb.WriteString("sources")
	}
	sb.WriteString(".\n\n")

	sb.WriteString("Entity IDs use 6-part dotted notation: org.platform.domain.system.type.instance\n\n")

	for _, f := range fetched {
		prefix := f.src.EntityPrefix
		if prefix != "" && strings.HasSuffix(prefix, ".") {
			prefix = strings.TrimSuffix(prefix, ".")
		}
		if prefix != "" {
			sb.WriteString(fmt.Sprintf("%s (prefix: %s):\n", f.src.Name, prefix))
		} else {
			sb.WriteString(fmt.Sprintf("%s:\n", f.src.Name))
		}

		for _, d := range f.summary.Domains {
			if len(d.Types) == 0 {
				continue
			}
			var typeParts []string
			for _, t := range d.Types {
				typeParts = append(typeParts, fmt.Sprintf("%s (%d)", t.Type, t.Count))
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", d.Domain, strings.Join(typeParts, ", ")))
			for _, ex := range f.examples[d.Domain] {
				sb.WriteString(fmt.Sprintf("    e.g. %s\n", ex))
			}
		}
		sb.WriteString("\n")
	}

	var prefixExample string
	for _, f := range fetched {
		if f.src.EntityPrefix != "" {
			prefixExample = strings.TrimSuffix(f.src.EntityPrefix, ".")
			if len(f.summary.Domains) > 0 {
				prefixExample = prefixExample + "." + f.summary.Domains[0].Domain
			}
			break
		}
	}
	if prefixExample == "" {
		prefixExample = "source.domain"
	}

	sb.WriteString("Query with graph_search:\n")
	sb.WriteString(fmt.Sprintf("  - Use \"prefix\" to scope by source (e.g. %q)\n", prefixExample))
	sb.WriteString("  - Use \"predicate\" for targeted property lookups\n")
	sb.WriteString("  - Use \"nlq\" for natural language questions")

	return sb.String()
}

// fetchExampleIDs queries the local graph source for a sample of entity IDs
// under the given semsource source's prefix, returning up to 3 per domain.
func (r *SourceRegistry) fetchExampleIDs(ctx context.Context, src *Source) map[string][]string {
	if src.EntityPrefix == "" {
		return nil
	}

	cacheKey := src.EntityPrefix
	r.examplesMu.Lock()
	entry, ok := r.examplesCache[cacheKey]
	r.examplesMu.Unlock()

	if ok && time.Since(entry.fetched) < summCacheTTL {
		return entry.byDomain
	}

	// Find the local graph source for the GraphQL query.
	graphqlURL := r.LocalGraphQLURL()
	if graphqlURL == "" {
		return nil
	}

	prefix := strings.TrimSuffix(src.EntityPrefix, ".")
	query := fmt.Sprintf(`{ entitiesByPrefix(prefix: %q, limit: 50) { id } }`, prefix)
	payload, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, graphqlURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Debug("fetchExampleIDs: GraphQL request failed", "source", src.Name, "error", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}

	var gqlResp struct {
		Data struct {
			EntitiesByPrefix []struct {
				ID string `json:"id"`
			} `json:"entitiesByPrefix"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil
	}

	// Clustering internal types/instances that add noise when shown as examples.
	clusteringNames := map[string]bool{"group": true, "container": true, "level": true}

	// Group entity IDs by domain (3rd segment of the 6-part ID).
	byDomain := make(map[string][]string)
	for _, e := range gqlResp.Data.EntitiesByPrefix {
		parts := strings.SplitN(e.ID, ".", 7)
		if len(parts) < 6 {
			continue
		}
		domain := parts[2]
		entityType := parts[4]
		instance := parts[5]
		if clusteringNames[entityType] || clusteringNames[instance] {
			continue
		}
		if len(byDomain[domain]) < 3 {
			byDomain[domain] = append(byDomain[domain], e.ID)
		}
	}

	r.examplesMu.Lock()
	r.examplesCache[cacheKey] = examplesCacheEntry{byDomain: byDomain, fetched: time.Now()}
	r.examplesMu.Unlock()

	return byDomain
}

// SourceSummaryData is the structured per-source summary for API consumers.
type SourceSummaryData struct {
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Ready         bool            `json:"ready"`
	EntityPrefix  string          `json:"entity_prefix,omitempty"`
	TotalEntities int             `json:"total_entities"`
	Domains       []SummaryDomain `json:"domains"`
}

// StructuredSummary returns per-source summary data for all configured semsource sources.
func (r *SourceRegistry) StructuredSummary(ctx context.Context) []SourceSummaryData {
	result := make([]SourceSummaryData, 0, len(r.sources))
	for _, src := range r.sources {
		if src.Type != "semsource" {
			continue
		}
		ready := r.checkAndUpdateReady(ctx, src)
		entry := SourceSummaryData{
			Name:         src.Name,
			Type:         src.Type,
			Ready:        ready,
			EntityPrefix: src.EntityPrefix,
			Domains:      []SummaryDomain{},
		}

		if ready {
			if summURL := src.SummaryURL(); summURL != "" {
				if sm := r.fetchSummaryWithCache(ctx, summURL); sm != nil {
					entry.TotalEntities = sm.TotalEntities
					examples := r.fetchExampleIDs(ctx, src)
					domains := make([]SummaryDomain, len(sm.Domains))
					copy(domains, sm.Domains)
					for i := range domains {
						domains[i].ExampleIDs = examples[domains[i].Domain]
					}
					entry.Domains = domains
				}
			}
		}

		result = append(result, entry)
	}
	return result
}

// SummaryWithText returns both formatted prompt text and structured per-source data.
func (r *SourceRegistry) SummaryWithText(ctx context.Context) (string, []SourceSummaryData) {
	text := r.FormatSummaryForPrompt(ctx)
	sources := r.StructuredSummary(ctx)
	return text, sources
}

// fetchSummaryWithCache retrieves a parsed sourceSummary, serving from cache when fresh.
func (r *SourceRegistry) fetchSummaryWithCache(ctx context.Context, url string) *sourceSummary {
	r.summaryMu.Lock()
	entry, ok := r.summaryCache[url]
	r.summaryMu.Unlock()

	if ok && time.Since(entry.fetched) < summCacheTTL {
		return entry.summary
	}

	sm, err := r.fetchSummary(ctx, url)
	if err != nil {
		r.logger.Debug("failed to fetch semsource summary for prompt", "url", url, "error", err)
		return nil
	}

	r.summaryMu.Lock()
	r.summaryCache[url] = summCacheEntry{summary: sm, fetched: time.Now()}
	r.summaryMu.Unlock()

	return sm
}

// fetchSummary calls a semsource /summary endpoint and parses the response.
func (r *SourceRegistry) fetchSummary(ctx context.Context, summaryURL string) (*sourceSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, summaryURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var sm sourceSummary
	if err := json.Unmarshal(body, &sm); err != nil {
		return nil, fmt.Errorf("parse summary response: %w", err)
	}

	return &sm, nil
}
