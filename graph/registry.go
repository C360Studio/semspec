// Package graph provides graph querying implementations for the knowledge graph.
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Source represents a single graph endpoint (local or semsource).
type Source struct {
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Phase       string    `json:"phase"` // seeding, ready, degraded
	EntityCount int       `json:"entity_count"`
	LastSeen    time.Time `json:"last_seen"`
	IsLocal     bool      `json:"is_local"`
}

// sourceManifestResponse matches semsource's /source-manifest/status response.
type sourceManifestResponse struct {
	Namespace     string `json:"namespace"`
	Phase         string `json:"phase"`
	TotalEntities int    `json:"total_entities"`
	Sources       []struct {
		InstanceName string `json:"instance_name"`
		SourceType   string `json:"source_type"`
		Phase        string `json:"phase"`
		EntityCount  int    `json:"entity_count"`
		ErrorCount   int    `json:"error_count"`
	} `json:"sources"`
	Timestamp string `json:"timestamp"`
}

// Registry tracks available graph endpoints (local + semsource instances).
// Semsource instances are discovered dynamically via manifest polling.
type Registry struct {
	sources         sync.Map // name → *Source
	localURL        string
	semsourceURLs   []semsourceEntry // all semsource sources to poll
	pollInterval    time.Duration
	queryTimeout    time.Duration
	readinessBudget time.Duration // max wait for semsource at first use (0 = no wait)
	logger          *slog.Logger
	httpClient      *http.Client

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// semsourceEntry tracks a semsource instance to poll.
type semsourceEntry struct {
	name string
	url  string
}

// SourceConfig describes a single graph source for the registry.
type SourceConfig struct {
	// Name identifies this source (e.g., "osh-core", "sandbox").
	Name string `json:"name"`
	// URL is the base URL for this source's GraphQL and manifest endpoints.
	URL string `json:"url"`
	// Type is "local" or "semsource". Local sources are always ready and
	// not polled. Semsource sources are polled for readiness.
	Type string `json:"type"` // "local" or "semsource"
}

// RegistryConfig configures the graph source registry.
type RegistryConfig struct {
	// LocalURL is the local graph-gateway endpoint (always present).
	LocalURL string

	// Sources is a list of graph sources to register. Each semsource
	// source is polled independently for readiness.
	Sources []SourceConfig

	// PollInterval is how often to poll semsource manifests (default 30s).
	PollInterval time.Duration

	// QueryTimeout is the per-source timeout for graph queries (default 3s).
	QueryTimeout time.Duration

	// ReadinessBudget is the max time callers should wait for semsource
	// readiness at first use (e.g., manifest fetch). Zero means no waiting.
	// This gates prompt assembly, not component startup.
	ReadinessBudget time.Duration

	Logger *slog.Logger
}

// NewRegistry creates a new graph source registry.
func NewRegistry(cfg RegistryConfig) *Registry {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.QueryTimeout == 0 {
		cfg.QueryTimeout = 3 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := &Registry{
		localURL:        cfg.LocalURL,
		pollInterval:    cfg.PollInterval,
		queryTimeout:    cfg.QueryTimeout,
		readinessBudget: cfg.ReadinessBudget,
		logger:          cfg.Logger.With("component", "graph-registry"),
		httpClient:      &http.Client{Timeout: 5 * time.Second},
	}

	// Local graph is always a source.
	if cfg.LocalURL != "" {
		r.sources.Store("local", &Source{
			Name:    "local",
			URL:     cfg.LocalURL,
			Phase:   "ready",
			IsLocal: true,
		})
	}

	// Register configured sources.
	for _, src := range cfg.Sources {
		if src.Type == "local" {
			if src.URL != cfg.LocalURL {
				r.sources.Store(src.Name, &Source{
					Name:    src.Name,
					URL:     src.URL,
					Phase:   "ready",
					IsLocal: true,
				})
			}
			continue
		}
		r.semsourceURLs = append(r.semsourceURLs, semsourceEntry{
			name: src.Name,
			url:  src.URL,
		})
	}

	return r
}

// Start begins polling semsource instances for graph source discovery.
// No-op if no semsource URLs are configured.
func (r *Registry) Start(ctx context.Context) {
	if len(r.semsourceURLs) == 0 {
		r.logger.Info("No semsource URLs configured, local-only mode")
		return
	}

	ctx, r.cancel = context.WithCancel(ctx)
	r.wg.Add(1)
	go r.pollLoop(ctx)

	r.logger.Info("Graph registry started",
		"semsource_count", len(r.semsourceURLs),
	)
}

// Stop halts the polling loop.
func (r *Registry) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// ReadySources returns all sources with phase == "ready".
func (r *Registry) ReadySources() []*Source {
	var ready []*Source
	r.sources.Range(func(_, value any) bool {
		src := value.(*Source)
		if src.Phase == "ready" || src.IsLocal {
			ready = append(ready, src)
		}
		return true
	})
	return ready
}

// AllSources returns all known sources.
func (r *Registry) AllSources() []*Source {
	var all []*Source
	r.sources.Range(func(_, value any) bool {
		all = append(all, value.(*Source))
		return true
	})
	return all
}

// SemsourceReady returns true if at least one semsource is in "ready" phase.
// Returns true in local-only mode (no semsource configured).
func (r *Registry) SemsourceReady() bool {
	if len(r.semsourceURLs) == 0 {
		return true // local-only mode
	}
	ready := false
	r.sources.Range(func(_, value any) bool {
		src := value.(*Source)
		if !src.IsLocal && src.Phase == "ready" {
			ready = true
			return false // stop iteration
		}
		return true
	})
	return ready
}

// WaitForSemsource blocks until at least one semsource is ready or the budget expires.
// Returns nil immediately in local-only mode.
func (r *Registry) WaitForSemsource(ctx context.Context, budget time.Duration) error {
	if len(r.semsourceURLs) == 0 {
		return nil
	}

	deadline := time.Now().Add(budget)
	backoff := 1 * time.Second
	maxBackoff := 8 * time.Second

	for {
		// Force a poll before checking.
		r.pollOnce()

		if r.SemsourceReady() {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("semsource not ready after %s", budget)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// SemsourceConfigured returns true if at least one semsource URL is configured.
func (r *Registry) SemsourceConfigured() bool {
	return len(r.semsourceURLs) > 0
}

// QueryTimeout returns the configured per-source query timeout.
func (r *Registry) QueryTimeout() time.Duration {
	return r.queryTimeout
}

// ReadinessBudget returns the max time callers should wait for semsource
// readiness at first use. Zero means no waiting.
func (r *Registry) ReadinessBudget() time.Duration {
	return r.readinessBudget
}

// pollLoop periodically fetches the semsource manifest.
func (r *Registry) pollLoop(ctx context.Context) {
	defer r.wg.Done()

	// Initial poll immediately.
	r.pollOnce()

	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce()
		}
	}
}

// pollOnce fetches manifests from all configured semsource instances and
// updates the source registry.
func (r *Registry) pollOnce() {
	for _, entry := range r.semsourceURLs {
		r.pollSource(entry)
	}

	// Remove stale semsource entries (not seen for 2x poll intervals).
	staleThreshold := time.Now().Add(-2 * r.pollInterval)
	r.sources.Range(func(key, value any) bool {
		src := value.(*Source)
		if !src.IsLocal && src.LastSeen.Before(staleThreshold) {
			r.sources.Delete(key)
			r.logger.Info("Removed stale graph source", "name", src.Name)
		}
		return true
	})
}

// pollSource fetches the manifest from a single semsource instance.
func (r *Registry) pollSource(entry semsourceEntry) {
	url := entry.url + "/source-manifest/status"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		r.logger.Debug("Failed to create manifest request", "source", entry.name, "url", url, "error", err)
		return
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.logger.Debug("Failed to fetch semsource manifest", "source", entry.name, "url", url, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		r.logger.Debug("Semsource manifest returned non-200", "source", entry.name, "status", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		r.logger.Debug("Failed to read manifest body", "source", entry.name, "error", err)
		return
	}

	var manifest sourceManifestResponse
	if err := json.Unmarshal(body, &manifest); err != nil {
		r.logger.Debug("Failed to parse manifest", "source", entry.name, "error", err)
		return
	}

	// Use configured name, or derive from namespace.
	name := entry.name
	if name == "" && manifest.Namespace != "" {
		name = "semsource-" + manifest.Namespace
	}
	if name == "" {
		name = "semsource"
	}

	r.sources.Store(name, &Source{
		Name:        name,
		URL:         entry.url,
		Phase:       manifest.Phase,
		EntityCount: manifest.TotalEntities,
		LastSeen:    time.Now(),
	})

	r.logger.Debug("Updated semsource source",
		"name", name,
		"phase", manifest.Phase,
		"entities", manifest.TotalEntities,
	)
}
