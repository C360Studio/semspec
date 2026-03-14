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
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Verify repo exists and is a git repository.
	if _, err := os.Stat(filepath.Join(*repoPath, ".git")); err != nil {
		slog.Error("repo path is not a git repository", "path", *repoPath, "error", err)
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

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down sandbox server")
		cancel()
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
