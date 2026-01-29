package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360/semspec/config"
)

func TestAppStartStop(t *testing.T) {
	// Create a temp directory for repo
	tmpDir, err := os.MkdirTemp("", "semspec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo in temp dir
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Repo.Path = tmpDir

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start the app
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Verify components are initialized
	if app.natsConn == nil {
		t.Error("NATS connection not initialized")
	}
	if app.js == nil {
		t.Error("JetStream not initialized")
	}
	if app.store == nil {
		t.Error("Store not initialized")
	}
	if app.embeddedServer == nil {
		t.Error("Embedded NATS server not started")
	}

	// Shutdown
	app.Shutdown(5 * time.Second)

	// Verify cleanup
	if app.embeddedServer.Running() {
		t.Error("Embedded server still running after shutdown")
	}
}

func TestAppSubmitTask(t *testing.T) {
	// Create a temp directory for repo
	tmpDir, err := os.MkdirTemp("", "semspec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo in temp dir
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Repo.Path = tmpDir

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Shutdown(5 * time.Second)

	// Submit a task
	result, err := app.SubmitTask(ctx, "Test task")
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	if !result.Success {
		t.Errorf("expected task to succeed, got error: %s", result.Error)
	}

	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestAppWithExternalNATS(t *testing.T) {
	// Skip if no external NATS is available
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		t.Skip("Skipping external NATS test: NATS_URL not set")
	}

	// Create a temp directory for repo
	tmpDir, err := os.MkdirTemp("", "semspec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Repo.Path = tmpDir
	cfg.NATS.URL = natsURL
	cfg.NATS.Embedded = false

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Shutdown(5 * time.Second)

	// Verify no embedded server when using external NATS
	if app.embeddedServer != nil {
		t.Error("embedded server should be nil when using external NATS")
	}

	// Verify external connection works
	if app.natsConn == nil {
		t.Error("NATS connection not initialized")
	}
}

func TestToolExecutors(t *testing.T) {
	// Create a temp directory for repo
	tmpDir, err := os.MkdirTemp("", "semspec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Repo.Path = tmpDir

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Verify file executor tools
	fileTools := app.fileExecutor.ListTools()
	if len(fileTools) != 3 {
		t.Errorf("expected 3 file tools, got %d", len(fileTools))
	}

	expectedFileTools := map[string]bool{"file_read": false, "file_write": false, "file_list": false}
	for _, tool := range fileTools {
		if _, ok := expectedFileTools[tool.Name]; ok {
			expectedFileTools[tool.Name] = true
		}
	}
	for name, found := range expectedFileTools {
		if !found {
			t.Errorf("missing file tool: %s", name)
		}
	}

	// Verify git executor tools
	gitTools := app.gitExecutor.ListTools()
	if len(gitTools) != 3 {
		t.Errorf("expected 3 git tools, got %d", len(gitTools))
	}

	expectedGitTools := map[string]bool{"git_status": false, "git_branch": false, "git_commit": false}
	for _, tool := range gitTools {
		if _, ok := expectedGitTools[tool.Name]; ok {
			expectedGitTools[tool.Name] = true
		}
	}
	for name, found := range expectedGitTools {
		if !found {
			t.Errorf("missing git tool: %s", name)
		}
	}
}

func TestGracefulShutdown(t *testing.T) {
	// Create a temp directory for repo
	tmpDir, err := os.MkdirTemp("", "semspec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Repo.Path = tmpDir

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Submit a task before shutdown
	_, err = app.SubmitTask(ctx, "Task before shutdown")
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	// Graceful shutdown with timeout
	start := time.Now()
	app.Shutdown(5 * time.Second)
	elapsed := time.Since(start)

	// Shutdown should complete reasonably quickly
	if elapsed > 10*time.Second {
		t.Errorf("shutdown took too long: %v", elapsed)
	}

	// Verify cleanup
	if app.embeddedServer.Running() {
		t.Error("embedded server still running after shutdown")
	}
}
