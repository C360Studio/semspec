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

	// Register tools via init()
	_ "github.com/c360studio/semspec/tools"

	// Register LLM providers via init()
	_ "github.com/c360studio/semspec/llm/providers"

	// Register vocabularies via init()
	_ "github.com/c360studio/semspec/vocabulary/source"

	"github.com/c360studio/semspec/llm"
	workflowdocuments "github.com/c360studio/semspec/output/workflow-documents"
	astindexer "github.com/c360studio/semspec/processor/ast-indexer"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	plancoordinator "github.com/c360studio/semspec/processor/plan-coordinator"
	planreviewer "github.com/c360studio/semspec/processor/plan-reviewer"
	"github.com/c360studio/semspec/processor/planner"
	projectapi "github.com/c360studio/semspec/processor/project-api"
	structuralvalidator "github.com/c360studio/semspec/processor/structural-validator"
	questionanswerer "github.com/c360studio/semspec/processor/question-answerer"
	questiontimeout "github.com/c360studio/semspec/processor/question-timeout"
	rdfexport "github.com/c360studio/semspec/processor/rdf-export"
	sourceingester "github.com/c360studio/semspec/processor/source-ingester"
	taskdispatcher "github.com/c360studio/semspec/processor/task-dispatcher"
	taskgenerator "github.com/c360studio/semspec/processor/task-generator"
	trajectoryapi "github.com/c360studio/semspec/processor/trajectory-api"
	workflowapi "github.com/c360studio/semspec/processor/workflow-api"
	workflowvalidator "github.com/c360studio/semspec/processor/workflow-validator"
	reviewaggregation "github.com/c360studio/semspec/workflow/aggregation"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
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

	// Initialize global LLM call store for trajectory tracking
	if err := llm.InitGlobalCallStore(ctx, natsClient); err != nil {
		// Log warning but don't fail - trajectory tracking is optional
		slog.Warn("Failed to initialize LLM call store for trajectory tracking", "error", err)
	} else {
		slog.Debug("LLM call store initialized for trajectory tracking")
	}

	// Initialize global tool call store for trajectory tracking
	if err := llm.InitGlobalToolCallStore(ctx, natsClient); err != nil {
		// Log warning but don't fail - trajectory tracking is optional
		slog.Warn("Failed to initialize tool call store for trajectory tracking", "error", err)
	} else {
		slog.Debug("Tool call store initialized for trajectory tracking")
	}

	slog.Info("Semspec ready",
		"version", Version,
		"repo_path", absRepoPath)

	// Create remaining infrastructure
	metricsRegistry := metric.NewMetricsRegistry()
	platform := extractPlatformMeta(cfg)

	// Create and start config manager (required for component-manager to access component configs)
	configManager, err := config.NewConfigManager(cfg, natsClient, logger)
	if err != nil {
		return fmt.Errorf("create config manager: %w", err)
	}
	if err := configManager.Start(ctx); err != nil {
		return fmt.Errorf("start config manager: %w", err)
	}
	defer configManager.Stop(5 * time.Second)

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

	if err := rdfexport.Register(componentRegistry); err != nil {
		return fmt.Errorf("register rdf-export: %w", err)
	}

	if err := workflowvalidator.Register(componentRegistry); err != nil {
		return fmt.Errorf("register workflow-validator: %w", err)
	}

	if err := workflowdocuments.Register(componentRegistry); err != nil {
		return fmt.Errorf("register workflow-documents: %w", err)
	}

	if err := questionanswerer.Register(componentRegistry); err != nil {
		return fmt.Errorf("register question-answerer: %w", err)
	}

	if err := questiontimeout.Register(componentRegistry); err != nil {
		return fmt.Errorf("register question-timeout: %w", err)
	}

	if err := sourceingester.Register(componentRegistry); err != nil {
		return fmt.Errorf("register source-ingester: %w", err)
	}

	if err := taskgenerator.Register(componentRegistry); err != nil {
		return fmt.Errorf("register task-generator: %w", err)
	}

	if err := taskdispatcher.Register(componentRegistry); err != nil {
		return fmt.Errorf("register task-dispatcher: %w", err)
	}

	if err := planner.Register(componentRegistry); err != nil {
		return fmt.Errorf("register planner: %w", err)
	}

	if err := contextbuilder.Register(componentRegistry); err != nil {
		return fmt.Errorf("register context-builder: %w", err)
	}

	if err := workflowapi.Register(componentRegistry); err != nil {
		return fmt.Errorf("register workflow-api: %w", err)
	}

	if err := trajectoryapi.Register(componentRegistry); err != nil {
		return fmt.Errorf("register trajectory-api: %w", err)
	}

	if err := plancoordinator.Register(componentRegistry); err != nil {
		return fmt.Errorf("register plan-coordinator: %w", err)
	}

	if err := planreviewer.Register(componentRegistry); err != nil {
		return fmt.Errorf("register plan-reviewer: %w", err)
	}

	if err := projectapi.Register(componentRegistry); err != nil {
		return fmt.Errorf("register project-api: %w", err)
	}

	if err := structuralvalidator.Register(componentRegistry); err != nil {
		return fmt.Errorf("register structural-validator: %w", err)
	}

	// Register review aggregator with semstreams aggregation system
	reviewaggregation.Register()

	// Note: semspec-tools is replaced by global tool registration via _ "github.com/c360studio/semspec/tools"

	factories := componentRegistry.ListFactories()
	slog.Info("Component factories registered", "count", len(factories))

	// Create service registry and manager (semstreams pattern)
	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return fmt.Errorf("register services: %w", err)
	}

	manager := service.NewServiceManager(serviceRegistry)
	ensureServiceManagerConfig(cfg)

	// Create service dependencies
	svcDeps := &service.Dependencies{
		NATSClient:        natsClient,
		MetricsRegistry:   metricsRegistry,
		Logger:            logger,
		Platform:          platform,
		Manager:           configManager,
		ComponentRegistry: componentRegistry,
	}

	// Configure and create services
	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	slog.Info("All services configured")

	// Setup signal handling
	signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// Start all services (includes HTTP server with health endpoints)
	slog.Info("Starting all services")
	if err := manager.StartAll(signalCtx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}
	slog.Info("All services started successfully")

	// Block until shutdown signal
	<-signalCtx.Done()
	slog.Info("Received shutdown signal")

	// Stop all services
	shutdownTimeout := 30 * time.Second
	if err := manager.StopAll(shutdownTimeout); err != nil {
		slog.Error("Error stopping services", "error", err)
	}

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
		// Load from file with environment variable substitution
		return loadConfigWithEnvSubstitution(configPath)
	}

	// Build minimal config programmatically
	return buildDefaultConfig(repoPath)
}

