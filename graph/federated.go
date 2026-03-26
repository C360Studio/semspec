package graph

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// FederatedGraphGatherer fans out graph queries to multiple sources (local + semsource)
// and merges results. Each source is queried via its own Gatherer instance.
type FederatedGraphGatherer struct {
	registry *Registry
	logger   *slog.Logger

	// Cache of per-URL Gatherer instances.
	gatherers sync.Map // URL → *Gatherer
}

// NewFederatedGraphGatherer creates a federated gatherer backed by the registry.
func NewFederatedGraphGatherer(registry *Registry, logger *slog.Logger) *FederatedGraphGatherer {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederatedGraphGatherer{
		registry: registry,
		logger:   logger.With("component", "federated-graph"),
	}
}

// getGatherer returns a cached Gatherer for a source URL.
func (f *FederatedGraphGatherer) getGatherer(url string) *Gatherer {
	if v, ok := f.gatherers.Load(url); ok {
		return v.(*Gatherer)
	}
	g := NewGraphGatherer(url)
	f.gatherers.Store(url, g)
	return g
}

// QueryEntitiesByPredicate fans out to all ready sources and merges results.
func (f *FederatedGraphGatherer) QueryEntitiesByPredicate(ctx context.Context, predicatePrefix string) ([]Entity, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		entities []Entity
		source   string
		err      error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			entities, err := f.getGatherer(src.URL).QueryEntitiesByPredicate(queryCtx, predicatePrefix)
			results <- result{entities: entities, source: src.Name, err: err}
		}()
	}

	var merged []Entity
	seen := make(map[string]bool)
	var firstErr error

	for range sources {
		r := <-results
		if r.err != nil {
			f.logger.Debug("Source query failed, continuing with others",
				"source", r.source, "error", r.err)
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		for _, e := range r.entities {
			if !seen[e.ID] {
				seen[e.ID] = true
				merged = append(merged, e)
			}
		}
	}

	// Return results even if some sources failed — graceful degradation.
	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// QueryEntitiesByIDPrefix fans out to all ready sources.
func (f *FederatedGraphGatherer) QueryEntitiesByIDPrefix(ctx context.Context, idPrefix string) ([]Entity, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		entities []Entity
		source   string
		err      error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			entities, err := f.getGatherer(src.URL).QueryEntitiesByIDPrefix(queryCtx, idPrefix)
			results <- result{entities: entities, source: src.Name, err: err}
		}()
	}

	var merged []Entity
	seen := make(map[string]bool)
	var firstErr error

	for range sources {
		r := <-results
		if r.err != nil {
			f.logger.Debug("Source query failed", "source", r.source, "error", r.err)
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		for _, e := range r.entities {
			if !seen[e.ID] {
				seen[e.ID] = true
				merged = append(merged, e)
			}
		}
	}

	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// GetEntity fans out to all sources, returns first match.
func (f *FederatedGraphGatherer) GetEntity(ctx context.Context, entityID string) (*Entity, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		entity *Entity
		source string
		err    error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			entity, err := f.getGatherer(src.URL).GetEntity(queryCtx, entityID)
			results <- result{entity: entity, source: src.Name, err: err}
		}()
	}

	var firstErr error
	for range sources {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if r.entity != nil {
			return r.entity, nil
		}
	}

	return nil, firstErr
}

// Ping checks if at least one source is reachable.
func (f *FederatedGraphGatherer) Ping(ctx context.Context) error {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil
	}

	// Try local first (fastest).
	for _, src := range sources {
		if src.IsLocal {
			return f.getGatherer(src.URL).Ping(ctx)
		}
	}
	// Fall back to any source.
	return f.getGatherer(sources[0].URL).Ping(ctx)
}

// WaitForReady waits for at least one source to be reachable.
func (f *FederatedGraphGatherer) WaitForReady(ctx context.Context, budget time.Duration) error {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil
	}

	// Try local graph first.
	for _, src := range sources {
		if src.IsLocal {
			return f.getGatherer(src.URL).WaitForReady(ctx, budget)
		}
	}
	return f.getGatherer(sources[0].URL).WaitForReady(ctx, budget)
}

