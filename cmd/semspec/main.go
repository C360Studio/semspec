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
	"github.com/c360studio/semspec/model"
	workflowdocuments "github.com/c360studio/semspec/output/workflow-documents"
	astindexer "github.com/c360studio/semspec/processor/ast-indexer"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	plancoordinator "github.com/c360studio/semspec/processor/plan-coordinator"
	planreviewer "github.com/c360studio/semspec/processor/plan-reviewer"
	"github.com/c360studio/semspec/processor/planner"
	projectapi "github.com/c360studio/semspec/processor/project-api"
	questionanswerer "github.com/c360studio/semspec/processor/question-answerer"
	questiontimeout "github.com/c360studio/semspec/processor/question-timeout"
	rdfexport "github.com/c360studio/semspec/processor/rdf-export"
	sourceingester "github.com/c360studio/semspec/processor/source-ingester"
	structuralvalidator "github.com/c360studio/semspec/processor/structural-validator"
	taskdispatcher "github.com/c360studio/semspec/processor/task-dispatcher"
	taskgenerator "github.com/c360studio/semspec/processor/task-generator"
	taskreviewer "github.com/c360studio/semspec/processor/task-reviewer"
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

// Version is the current semspec release version.
const Version = "0.1.0"

// BuildTime records when the binary was compiled; overridden via ldflags.
const BuildTime = "dev"

const appName = "semspec"

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
		RunE: func(_ *cobra.Command, _ []string) error {
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
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("%s version %s (build: %s)\n", appName, Version, BuildTime)
		},
	})

	return cmd
}