// loadConfigWithEnvSubstitution reads a config file and expands environment
// variables before parsing. Supports ${VAR} and $VAR syntax.
func loadConfigWithEnvSubstitution(configPath string) (*config.Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	expanded := config.ExpandEnvWithDefaults(string(data))

	// Load using semstreams loader (preserves defaults, validation, env overrides)
	loader := config.NewLoader()
	return loader.LoadFromBytes([]byte(expanded))
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

	// Note: Tools are registered globally via _ "github.com/c360studio/semspec/tools"
	// and executed by agentic-tools component from semstreams

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
		Services: types.ServiceConfigs{},
		Components: config.ComponentConfigs{
			"ast-indexer": types.ComponentConfig{
				Name:    "ast-indexer",
				Type:    types.ComponentTypeProcessor,
				Enabled: true,
				Config:  astIndexerJSON,
			},
		},
		Streams: config.StreamConfigs{
			"AGENT": config.StreamConfig{
				Subjects: []string{
					"tool.execute.>",
					"tool.result.>",
				},
				MaxAge:   "24h",
				Storage:  "memory",
				Replicas: 1,
			},
			"GRAPH": config.StreamConfig{
				Subjects: []string{
					"graph.ingest.entity",
					"graph.export.>",
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
		natsclient.WithCircuitBreakerThreshold(20), // Higher threshold for startup bursts
		natsclient.WithHealthInterval(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create NATS client: %w", err)
	}

	if err := client.Connect(ctx); err != nil {
		return nil, wrapNATSError(err, natsURLs)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.WaitForConnection(connCtx); err != nil {
		return nil, wrapNATSError(err, natsURLs)
	}

	logger.Info("Connected to NATS", "url", natsURLs)
	return client, nil
}

// wrapNATSError provides helpful guidance when NATS connection fails.
func wrapNATSError(err error, url string) error {
	errStr := err.Error()

	// Check for common connection errors
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no servers available") ||
		strings.Contains(errStr, "timeout") {
		return fmt.Errorf(`NATS connection failed: %w

NATS is not running at %s.

To start NATS:
  docker compose up -d nats

Or set NATS_URL environment variable to point to your NATS server.`, err, url)
	}

	return fmt.Errorf("NATS connection failed: %w", err)
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

func extractPlatformMeta(cfg *config.Config) types.PlatformMeta {
	platformID := cfg.Platform.InstanceID
	if platformID == "" {
		platformID = cfg.Platform.ID
	}

	return types.PlatformMeta{
		Org:      cfg.Platform.Org,
		Platform: platformID,
	}
}

// ensureServiceManagerConfig ensures service-manager config exists with defaults
func ensureServiceManagerConfig(cfg *config.Config) {
	if cfg.Services == nil {
		cfg.Services = make(types.ServiceConfigs)
	}

	if _, exists := cfg.Services["service-manager"]; !exists {
		slog.Debug("Adding default service-manager config")
		defaultConfig := map[string]any{
			"http_port":  8080,
			"swagger_ui": false,
			"server_info": map[string]string{
				"title":       "Semspec API",
				"description": "semantic development agent - AST indexing and file/git tools",
				"version":     Version,
			},
		}
		defaultConfigJSON, _ := json.Marshal(defaultConfig)
		cfg.Services["service-manager"] = types.ServiceConfig{
			Name:    "service-manager",
			Enabled: true,
			Config:  defaultConfigJSON,
		}
		slog.Debug("Service-manager config added", "enabled", true)
	}
}

// configureAndCreateServices configures the manager and creates all services
func configureAndCreateServices(
	cfg *config.Config,
	manager *service.Manager,
	svcDeps *service.Dependencies,
) error {
	slog.Debug("Configuring Manager")
	if err := manager.ConfigureFromServices(cfg.Services, svcDeps); err != nil {
		return fmt.Errorf("configure service manager: %w", err)
	}

	slog.Debug("Creating services from config", "count", len(cfg.Services))
	for name, svcConfig := range cfg.Services {
		if name == "service-manager" {
			slog.Debug("Skipping service-manager (configured directly)")
			continue
		}

		if err := createServiceIfEnabled(manager, name, svcConfig, svcDeps); err != nil {
			return err
		}
	}

	return nil
}

// createServiceIfEnabled creates a service if it's enabled and registered
func createServiceIfEnabled(
	manager *service.Manager,
	name string,
	svcConfig types.ServiceConfig,
	svcDeps *service.Dependencies,
) error {
	slog.Debug("Processing service config", "key", name, "name", svcConfig.Name, "enabled", svcConfig.Enabled)

	if !svcConfig.Enabled {
		slog.Info("Service disabled in config", "name", name)
		return nil
	}

	if !manager.HasConstructor(name) {
		slog.Warn("Service configured but not registered", "key", name, "available_constructors", manager.ListConstructors())
		return nil
	}

	slog.Debug("Creating service", "name", name, "has_constructor", true)
	if _, err := manager.CreateService(name, svcConfig.Config, svcDeps); err != nil {
		return fmt.Errorf("create service %s: %w", name, err)
	}

	slog.Info("Created service", "name", name, "config_name", svcConfig.Name)
	return nil
}
