// Package main implements the semspec CLI - a semantic development agent.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/c360/semspec/config"
)

// Build information (set via ldflags)
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath string
		natsURL    string
	)

	rootCmd := &cobra.Command{
		Use:   "semspec [task]",
		Short: "Semantic development agent",
		Long: `Semspec is a semantic development agent that helps with software engineering tasks.

Run without arguments for interactive REPL mode, or provide a task for one-shot execution.`,
		Version: fmt.Sprintf("%s (built %s)", Version, BuildTime),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(cmd.Context(), configPath, natsURL, args)
		},
	}

	rootCmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	rootCmd.Flags().StringVar(&natsURL, "nats-url", "", "NATS server URL (default: embedded)")

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return rootCmd.ExecuteContext(ctx)
}

func runAgent(ctx context.Context, configPath, natsURL string, args []string) error {
	// Create a quiet logger for config loading
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Load configuration
	loader := config.NewLoader(logger)

	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Override NATS URL if provided
	if natsURL != "" {
		cfg.NATS.URL = natsURL
		cfg.NATS.Embedded = false
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Check Ollama connectivity
	if err := checkOllama(ctx, cfg.Model.Endpoint); err != nil {
		return fmt.Errorf("ollama not available at %s: %w\n\nMake sure Ollama is running: ollama serve", cfg.Model.Endpoint, err)
	}

	// Determine mode
	if len(args) > 0 {
		// One-shot mode
		task := args[0]
		return runOneShot(ctx, cfg, task)
	}

	// REPL mode
	return runREPL(ctx, cfg)
}

func checkOllama(ctx context.Context, endpoint string) error {
	// Simple HTTP check to verify Ollama is running
	// This is a placeholder - actual implementation would make HTTP request
	_ = endpoint
	// For now, return nil to allow development without Ollama
	return nil
}

func runOneShot(ctx context.Context, cfg *config.Config, task string) error {
	fmt.Printf("Running task: %s\n", task)
	fmt.Printf("Model: %s\n", cfg.Model.Default)
	fmt.Printf("Repo: %s\n", cfg.Repo.Path)

	// Initialize components
	app, err := NewApp(cfg)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer app.Shutdown(5 * time.Second)

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start app: %w", err)
	}

	// Submit task and wait for completion
	result, err := app.SubmitTask(ctx, task)
	if err != nil {
		return fmt.Errorf("task failed: %w", err)
	}

	if !result.Success {
		fmt.Fprintf(os.Stderr, "Task failed: %s\n", result.Error)
		return fmt.Errorf("task failed")
	}

	fmt.Println(result.Output)
	return nil
}

func runREPL(ctx context.Context, cfg *config.Config) error {
	fmt.Println("Semspec - Semantic Development Agent")
	fmt.Printf("Model: %s\n", cfg.Model.Default)
	fmt.Printf("Repo: %s\n", cfg.Repo.Path)
	fmt.Println("Type 'quit' or 'exit' to exit, or press Ctrl+D")
	fmt.Println()

	// Initialize components
	app, err := NewApp(cfg)
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer app.Shutdown(5 * time.Second)

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start app: %w", err)
	}

	// Run REPL loop
	return app.RunREPL(ctx)
}
