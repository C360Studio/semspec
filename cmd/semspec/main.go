// Package main provides the semspec binary entry point.
// Semspec connects to a semstreams infrastructure via NATS and registers
// file and git tool executors for agentic operations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/c360/semspec/tools/file"
	"github.com/c360/semspec/tools/git"
	"github.com/c360/semstreams/agentic"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/errs"
	"github.com/c360/semstreams/pkg/retry"
	agentictools "github.com/c360/semstreams/processor/agentic-tools"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/spf13/cobra"
)

const (
	defaultNATSURL    = "nats://localhost:4222"
	defaultStreamName = "AGENT"
	toolExecutePrefix = "tool.execute."
	toolResultPrefix  = "tool.result."
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		natsURL    string
		repoPath   string
		streamName string
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "semspec",
		Short: "Semantic development agent tools",
		Long: `Semspec registers file and git operation tools with a semstreams
infrastructure via NATS. It connects to the NATS server and subscribes
to tool execution requests, processing them using the configured
repository path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve repo path to absolute
			absRepoPath, err := filepath.Abs(repoPath)
			if err != nil {
				return errs.WrapInvalid(err, "semspec", "run", "resolve repo path")
			}

			// Verify repo path exists
			info, err := os.Stat(absRepoPath)
			if err != nil {
				return errs.WrapInvalid(err, "semspec", "run", "stat repo path")
			}
			if !info.IsDir() {
				return errs.WrapInvalid(
					fmt.Errorf("not a directory: %s", absRepoPath),
					"semspec", "run", "validate repo path",
				)
			}

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

			// Create semspec service
			svc := &Service{
				natsURL:    natsURL,
				repoPath:   absRepoPath,
				streamName: streamName,
				logger:     logger,
			}

			// Run with graceful shutdown
			return svc.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&natsURL, "nats-url", getEnvOrDefault("NATS_URL", defaultNATSURL), "NATS server URL")
	cmd.Flags().StringVar(&repoPath, "repo", ".", "Repository path to operate on")
	cmd.Flags().StringVar(&streamName, "stream", defaultStreamName, "JetStream stream name")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// slogAdapter adapts slog.Logger to natsclient.Logger interface.
// Note: Printf-style interface loses structured logging benefits.
// Messages from natsclient will appear as unstructured strings.
type slogAdapter struct {
	logger *slog.Logger
}

func (a slogAdapter) Printf(format string, v ...any) {
	a.logger.Info(fmt.Sprintf(format, v...))
}

func (a slogAdapter) Errorf(format string, v ...any) {
	a.logger.Error(fmt.Sprintf(format, v...))
}

func (a slogAdapter) Debugf(format string, v ...any) {
	a.logger.Debug(fmt.Sprintf(format, v...))
}

// Service manages the semspec tool registration service
type Service struct {
	natsURL    string
	repoPath   string
	streamName string
	logger     *slog.Logger

	client     *natsclient.Client
	registry   *agentictools.ExecutorRegistry
	consumeCtx jetstream.ConsumeContext
}

// Run starts the service and blocks until shutdown
func (s *Service) Run(ctx context.Context) error {
	// Set up signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Connect to NATS
	if err := s.connect(ctx); err != nil {
		return err
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := s.client.Close(closeCtx); err != nil {
			s.logger.Error("Error closing NATS client", "error", err)
		}
	}()

	// Initialize tool executors
	s.registry = agentictools.NewExecutorRegistry()
	fileExec := file.NewExecutor(s.repoPath)
	gitExec := git.NewExecutor(s.repoPath)

	// Register executors with local registry
	for _, tool := range fileExec.ListTools() {
		if err := s.registry.RegisterTool(tool.Name, fileExec); err != nil {
			return errs.WrapFatal(err, "semspec", "run", "register file tool "+tool.Name)
		}
	}
	for _, tool := range gitExec.ListTools() {
		if err := s.registry.RegisterTool(tool.Name, gitExec); err != nil {
			return errs.WrapFatal(err, "semspec", "run", "register git tool "+tool.Name)
		}
	}

	// Subscribe to tool execution requests
	if err := s.subscribeToToolCalls(ctx); err != nil {
		return err
	}

	// Advertise available tools
	if err := s.advertiseTools(ctx, fileExec, gitExec); err != nil {
		s.logger.Warn("Failed to advertise tools", "error", err)
		// Not fatal - tools may still work if manually configured
	}

	s.logger.Info("Semspec tools registered and listening",
		"nats_url", s.natsURL,
		"repo_path", s.repoPath,
		"stream", s.streamName,
		"tools", len(s.registry.ListTools()))

	// Block until shutdown signal
	<-ctx.Done()
	s.logger.Info("Shutting down semspec...")

	// Stop consumer before closing NATS client
	if s.consumeCtx != nil {
		s.consumeCtx.Stop()
	}

	return nil
}

// connect establishes connection to NATS using natsclient with circuit breaker
func (s *Service) connect(ctx context.Context) error {
	s.logger.Info("Connecting to NATS", "url", s.natsURL)

	// Create natsclient with configuration
	client, err := natsclient.NewClient(s.natsURL,
		natsclient.WithName("semspec"),
		natsclient.WithMaxReconnects(-1),
		natsclient.WithReconnectWait(time.Second),
		natsclient.WithCircuitBreakerThreshold(5),
		natsclient.WithHealthInterval(30*time.Second),
		natsclient.WithLogger(slogAdapter{s.logger}),
		natsclient.WithDisconnectCallback(func(err error) {
			if err != nil {
				s.logger.Warn("NATS disconnected", "error", err)
			}
		}),
		natsclient.WithReconnectCallback(func() {
			s.logger.Info("NATS reconnected")
		}),
		natsclient.WithHealthChangeCallback(func(healthy bool) {
			if healthy {
				s.logger.Debug("NATS connection healthy")
			} else {
				s.logger.Warn("NATS connection unhealthy")
			}
		}),
	)
	if err != nil {
		return errs.WrapFatal(err, "semspec", "connect", "create natsclient")
	}

	// Connect with context
	if err := client.Connect(ctx); err != nil {
		// Circuit breaker open indicates persistent failures
		if errors.Is(err, natsclient.ErrCircuitOpen) {
			return errs.WrapFatal(err, "semspec", "connect", "circuit breaker open")
		}
		return errs.WrapTransient(err, "semspec", "connect", "establish connection")
	}

	s.client = client
	s.logger.Info("Connected to NATS", "url", s.natsURL)
	return nil
}

// subscribeToToolCalls subscribes to tool execution requests via JetStream
func (s *Service) subscribeToToolCalls(ctx context.Context) error {
	// Wait for stream to be available
	if err := s.waitForStream(ctx); err != nil {
		return errs.WrapFatal(err, "semspec", "subscribeToToolCalls", "wait for stream "+s.streamName)
	}

	// Get JetStream context
	js, err := s.client.JetStream()
	if err != nil {
		return errs.WrapFatal(err, "semspec", "subscribeToToolCalls", "get JetStream")
	}

	// Get all registered tools
	tools := s.registry.ListTools()

	// Create a durable consumer for semspec tools
	consumerName := "semspec-tools"

	// Build filter subjects for all our tools
	filterSubjects := make([]string, len(tools))
	for i, tool := range tools {
		filterSubjects[i] = toolExecutePrefix + tool.Name
	}

	s.logger.Info("Setting up JetStream consumer",
		"stream", s.streamName,
		"consumer", consumerName,
		"filter_subjects", filterSubjects)

	// Create consumer config
	consumerCfg := jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		FilterSubject: toolExecutePrefix + ">", // Subscribe to all tool.execute.* initially
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    3,
	}

	// If multiple filter subjects, use FilterSubjects (NATS 2.10+)
	if len(filterSubjects) > 1 {
		consumerCfg.FilterSubject = "" // Clear single filter
		consumerCfg.FilterSubjects = filterSubjects
	} else if len(filterSubjects) == 1 {
		consumerCfg.FilterSubject = filterSubjects[0]
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, s.streamName, consumerCfg)
	if err != nil {
		return errs.WrapFatal(err, "semspec", "subscribeToToolCalls", "create consumer")
	}

	// Start consuming - store consume context for lifecycle management
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		s.handleToolCall(msg)
	})
	if err != nil {
		return errs.WrapFatal(err, "semspec", "subscribeToToolCalls", "start consuming")
	}
	s.consumeCtx = consumeCtx

	s.logger.Info("Subscribed to tool calls",
		"stream", s.streamName,
		"consumer", consumerName,
		"tools", len(tools))

	return nil
}

// waitForStream waits for the JetStream stream to be available using retry package
func (s *Service) waitForStream(ctx context.Context) error {
	js, err := s.client.JetStream()
	if err != nil {
		// JetStream not initialized is fatal - indicates misconfiguration
		return errs.WrapFatal(err, "semspec", "waitForStream", "get JetStream")
	}

	// Use a longer retry config for stream availability during startup
	cfg := retry.Config{
		MaxAttempts:  30, // Wait up to ~30 seconds
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   1.5,
		AddJitter:    true,
	}

	return retry.Do(ctx, cfg, func() error {
		_, err := js.Stream(ctx, s.streamName)
		if err != nil {
			s.logger.Debug("Stream not yet available, retrying",
				"stream", s.streamName,
				"error", err)
			return err
		}
		return nil
	})
}

// handleToolCall processes a tool execution request
func (s *Service) handleToolCall(msg jetstream.Msg) {
	// Create per-message context with timeout to avoid being tied to service lifecycle
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse tool call
	var call agentic.ToolCall
	if err := json.Unmarshal(msg.Data(), &call); err != nil {
		s.logger.Error("Failed to unmarshal tool call",
			"error", err,
			"subject", msg.Subject())
		_ = msg.Term() // Malformed data is never retryable
		return
	}

	s.logger.Debug("Processing tool call",
		"tool", call.Name,
		"call_id", call.ID)

	// Execute the tool
	startTime := time.Now()
	result, err := s.registry.Execute(ctx, call)
	duration := time.Since(startTime)

	if err != nil {
		s.logger.Error("Tool execution failed",
			"tool", call.Name,
			"call_id", call.ID,
			"error", err,
			"duration", duration)

		// Classify the execution error
		if errs.IsTransient(err) {
			s.logger.Warn("Transient execution error, will retry", "call_id", call.ID)
			_ = msg.Nak()
		} else {
			// Non-retryable errors (unknown tool, invalid arguments, etc.)
			s.logger.Error("Non-retryable execution error", "call_id", call.ID)
			_ = msg.Term()
		}
		return
	}

	if result.Error != "" {
		s.logger.Debug("Tool returned error",
			"tool", call.Name,
			"call_id", call.ID,
			"error", result.Error,
			"duration", duration)
	} else {
		s.logger.Debug("Tool executed successfully",
			"tool", call.Name,
			"call_id", call.ID,
			"duration", duration)
	}

	// Publish result with error classification
	if err := s.publishResult(ctx, result); err != nil {
		if errs.IsTransient(err) {
			s.logger.Warn("Transient error publishing result, will retry",
				"call_id", call.ID,
				"error", err)
			_ = msg.Nak() // Requeue for retry
		} else {
			s.logger.Error("Fatal error publishing result",
				"call_id", call.ID,
				"error", err)
			_ = msg.Term() // Don't retry
		}
		return
	}

	// Acknowledge the message
	if err := msg.Ack(); err != nil {
		s.logger.Error("Failed to ack message", "error", err)
	}
}

// publishResult publishes a tool result to JetStream
func (s *Service) publishResult(ctx context.Context, result agentic.ToolResult) error {
	if result.CallID == "" {
		return errs.WrapInvalid(
			fmt.Errorf("empty call ID in result"),
			"semspec", "publishResult", "validate result")
	}

	data, err := json.Marshal(result)
	if err != nil {
		return errs.WrapInvalid(err, "semspec", "publishResult", "marshal result")
	}

	subject := toolResultPrefix + result.CallID

	if err := s.client.PublishToStream(ctx, subject, data); err != nil {
		return errs.WrapTransient(err, "semspec", "publishResult", "publish to "+subject)
	}

	return nil
}

// advertiseTools publishes tool definitions for discovery
func (s *Service) advertiseTools(ctx context.Context, executors ...agentictools.ToolExecutor) error {
	for _, exec := range executors {
		for _, tool := range exec.ListTools() {
			data, err := json.Marshal(tool)
			if err != nil {
				return errs.WrapInvalid(err, "semspec", "advertiseTools", "marshal tool definition")
			}

			subject := "tool.register." + tool.Name
			if err := s.client.Publish(ctx, subject, data); err != nil {
				return errs.WrapTransient(err, "semspec", "advertiseTools", "publish tool "+tool.Name)
			}

			s.logger.Debug("Advertised tool", "name", tool.Name)
		}
	}

	return nil
}
