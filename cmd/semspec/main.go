// Package main provides the semspec binary entry point.
// Semspec is a semantic development agent that extends semstreams
// with AST indexing and file/git tool capabilities.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/c360/semspec/processor/ast-indexer"
	"github.com/c360/semspec/processor/semspec-tools"
	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
	"github.com/spf13/cobra"
)

const (
	Version   = "0.1.0"
	BuildTime = "dev"
	appName   = "semspec"
)

func main() {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		configPath string
		repoPath   string
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "semspec",
		Short: "Semantic development agent",
		Long: `Semspec is a semantic development agent that extends semstreams
with AST indexing and file/git tool capabilities.

It provides:
- AST indexing for Go code entity extraction
- File operations (read, write, list)
- Git operations (status, branch, commit)

All components communicate via NATS using the semstreams framework.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(configPath, repoPath, logLevel)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Config file path (JSON)")
	cmd.Flags().StringVar(&repoPath, "repo", ".", "Repository path to operate on")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// Version command
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s version %s (build: %s)\n", appName, Version, BuildTime)
		},
	})

	return cmd
}

func run(configPath, repoPath, logLevel string) error {
	// Print banner
	printBanner()

	// Configure logging
	level := slog.LevelInfo
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Resolve repo path
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}

	// Verify repo path exists
	info, err := os.Stat(absRepoPath)
	if err != nil {
		return fmt.Errorf("stat repo path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", absRepoPath)
	}

	// Load configuration
	cfg, err := loadConfig(configPath, absRepoPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Connect to NATS
	ctx := context.Background()
	natsClient, err := connectToNATS(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer natsClient.Close(ctx)

	// Ensure JetStream streams exist
	if err := ensureStreams(ctx, cfg, natsClient, logger); err != nil {
		return err
	}

	slog.Info("Semspec ready",
		"version", Version,
		"repo_path", absRepoPath)

	// Create remaining infrastructure
	metricsRegistry := metric.NewMetricsRegistry()
	platform := extractPlatformMeta(cfg)

	slog.Info("Platform identity configured",
		"org", platform.Org,
		"platform", platform.Platform)

	// Create and populate component registry
	componentRegistry := component.NewRegistry()

	// Register all semstreams components
	slog.Debug("Registering semstreams component factories")
	if err := componentregistry.Register(componentRegistry); err != nil {
		return fmt.Errorf("register semstreams components: %w", err)
	}

	// Register semspec-specific components
	slog.Debug("Registering semspec component factories")
	if err := astindexer.Register(componentRegistry); err != nil {
		return fmt.Errorf("register ast-indexer: %w", err)
	}
	if err := semspectools.Register(componentRegistry); err != nil {
		return fmt.Errorf("register semspec-tools: %w", err)
	}

	factories := componentRegistry.ListFactories()
	slog.Info("Component factories registered", "count", len(factories))

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient:      natsClient,
		MetricsRegistry: metricsRegistry,
		Logger:          logger,
		Platform:        platform,
	}

	// Create and start components from config
	if err := createAndStartComponents(ctx, cfg, componentRegistry, deps); err != nil {
		return err
	}

	slog.Info("All components started successfully")

	// Block until shutdown signal
	signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	<-signalCtx.Done()
	slog.Info("Received shutdown signal")

	// Stop all components
	stopComponents(componentRegistry)

	slog.Info("Semspec shutdown complete")
	return nil
}

func printBanner() {
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║             Semspec v" + Version + "                     ║")
	fmt.Println("║      Semantic Development Agent               ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
}

func loadConfig(configPath, repoPath string) (*config.Config, error) {
	if configPath != "" {
		// Load from file
		loader := config.NewLoader()
		return loader.LoadFile(configPath)
	}

	// Build minimal config programmatically
	return buildDefaultConfig(repoPath)
}

func buildDefaultConfig(repoPath string) (*config.Config, error) {
	// Extract project name from repo path
	projectName := filepath.Base(repoPath)

	// Build component configs
	astIndexerConfig := map[string]any{
		"repo_path":      repoPath,
		"org":            "semspec",
		"project":        projectName,
		"watch_enabled":  true,
		"index_interval": "5m",
	}
	astIndexerJSON, _ := json.Marshal(astIndexerConfig)

	toolsConfig := map[string]any{
		"repo_path":   repoPath,
		"stream_name": "AGENT",
		"timeout":     "30s",
	}
	toolsJSON, _ := json.Marshal(toolsConfig)

	return &config.Config{
		Version: "1.0.0",
		Platform: config.PlatformConfig{
			Org:         "semspec",
			ID:          "semspec-local",
			Environment: "dev",
		},
		NATS: config.NATSConfig{
			URLs:          []string{"nats://localhost:4222"},
			MaxReconnects: -1,
			ReconnectWait: 2 * time.Second,
			JetStream: config.JetStreamConfig{
				Enabled: true,
			},
		},
		Components: config.ComponentConfigs{
			"ast-indexer": types.ComponentConfig{
				Name:    "ast-indexer",
				Type:    types.ComponentTypeProcessor,
				Enabled: true,
				Config:  astIndexerJSON,
			},
			"semspec-tools": types.ComponentConfig{
				Name:    "semspec-tools",
				Type:    types.ComponentTypeProcessor,
				Enabled: true,
				Config:  toolsJSON,
			},
		},
		Streams: config.StreamConfigs{
			"AGENT": config.StreamConfig{
				Subjects: []string{
					"tool.execute.>",
					"tool.result.>",
					"graph.ingest.>",
				},
				MaxAge:   "24h",
				Storage:  "memory",
				Replicas: 1,
			},
		},
	}, nil
}

func connectToNATS(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*natsclient.Client, error) {
	natsURLs := "nats://localhost:4222"

	// Environment variable override takes precedence
	if envURL := os.Getenv("NATS_URL"); envURL != "" {
		natsURLs = envURL
	} else if envURL := os.Getenv("SEMSPEC_NATS_URL"); envURL != "" {
		natsURLs = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURLs = strings.Join(cfg.NATS.URLs, ",")
	}

	logger.Info("Connecting to NATS", "url", natsURLs)

	client, err := natsclient.NewClient(natsURLs,
		natsclient.WithName("semspec"),
		natsclient.WithMaxReconnects(-1),
		natsclient.WithReconnectWait(time.Second),
		natsclient.WithCircuitBreakerThreshold(5),
		natsclient.WithHealthInterval(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create NATS client: %w", err)
	}

	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.WaitForConnection(connCtx); err != nil {
		return nil, fmt.Errorf("NATS connection timeout: %w", err)
	}

	logger.Info("Connected to NATS", "url", natsURLs)
	return client, nil
}

func ensureStreams(ctx context.Context, cfg *config.Config, natsClient *natsclient.Client, logger *slog.Logger) error {
	logger.Debug("Creating JetStream streams")
	streamsManager := config.NewStreamsManager(natsClient, logger)

	if err := streamsManager.EnsureStreams(ctx, cfg); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	logger.Debug("JetStream streams ready")
	return nil
}

func extractPlatformMeta(cfg *config.Config) component.PlatformMeta {
	platformID := cfg.Platform.InstanceID
	if platformID == "" {
		platformID = cfg.Platform.ID
	}

	return component.PlatformMeta{
		Org:      cfg.Platform.Org,
		Platform: platformID,
	}
}

func createAndStartComponents(
	ctx context.Context,
	cfg *config.Config,
	registry *component.Registry,
	deps component.Dependencies,
) error {
	for instanceName, compConfig := range cfg.Components {
		if !compConfig.Enabled {
			slog.Info("Component disabled", "name", instanceName)
			continue
		}

		slog.Debug("Creating component", "name", instanceName, "type", compConfig.Name)

		// Create the component instance
		comp, err := registry.CreateComponent(instanceName, compConfig, deps)
		if err != nil {
			return fmt.Errorf("create component %s: %w", instanceName, err)
		}

		// Initialize and start the component
		if initializable, ok := comp.(interface{ Initialize() error }); ok {
			if err := initializable.Initialize(); err != nil {
				return fmt.Errorf("initialize component %s: %w", instanceName, err)
			}
		}

		if startable, ok := comp.(interface{ Start(context.Context) error }); ok {
			if err := startable.Start(ctx); err != nil {
				return fmt.Errorf("start component %s: %w", instanceName, err)
			}
		}

		slog.Info("Component started", "name", instanceName)
	}

	return nil
}

func stopComponents(registry *component.Registry) {
	components := registry.ListComponents()
	timeout := 10 * time.Second

	for name, comp := range components {
		slog.Debug("Stopping component", "name", name)

		if stoppable, ok := comp.(interface{ Stop(time.Duration) error }); ok {
			if err := stoppable.Stop(timeout); err != nil {
				slog.Error("Error stopping component", "name", name, "error", err)
			}
		}
	}
}
