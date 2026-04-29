package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const ollamaProbeTimeout = 3 * time.Second

// ProbeOllamaEndpoints performs a one-shot existence check against every
// ollama-provider endpoint in the registry and WARNs when a model is not
// pulled or the server is unreachable. Probes run concurrently with a
// short per-endpoint timeout so boot is never blocked by a slow Ollama.
//
// This catches the trap that Validate() can't see: structural consistency
// among config entries says nothing about whether the named model is
// actually present on the host. semstreams' agentic-model has its own
// num_ctx probe that fires lazily on first request — this is complementary
// and runs at boot so the operator hears about misconfiguration before
// the first agent loop.
//
// Set SEMSPEC_SKIP_OLLAMA_PROBE=1 to suppress (e.g., CI without Ollama).
func ProbeOllamaEndpoints(ctx context.Context, registry *Registry, logger *slog.Logger) {
	if logger == nil || registry == nil {
		return
	}
	if os.Getenv("SEMSPEC_SKIP_OLLAMA_PROBE") != "" {
		return
	}

	type target struct {
		name string
		cfg  *EndpointConfig
	}
	var targets []target
	registry.mu.RLock()
	for name, cfg := range registry.endpoints {
		if cfg != nil && cfg.Provider == "ollama" {
			targets = append(targets, target{name: name, cfg: cfg})
		}
	}
	registry.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			if err := probeOllamaModel(ctx, t.cfg.URL, t.cfg.Model); err != nil {
				logger.Warn("ollama endpoint probe failed — agent runs may stall on first dispatch",
					slog.String("endpoint", t.name),
					slog.String("model", t.cfg.Model),
					slog.String("url", t.cfg.URL),
					slog.String("error", err.Error()),
					slog.String("hint", "verify with `ollama list` or `ollama pull <model>`"))
				return
			}
			logger.Info("ollama endpoint probe ok",
				slog.String("endpoint", t.name),
				slog.String("model", t.cfg.Model))
		}(t)
	}
	wg.Wait()
}

// probeOllamaModel hits Ollama's native /api/show and returns nil iff the
// model exists on the host. Strips the OpenAI-compat /v1 suffix from the
// configured URL since /api/show lives on the root path.
func probeOllamaModel(ctx context.Context, rawURL, modelName string) error {
	base := strings.TrimSuffix(strings.TrimRight(rawURL, "/"), "/v1")
	if base == "" {
		return fmt.Errorf("endpoint URL is empty")
	}
	if modelName == "" {
		return fmt.Errorf("endpoint model is empty")
	}

	body, err := json.Marshal(map[string]string{"name": modelName})
	if err != nil {
		return err
	}

	probeCtx, cancel := context.WithTimeout(ctx, ollamaProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, base+"/api/show", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("model not pulled on host (HTTP 404)")
	default:
		return fmt.Errorf("unexpected HTTP %d from /api/show", resp.StatusCode)
	}
}
