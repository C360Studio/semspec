// Package main provides the semspec binary entry point.
// Semspec connects to a semstreams infrastructure via NATS and registers
// file and git tool executors for agentic operations.
package main

import (
	"context"
	"encoding/json"
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
	agentictools "github.com/c360/semstreams/processor/agentic-tools"
	"github.com/nats-io/nats.go"
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
				return fmt.Errorf("failed to resolve repo path: %w", err)
			}

			// Verify repo path exists
			info, err := os.Stat(absRepoPath)
			if err != nil {
				return fmt.Errorf("repo path does not exist: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("repo path is not a directory: %s", absRepoPath)
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

// Service manages the semspec tool registration service
type Service struct {
	natsURL    string
	repoPath   string
	streamName string
	logger     *slog.Logger

	nc       *nats.Conn
	js       jetstream.JetStream
	registry *agentictools.ExecutorRegistry
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
	defer s.nc.Close()

	// Initialize tool executors
	s.registry = agentictools.NewExecutorRegistry()
	fileExec := file.NewExecutor(s.repoPath)
	gitExec := git.NewExecutor(s.repoPath)

	// Register executors with local registry
	for _, tool := range fileExec.ListTools() {
		if err := s.registry.RegisterTool(tool.Name, fileExec); err != nil {
			return fmt.Errorf("failed to register file tool %s: %w", tool.Name, err)
		}
	}
	for _, tool := range gitExec.ListTools() {
		if err := s.registry.RegisterTool(tool.Name, gitExec); err != nil {
			return fmt.Errorf("failed to register git tool %s: %w", tool.Name, err)
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
	return nil
}

// connect establishes connection to NATS and JetStream
func (s *Service) connect(ctx context.Context) error {
	s.logger.Info("Connecting to NATS", "url", s.natsURL)

	// Connect with retry
	var nc *nats.Conn
	var err error
	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		nc, err = nats.Connect(s.natsURL,
			nats.Name("semspec"),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(time.Second),
			nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
				if err != nil {
					s.logger.Warn("NATS disconnected", "error", err)
				}
			}),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				s.logger.Info("NATS reconnected", "url", nc.ConnectedUrl())
			}),
		)
		if err == nil {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
			retryInterval = min(retryInterval*2, maxInterval)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	s.nc = nc

	// Get JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}
	s.js = js

	s.logger.Info("Connected to NATS", "server", nc.ConnectedUrl())
	return nil
}

// subscribeToToolCalls subscribes to tool execution requests via JetStream
func (s *Service) subscribeToToolCalls(ctx context.Context) error {
	// Wait for stream to be available
	if err := s.waitForStream(ctx); err != nil {
		return fmt.Errorf("stream %s not available: %w", s.streamName, err)
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

	consumer, err := s.js.CreateOrUpdateConsumer(ctx, s.streamName, consumerCfg)
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	// Start consuming
	_, err = consumer.Consume(func(msg jetstream.Msg) {
		s.handleToolCall(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("failed to start consuming: %w", err)
	}

	s.logger.Info("Subscribed to tool calls",
		"stream", s.streamName,
		"consumer", consumerName,
		"tools", len(tools))

	return nil
}

// waitForStream waits for the JetStream stream to be available
func (s *Service) waitForStream(ctx context.Context) error {
	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := s.js.Stream(ctx, s.streamName)
		if err == nil {
			return nil
		}

		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}

	return fmt.Errorf("stream %s not found after %d retries", s.streamName, maxRetries)
}

// handleToolCall processes a tool execution request
func (s *Service) handleToolCall(ctx context.Context, msg jetstream.Msg) {
	// Parse tool call
	var call agentic.ToolCall
	if err := json.Unmarshal(msg.Data(), &call); err != nil {
		s.logger.Error("Failed to unmarshal tool call", "error", err)
		_ = msg.Nak()
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
	} else if result.Error != "" {
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

	// Publish result
	if err := s.publishResult(ctx, result); err != nil {
		s.logger.Error("Failed to publish result",
			"call_id", call.ID,
			"error", err)
		_ = msg.Nak()
		return
	}

	// Acknowledge the message
	if err := msg.Ack(); err != nil {
		s.logger.Error("Failed to ack message", "error", err)
	}
}

// publishResult publishes a tool result to JetStream
func (s *Service) publishResult(ctx context.Context, result agentic.ToolResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	subject := toolResultPrefix + result.CallID

	_, err = s.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish to %s: %w", subject, err)
	}

	return nil
}

// advertiseTools publishes tool definitions for discovery
func (s *Service) advertiseTools(ctx context.Context, executors ...agentictools.ToolExecutor) error {
	for _, exec := range executors {
		for _, tool := range exec.ListTools() {
			data, err := json.Marshal(tool)
			if err != nil {
				return fmt.Errorf("failed to marshal tool definition: %w", err)
			}

			subject := "tool.register." + tool.Name
			if err := s.nc.Publish(subject, data); err != nil {
				return fmt.Errorf("failed to advertise tool %s: %w", tool.Name, err)
			}

			s.logger.Debug("Advertised tool", "name", tool.Name)
		}
	}

	return nil
}
