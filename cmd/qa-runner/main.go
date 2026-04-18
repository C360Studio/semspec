// Package main implements the qa-runner service.
// qa-runner subscribes to QARequestedEvent{Mode in {integration, full}} and
// executes .github/workflows/qa.yml via nektos/act against a Docker-mounted
// workspace. Phase 3 returns canned success to validate topology; Phase 4
// wires real act invocation.
//
// SECURITY: qa-runner mounts /var/run/docker.sock. It runs TRUSTED code
// (ours). Agent-authored code runs inside containers act spawns, not in
// the qa-runner process itself. qa-runner's blast radius is limited to
// orchestration logic we wrote — not agent-authored test files or app code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

func main() {
	addr := flag.String("addr", ":8091", "HTTP health listen address")
	natsURL := flag.String("nats-url", "", "NATS server URL for QA event subscription (empty = disable, uses NATS_URL env)")
	projectHostPath := flag.String("project-host-path", "", "HOST filesystem path of the project workspace (uses PROJECT_HOST_PATH env)")
	timeout := flag.Duration("timeout", 10*time.Minute, "Default QA run timeout")
	flag.Parse()

	// Environment variable overrides — consistent with semspec / sandbox convention.
	if *natsURL == "" {
		*natsURL = os.Getenv("NATS_URL")
	}
	if *projectHostPath == "" {
		*projectHostPath = os.Getenv("PROJECT_HOST_PATH")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("qa-runner starting",
		"addr", *addr,
		"nats_url", *natsURL,
		"project_host_path", *projectHostPath,
		"timeout", timeout.String())

	if *projectHostPath == "" {
		logger.Error("project-host-path is required — set -project-host-path flag or PROJECT_HOST_PATH env var")
		os.Exit(1)
	}

	// Log the installed act version at startup for toolchain traceability.
	logActVersion(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Optional NATS subscriber — when -nats-url is empty, qa-runner runs
	// HTTP-only (health endpoint only). In practice NATS is always required.
	connectNATSQA(ctx, *natsURL, *projectHostPath, *timeout, logger)

	// Health endpoint — lightweight 200 OK for compose healthcheck.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	httpServer := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down qa-runner")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("qa-runner HTTP server listening", "addr", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

// healthHandler returns 200 OK with a JSON health body.
// Used by compose healthcheck and external readiness probes.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// connectNATSQA connects to NATS and starts the QA integration/full subscriber.
// When url is empty, logs a notice and returns — qa-runner runs HTTP-only.
// On any connection or subscription error, logs and exits.
func connectNATSQA(ctx context.Context, url, projectHostPath string, defaultTimeout time.Duration, logger *slog.Logger) {
	if url == "" {
		logger.Info("NATS not configured — running HTTP-only (set -nats-url or NATS_URL to enable QA subscriber)")
		return
	}

	nc, err := natsclient.NewClient(url,
		natsclient.WithName("qa-runner"),
		natsclient.WithMaxReconnects(-1),
		natsclient.WithReconnectWait(time.Second),
	)
	if err != nil {
		logger.Error("failed to create NATS client", "url", url, "error", err)
		os.Exit(1)
	}
	if err := nc.Connect(ctx); err != nil {
		logger.Error("failed to connect to NATS", "url", url, "error", err)
		os.Exit(1)
	}

	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()
	if err := nc.WaitForConnection(connCtx); err != nil {
		logger.Error("NATS connection timed out", "url", url, "error", err)
		os.Exit(1)
	}
	logger.Info("NATS connected — QA integration/full subscriber enabled", "url", url)

	if err := startQASubscriber(ctx, nc, projectHostPath, defaultTimeout, logger); err != nil {
		logger.Error("failed to start QA subscriber", "error", err)
		os.Exit(1)
	}
}