// HydrateEntity fans out to all sources, returns first successful hydration.
func (f *FederatedGraphGatherer) HydrateEntity(ctx context.Context, entityID string, depth int) (string, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return "", nil
	}

	type result struct {
		content string
		err     error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			content, err := f.getGatherer(src.URL).HydrateEntity(queryCtx, entityID, depth)
			results <- result{content: content, err: err}
		}()
	}

	var firstErr error
	for range sources {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if r.content != "" {
			return r.content, nil
		}
	}
	return "", firstErr
}

// GetCodebaseSummary fans out to all sources and concatenates summaries.
func (f *FederatedGraphGatherer) GetCodebaseSummary(ctx context.Context) (string, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return "", nil
	}

	type result struct {
		summary string
		source  string
		err     error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			summary, err := f.getGatherer(src.URL).GetCodebaseSummary(queryCtx)
			results <- result{summary: summary, source: src.Name, err: err}
		}()
	}

	var parts []string
	for range sources {
		r := <-results
		if r.err != nil {
			f.logger.Debug("Source codebase summary failed", "source", r.source, "error", r.err)
			continue
		}
		if r.summary != "" {
			parts = append(parts, r.summary)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// TraverseRelationships fans out to all sources and merges results.
func (f *FederatedGraphGatherer) TraverseRelationships(ctx context.Context, startEntity, predicate, direction string, depth int) ([]Entity, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		entities []Entity
		source   string
		err      error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			entities, err := f.getGatherer(src.URL).TraverseRelationships(queryCtx, startEntity, predicate, direction, depth)
			results <- result{entities: entities, source: src.Name, err: err}
		}()
	}

	var merged []Entity
	seen := make(map[string]bool)
	var firstErr error

	for range sources {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		for _, e := range r.entities {
			if !seen[e.ID] {
				seen[e.ID] = true
				merged = append(merged, e)
			}
		}
	}

	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// QueryProjectSources fans out to all sources and merges results.
func (f *FederatedGraphGatherer) QueryProjectSources(ctx context.Context, projectID string) ([]Entity, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		entities []Entity
		source   string
		err      error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			entities, err := f.getGatherer(src.URL).QueryProjectSources(queryCtx, projectID)
			results <- result{entities: entities, source: src.Name, err: err}
		}()
	}

	var merged []Entity
	seen := make(map[string]bool)
	var firstErr error

	for range sources {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		for _, e := range r.entities {
			if !seen[e.ID] {
				seen[e.ID] = true
				merged = append(merged, e)
			}
		}
	}

	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// GraphSummary fans out to all ready sources and merges the summaries.
func (f *FederatedGraphGatherer) GraphSummary(ctx context.Context) ([]SourceSummary, error) {
	sources := f.registry.ReadySources()
	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		summaries []SourceSummary
		source    string
		err       error
	}

	results := make(chan result, len(sources))
	timeout := f.registry.QueryTimeout()

	for _, src := range sources {
		src := src
		go func() {
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			summaries, err := f.getGatherer(src.URL).GraphSummary(queryCtx)
			results <- result{summaries: summaries, source: src.Name, err: err}
		}()
	}

	var merged []SourceSummary
	for range sources {
		r := <-results
		if r.err != nil {
			f.logger.Debug("Source graph summary failed", "source", r.source, "error", r.err)
			continue
		}
		merged = append(merged, r.summaries...)
	}

	return merged, nil
}

// LocalGatherer returns the local graph gatherer for direct access.
// Used by components that only need the local graph (e.g., entity triple writes).
func (f *FederatedGraphGatherer) LocalGatherer() *Gatherer {
	var localURL string
	f.registry.sources.Range(func(_, value any) bool {
		src := value.(*Source)
		if src.IsLocal {
			localURL = src.URL
			return false
		}
		return true
	})
	if localURL == "" {
		return nil
	}
	return f.getGatherer(localURL)
}