func run(configPath, repoPath, logLevel string) error {
	// Print banner
	printBanner()

	logger := setupLogger(logLevel)

	absRepoPath, err := resolveAndValidateRepoPath(repoPath)
	if err != nil {
		return err
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

	ctx := context.Background()
	_, manager, cleanup, err := setupInfrastructure(ctx, cfg, logger, absRepoPath)
	if err != nil {
		return err
	}
	defer cleanup()

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

// setupLogger creates and installs a structured logger at the requested level.
func setupLogger(logLevel string) *slog.Logger {
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
	return logger
}

// resolveAndValidateRepoPath converts repoPath to an absolute path and verifies it is a directory.
func resolveAndValidateRepoPath(repoPath string) (string, error) {
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}
	info, err := os.Stat(absRepoPath)
	if err != nil {
		return "", fmt.Errorf("stat repo path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", absRepoPath)
	}
	return absRepoPath, nil
}

// initTrajectoryStores initializes the global LLM call and tool call stores.
// Failures are logged as warnings only — trajectory tracking is optional.
func initTrajectoryStores(ctx context.Context, natsClient *natsclient.Client) {
	if err := llm.InitGlobalCallStore(ctx, natsClient); err != nil {
		slog.Warn("Failed to initialize LLM call store for trajectory tracking", "error", err)
	} else {
		slog.Debug("LLM call store initialized for trajectory tracking")
	}
	if err := llm.InitGlobalToolCallStore(ctx, natsClient); err != nil {
		slog.Warn("Failed to initialize tool call store for trajectory tracking", "error", err)
	} else {
		slog.Debug("Tool call store initialized for trajectory tracking")
	}
}

// registerSemspecComponents registers all semspec-specific component factories
// and the semstreams review aggregation system.
func registerSemspecComponents(componentRegistry *component.Registry) error {
	slog.Debug("Registering semspec component factories")
	type registerFn func() error
	steps := []registerFn{
		func() error { return astindexer.Register(componentRegistry) },
		func() error { return rdfexport.Register(componentRegistry) },
		func() error { return workflowvalidator.Register(componentRegistry) },
		func() error { return workflowdocuments.Register(componentRegistry) },
		func() error { return questionanswerer.Register(componentRegistry) },
		func() error { return questiontimeout.Register(componentRegistry) },
		func() error { return sourceingester.Register(componentRegistry) },
		func() error { return taskgenerator.Register(componentRegistry) },
		func() error { return taskreviewer.Register(componentRegistry) },
		func() error { return taskdispatcher.Register(componentRegistry) },
		func() error { return planner.Register(componentRegistry) },
		func() error { return contextbuilder.Register(componentRegistry) },
		func() error { return workflowapi.Register(componentRegistry) },
		func() error { return trajectoryapi.Register(componentRegistry) },
		func() error { return plancoordinator.Register(componentRegistry) },
		func() error { return planreviewer.Register(componentRegistry) },
		func() error { return projectapi.Register(componentRegistry) },
		func() error { return structuralvalidator.Register(componentRegistry) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	// Register review aggregator with semstreams aggregation system.
	// Note: semspec-tools is replaced by global tool registration via _ "github.com/c360studio/semspec/tools"
	reviewaggregation.Register()
	return nil
}

// setupServiceManager creates the service registry, manager, and configures all services.
func setupServiceManager(
	cfg *config.Config,
	natsClient *natsclient.Client,
	metricsRegistry *metric.MetricsRegistry,
	logger *slog.Logger,
	platform types.PlatformMeta,
	configManager *config.Manager,
	componentRegistry *component.Registry,
) (*service.Manager, error) {
	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return nil, fmt.Errorf("register services: %w", err)
	}
	ensureServiceManagerConfig(cfg)
	manager := service.NewServiceManager(serviceRegistry)
	svcDeps := &service.Dependencies{
		NATSClient:        natsClient,
		MetricsRegistry:   metricsRegistry,
		Logger:            logger,
		Platform:          platform,
		Manager:           configManager,
		ComponentRegistry: componentRegistry,
	}
	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return nil, err
	}
	slog.Info("All services configured")
	return manager, nil
}

// setupInfrastructure connects to NATS, initializes stores, and builds the component and service managers.
// The returned cleanup function must be called via defer.
func setupInfrastructure(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	absRepoPath string,
) (*natsclient.Client, *service.Manager, func(), error) {
	natsClient, err := connectToNATS(ctx, cfg, logger)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ensureStreams(ctx, cfg, natsClient, logger); err != nil {
		natsClient.Close(ctx)
		return nil, nil, nil, err
	}
	initTrajectoryStores(ctx, natsClient)
	slog.Info("Semspec ready", "version", Version, "repo_path", absRepoPath)

	metricsRegistry := metric.NewMetricsRegistry()
	platform := extractPlatformMeta(cfg)

	configManager, err := config.NewConfigManager(cfg, natsClient, logger)
	if err != nil {
		natsClient.Close(ctx)
		return nil, nil, nil, fmt.Errorf("create config manager: %w", err)
	}
	if err := configManager.Start(ctx); err != nil {
		natsClient.Close(ctx)
		return nil, nil, nil, fmt.Errorf("start config manager: %w", err)
	}
	slog.Info("Platform identity configured", "org", platform.Org, "platform", platform.Platform)

	componentRegistry := component.NewRegistry()
	slog.Debug("Registering semstreams component factories")
	if err := componentregistry.Register(componentRegistry); err != nil {
		configManager.Stop(5 * time.Second)
		natsClient.Close(ctx)
		return nil, nil, nil, fmt.Errorf("register semstreams components: %w", err)
	}
	if err := registerSemspecComponents(componentRegistry); err != nil {
		configManager.Stop(5 * time.Second)
		natsClient.Close(ctx)
		return nil, nil, nil, err
	}
	factories := componentRegistry.ListFactories()
	slog.Info("Component factories registered", "count", len(factories))

	manager, err := setupServiceManager(cfg, natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)
	if err != nil {
		configManager.Stop(5 * time.Second)
		natsClient.Close(ctx)
		return nil, nil, nil, err
	}

	cleanup := func() {
		configManager.Stop(5 * time.Second)
		natsClient.Close(ctx)
	}
	return natsClient, manager, cleanup, nil
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

	// Initialize model registry from config if present
	// This must happen before component initialization since components use model.Global()
	if err := initModelRegistryFromConfig([]byte(expanded)); err != nil {
		slog.Warn("Failed to load model_registry from config, using defaults", "error", err)
	}

	// Load using semstreams loader (preserves defaults, validation, env overrides)
	loader := config.NewLoader()
	return loader.LoadFromBytes([]byte(expanded))
}

// initModelRegistryFromConfig loads model_registry section from config JSON and
// initializes the global model registry. If no model_registry is present, no action is taken.
func initModelRegistryFromConfig(data []byte) error {
	registry, err := model.LoadFromJSON(data)
	if err != nil {
		// No model_registry in config - this is fine, use defaults
		return nil
	}

	// Initialize global registry with loaded config
	model.InitGlobal(registry)
	slog.Info("Model registry initialized from config",
		"endpoints", len(registry.ListEndpoints()))
	return nil
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
		return fmt.Errorf(`nats connection failed: %w

NATS is not running at %s.

To start NATS:
  docker compose up -d nats

Or set NATS_URL environment variable to point to your NATS server`, err, url)
	}

	return fmt.Errorf("nats connection failed: %w", err)
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
