// Package main implements the sandbox HTTP server for isolated agent task execution.
// It manages git worktrees so each task runs in an isolated branch, receives
// file and command requests from semspec agents, and merges completed work back
// into the main repository.
//
// The repository is volume-mounted at a configurable path (default /repo).
// Worktrees are created at {repo}/.semspec/worktrees/{task_id}/.
//
// Usage:
//
//	sandbox -addr :8090 -repo /repo
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

func main() {
	addr := flag.String("addr", ":8090", "HTTP listen address")
	repoPath := flag.String("repo", "/repo", "Path to the mounted repository")
	defaultTimeout := flag.Duration("timeout", 30*time.Second, "Default command execution timeout")
	maxTimeout := flag.Duration("max-timeout", 5*time.Minute, "Maximum allowed command timeout")
	cleanupInterval := flag.Duration("cleanup-interval", 1*time.Hour, "Interval for stale worktree cleanup")
	cleanupAge := flag.Duration("cleanup-age", 24*time.Hour, "Remove worktrees older than this")
	maxOutputBytes := flag.Int("max-output", 100*1024, "Maximum stdout/stderr capture size in bytes")
	maxFileSize := flag.Int64("max-file-size", 1*1024*1024, "Maximum file size for write operations")
	natsURL := flag.String("nats-url", "", "NATS server URL for QA event subscription (empty = disable NATS)")
	flag.Parse()

	// Environment variable override for NATS URL (consistent with semspec convention).
	if *natsURL == "" {
		*natsURL = os.Getenv("NATS_URL")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Verify repo exists and is a git repository.
	if _, err := os.Stat(filepath.Join(*repoPath, ".git")); err != nil {
		slog.Error("repo path is not a git repository", "path", *repoPath, "error", err)
		os.Exit(1)
	}

	// Ensure HEAD is valid. If the repo has no commits, create an initial
	// commit so that worktree operations (which reference HEAD) always work.
	if err := ensureHEAD(context.Background(), *repoPath); err != nil {
		slog.Error("failed to ensure valid HEAD in repository", "path", *repoPath, "error", err)
		os.Exit(1)
	}

	// Ensure worktree parent directory exists.
	worktreeRoot := filepath.Join(*repoPath, ".semspec", "worktrees")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		slog.Error("failed to create worktree root", "path", worktreeRoot, "error", err)
		os.Exit(1)
	}

	srv := &Server{
		repoPath:       *repoPath,
		worktreeRoot:   worktreeRoot,
		defaultTimeout: *defaultTimeout,
		maxTimeout:     *maxTimeout,
		maxOutputBytes: *maxOutputBytes,
		maxFileSize:    *maxFileSize,
		logger:         logger,
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: *maxTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.CleanupLoop(ctx, *cleanupInterval, *cleanupAge)

	// Optional NATS subscriber for unit-level QA requests.
	// When -nats-url is empty and NATS_URL is unset, the sandbox runs HTTP-only
	// — existing callers are unaffected.
	connectNATSQA(ctx, srv, *natsURL, logger)

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down sandbox server")
		cancel() // cancels ctx, which stops the NATS consumer and cleanup loop
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	slog.Info("sandbox server starting", "addr", *addr, "repo", *repoPath)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

// connectNATSQA connects to NATS and starts the QA unit-mode subscriber.
// When url is empty, logs a notice and returns — sandbox operates HTTP-only.
// On any connection error, logs and exits so the process doesn't start
// partially configured.
func connectNATSQA(ctx context.Context, srv *Server, url string, logger *slog.Logger) {
	if url == "" {
		slog.Info("NATS not configured — running HTTP-only (set -nats-url or NATS_URL to enable QA subscriber)")
		return
	}

	nc, err := natsclient.NewClient(url,
		natsclient.WithName("sandbox"),
		natsclient.WithMaxReconnects(-1),
		natsclient.WithReconnectWait(time.Second),
	)
	if err != nil {
		slog.Error("failed to create NATS client", "url", url, "error", err)
		os.Exit(1)
	}
	if err := nc.Connect(ctx); err != nil {
		slog.Error("failed to connect to NATS", "url", url, "error", err)
		os.Exit(1)
	}

	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()
	if err := nc.WaitForConnection(connCtx); err != nil {
		slog.Error("NATS connection timed out", "url", url, "error", err)
		os.Exit(1)
	}
	slog.Info("NATS connected — QA subscriber enabled", "url", url)

	if err := startQASubscriber(ctx, srv, nc, logger); err != nil {
		slog.Error("failed to start QA subscriber", "error", err)
		os.Exit(1)
	}
}
